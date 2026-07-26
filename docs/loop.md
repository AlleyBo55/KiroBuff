# Loops

A verifier the agent cannot grade itself against, a scored ledger, and a stop
condition.

## `loop init` — remove yourself from the inner cycle

```bash
kirobuff loop init -goal "cut allocations in the hot path" \
                   -editable "internal/**" -max-attempts 5 \
                   -metric "go test -bench=. -benchmem | awk '/allocs/{print \$5}'" \
                   -direction lower
```

A Karpathy-style loop needs three things: a verifier the agent can't grade
itself against, a state ledger so the next iteration resumes instead of
restarts, and a stop condition.

| File | Role |
|---|---|
| `.kiro/loop/program.md` | goal, editable glob, protocol, scoring rule |
| `.kiro/loop/verify.sh` | correctness gate, then score comparison |
| `.kiro/loop/state.json` | attempt ledger, written by the agent |
| `.kiro/loop/best` | best score so far, written by the **verifier** |
| `.kiro/loop/score.log` | append-only history |
| `.kiro/agents/loop.json` | wires the hooks together |

**Without `-metric` this is a gate, not a search.** It rejects broken changes but
cannot tell an improvement from a no-op, and both `verify.sh` and `program.md`
say so. With a metric it becomes a search: correctness first, then keep only if
the number improved.

Verified against a real metric:

```
run 1  baseline established at 5             exit=0
run 2  improved 5 -> 3                       exit=0
run 3  no improvement (9 vs best 3)          exit=1
best=3   score.log has all three
```

The verifier owns `best`, and `best` plus `score.log` are in the agent's
`write.deniedPaths`. Whoever owns the score can win by rewriting it.

`verify.sh` is likewise unwritable, because an agent that can edit its own
verifier will eventually edit it to pass.

Toolchain detected from `go.mod`, `Cargo.toml`, `pyproject.toml`,
`package.json`, or `Makefile`. With none of those the verifier is a **failing
placeholder** by design.

Loops and thrift pull against each other: a loop buys quality by spending
tokens on search. They reconcile only when per-iteration cost drops enough that
N cheap iterations beat one expensive one. Hence the deterministic verifier (an
LLM judge doubles per-iteration cost *and* restores self-grading) and the small
structured ledger instead of a transcript.

Before building one, apply the four-point test: the task repeats weekly or
more, verification is automated, there's budget for wasted retries, and the
agent has real tools. Miss one and setup cost exceeds return.

---

[← Back to README](../README.md)
