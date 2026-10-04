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

### A request has two phases: prefill and decode

Every generation request goes through two phases (`ai-platform-basics.md` §5):

- **Prefill** (Ollama calls it *prompt eval*): the model reads the whole prompt
  in one pass and processes all the input tokens in parallel. This phase sets
  **TTFT**, the time to first token.
- **Decode** (Ollama calls it *eval*): the model writes the answer one token at
  a time, and each token needs a full pass over all the weights. This phase sets
  how fast the answer streams out.

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

The VM has **no GPU**, so the CPU does the math and the weights sit in ordinary
RAM. Each decode step reads every weight once, so decode speed should roughly
follow "how many GB must be read per token", which is the model's file size.
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

### 6. Measure tokens/sec: 4-bit vs 8-bit, 7B vs 27B

Define a shell function that sends the same request to any model and computes
both rates. `temperature: 0` and a fixed `seed` keep the runs comparable (§5).
`num_predict` caps the output length (default 256, override with
`NUM_PREDICT=...`). The optional third argument sets `think`. Send it only to
Qwen3.8, because qwen2.5 isn't a thinking model:

```bash
bench() {
  jq -n --arg m "$1" --arg p "$2" --arg t "${3:-}" --argjson n "${NUM_PREDICT:-256}" \
    '{model:$m, prompt:$p, stream:false,
      options:{temperature:0, seed:42, num_predict:$n}}
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

**6a. Speed, with thinking off.** Each model runs twice, cold then warm. Then
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

**6b. What thinking costs.** Same model, same short question, thinking off and
then on. Raise the cap so the reasoning has room to finish:

```bash
Q="A KubeDB Postgres pod is Running but the Postgres object stays NotReady. List 3 likely causes."
NUM_PREDICT=2048 bench qwen3.8:27b-q4_K_M "$Q" false
NUM_PREDICT=2048 bench qwen3.8:27b-q4_K_M "$Q" true
```

Before you run the `true` line, estimate the worst case: 2048 ÷ your
`decode_tok_s` from 6a is how long it could take, in seconds. Use that to
decide whether to wait or lower `NUM_PREDICT`.

Ollama's CLI prints the same two rates if you pass `--verbose`, which is a quick way to cross-check:

```bash
ollama run qwen2.5:7b-instruct --verbose "Explain Postgres replication lag in 3 bullets."
# ... prompt eval rate: N tokens/s ... eval rate: N tokens/s
```

### 7. Pick the chat model

Read the `answer` fields from 6a next to each other, then weigh quality against
decode speed. Two questions to settle:

- Is the 8-bit answer actually better than the 4-bit answer of the same model,
  and is it worth the speed you measured?
- Is Qwen3.8's answer better enough than qwen2.5's to pay for its decode speed,
  with thinking off? And does thinking improve the 6b answer enough to pay for
  `output_tokens` growing?

`rag` defaults to `qwen2.5:7b-instruct`. If you choose a different model,
export it before every later step:

```bash
export RAG_CHAT_MODEL=qwen3.8:27b-q4_K_M      # only if you picked it
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
5. **Bytes vs speed.** Divide each model's `decode_tok_s` by the
   `qwen2.5:7b-instruct` rate and compare with the predicted column in "Why
   bigger or higher-precision models are slower here". If the measured ratios
   follow file size, decode on this VM is limited by how fast RAM can be read.
6. **Thinking cost (6b).** With `think: true`, `thinking_chars` is non-zero and
   `output_tokens` should jump, because the reasoning is generated one token at
   a time like the answer. `total_s` grows by about the extra tokens ÷
   `decode_tok_s`.
7. **The warm prefill may be suspiciously fast.** On the second identical
   request, Ollama can reuse the cached prompt (KV cache), so
   `prompt_eval_count` may drop and `prefill_tok_s` may come back `null`. Use
   the first run for the prefill rate. Use either run for decode.
8. **`free -g` while a 27B model is loaded.** The RAM it uses is the weights
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

| Model | load_s (cold) | prompt tokens | prefill tok/s | output tokens | decode tok/s | total_s | decode ratio vs 7B q4 (predicted) |
|---|---|---|---|---|---|---|---|
| qwen2.5:7b-instruct | | | | | | | 1× (1×) |
| qwen2.5:7b-instruct-q8_0 | | | | | | | (~0.6×) |
| qwen3.8:27b-q4_K_M | | | | | | | (~0.26×) |
| qwen3.8:27b-q8_0 | | | | | | | (~0.16×) |

Thinking (6b, qwen3.8:27b-q4_K_M): output tokens off / on = ____ / ____, total_s off / on = ____ / ____

Quality notes (4-bit vs 8-bit; qwen2.5 7B vs qwen3.8 27B; thinking off vs on):

Break it: default `num_ctx` = ____, `prompt_eval_count` before / after = ____ / ____, prefill_s at 16K = ____

Chosen chat model (and why):
