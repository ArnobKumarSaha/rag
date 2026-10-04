# Step 0: Run a local model and measure its speed

Notes: §19 (the model landscape) and §20 (running a model locally) in
`llm-basics-2.md`. Prefill and decode are covered in §5 of `ai-platform-basics.md`.

There is no Go code in this step. You set up the VM, talk to the model with
`curl`, and measure how fast it runs. Every later step uses this setup, and the
numbers you record here are the speeds you'll be working with.

---

## Concept

### Ollama is a model server

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

### Picking the models

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
- **Quantization** stores each weight in fewer bits (§19). The suffix names the
  format: `Q4_K_M` is about 4.5 bits per weight, `q8_0` is 8 bits. 8-bit loses
  less quality than 4-bit, but it is about twice as big. Which matters more on
  this VM is what you measure.
- **Thinking**: Qwen3.8 writes a hidden chain of reasoning before its answer by
  default (§19, model card item 8). Those are extra *output* tokens. Ollama's
  native API turns it off with `"think": false`.
- **`bf16` (16-bit) doesn't fit.** `qwen3.8:27b-bf16` is 56 GB and the VM has
  48 GB of RAM. That's the §19 formula: 27B × 2 bytes = 54 GB.
- **`nomic-embed-text`** is an **embedding model**. It turns text into a vector
  of numbers instead of writing text. Step 1 uses it. Here you only pull it and
  check that it answers.

### CPU and memory: a kitchen

To understand the speeds in this step, you need a picture of where the model
lives and who does the work. Think of a kitchen.

**The CPU is the cook.** It does the work (add, multiply, compare), very fast,
but only on what's right in front of it.

- A **core** is one cook. The VM has 32 virtual cores (**vCPUs**).
- A **thread** is one task handed to a cook. `num_thread: 8` tells Ollama to
  split the work into 8 tasks, run by 8 cooks at once.

**Memory is where the ingredients wait.** There are three places, at three
distances:

| Kitchen | Computer | Size on this VM (roughly) | Reach time |
|---|---|---|---|
| Cutting board, under the cook's hands | **Cache**, inside the CPU | a few MB | instant |
| Fridge in the next room | **RAM** | 48 GB | slower |
| Warehouse across town | **Disk** | 150 GB | much slower |

Data travels from the fridge to the cutting board on a **conveyor belt**. The
belt's speed, in GB per second, is the **memory bandwidth**. All cooks share one
belt. Hiring more cooks doesn't make the belt faster.

**Where a model lives, step by step:**

1. `ollama pull` puts the weights in the **warehouse** (disk, under
   `/usr/share/ollama/.ollama/models`). They stay there until `ollama rm`.
2. The first request to a model copies the weights from disk into the
   **fridge** (RAM). That's the `load_duration` in the response, and it takes
   seconds.
3. While the model stays loaded, every token reads the weights from RAM, never
   from disk. `ollama ps` shows what's in RAM right now.
4. After 5 idle minutes, Ollama empties that part of the fridge. The disk copy
   stays, so the next request loads it again.

**Two kinds of work.**

- **The belt is the limit** (**memory-bandwidth-bound**). Each ingredient needs
  very little cooking, for example "add 1 to each of a billion numbers". The
  cooks finish instantly and wait for the belt. More cooks means more people
  waiting at the same belt.
- **The cooks are the limit** (**compute-bound**). Each ingredient needs lots of
  work once it's on the board. Here more cooks help, up to the number of real
  cooks you have.

The next section shows that a model does both kinds of work, one after the other.

### A request has two phases: prefill and decode

Every generation request goes through two phases (`ai-platform-basics.md` §5):

- **Prefill** (Ollama calls it *prompt eval*): the model reads the whole prompt
  in one pass. Each weight rides the belt once and is then used for *every*
  prompt token, so there's plenty of cooking per trip. That makes prefill
  **compute-bound**. This phase sets **TTFT**, the time to first token.
- **Decode** (Ollama calls it *eval*): the model writes the answer one token at
  a time. Each token needs *all* the weights, the weights are far bigger than
  the cache, and each weight gets only about one multiply-add. So for every
  token the whole model rides the belt again. That makes decode
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

Ollama's native response reports these timings in **nanoseconds**:

| Field | Meaning |
|---|---|
| `load_duration` | Time spent loading the weights into RAM. Large on the first call, near zero while the model stays loaded. |
| `prompt_eval_count` / `prompt_eval_duration` | Prefill: number of tokens and the time they took |
| `eval_count` / `eval_duration` | Decode: number of tokens and the time they took |
| `total_duration` | The whole request |

### Why bigger or higher-precision models are slower here

The VM has **no GPU**, so the CPU does the math and the loaded weights sit in
ordinary RAM. Decode is belt-bound: every token moves the whole model across the
belt. So decode speed should roughly follow "how many GB ride the belt per
token", which is the model's file size.
That gives a hypothesis, relative to `qwen2.5:7b-instruct` at 4.7 GB:

| Model | Size | Predicted decode speed |
|---|---|---|
| `qwen2.5:7b-instruct` | 4.7 GB | 1× |
| `qwen2.5:7b-instruct-q8_0` | 8.1 GB | ~0.6× |
| `qwen3.8:27b-q4_K_M` | 18 GB | ~0.26× |
| `qwen3.8:27b-q8_0` | 30 GB | ~0.16× |

These are predictions, not measurements. You fill in the real numbers.

Thinking multiplies the cost: if Qwen3.8 thinks for 600 tokens before a
200-token answer, you wait for 800 tokens at its slowest decode speed.

### Threads: more cooks can be slower

By default Ollama uses one thread per vCPU, so 32 on this VM. The Ollama log
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

Two things explain the table:

1. **Decode is flat from 4 to 10 threads.** That's the belt limit. 4 cooks
   already take everything the belt delivers: 4.7 GB × 7.5 tok/s ≈ 35 GB/s.
   Prefill keeps getting faster with more threads (43 → 80 tok/s), because it's
   compute-bound and more cooks help.
2. **Past ~12 threads, decode gets slower.** The model works layer by layer
   (about 28 layers for this model). In each layer, every cook does a share,
   and **nobody starts the next layer until all cooks are done**. The 32 vCPUs
   are borrowed: Harvester shares the real cores with other VMs, and sometimes
   pauses a vCPU to run someone else's work. That pause is **steal time**, the
   `st` column in `vmstat`. It was 22–38% during the 32-thread run and about
   0% just before it. With 32 cooks, at almost every layer boundary one of them
   has been paused, and the other 31 stand waiting. That's 625 ms per token at
   32 threads vs 135 ms at 8.

**Rule:** when the work is belt-bound, use just enough cooks to keep the belt
busy. On this VM that's 4–10 threads, so 8 is a safe pick.

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

Run everything on the VM (Ubuntu 24.04).

### 1. Install Ollama and the tools

```bash
curl -fsSL https://ollama.com/install.sh | sh     # installs a systemd service
sudo apt-get install -y jq git
systemctl status ollama --no-pager                # should be "active (running)"
ollama --version
export OLLAMA_HOST=http://localhost:11434         # the same env var `rag` reads
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
(about 3 on this VM). Step 6 fixes that. For now, just look at the format.

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

Define a shell function that sends the same request to any model and computes
both rates. `temperature: 0` and a fixed `seed` keep the runs comparable (§5).
`num_predict` caps the output length (default 256, override with
`NUM_PREDICT=...`). `num_thread` sets the number of cooks (default 8, override
with `NUM_THREAD=...`). The optional third argument sets `think`. Send it only
to Qwen3.8, because qwen2.5 isn't a thinking model:

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

**6a. Find the thread count.** Reproduce the thread table from the Concept
section. Run `vmstat 2` in a second terminal while this runs, and watch the
`st` (steal) column:

```bash
for th in 4 8 16 32; do
  NUM_THREAD=$th NUM_PREDICT=48 bench qwen2.5:7b-instruct "Explain Postgres replication lag in 3 bullets." |
    jq -c --arg th $th '{threads:$th, prefill_tok_s, decode_tok_s}'
done
```

Changing `num_thread` makes Ollama reload the model, so each line includes a
few seconds of loading. `decode_tok_s` doesn't include that time. Then check
the 27B model too. Its best count may differ:

```bash
for th in 8 16; do
  NUM_THREAD=$th NUM_PREDICT=32 bench qwen3.8:27b-q4_K_M "hi" false | jq -c --arg th $th '{threads:$th, decode_tok_s}'
done
```

Use the best number as `NUM_THREAD` for the rest of step 6 if it isn't 8.

**6b. Speed, with thinking off.** Each model runs twice, cold then warm. Then
`ollama stop` unloads it, so the next model gets the RAM. 18 GB and 30 GB
models don't fit side by side with the others.

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

Ollama's CLI prints the same two rates if you pass `--verbose`, which is a quick way to cross-check:

```bash
ollama run qwen2.5:7b-instruct --verbose "Explain Postgres replication lag in 3 bullets."
# ... prompt eval rate: N tokens/s ... eval rate: N tokens/s
```

### 7. Pick the chat model

Read the `answer` fields from 6b next to each other, then weigh quality against
decode speed. Two questions to settle:

- Is the 8-bit answer actually better than the 4-bit answer of the same model,
  and is it worth the speed you measured?
- Is Qwen3.8's answer better enough than qwen2.5's to pay for its decode speed,
  with thinking off? And does thinking improve the 6c answer enough to pay for
  `output_tokens` growing?

`rag` talks to the `/v1` endpoint (step 4a onward), which can't send
`num_thread`. So bake your thread count into a derived model of the one you
picked:

```bash
printf 'FROM qwen2.5:7b-instruct\nPARAMETER num_thread 8\n' > /tmp/Modelfile
ollama create qwen2.5-7b-t8 -f /tmp/Modelfile
ollama show qwen2.5-7b-t8 --parameters        # should list num_thread 8
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
echo 'export OLLAMA_HOST=http://localhost:11434' >> ~/.bashrc
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
3. **Cold vs warm.** `load_s` is several seconds on the first run and near zero
   on the second. Run `ollama ps` to see which models are loaded and when they
   will be unloaded (5 minutes idle by default).
4. **Prefill is much faster than decode.** Compare `prefill_tok_s` with
   `decode_tok_s` for each model. This is §2's "input is fast, output is slow",
   measured on your own hardware.
5. **Threads (6a).** `decode_tok_s` should stay flat over a range of thread
   counts and then fall. `prefill_tok_s` keeps rising. In `vmstat`, `st`
   jumps when you run 32 threads. That's the belt limit plus the waiting at
   each layer, from "Threads: more cooks can be slower".
6. **Bytes vs speed.** Divide each model's `decode_tok_s` by the
   `qwen2.5:7b-instruct` rate and compare with the predicted column in "Why
   bigger or higher-precision models are slower here". If the measured ratios
   follow file size, decode on this VM is limited by how fast RAM can be read.
7. **Thinking cost (6c).** With `think: true`, `thinking_chars` is non-zero and
   `output_tokens` should jump, because the reasoning is generated one token at
   a time like the answer. `total_s` grows by about the extra tokens ÷
   `decode_tok_s`.
8. **The warm prefill may be suspiciously fast.** On the second identical
   request, Ollama can reuse the cached prompt (KV cache), so
   `prompt_eval_count` may drop and `prefill_tok_s` may come back `null`. Use
   the first run for the prefill rate. Use either run for decode.
9. **`free -g` while a 27B model is loaded.** The RAM it uses is the weights
   (`ollama list` size) plus the KV cache (§20). With `qwen3.8:27b-q8_0`,
   that's 30 GB of 48 GB before the conversation even starts.

---

## Break it

**Overflow the context window without noticing.**

Ollama doesn't run a model at its full context window. It uses a smaller
default `num_ctx`, which is 4096 tokens in recent versions (older versions:
2048; check `ollama show qwen2.5:7b-instruct` and the docs for your version).
If the prompt is longer than `num_ctx`, Ollama **silently drops the start of
the prompt**. There is no error.

Put a fact at the very beginning, bury it under ~30 KB of docs (~7,500 tokens),
and then ask for it:

```bash
LONG="The secret word is KUBEPANDA.

$(find ~/kubedb-docs -name '*.md' | head -n 40 | xargs cat | head -c 30000)

What is the secret word? Answer with one word."

jq -n --arg p "$LONG" '{model:"qwen2.5:7b-instruct", prompt:$p, stream:false,
  options:{temperature:0}}' |
curl -s $OLLAMA_HOST/api/generate -d @- | jq '{response, prompt_eval_count}'
```

What should happen: `prompt_eval_count` stops at about your `num_ctx`, even
though you sent ~7,500 tokens. The model doesn't know the word, or makes one up.

Now raise the window and run it again:

```bash
jq -n --arg p "$LONG" '{model:"qwen2.5:7b-instruct", prompt:$p, stream:false,
  options:{temperature:0, num_ctx:16384}}' |
curl -s $OLLAMA_HOST/api/generate -d @- | jq '{response, prompt_eval_count, prefill_s:(.prompt_eval_duration/1e9)}'
```

`prompt_eval_count` should now show the full prompt, and the model should
answer `KUBEPANDA`. Look at `prefill_s`. That's TTFT growing with input size
(§3), on a CPU.

The lesson for step 4b: when RAG pastes chunks into the prompt, check
`prompt_eval_count` against `num_ctx`. Otherwise you may be throwing away your
own retrieved context.

---

## My results

Ollama version:

Threads (6a), decode tok/s: 4 = ____, 8 = ____, 16 = ____, 32 = ____; steal % at 32 = ____; best for 27B = ____

| Model | load_s (cold) | prompt tokens | prefill tok/s | output tokens | decode tok/s | total_s | decode ratio vs 7B q4 (predicted) |
|---|---|---|---|---|---|---|---|
| qwen2.5:7b-instruct | | | | | | | 1× (1×) |
| qwen2.5:7b-instruct-q8_0 | | | | | | | (~0.6×) |
| qwen3.8:27b-q4_K_M | | | | | | | (~0.26×) |
| qwen3.8:27b-q8_0 | | | | | | | (~0.16×) |

Thinking (6c, qwen3.8:27b-q4_K_M): output tokens off / on = ____ / ____, total_s off / on = ____ / ____

Quality notes (4-bit vs 8-bit; qwen2.5 7B vs qwen3.8 27B; thinking off vs on):

Break it: default `num_ctx` = ____, `prompt_eval_count` before / after = ____ / ____, prefill_s at 16K = ____

Chosen chat model (and why):
