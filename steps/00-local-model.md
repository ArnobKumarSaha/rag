# Step 0: Run a local model and measure its speed

Notes: §19 (the model landscape) and §20 (running a model locally) in
`llm-basics-2.md`. Prefill and decode are covered in §5 of `ai-platform-basics.md`.

There is no Go code in this step. You set up the VM, talk to the model with
`curl`, and measure how fast it runs. Every later step uses this setup, and the
numbers you record here are the speeds you'll be working with.

---

## Concept

### 1. The computer as a kitchen

Every speed in this step comes down to two questions: **who does the work**, and
**how fast can the work get to them**. A kitchen answers both.

#### The cooks: CPU, cores, threads

| Word | Kitchen | On this VM |
|---|---|---|
| **CPU** (the chip) | The whole crew | `AMD EPYC-Milan Processor` in `lscpu` |
| **Core** | One cook with one stove. Does the work: add, multiply, compare. | 32 (`Core(s) per socket: 32`) |
| **Hardware thread** (SMT, or "hyperthreading") | One cook juggling two orders on the same stove. Faster than one order, but nowhere near 2×, because the stove is shared. | `Thread(s) per core: 1`, so none inside the VM |
| **vCPU** | A cook **rented** from a landlord (the hypervisor). It may be a whole real cook, or one hand of a cook who is also juggling another restaurant's order. | 32 vCPUs, which Harvester schedules onto its real cores |
| **Software thread** (what Ollama's `num_thread` sets) | An **order ticket**: one piece of work waiting for a cook | `num_thread: 8` = 8 tickets per step of work |
| **OS scheduler** | The **head chef**, who hands tickets to cooks | the Linux kernel |

The rented part matters. The landlord sometimes borrows your cook to work for
another restaurant (another VM) for a moment. That's **steal time**: the `st`
column in `vmstat`, as a percentage of time.

If there are more tickets than cooks, each cook keeps switching between
tickets. That's **context switching**. It adds overhead and gains no speed.

#### Where things are kept: cache, RAM, disk, ROM

A cook can only work on what is right in front of them. Everything else waits
somewhere further away:

| Kitchen | Computer | Size on this VM | How fast to reach | Kept when power is off? |
|---|---|---|---|---|
| Cutting board, under the cook's hands | **Cache**, inside the CPU | a few MB (`lscpu \| grep cache`) | instant | no |
| Fridge in the next room | **RAM** (memory) | 48 GB | slower | no |
| Warehouse across town | **Disk** (storage) | 150 GB | much slower | yes |
| Recipe card bolted to the wall | **ROM** (read-only memory) | tiny | — | yes |

- **RAM forgets on power-off. Disk doesn't.** That's why a model must be
  *loaded* from disk into RAM again after a restart.
- **ROM** holds the firmware (BIOS/UEFI): the fixed instructions for starting
  the machine. It plays no part in running a model. Phones often call their
  storage "ROM", but that's really flash storage, the same role as disk.

#### The conveyor belt: memory bandwidth

Data moves from the fridge (RAM) to the cutting board (cache) on a **conveyor
belt**. Its speed, in GB per second, is the **memory bandwidth**. All cooks
share one belt. Hiring more cooks doesn't make the belt faster.

#### Two kinds of work

- **The belt is the limit** (**memory-bandwidth-bound**). Each ingredient needs
  very little cooking, for example "add 1 to each of a billion numbers". The
  cooks finish instantly and wait for the belt. More cooks means more people
  waiting at the same belt.
- **The cooks are the limit** (**compute-bound**). Each ingredient needs lots of
  work once it's on the board. Here more cooks help, up to the number of real
  cooks you have.

There's a third problem when cooks must work **in step**: if every cook must
finish their share before anyone starts the next stage, the whole kitchen moves
at the pace of the slowest cook. A cook who is out on loan (steal time) holds
everyone up.

#### The kitchen next door: the GPU

A **GPU** is a second kitchen, built for one kind of job: the same simple step
done to a huge amount of data at once. Model math is mostly multiplying big
grids of numbers (**matrix multiplication**), which is exactly that kind of job.

There are two kinds of GPU:

- **Discrete GPU**: a separate card or chip, like an NVIDIA RTX 4090 or H100.
  It is a real kitchen next door, with its **own fridge**, called **VRAM**
  (§20), and its own much faster belt.
- **Integrated GPU**: built into the CPU chip, like Intel/AMD laptop graphics
  or Apple Silicon. These are extra line cooks squeezed into the CPU kitchen.
  They have **no fridge of their own** and share the RAM and its belt.

| | CPU kitchen (this VM) | Discrete GPU kitchen |
|---|---|---|
| Cooks | A few skilled chefs. Each can handle any recipe. | Thousands of simple line cooks, all doing the same step together |
| Fridge | RAM: 48 GB here | VRAM: e.g. 24 GB on an RTX 4090, 80 GB on an H100 |
| Belt | ~35 GB/s here (measured indirectly in "Threads" below) | ~1,000 GB/s on an RTX 4090, ~3,350 GB/s on an H100 SXM (NVIDIA spec sheets) |

**PCIe** is the road between the CPU kitchen and a discrete GPU. It's slower
than either belt, so it's used mainly once, to carry the weights into VRAM.

What a discrete GPU changes:

- **Compute-bound work** gets thousands of cooks, a big speed-up.
- **Belt-bound work** gets a belt that is 30–100× faster.
- **What has to fit** is VRAM, not RAM. RAM still holds the OS, Ollama, your
  own programs, and anything that doesn't fit in VRAM.
- **If the job doesn't fit in VRAM**, part of it runs in each kitchen, and the
  slow kitchen sets the pace.

**Apple Silicon** is the integrated kind done well: one fridge shared by CPU
and GPU (**unified memory**, §20), with a faster belt than ordinary PC RAM.
There's no PCIe trip, and a 64 GB Mac can hold models that no 24 GB GPU can.
But it has far fewer cooks than a big discrete GPU.

**This VM:** 32 rented cooks, a 48 GB fridge, a 150 GB warehouse, and **no
kitchen next door**. The Ollama log says `inference compute ... library=cpu`.

### 2. Ollama is a model server

**Ollama** (<https://ollama.com>) is a single daemon that downloads models and
serves them over HTTP on port `11434`. It works like `dockerd`: `ollama pull`
fetches a model, the way `docker pull` fetches an image, and one daemon on one
port serves every model you have pulled. The model you want goes in the
`"model"` field of each request.

Ollama has two HTTP APIs:

| API | Paths | Why it exists |
|---|---|---|
| **OpenAI-compatible** | `/v1/chat/completions`, `/v1/embeddings` | The format most providers and servers use (§19, "provider-agnostic"). Steps 4a and later use it, so switching to vLLM or a hosted API only changes a URL. |
| **Native** | `/api/generate`, `/api/chat`, `/api/embed` | Ollama's own format. It also reports **timings**, which you need for this step. |

API reference: <https://github.com/ollama/ollama/blob/main/docs/api.md> and
<https://github.com/ollama/ollama/blob/main/docs/openai.md>.

**Where a model lives, in kitchen terms:**

1. `ollama pull` puts the weights in the **warehouse** (disk, under
   `/usr/share/ollama/.ollama/models`). They stay there until `ollama rm`.
2. The first request to a model copies the weights into the **fridge**: RAM
   here, or VRAM on a machine with a GPU. That copy is the `load_duration` in
   the response, and it takes seconds.
3. While the model stays loaded, every token reads the weights from the
   fridge, never from disk.
4. After 5 idle minutes, Ollama empties that part of the fridge. The disk copy
   stays, so the next request loads it again.

`ollama ps` lists what's in the fridge right now:

- **`PROCESSOR`** shows **which fridge** the model is in. It isn't a CPU count
  or a thread count. `100% CPU` means all of it is in RAM, `100% GPU` means all
  of it is in VRAM, and `48%/52% CPU/GPU` is a split
  (<https://docs.ollama.com/faq>). On this VM it's always `100% CPU`.
- **`SIZE`** is bigger than the file (5.1 GB vs 4.7 GB for
  `qwen2.5:7b-instruct`). It includes working buffers and the **KV cache**: the
  model's notes about the tokens it has already read, so it doesn't redo them
  (§20). The KV cache grows with the context length.

### 3. Picking the models

You compare two questions at once: **4-bit vs 8-bit** (same model) and
**7B vs 27B** (an older small model vs a newer big one).

| Tag | What it is | Size (ollama.com, Oct 2026) |
|---|---|---|
| `qwen2.5:7b-instruct` | Qwen2.5 7B, 4-bit `Q4_K_M`. `rag`'s default chat model. | 4.7 GB |
| `qwen2.5:7b-instruct-q8_0` | The same model at 8-bit | 8.1 GB |
| `qwen3.8:27b-q4_K_M` | Qwen3.8 27B, 4-bit. Newer, 4× the parameters, can "think". | 18 GB |
| `qwen3.8:27b-q8_0` | The same at 8-bit (optional) | 30 GB |

Sources: <https://ollama.com/library/qwen2.5/tags>,
<https://ollama.com/library/qwen3.8/tags>.

- **Instruct**: `qwen2.5:7b-instruct` follows instructions. A *base* model only
  continues text (§19, "Base vs Instruct"). Qwen3.8 is post-trained, so it
  follows instructions too (§19, model card item 2).
- **Quantization** stores each weight in fewer bits (§19). 8-bit loses less
  quality than 4-bit, but it is about twice as big, so twice as much has to
  ride the belt. Which matters more on this VM is what you measure. The suffix
  names the llama.cpp format (<https://github.com/ggml-org/llama.cpp/pull/1684>):
  - **`Q4`**: each weight is stored as a whole number 0–15 (4 bits).
  - **`K`**: weights are quantized in blocks of 32, and each block keeps its own
    *min* and *scale* (step size). Example: a block spans −0.12 to +0.09, so
    scale = 0.21 / 15 = 0.014. The weight 0.031 is stored as
    (0.031 + 0.12) / 0.014 ≈ 11 and read back as −0.12 + 11 × 0.014 = 0.034,
    an error of 0.003. A small block has a narrow range, so the steps are fine.
    The scales and mins themselves are stored in 6 bits, so `Q4_K` costs 4.5
    bits per weight.
  - **`M`** (medium): a mix. About half of two sensitive tensors
    (`attention.wv`, `feed_forward.w2`) use 6-bit `Q6_K`, and the rest use
    `Q4_K`. `S` uses fewer 6-bit tensors, `L` uses more. That's why the file is
    4.7 GB, not 7.62B × 4 bits = 3.8 GB: it works out to about 4.9 bits per
    weight.
  - **`q8_0`**: an older, simpler format. 8 bits per weight, blocks of 32, a
    scale only. `_0` means no min, so the values are symmetric around zero.
- **Thinking**: Qwen3.8 writes a hidden chain of reasoning before its answer by
  default (§19, model card item 8). Those are extra *output* tokens. Ollama's
  native API turns it off with `"think": false`.
- **`bf16` (16-bit) doesn't fit.** `qwen3.8:27b-bf16` is 56 GB and the VM has
  48 GB of RAM. That's the §19 formula: 27B × 2 bytes = 54 GB.
- **`nomic-embed-text`** is an **embedding model**. It turns text into a vector
  of numbers instead of writing text. Step 1 uses it. Here you only pull it and
  check that it answers.

### 4. A request has two phases: prefill and decode

Every generation request goes through two phases (`ai-platform-basics.md` §5),
and they are the two kinds of work from the kitchen:

- **Prefill** (Ollama calls it *prompt eval*): the model reads the whole prompt
  in one pass. Each weight rides the belt once and is then used for *every*
  prompt token, so there's plenty of cooking per trip. Prefill is
  **compute-bound**. This phase sets **TTFT**, the time to first token.
- **Decode** (Ollama calls it *eval*): the model writes the answer one token at
  a time. Each token needs *all* the weights, the weights are far bigger than
  the cutting board (cache), and each weight gets only about one multiply-add.
  So for every token the whole model rides the belt again. Decode is
  **memory-bandwidth-bound**. This phase sets how fast the answer streams out.

So a model has **two speeds**, both measured in tokens per second:

```
prefill rate = prompt tokens / prefill time    (high: tokens processed in parallel)
decode  rate = output tokens / decode time     (low: one token per pass)
```

Example with made-up numbers: prompt 1,000 tokens at 100 tok/s is 10 s of
prefill. Answer 200 tokens at 5 tok/s is 40 s of decode. Total is about 50 s,
and most of it is decode. This is the latency rule from §16: total time is
dominated by *output* tokens.

With a discrete GPU, both phases speed up for the reasons in the kitchen
section. Decode is still belt-bound there, just on a faster belt. Rough upper
limit for a 4.7 GB model: 4.7 GB ÷ 1,000 GB/s ≈ 200 tok/s on an RTX 4090. Real
numbers come in lower.

Ollama's native response reports these timings in **nanoseconds**:

| Field | Meaning |
|---|---|
| `load_duration` | Time spent loading the weights into the fridge. Large on the first call, near zero while the model stays loaded. |
| `prompt_eval_count` / `prompt_eval_duration` | Prefill: number of tokens and the time they took |
| `eval_count` / `eval_duration` | Decode: number of tokens and the time they took |
| `total_duration` | The whole request |

### 5. Why bigger or higher-precision models are slower here

Decode is belt-bound: every token moves the whole model across the belt. So
decode speed should roughly follow "how many GB ride the belt per token", which
is the model's file size. That gives a hypothesis, relative to
`qwen2.5:7b-instruct` at 4.7 GB:

| Model | Size | Predicted decode speed |
|---|---|---|
| `qwen2.5:7b-instruct` | 4.7 GB | 1× |
| `qwen2.5:7b-instruct-q8_0` | 8.1 GB | ~0.6× |
| `qwen3.8:27b-q4_K_M` | 18 GB | ~0.26× |
| `qwen3.8:27b-q8_0` | 30 GB | ~0.16× |

These are predictions, not measurements. You fill in the real numbers.

Thinking multiplies the cost: if Qwen3.8 thinks for 600 tokens before a
200-token answer, you wait for 800 tokens at its slowest decode speed.

### 6. Threads: more cooks can be slower

By default Ollama writes one ticket per vCPU, so 32 on this VM. The Ollama log
shows `n_threads = 32`. You'd expect 32 cooks to beat 8. On this VM, they
don't. These are **measured** numbers from this VM (2026-10-04,
`qwen2.5:7b-instruct` already loaded, short prompt, 48 output tokens):

| Threads | prefill tok/s | decode tok/s |
|---|---|---|
| 4 | 43 | 7.5 |
| 6 | 57 | 7.2 |
| 8 | – | 7.4 |
| 10 | 72 | 7.7 |
| 12 | 80 | 6.9 |
| 16 | – | 5.3 |
| 32 (default) | – | 1.6–1.7 |

The kitchen explains the table:

1. **Decode is flat from 4 to 10 threads.** That's the belt limit. 4 cooks
   already take everything the belt delivers: 4.7 GB × 7.5 tok/s ≈ 35 GB/s.
   Prefill keeps getting faster with more threads (43 → 80 tok/s), because it's
   compute-bound and more cooks help.
2. **Past ~12 threads, decode gets slower.** The model works layer by layer
   (about 28 layers for this model). In each layer every cook does a share, and
   **nobody starts the next layer until all cooks are done**: the cooks work
   in step. The 32 cooks are rented, and steal time was 22–38% during the
   32-thread run (about 0% just before it). With 32 cooks, at almost every
   layer boundary one of them is out on loan, and the other 31 stand waiting.
   That's 625 ms per token at 32 threads vs 135 ms at 8.

**Rule:** when the work is belt-bound, use **the fewest cooks that keep the belt
busy**. Extra cooks only add more people who must wait for each other. On this
VM that's 4–10 threads, so 8 is a safe pick for this model.

**The best count depends on the model and the machine**, so measure it once
per model. A different format like `q8_0` needs a different amount of
arithmetic per byte, so its plateau may start at a different count. Run it
step 6a has a sweep script for this.

**How to set it.** There are two ways:

- **Per request:** `"options": {"num_thread": 8}`. This works on the native
  `/api/*` endpoints only. The OpenAI-compatible `/v1` request has no field for
  it.
- **Bake it into a derived model** with a **Modelfile**, Ollama's recipe file
  that adds parameters on top of existing weights. This works for `/v1` too,
  and it doesn't copy the weights:

  ```
  FROM qwen2.5:7b-instruct
  PARAMETER num_thread 8
  ```

---

## What the code does

This step has no Go. The only file is `Makefile`, which every later step uses:

- `Makefile:4` builds the binary with `go build -o bin/rag .`. It fails until step 1 adds `go.mod` and `main.go`.
- `Makefile:7` formats the code with `gofmt -s -w .`.

`.gitignore` keeps `bin/` out of git.

---

## Run it

Run everything on the VM (`ssh ubuntu@10.2.1.49`, Ubuntu 24.04).

### 1. Install Ollama and the tools

```bash
curl -fsSL https://ollama.com/install.sh | sh     # installs a systemd service
sudo apt-get install -y jq git
systemctl status ollama --no-pager                # should be "active (running)"
ollama --version
echo 'export OLLAMA_HOST=http://localhost:11434' >> ~/.bashrc   # the env var `rag` reads
source ~/.bashrc
```

Look at the kitchen you got:

```bash
lscpu | grep -E 'Model name|^CPU\(s\)|Thread|Core|Socket|cache'   # cooks, cutting boards
free -g                                                            # fridge
df -h /                                                            # warehouse
journalctl -u ollama --no-pager | grep 'inference compute'         # library=cpu: no GPU
```

### 2. Pull the models

About 61 GB of downloads in total (the VM disk is 150 GB). Skip the last
Qwen3.8 line if you don't want the optional 8-bit 27B:

```bash
ollama pull qwen2.5:7b-instruct
ollama pull qwen2.5:7b-instruct-q8_0
ollama pull qwen3.8:27b-q4_K_M
ollama pull qwen3.8:27b-q8_0            # optional
ollama pull nomic-embed-text
ollama list
```

If a `qwen3.8` pull says it needs a newer Ollama version, re-run the install
script from step 1. It upgrades Ollama in place.

Illustrative output. The sizes should match the table in **Picking the models**:

```
NAME                        ID              SIZE      MODIFIED
qwen3.8:27b-q8_0            ...             30 GB     ...
qwen3.8:27b-q4_K_M          ...             18 GB     ...
qwen2.5:7b-instruct-q8_0    ...             8.1 GB    ...
qwen2.5:7b-instruct         ...             4.7 GB    ...
nomic-embed-text:latest     ...             274 MB    ...
```

Confirm what the plain `qwen2.5:7b-instruct` tag really is:

```bash
ollama show qwen2.5:7b-instruct        # look at the "quantization" line: Q4_K_M
ollama show qwen3.8:27b-q4_K_M         # also check "capabilities": it should list thinking
```

### 3. Clone the docs and check cluster access

```bash
git clone --depth 1 https://github.com/kubedb/docs ~/kubedb-docs
find ~/kubedb-docs -name '*.md' | wc -l           # how many markdown files RAG will index

export KUBECONFIG=~/.kube/kubedb-config           # wherever you copied the cluster's kubeconfig
kubectl get nodes
kubectl get crd | grep kubedb.com | head          # KubeDB CRDs are installed
```

### 4. Call the OpenAI-compatible endpoint by hand

```bash
curl -s $OLLAMA_HOST/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"qwen2.5:7b-instruct",
       "messages":[{"role":"user","content":"Explain Postgres replication lag in 3 bullets."}]}' | jq
```

This runs with Ollama's default of 32 threads, so expect it to take minutes
(about 3 on this VM). Step 6 shows why, and step 7 fixes it. For now, just look
at the format.

Illustrative output, shortened:

```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "model": "qwen2.5:7b-instruct",
  "choices": [
    { "index": 0,
      "message": { "role": "assistant", "content": "- Replication lag is ..." },
      "finish_reason": "stop" }
  ],
  "usage": { "prompt_tokens": 38, "completion_tokens": 112, "total_tokens": 150 }
}
```

### 5. Call the native endpoint by hand

```bash
curl -s $OLLAMA_HOST/api/generate \
  -d '{"model":"qwen2.5:7b-instruct","prompt":"Explain Postgres replication lag in 3 bullets.","stream":false}' \
  | jq 'del(.context)'
```

`del(.context)` hides a long array of token IDs. Illustrative output:

```json
{
  "model": "qwen2.5:7b-instruct",
  "response": "- Replication lag is ...",
  "done": true,
  "done_reason": "stop",
  "total_duration": 21500000000,
  "load_duration": 45000000,
  "prompt_eval_count": 38,
  "prompt_eval_duration": 900000000,
  "eval_count": 112,
  "eval_duration": 20500000000
}
```

Also check that the embedding model answers. Step 1 looks at the actual vector:

```bash
curl -s $OLLAMA_HOST/api/embed \
  -d '{"model":"nomic-embed-text","input":"replication lag is high"}' | jq '.embeddings | length'
# 1
```

### 6. Measure tokens/sec: threads, 4-bit vs 8-bit, 7B vs 27B

**6a. Find the best thread count for each model.** Save this script once:

```bash
cat > ~/sweep.bash <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

host="${OLLAMA_HOST:-http://localhost:11434}"

sweep() {   # usage: sweep <model> [think]
  for th in ${THREADS:-4 6 8 10 12 16 32}; do
    jq -n --arg m "$1" --arg t "${2:-}" --argjson th "$th" \
      '{model:$m, prompt:"Explain Postgres replication lag in 3 bullets.", stream:false,
        options:{temperature:0, num_predict:48, num_thread:$th}}
       + (if $t == "" then {} else {think: ($t|fromjson)} end)' |
    curl -sf "$host/api/generate" -d @- |
    jq -c --argjson th "$th" '{threads:$th,
      prefill_tok_s:(.prompt_eval_count/(.prompt_eval_duration/1e9)),
      decode_tok_s:(.eval_count/(.eval_duration/1e9))}'
  done
  ollama stop "$1"
}

[ $# -ge 1 ] || { echo "usage: $0 <model> [think]" >&2; exit 1; }
sweep "$@"
EOF
chmod +x ~/sweep.bash
```

The file defines the function `sweep`, and the last line actually calls it with
the script's arguments. Without that last line, running the script would only
define the function and exit, printing nothing.

Run it for each model. Pass `false` as the second argument for Qwen3.8 to turn
thinking off. Run `vmstat 2` in a second terminal and watch the `st` column:

```bash
~/sweep.bash qwen2.5:7b-instruct
~/sweep.bash qwen2.5:7b-instruct-q8_0
THREADS="6 8 12 16" ~/sweep.bash qwen3.8:27b-q4_K_M false      # fewer points: each 18 GB reload is slow
```

Each new thread count makes Ollama reload the model. `decode_tok_s` doesn't
include that load time, but the run takes longer.

How to read it: pick the **smallest** thread count where `decode_tok_s` stops
improving. Illustrative output, **not measured**:

```
{"threads":4,"prefill_tok_s":30.1,"decode_tok_s":3.9}
{"threads":6,"prefill_tok_s":41.0,"decode_tok_s":4.4}
{"threads":8,"prefill_tok_s":50.2,"decode_tok_s":4.5}    ← plateau starts: pick 8
{"threads":10,"prefill_tok_s":57.9,"decode_tok_s":4.5}
{"threads":12,"prefill_tok_s":61.3,"decode_tok_s":4.3}
{"threads":16,"prefill_tok_s":60.8,"decode_tok_s":3.1}   ← cooks waiting on each other
{"threads":32,"prefill_tok_s":40.5,"decode_tok_s":1.2}
```

Smallest, because fewer cooks leave the rest free for everything else on the VM
(`rag`, kubectl, and Postgres in later steps), and they're less exposed to
steal time.

**6b. Speed, with thinking off.** Define a function that sends the same request
to any model and computes both rates. `temperature: 0` and a fixed `seed` keep
the runs comparable (§5). `num_predict` caps the output length (default 256,
override with `NUM_PREDICT=...`). `num_thread` defaults to 8 (override with
`NUM_THREAD=...` if 6a picked something else for a model). The optional third
argument sets `think`. Send it only to Qwen3.8, because qwen2.5 isn't a
thinking model:

```bash
bench() {
  jq -n --arg m "$1" --arg p "$2" --arg t "${3:-}" \
        --argjson n "${NUM_PREDICT:-256}" --argjson th "${NUM_THREAD:-8}" \
    '{model:$m, prompt:$p, stream:false,
      options:{temperature:0, seed:42, num_predict:$n, num_thread:$th}}
     + (if $t == "" then {} else {think: ($t|fromjson)} end)' |
  curl -s "$OLLAMA_HOST/api/generate" -d @- |
  jq '{model,
       load_s:          (.load_duration/1e9),
       prompt_tokens:   .prompt_eval_count,
       prefill_tok_s:   (if (.prompt_eval_duration // 0) > 0 then .prompt_eval_count/(.prompt_eval_duration/1e9) else null end),
       output_tokens:   .eval_count,
       decode_tok_s:    (.eval_count/(.eval_duration/1e9)),
       total_s:         (.total_duration/1e9),
       thinking_chars:  ((.thinking // "") | length),
       answer:          .response}'
}

PROMPT="$(head -c 4000 ~/kubedb-docs/README.md)

Summarise the text above in 5 bullets."
```

The prompt is ~4,000 characters, which is about 1,000 tokens by the "1 token ≈
4 characters" rule (§2). If `README.md` is shorter, any KubeDB `.md` file works.

Each model runs twice, cold then warm. Then `ollama stop` empties its part of
the fridge, so the next model gets the RAM. 18 GB and 30 GB models don't fit
side by side with the others.

```bash
for m in qwen2.5:7b-instruct qwen2.5:7b-instruct-q8_0; do
  bench "$m" "$PROMPT"; bench "$m" "$PROMPT"; ollama stop "$m"
done
for m in qwen3.8:27b-q4_K_M qwen3.8:27b-q8_0; do         # drop q8_0 if you skipped it
  bench "$m" "$PROMPT" false; bench "$m" "$PROMPT" false; ollama stop "$m"
done
```

Illustrative output, **not measured**. Your numbers will be different:

```json
{ "model": "qwen2.5:7b-instruct", "load_s": 3.1, "prompt_tokens": 1012,
  "prefill_tok_s": 60.0, "output_tokens": 256, "decode_tok_s": 6.0, "total_s": 63.0,
  "thinking_chars": 0, "answer": "- KubeDB is ..." }
```

Ollama's CLI prints the same two rates if you pass `--verbose`, which is a quick
way to cross-check. It uses the model's default thread count, so expect a slow
`eval rate`:

```bash
ollama run qwen2.5:7b-instruct --verbose "Explain Postgres replication lag in 3 bullets."
# ... prompt eval rate: N tokens/s ... eval rate: N tokens/s
```

**6c. What thinking costs.** Same model, same short question, thinking off and
then on. Raise the cap so the reasoning has room to finish:

```bash
Q="A KubeDB Postgres pod is Running but the Postgres object stays NotReady. List 3 likely causes."
NUM_PREDICT=2048 bench qwen3.8:27b-q4_K_M "$Q" false
NUM_PREDICT=2048 bench qwen3.8:27b-q4_K_M "$Q" true
```

Before you run the `true` line, estimate the worst case: 2048 ÷ your
`decode_tok_s` from 6b is how long it could take, in seconds. Use that to
decide whether to wait or lower `NUM_PREDICT`.

### 7. Pick the chat model

Read the `answer` fields from 6b next to each other, then weigh quality against
decode speed. Two questions to settle:

- Is the 8-bit answer actually better than the 4-bit answer of the same model,
  and is it worth the speed you measured?
- Is Qwen3.8's answer better enough than qwen2.5's to pay for its decode speed,
  with thinking off? And does thinking improve the 6c answer enough to pay for
  `output_tokens` growing?

`rag` talks to the `/v1` endpoint (step 4a onward), which can't send
`num_thread`. So bake the thread count from 6a into a derived model of the one
you picked. One derived model per base model:

```bash
printf 'FROM qwen2.5:7b-instruct\nPARAMETER num_thread 8\n' > /tmp/Modelfile
ollama create qwen2.5-7b-t8 -f /tmp/Modelfile
ollama show qwen2.5-7b-t8 --parameters        # should list num_thread 8

# the same pattern for any other model, e.g.:
printf 'FROM qwen3.8:27b-q4_K_M\nPARAMETER num_thread 8\n' > /tmp/Modelfile
ollama create qwen3.8-27b-t8 -f /tmp/Modelfile
```

Check it over `/v1`. The same request as Run it step 4 dropped from about 3 min
to 32 s on this VM (including ~8 s of loading):

```bash
time curl -s $OLLAMA_HOST/v1/chat/completions -H 'Content-Type: application/json' \
  -d '{"model":"qwen2.5-7b-t8","messages":[{"role":"user","content":"Explain Postgres replication lag in 3 bullets."}]}' \
  | jq .usage
```

`rag` reads the model name from `RAG_CHAT_MODEL` (default
`qwen2.5:7b-instruct`, which runs at 32 threads). Plain `curl` commands don't
use it, because you type the model name in the request yourself. Export it
once, so `rag` picks it up later:

```bash
echo 'export RAG_CHAT_MODEL=qwen2.5-7b-t8' >> ~/.bashrc
source ~/.bashrc
```

---

## Observe

1. **Same model, two formats.** The OpenAI format puts the answer in
   `choices[0].message.content` and counts tokens in `usage`. The native format
   puts it in `response` and adds timings. Same model, same weights, two wire
   formats on top.
2. **`prompt_tokens` vs `prompt_eval_count`.** Your 8-word question took about
   38 tokens, not 8. The model's **chat template** wraps your text in role
   markers and a default system prompt (§4) before tokenizing it.
3. **Cold vs warm.** `load_s` is several seconds on the first run (warehouse →
   fridge) and near zero on the second. Run `ollama ps` to see which models are
   loaded and when they will be unloaded. `PROCESSOR` should say `100% CPU`:
   all of the model is in RAM, since there's no GPU.
4. **Prefill is much faster than decode.** Compare `prefill_tok_s` with
   `decode_tok_s` for each model. This is §2's "input is fast, output is slow",
   measured on your own hardware.
5. **Threads (6a).** `decode_tok_s` should stay flat over a range of thread
   counts and then fall. `prefill_tok_s` keeps rising for longer. In `vmstat`,
   `st` jumps at 32 threads. That's the belt limit, plus cooks in step waiting
   for a cook out on loan.
6. **Bytes vs speed (6b).** Divide each model's `decode_tok_s` by the
   `qwen2.5:7b-instruct` rate and compare with the prediction table in Concept
   §5. If the measured ratios follow file size, decode on this VM is
   belt-bound.
7. **Thinking cost (6c).** With `think: true`, `thinking_chars` is non-zero and
   `output_tokens` should jump, because the reasoning is generated one token at
   a time like the answer. `total_s` grows by about the extra tokens ÷
   `decode_tok_s`.
8. **The warm prefill may be suspiciously fast.** On the second identical
   request, Ollama can reuse the KV cache from the first, so
   `prompt_eval_count` may drop and `prefill_tok_s` may come back `null`. Use
   the first run for the prefill rate. Use either run for decode.
9. **`free -g` while a 27B model is loaded.** The RAM it uses is the weights
   (`ollama list` size) plus the KV cache. With `qwen3.8:27b-q8_0`, that's
   30 GB of the 48 GB fridge before the conversation even starts.

---

## Break it

**Overflow the context window without noticing.**

Ollama doesn't run a model at its full context window. It uses a smaller
default `num_ctx`, which is 4096 tokens in recent versions (older versions:
2048; check `ollama show qwen2.5:7b-instruct` and the docs for your version).
`ollama ps` shows it in the `CONTEXT` column. If the prompt is longer than
`num_ctx`, Ollama **silently drops the start of the prompt**. There is no error.

Put a fact at the very beginning, bury it under ~30 KB of docs (~7,500 tokens),
and then ask for it:

```bash
LONG="The secret word is KUBEPANDA.

$(find ~/kubedb-docs -name '*.md' | head -n 40 | xargs cat | head -c 30000)

What is the secret word? Answer with one word."

jq -n --arg p "$LONG" '{model:"qwen2.5:7b-instruct", prompt:$p, stream:false,
  options:{temperature:0, num_thread:8}}' |
curl -s $OLLAMA_HOST/api/generate -d @- | jq '{response, prompt_eval_count}'
```

What should happen: `prompt_eval_count` stops at about your `num_ctx`, even
though you sent ~7,500 tokens. The model doesn't know the word, or makes one up.

Now raise the window and run it again:

```bash
jq -n --arg p "$LONG" '{model:"qwen2.5:7b-instruct", prompt:$p, stream:false,
  options:{temperature:0, num_thread:8, num_ctx:16384}}' |
curl -s $OLLAMA_HOST/api/generate -d @- | jq '{response, prompt_eval_count, prefill_s:(.prompt_eval_duration/1e9)}'
```

`prompt_eval_count` should now show the full prompt, and the model should
answer `KUBEPANDA`. Look at `prefill_s`. That's TTFT growing with input size
(§3), on a CPU. Run `ollama ps` too: `SIZE` grows, because a bigger window
means a bigger KV cache.

The lesson for step 4b: when RAG pastes chunks into the prompt, check
`prompt_eval_count` against `num_ctx`. Otherwise you may be throwing away your
own retrieved context.

---

## My results

Ollama version:

Kitchen (Run it 1): CPU model ____, vCPUs ____, cache sizes ____, RAM ____, disk ____

Threads (6a), best count / decode tok/s at it: qwen2.5 7B q4 = ____ / ____, 7B q8 = ____ / ____, qwen3.8 27B q4 = ____ / ____; steal % at 32 threads = ____

| Model | load_s (cold) | prompt tokens | prefill tok/s | output tokens | decode tok/s | total_s | decode ratio vs 7B q4 (predicted) |
|---|---|---|---|---|---|---|---|
| qwen2.5:7b-instruct | | | | | | | 1× (1×) |
| qwen2.5:7b-instruct-q8_0 | | | | | | | (~0.6×) |
| qwen3.8:27b-q4_K_M | | | | | | | (~0.26×) |
| qwen3.8:27b-q8_0 | | | | | | | (~0.16×) |

Thinking (6c, qwen3.8:27b-q4_K_M): output tokens off / on = ____ / ____, total_s off / on = ____ / ____

Quality notes (4-bit vs 8-bit; qwen2.5 7B vs qwen3.8 27B; thinking off vs on):

Break it: default `num_ctx` = ____, `prompt_eval_count` before / after = ____ / ____, prefill_s at 16K = ____

Chosen chat model and thread count (and why):
