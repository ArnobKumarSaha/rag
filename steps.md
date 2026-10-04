# Plan — rag (learn RAG + tool use + MCP on KubeDB, step by step)

Repo: `~/go/src/github.com/ArnobKumarSaha/rag`, module `github.com/ArnobKumarSaha/rag`.
One step = one commit = one concept. Each step builds on the previous; nothing is used before it's built.
Each step adds `steps/NN-<name>.md`: concept, how to run it on the VM, what to observe, one "break it on purpose" experiment, and an empty **My results** section filled in on the VM.

Runtime (VM): Ollama (`qwen2.5:7b-instruct` chat, `nomic-embed-text` embeddings), a clone of `github.com/kubedb/docs`, kubeconfig of the existing KubeDB cluster.

| # | Concept (notes §) | What you build | New deps | Size |
|---|---|---|---|---|
| 0 | Local model serving, baseline speed (§19, §20) | No Go. VM setup (Ollama, models, docs clone, kubeconfig); `curl` the OpenAI-compatible and native endpoints by hand; measure tokens/sec (prompt eval vs decode) for 3B / 7B / 14B on the same prompt. Pick the chat model from measured numbers. Adds `Makefile` (`build`, `fmt`). | none | md + Makefile |
| 1 | Embeddings & cosine similarity (§7) | `rag embed "a" "b" "c"` → calls Ollama `/api/embed`, prints vector dims + pairwise cosine matrix. | none | ~80 lines |
| 2 | Chunking (§8) | `rag chunk --docs <dir>` → walks kubedb docs `*.md`, strips Hugo front-matter, splits on H2/H3, prints chunk count + size histogram (~token estimate). Experiment: fixed 100-token chunks vs heading chunks. | none | ~120 |
| 3 | Indexing, vector search & a first eval (§9, §17) | `rag index` → embeds every chunk, saves `index.json`; `rag search "q"` → brute-force cosine, top-5 with `path#heading`. `evals/retrieval.json`: 10 questions → expected chunk; `rag eval` prints hit@5. Every later retrieval change gets a number. | none | ~150 |
| 4a | Talking to the model (§4, §5) | Hand-written OpenAI-compatible chat client (print raw request/response JSON with `--debug`); `rag chat "q"` with system prompt, temperature, streaming, and `usage` token counts. | none | ~100 |
| 4b | RAG answer (§12) | `rag ask "q"` → retrieve top-5, build system prompt with chunks, answer with `path#heading` citations. Experiment: no-context vs with-context, top-k 2 vs 8. | none | ~80 |
| 5 | Tool calling + agent loop (§13, §14) | One tool `kubectl_get` (exec kubectl, allow-listed resources incl. KubeDB CRDs, read-only, no shell, output truncated). `rag agent "is anything unhealthy in ns X?"` → loop until final answer, max 8 steps, prints each tool call. | none | ~150 |
| 6 | MCP (§15) | Move tools into `rag mcp` (stdio MCP server): `kubectl_get`, `kubectl_describe`, `kubectl_events`, `search_kubedb_docs`. Agent becomes MCP client. Also usable from Claude Code: `claude mcp add rag -- rag mcp` → compare big model vs local 7B. | `github.com/modelcontextprotocol/go-sdk` | ~150 |
| 7 | Agent evaluation on a live cluster (§17) | `scenarios/*.yaml` in ns `agent-lab`: 4 broken KubeDB manifests (bad version, missing StorageClass, OOM memory limit, OpsRequest on missing DB) + 1 healthy control, each with expected diagnosis. Grow `evals/retrieval.json` to ~20 questions. | none | ~100 + yamls |
| 8 | Real vector DB (§9) | Replace `index.json` with Postgres + pgvector running as a KubeDB Postgres; HNSW index; compare latency and hit@5 against brute force. | `github.com/jackc/pgx/v5`, `github.com/pgvector/pgvector-go` | ~120 |

Single binary `rag` with subcommands (stdlib `flag`, no cobra) — keeps each step a small, readable diff.

Later: BM25 hybrid + re-ranker (§22 step 5), measured with the step-3 eval.

## Learning mode
Per step, choose: Claude writes all of it, or "I'll write X myself" (e.g. cosine in 1, chunker in 2, the agent loop in 5) — X is left as a stub that fails clearly, with its contract in the step md.

## Git
Commit directly on `master`, one `-s` commit per step, tagged `step-NN`. `git diff step-02 step-03` shows exactly one concept.

## Verification per step
`go build ./...` + `go vet ./...` + `make fmt` on the Mac. Running against Ollama/cluster happens on the VM.
