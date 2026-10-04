# Plan — rag (learn RAG + tool use + MCP on KubeDB, step by step)

Repo: `~/go/src/github.com/ArnobKumarSaha/rag`, module `github.com/ArnobKumarSaha/rag`.
One step = one commit = one concept. Each step builds on the previous; nothing is used before it's built.
Each commit also adds `steps/NN-<name>.md`: what the concept is, how to run it on the VM, what to observe, one "break it on purpose" experiment.

Runtime (VM): Ollama (`qwen2.5:7b-instruct` chat, `nomic-embed-text` embeddings), a clone of `github.com/kubedb/docs`, kubeconfig of your existing KubeDB cluster.

| # | Concept (notes §) | What you build | New deps | Size |
|---|---|---|---|---|
| 1 | Embeddings & cosine similarity (§7) | `rag embed "a" "b" "c"` → calls Ollama `/api/embed`, prints vector dims + pairwise cosine matrix. Includes VM setup in `steps/01`. | none | ~80 lines |
| 2 | Chunking (§8) | `rag chunk <docs-dir>` → walks kubedb docs `*.md`, strips Hugo front-matter, splits on H2/H3, prints chunk count + size histogram (~token estimate). Experiment: fixed 100-token chunks vs heading chunks. | none | ~120 |
| 3 | Indexing & vector search (§9) | `rag index` → embeds every chunk, saves `index.json`; `rag search "q"` → brute-force cosine, top-5 with `path#heading`. Shows what a vector DB does before using one. | none | ~100 |
| 4 | RAG answer (§4, §5, §12) | Hand-written OpenAI-compatible chat client (see raw JSON); `rag ask "q"` → retrieve top-5, build system prompt with chunks, answer with citations. Experiment: temperature, no-context vs with-context. | none | ~120 |
| 5 | Tool calling + agent loop (§13, §14) | One tool `kubectl_get` (exec kubectl, allow-listed resources incl. KubeDB CRDs, read-only, no shell, output truncated). `rag agent "is anything unhealthy in ns X?"` → loop until final answer, max 8 steps, prints each tool call. | none | ~150 |
| 6 | MCP (§15) | Move tools into `rag mcp` (stdio MCP server): `kubectl_get`, `kubectl_describe`, `kubectl_events`, `search_kubedb_docs`. Agent becomes MCP client. Also usable from Claude Code: `claude mcp add rag -- rag mcp` → compare big model vs local 7B. | `github.com/modelcontextprotocol/go-sdk` | ~150 |
| 7 | Evaluation (§17) | `scenarios/*.yaml`: 4 broken KubeDB manifests (bad version, missing StorageClass, OOM memory limit, OpsRequest on missing DB) + 1 healthy control, each with expected diagnosis. `evals/retrieval.json` ~20 questions → `rag eval` prints hit@5. | none | ~100 + yamls |

Single binary `rag` with subcommands (stdlib `flag`, no cobra) — keeps each step a small, readable diff.

Later (not in these 7): pgvector instead of `index.json`, BM25 hybrid + re-ranker (§22 step 5).

## Git
New repo, `git init` on `master` with an empty initial commit, then branch `arnob-rag-steps`; one `-s` commit per step. I stop after each step so you read/run it before I write the next. No push unless you ask.

## Verification per step
`go build ./...` + `go vet ./...` on the Mac. Running against Ollama/cluster is yours on the VM.
