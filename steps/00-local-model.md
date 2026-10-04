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

- **`qwen2.5:Nb-instruct`**: the *instruct* version, which follows instructions.
  A *base* model only continues text (§19, "Base vs Instruct").
- Ollama's default tags are **quantized** to about 4 bits per weight (`Q4_K_M`).
  That shrinks a 7B model from ~14 GB to roughly 4–5 GB (§19, "Quantization").
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

### Why a bigger model is slower here

The VM has **no GPU**, so the CPU does the math and the weights sit in ordinary
RAM. Each decode step reads every weight once, so decode speed roughly follows
"how many GB of weights must be read per token". A 14B model has about twice
the weights of a 7B model, so expect roughly half the decode speed. That is the
hypothesis. You will measure the real ratio, so don't trust this one until you
have numbers.

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

```bash
ollama pull qwen2.5:3b-instruct
ollama pull qwen2.5:7b-instruct
ollama pull qwen2.5:14b-instruct
ollama pull nomic-embed-text
ollama list
```

Illustrative output. Compare the sizes with the §20 table:

```
NAME                    ID              SIZE      MODIFIED
qwen2.5:14b-instruct    ...             9.0 GB    ...
qwen2.5:7b-instruct     ...             4.7 GB    ...
qwen2.5:3b-instruct     ...             1.9 GB    ...
nomic-embed-text:latest ...             274 MB    ...
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

### 6. Measure tokens/sec for 3B, 7B, and 14B

Define a shell function that sends the same request to any model and computes
both rates. `temperature: 0` and a fixed `seed` keep the runs comparable (§5),
and `num_predict` caps the answer length:

```bash
bench() {
  jq -n --arg m "$1" --arg p "$2" \
    '{model:$m, prompt:$p, stream:false,
      options:{temperature:0, seed:42, num_predict:256}}' |
  curl -s "$OLLAMA_HOST/api/generate" -d @- |
  jq '{model,
       load_s:        (.load_duration/1e9),
       prompt_tokens: .prompt_eval_count,
       prefill_tok_s: (if (.prompt_eval_duration // 0) > 0 then .prompt_eval_count/(.prompt_eval_duration/1e9) else null end),
       output_tokens: .eval_count,
       decode_tok_s:  (.eval_count/(.eval_duration/1e9)),
       total_s:       (.total_duration/1e9)}'
}

PROMPT="$(head -c 4000 ~/kubedb-docs/README.md)

Summarise the text above in 5 bullets."

for m in qwen2.5:3b-instruct qwen2.5:7b-instruct qwen2.5:14b-instruct; do
  bench "$m" "$PROMPT"     # 1st run: cold, includes load time
  bench "$m" "$PROMPT"     # 2nd run: warm
done
```

The prompt is ~4,000 characters, which is about 1,000 tokens by the "1 token ≈ 4 characters" rule (§2). If `README.md` is shorter, any KubeDB `.md` file works.

Illustrative output, **not measured**. Your numbers will be different:

```json
{ "model": "qwen2.5:7b-instruct", "load_s": 3.1, "prompt_tokens": 1012,
  "prefill_tok_s": 60.0, "output_tokens": 256, "decode_tok_s": 6.0, "total_s": 63.0 }
```

Ollama's CLI prints the same two rates if you pass `--verbose`, which is a quick way to cross-check:

```bash
ollama run qwen2.5:7b-instruct --verbose "Explain Postgres replication lag in 3 bullets."
# ... prompt eval rate: N tokens/s ... eval rate: N tokens/s
```

### 7. Pick the chat model

Read the 5-bullet summaries from the three models next to each other, then
weigh quality against decode speed. `rag` defaults to `qwen2.5:7b-instruct`. If
you choose a different model, export it before every later step:

```bash
export RAG_CHAT_MODEL=qwen2.5:14b-instruct      # only if you picked it
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
5. **Model size vs speed.** Divide the 7B decode rate by the 14B decode rate.
   Is it close to 2, as the hypothesis predicted?
6. **The warm prefill may be suspiciously fast.** On the second identical
   request, Ollama can reuse the cached prompt (KV cache), so
   `prompt_eval_count` may drop and `prefill_tok_s` may come back `null`. Use
   the first run for the prefill rate. Use either run for decode.
7. **`free -g` while the 14B model is loaded.** The RAM it uses is the weights
   (`ollama list` size) plus the KV cache (§20).

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

| Model | load_s (cold) | prompt tokens | prefill tok/s | output tokens | decode tok/s | total_s |
|---|---|---|---|---|---|---|
| qwen2.5:3b-instruct | | | | | | |
| qwen2.5:7b-instruct | | | | | | |
| qwen2.5:14b-instruct | | | | | | |

7B ÷ 14B decode ratio:

Quality notes (3B vs 7B vs 14B summaries):

Break it: default `num_ctx` = ____, `prompt_eval_count` before / after = ____ / ____, prefill_s at 16K = ____

Chosen chat model (and why):
