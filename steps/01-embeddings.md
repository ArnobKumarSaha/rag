# Step 1: Embeddings and cosine similarity

Notes: §7 (embeddings and vectors) in `llm-basics-1.md`. The `/api/embed` call
was first shown in §20 of `llm-basics-2.md`.

This is the first Go code. `rag embed "a" "b" "c"` sends the texts to Ollama's
embedding model, prints the length of each vector it gets back, and prints how
similar every pair is. Step 3's search is built on this one number.

---

## Concept

### 1. An embedding is a list of numbers that stands for a meaning

An **embedding model** is a small model that reads a piece of text and returns a
fixed-length list of decimal numbers. The list is called a **vector** or an
**embedding**. It doesn't write text like a chat model does.

`nomic-embed-text`, the model you pulled in step 0, returns 768 numbers for any
input, whether it's 3 words or 3 paragraphs
(<https://huggingface.co/nomic-ai/nomic-embed-text-v1.5>):

```
embed("replication lag is high")  →  [0.021, -0.118, 0.334, ..., 0.007]   # 768 numbers
```

- **Dimensions**: the length of the vector, 768 here. It's fixed by the model,
  not by the input.
- **Dimensions are not parameters** (§1). Dimensions are the length of the
  *output*. Parameters are the size of the model that *produces* it.
  `nomic-embed-text` has ~137M parameters and a 274 MB file.

No single number means anything on its own. Number 17 is not "about databases".
What matters is how the whole vector points.

### 2. Similar meaning, similar direction

Think of each vector as an arrow from the origin. With 2 numbers it's an arrow
on paper. With 768 it's an arrow in a space you can't draw, but the maths is the
same. The model is trained so that **texts with similar meaning give arrows that
point in similar directions**, even when they share no words:

```
"replication lag is high"        ─┐ point almost the same way
"the standby is falling behind"  ─┘

"disk is full"                   ─── points somewhere else
```

That's what makes **search by meaning** possible. `grep "replication lag"`
never finds a runbook titled "standby falling behind". Comparing arrows can.

### 3. Cosine similarity measures "same direction"

**Cosine similarity** is the cosine of the angle between two arrows:

```
cosine(a, b) = (a · b) / (|a| × |b|)

a · b   dot product: multiply matching positions, add them up
|a|     norm (length): square root of a · a
```

- `1.0`: same direction (same meaning)
- `0`: at right angles (unrelated)
- `-1`: opposite directions

A 2-number example you can check by hand:

```
a = (3, 4)    b = (4, 3)    c = (-4, 3)

|a| = √(9 + 16) = 5        |b| = 5        |c| = 5
a · b = 3×4 + 4×3 = 24     cosine(a, b) = 24 / (5 × 5) = 0.96   nearly the same way
a · c = 3×(-4) + 4×3 = 0   cosine(a, c) = 0 / 25       = 0      at right angles
```

Dividing by the lengths means only the **direction** counts, not how long the
arrow is. `(3, 4)` and `(30, 40)` have cosine `1.0`.

A vector of length 1 is **normalised**. For normalised vectors `|a| × |b| = 1`,
so cosine is just the dot product. That's why §7 says cosine, dot product and
Euclidean distance all rank results the same way for normalised vectors.

### 4. The two hard rules

From §7, and they matter from step 3 on:

1. **Use the same embedding model** for the documents and for the question.
   Each model has its own space. A 768-number vector from `nomic-embed-text`
   can't be compared with a 384-number vector from `all-minilm`, and even two
   models with the same length don't line up.
2. **Changing the model means re-embedding everything.** That's why the model
   name is a setting (`RAG_EMBED_MODEL`), not something you change casually.

---

## What the code does

Three files. `go.mod` makes this a Go module, `main.go` holds the `rag` command,
and `internal/embed` holds the logic.

- `internal/embed/embed.go:29` builds the URL `$OLLAMA_HOST/api/embed`. The
  request body is `{"model": "...", "input": ["a", "b", "c"]}`: all texts go in
  one request, and the `embeddings` array in the response comes back in the same
  order.
- `internal/embed/cosine.go:17` is `Cosine`: the dot product divided by the two
  norms, exactly the formula above. It refuses vectors of different lengths
  (rule 1) and zero-length vectors (division by zero).
- `main.go:45` reads `OLLAMA_HOST` and `RAG_EMBED_MODEL`, with the defaults from
  `CLAUDE.md`.
- `main.go:56` prints each vector's norm and its first 4 numbers. `main.go:67`
  fills the cosine matrix: every text against every other text.

There's no `--debug` flag yet to dump the raw JSON. Step 4a adds one for chat.
For now, **Run it** step 2 shows the same request with `curl`.

---

## Run it

On the VM (`ssh ubuntu@10.2.1.49`). Ollama and `nomic-embed-text` are already
there from step 0.

### 1. Get the binary onto the VM

Either build on the VM (needs Go there, and the step pushed to GitHub):

```bash
sudo snap install go --classic        # once; any Go >= 1.24
git clone https://github.com/ArnobKumarSaha/rag ~/rag   # or: cd ~/rag && git pull
cd ~/rag && make build                # writes bin/rag
```

Or cross-compile on the Mac and copy it over:

```bash
GOOS=linux GOARCH=amd64 go build -o bin/rag-linux . && scp bin/rag-linux ubuntu@10.2.1.49:~/rag/bin/rag
```

### 2. Look at the wire format with curl

This is the request `rag` sends:

```bash
curl -s $OLLAMA_HOST/api/embed \
  -d '{"model":"nomic-embed-text","input":["replication lag is high","disk is full"]}' \
  | jq '{model, count: (.embeddings|length), dims: (.embeddings[0]|length),
         first4: (.embeddings[0][:4]), prompt_eval_count}'
```

Illustrative output:

```json
{ "model": "nomic-embed-text", "count": 2, "dims": 768,
  "first4": [0.021, -0.118, 0.334, 0.007], "prompt_eval_count": 12 }
```

### 3. Run rag embed

```bash
cd ~/rag
./bin/rag embed "replication lag is high" "the standby is falling behind" "disk is full"
```

Illustrative output, **not measured**:

```
model nomic-embed-text, 3 vectors, 768 dims, 18 input tokens

[0] norm=1.000 first=[0.021 -0.118 0.334 0.007] "replication lag is high"
[1] norm=1.000 first=[0.035 -0.090 0.291 -0.012] "the standby is falling behind"
[2] norm=1.000 first=[-0.044 0.061 0.180 0.052] "disk is full"

cosine     [0]     [1]     [2]
[0]      1.000   0.720   0.480
[1]      0.720   1.000   0.450
[2]      0.480   0.450   1.000
```

Try a few KubeDB-flavoured sets too:

```bash
./bin/rag embed "how do I back up a MongoDB database" "configure KubeStash for MongoDB" "scale Postgres replicas"
./bin/rag embed "pod is OOMKilled" "container ran out of memory" "the certificate expired"
```

---

## Observe

1. **`dims` is 768 for every text**, short or long. The input length changes
   `input tokens`, never the vector length.
2. **The diagonal is `1.000`**: every text points exactly the same way as
   itself. The matrix is symmetric, because cosine(a, b) = cosine(b, a).
3. **Meaning beats shared words.** `[0]`–`[1]` ("replication lag" vs "standby
   falling behind") share no words but should score clearly higher than either
   does with "disk is full".
4. **Unrelated is not `0`.** Real embedding models rarely give `0` for
   unrelated texts. Scores tend to sit in a narrow band (often somewhere around
   0.3–0.8). The *ranking* is what carries the meaning, not the absolute value.
   Step 3 only ever asks "which chunks score highest", never "is it above X".
5. **`norm` should be `1.000`.** Ollama's `/api/embed` returns normalised
   vectors (I inferred this from Ollama's server code; your `norm` column
   confirms or refutes it). If it is 1, `Cosine` is doing more work than
   needed: the dot product alone would give the same numbers.
6. **The first 4 numbers tell you nothing.** Compare `first=` for `[0]` and
   `[1]`: they don't look alike, even when the cosine is high. Meaning lives in
   all 768 together.

---

## Break it

**Flip the meaning, keep the words.**

```bash
./bin/rag embed "replication lag is high" "the standby is falling behind" "replication lag is not high"
```

What should happen: `[0]`–`[2]` scores high, likely higher than the true
paraphrase `[0]`–`[1]`, even though `[2]` says the opposite. Embedding models
pack a whole sentence into one direction, and "not" barely moves it. The words
"replication lag high" dominate.

The lesson for RAG: vector search finds text that is **about** the same thing.
It doesn't check whether that text **agrees** with the question. Retrieval
brings back the chunks, and the chat model (step 4b) still has to read them
properly.

---

## My results

Ollama version:

`dims` from curl and from `rag embed`:

`norm` values (are they 1.000?):

Cosine matrix for "replication lag is high" / "the standby is falling behind" / "disk is full":

Range of "unrelated" scores I saw:

Break it: `[0]`–`[1]` (paraphrase) = ____, `[0]`–`[2]` (negation) = ____

Notes:
