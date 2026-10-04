# rag — a learning repo, built one step at a time

The goal is learning, not shipping. The user is a DevOps/platform engineer (Go, Kubernetes, KubeDB) who is new to LLM engineering. They build RAG, tool use and MCP against the KubeDB docs and a live KubeDB cluster, one small concept per step. Every change has to teach something they can run and observe.

## Sources of truth
- `steps.md` is the roadmap: the step table, what each step builds, and its notes § reference.
- The user's study notes are in `~/yamls/learn/ai/llm-basics-1.md` and `llm-basics-2.md`. The `§N` numbers in `steps.md` refer to sections in them. Read the relevant section before writing a step, and use its terms and examples, so the code and notes don't contradict each other.
- Progress lives in git. A step is done when `steps/NN-*.md` exists and a `step NN: ...` commit is on `master`. Don't tag steps. The next step is the first row in `steps.md` (in table order) that has no `steps/NN-*.md`. `NN` is zero-padded, and split steps keep their letter: `00`, `01`, …, `04a`, `04b`.

## Doing a step (one per session turn, then stop)
1. Read the step's row in `steps.md`, the code from earlier steps, and the referenced notes §.
2. Write only that step. Don't add code, flags or abstractions a later step will need: no forward references. Keep it at or under the size in `steps.md`. If it would go over, say so and suggest splitting the step.
3. Write `steps/NN-<name>.md`, readable top to bottom by a beginner:
   - **Concept**: the why, with terms defined on first use and a concrete example.
   - **What the code does**: point to `file:line` for the 2–4 lines that matter.
   - **Run it**: exact commands for the VM, plus example output marked as illustrative.
   - **Observe**: what to look at in the output and what it means.
   - **Break it**: one experiment to run on purpose, and what should happen.
   - **My results**: left empty for the user to fill in on the VM.
4. Run `go build ./...`, `go vet ./...` and `make fmt`. Report any failure verbatim. Step 0 has no Go code; it creates the `Makefile`.
5. Commit directly on `master`. This repo overrides the global feature-branch rule. Make one commit, `git commit -s -m "step NN: <concept>"`. Ask before pushing.
6. Stop. Summarize what to read first and what to run. Don't start the next step.

If the user says "I'll write X myself", write the rest with `X` as a stub that fails clearly (for example returning `errors.New("implement me: cosine")`). In `steps/NN-*.md`, explain what X must do and how to check it.

## Code rules
- One binary, `rag`. `main.go` dispatches subcommands using the stdlib `flag` package (no cobra). Logic goes in `internal/<pkg>`.
- Stdlib only, except the deps listed in a step's "New deps" column in `steps.md` (step 6: MCP SDK; step 8: pgx and pgvector-go). Add those only after the user approves. Don't use an OpenAI or Ollama SDK. The HTTP and JSON are hand-written on purpose, so the user sees the wire format.
- Configuration comes from env and flags, never hardcoded:
  - `OLLAMA_HOST` (default `http://localhost:11434`)
  - `RAG_CHAT_MODEL` (default `qwen2.5:7b-instruct`)
  - `RAG_EMBED_MODEL` (default `nomic-embed-text`)
  - `--docs` for the docs path
  - kubectl uses the standard `KUBECONFIG`
- kubectl tools are read-only. Run `exec.Command` with no shell. Verbs and resources come from allow-lists. Secrets return names only. Output is truncated to a fixed byte cap.
- The user's global code style applies: no comments except the *why*, errors surfaced and never swallowed, idiomatic Go.

## Runtime (where the user runs it)
- The VM is `ssh ubuntu@10.2.1.49`, on Harvester: Ubuntu 24.04, 30 vCPU, 48 GB RAM, 150 GB disk, **no GPU**. Ollama runs CPU-only, so speeds are a few tokens/sec. Never claim timings that haven't been measured.
- The KubeDB docs are a clone of `github.com/kubedb/docs`.
- The cluster is an existing KubeDB cluster, reached via kubeconfig. Lab namespace: `agent-lab`.
- Claude can reach the VM over SSH, but the user runs the step commands there. Verify with build and vet; only run things on the VM when the user asks.
