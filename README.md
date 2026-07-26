# kirobuff

**A buff for Kiro CLI.**

Your agent already has the stats. This is the status effect.

---

## You know the moment

The agent says *done*. Tests green. You ship it.

Three days later a customer reports something odd, and you find it: a default
value quietly changed. Not a bug the agent introduced — a decision it made, on
its own, in a diff you skimmed.

Nobody wrote a test for it. Why would they? It used to work.

That is not a model problem. It's a **permission** problem. Your agent was
allowed to do that.

## Now it isn't

```bash
kirobuff install
```

That's it. There is no step two.

From that moment your agent classifies every change before it touches a file.
Add a function, write a test, extend a flag — it goes, no questions. Change a
default, alter a signature, delete something? It stops and asks.

And for four things, it doesn't get to decide at all:

```
git commit -s -m "fix"              blocked   agents cannot sign the DCO
rm internal/parser_test.go          blocked   a failing test is information
edit removing 1 of 2 assertions     blocked   that is the bug, staying hidden
git reset --hard HEAD~3             blocked   irreversible, ask a human
```

Not a warning. Not a note in the summary. **Blocked** — the tool call never runs,
and the model is told why.

Every other coding guardrail is a suggestion in a markdown file. This one is a
wall.

---

## Buffs, not features

Kiro CLI is your character. kirobuff is what you cast on it.

**Passive** — always on, no thought required
`guardrails` change safety in every session, forever · `enforce` five rules that
block rather than advise

**Active** — toggle when you want them
`tech-cofounder` argues with the premise, not just the code · `ctrl+shift+t` on,
same keys off

**Stats** — tuned instead of maxed
`tune` Opus ships at `xhigh` effort and deliberates over renames. Drop the floor,
raise it per task. Faster answers, fewer credits, same result.

**HUD** — see the state you're in
`statusline` mode and live context cost, in your tab title

**Utility** — for when it matters
`budget` finds the tokens you burn every single turn · `loop` runs experiments
without you in the middle · `attest` emits Linux-kernel-compliant AI attribution

---

## Why you'll leave it on

Because you stop reading diffs defensively.

The trust problem with agents was never capability. It was not knowing which
changes were safe to wave through. kirobuff answers that mechanically: the risky
ones came to you, the safe ones already shipped, and the four that are never okay
never happened.

You go faster because you're checking less.

---

## Install

```bash
git clone <your-repo> kirobuff && cd kirobuff
make install          # builds and puts kirobuff on your PATH
kirobuff install      # configures Kiro CLI
```

`kirobuff install` is idempotent and non-destructive. It never overwrites a file
you wrote yourself; it says so and moves on.

What it touches:

```
~/.kiro/steering/00-kirobuff-guardrails.md   always-on change safety
~/.kiro/agents/tech-cofounder.json           off until you switch to it
~/.kiro/settings/cli.json                    effort default for Opus
```

Respects `KIRO_HOME`. Nothing else on your system is modified.

To skip the effort change: `kirobuff install -no-tune`

---

## Capabilities

| Command | What it does | Scope |
|---|---|---|
| `install` | everything below, in one shot | global |
| `guardrails install` | change-safety policy, inherited by every agent | global |
| `enforce install` | five rules that block the tool call | per agent |
| `tune` | per-model reasoning effort defaults | global |
| `mode install` | opt-in personas you switch into | global |
| `budget` | measure recurring per-turn token cost | per agent |
| `guard install` | warn at session start when over budget | per agent |
| `statusline install` | mode and cost in the tab title | per agent |
| `loop init` | verifier, score ledger, stop condition | per project |
| `attest` | `Assisted-by` trailers and DCO validation | per commit |
| `status` | what is installed where | — |

### `install` — everything at once

```bash
kirobuff install [-effort medium] [-no-tune]
```

### `tune` — stop Opus overthinking

```bash
kirobuff tune                                  # Opus -> medium
kirobuff tune -model openai-gpt-5.4 -effort high
kirobuff tune -show                            # what's configured now
```

Kiro CLI's built-in default for the Opus family is `xhigh`. On mechanical work
that buys nothing and costs both latency and credits. This sets a floor, not a
ceiling — raise it for one session with `/effort high` when a task earns it.

The JSON path is model-specific and unforgiving: Claude reads
`output_config.effort`, GPT reads `reasoning.effort`, and a value at the wrong
path is silently ignored at bootstrap. kirobuff writes the right one and
**refuses** to guess for models with no documented effort field, rather than
writing a setting that quietly does nothing.

### `guardrails` — always on, every agent

```bash
kirobuff guardrails install [-scope global|workspace]
```

Steering files are inherited by every agent by default, so this applies to
agents you create tomorrow. Nothing opts in.

The policy is a decision procedure, not a sentiment. Before editing, the agent
classifies the change:

| Category | Action |
|---|---|
| **Additive** — new file, function, test, or a flag defaulting to current behaviour | proceed, don't ask |
| **Behaviour-preserving** — refactor covered by passing tests | proceed, run tests |
| **Behaviour-changing** — signature, default, return shape, schema, output contract | **stop and ask** |
| **Subtractive** — deleting or narrowing anything, including "dead" code | **stop and ask** |

Riskiest category wins when a change spans several.

Hard prohibitions: never delete or skip a test to make a suite pass, never
weaken an assertion, never silence a compiler error without understanding it,
never leave the tree unbuildable, never claim something works without running
it.

And the part people forget — **asking about safe work is its own failure.** If
the change is additive and the tests pass, it keeps going. No check-ins, no
plan confirmations, no permission for work that risks nothing.

### `mode` — the tech co-founder, on demand

```bash
kirobuff mode list
kirobuff mode install tech-cofounder
```

Then `/agent tech-cofounder`, or `ctrl+shift+t` to toggle.

It argues with the premise, not just the implementation: what problem, for
whom, should this exist at all, is this a one-way door, what does it really
cost. It treats "don't build this" as a real answer and names the tradeoff
instead of presenting a choice as free.

Off until you switch to it. On switch, its `welcomeMessage` prints so you can
see it's on, and `/agent` marks it with an arrow. `model` is deliberately
omitted so switching mode never silently switches models.

### `budget` — measure what context actually costs

```bash
kirobuff budget .kiro/agents/mine.json
```

```
[high  ]  7,522 tok/turn  file://internal/**/*.go
           4 files loaded into context on every turn
           fix: narrow the glob, or move the reference material into a skill

[low   ]    233 tok/turn  file://.kiro/skills/**/SKILL.md
           fix: switch to skill:// so only name+description load until needed

~10,315 tokens per turn recoverable
over a 50-turn session that is ~515,750 tokens
```

The distinction that matters is per-turn versus on-demand. A `file://` resource
is re-sent every request for the whole session; a `skill://` resource
contributes only name, description, and path until the model asks. Tool schemas
are per-turn too. Anything per-turn multiplies by conversation length.

| Rule | Cost model |
|---|---|
| `always-loaded` | `file://` bytes ÷ 4, every turn |
| `uncached-hook` | `userPromptSubmit` output at `max_output_size`, uncached |
| `wide-tool-surface` | `tools: ["*"]` ships every schema every request |
| `dead-resource` | matches nothing; free but misleading |

`agentSpawn` hooks are exempt — they run once and Kiro CLI never caches them,
so charging per turn would be wrong.

### `guard` — make the budget check automatic

```bash
kirobuff guard install .kiro/agents/mine.json -max 2000
```

Adds an `agentSpawn` hook that re-runs the check every session. The exit-code
contract is what makes it free: Kiro CLI sends a hook's stdout to the **model**
on exit 0, and stderr to the **user** on any other code. Quiet mode keeps stdout
empty unconditionally, so the warning reaches you and never reaches the model.

| Condition | stdout | stderr | exit |
|---|---|---|---|
| over budget | empty | one-line warning | 1 |
| under budget | empty | empty | 0 |

### `statusline` — mode and cost in your tab title

```bash
kirobuff statusline install .kiro/agents/mine.json -max 2000
```

```
kirobuff · myproject · tech-cofounder · 3.0k/2.0k! tok
```

Kiro CLI has **no** `statusLine` setting. The terminal window title is the only
surface available, so kirobuff writes an OSC 0 sequence straight to `/dev/tty`,
bypassing the hook's captured stdout. Nothing goes to stdout, so it costs
nothing per turn.

**Credits are not shown.** Account usage is reachable only through the
in-session `/usage` command — `kiro-cli user` has no usage subcommand. A number
here would be invented or stale, so there isn't one.

### `enforce` — guardrails with teeth

```bash
kirobuff enforce install .kiro/agents/mine.json
```

The steering file states a policy. A policy in a prompt is a hope. This is a
wall: `preToolUse` exit **2** blocks the tool call and returns the reason to the
model, so it learns why and chooses differently.

| Rule | Blocks |
|---|---|
| `no-agent-signoff` | `git commit -s`, or a `Signed-off-by` written by hand |
| `no-test-deletion` | `rm` on a test file or test directory |
| `no-assertion-weakening` | an edit that reduces a test's assertion count |
| `protect-verifier` | writes to `.kiro/loop/verify.sh`, `program.md`, `best` |
| `no-destructive-git` | `reset --hard`, `push --force`, `clean -fd`, `branch -D` |

Verified behaviour:

```
git commit -s -m fix              exit=2  [no-agent-signoff]
rm internal/x_test.go             exit=2  [no-test-deletion]
strReplace dropping 1 of 2 asserts exit=2 [no-assertion-weakening]
git commit -m fix                 exit=0
malformed payload                 exit=0   (fails open)
```

Unknown tools and unparseable input fail open deliberately: a hook that blocks a
shape it doesn't recognise would break every session after a schema change.

Only rules decidable from the tool input live here. "Prefer the smallest change"
is judgment and stays in steering.

**Structural limit:** hooks can only live in agent config *files*, and built-in
agents like `kiro_default` are not files. Enforcement therefore requires a
custom agent. Every agent kirobuff generates ships with it already wired.

### `attest` — kernel-compliant AI attribution

```bash
kirobuff attest -model claude-opus-4.7 -agent kiro-cli/loop -tools "gofmt,sparse"
# Assisted-by: Claude:claude-opus-4.7 kiro-cli/loop gofmt sparse

kirobuff attest -model claude-opus-4.7 -agent kiro-cli -f .git/COMMIT_EDITMSG -w
kirobuff attest -check -f .git/COMMIT_EDITMSG
```

The Linux kernel codified a formal AI policy in April 2026:

1. AI agents cannot add `Signed-off-by` — only a human certifies the DCO
2. Contributions using AI must carry an `Assisted-by` trailer naming the model,
   agent, and auxiliary tools
3. The human submitter bears full responsibility

This is the only part of kirobuff answering to an **external mandatory
standard** rather than to taste. Trailers are appended correctly into git's
final-paragraph trailer block, idempotently, and an unmapped model is **refused**
rather than given a guessed vendor label — a wrong vendor in a legally
significant trailer is worse than no trailer.

`-check` requires attribution but does **not** flag a human's `Signed-off-by`,
which the DCO requires. Add `-as-agent` to reject sign-offs, for use when the
check runs in an agent's own context.

**Format caveat:** one example trailer has been published, so the
`Vendor:model tool tool` shape is followed exactly. Anything beyond that shape
would be inference.

### `loop init` — remove yourself from the inner cycle

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

### `status` — what's installed where

```bash
kirobuff status
```

Scans both Claude Code and Kiro CLI across user and workspace scope, and reports
which artifacts are already shared via symlink.

---

## Develop

```bash
make build      # ./bin/kirobuff
make test       # go test ./...
make check      # gofmt -l, go vet, go test
make install    # build + copy to ~/.local/bin
make clean
```

Layout:

```
cmd/kirobuff/          CLI: flag parsing, output, nothing else
internal/budget/       recurring token-cost analysis
internal/discover/      locate config artifacts in both harnesses
internal/enforce/       preToolUse rules that block, not advise
internal/guard/        hook installation into agent configs
internal/loop/         Karpathy loop scaffolding
internal/persona/      switchable agent modes
internal/statusline/   OSC 0 terminal-title indicator
internal/steering/     always-on guardrails
internal/tune/         per-model effort defaults
```

Conventions:

- **Read-only until asked.** Nothing writes without an explicit subcommand.
- **Never clobber.** Anything kirobuff didn't write is left alone unless
  `-force`. Managed files carry a marker.
- **Preserve unknown fields.** Configs are patched through
  `map[string]json.RawMessage`, so fields kirobuff doesn't model survive.
- **Refuse rather than guess.** An unknown model family gets an error, not a
  plausible JSON path that silently does nothing.
- **Zero dependencies.** Standard library only. No `go.sum`.

Set the module path in `go.mod` before publishing — it is currently the bare
name `kirobuff`.

---

## Known limits

Honest list. These are properties of the tools, not bugs to file.

**No real status line.** Kiro CLI has no `statusLine` setting. The tab title is
all there is.

**Status line is UNVERIFIED.** Whether a Kiro CLI hook subprocess inherits a
controlling terminal has not been confirmed — the shell used to develop this had
none. The escape sequence and the graceful fallback are tested; the `/dev/tty`
write is not. Start a session and check your tab title.

**No credit balance anywhere.** In-session `/usage` only.

**macOS only, in practice.** `filepath` is used throughout so paths should
hold, but nothing has been run on Linux or Windows, where symlink semantics
differ.

**Hardcoded paths.** `KIRO_HOME` is honoured. `~/.claude` and `~/.agents` are
not configurable.

**bytes/4 is an estimate.** It matches Kiro CLI's own `/context`
approximation. Use it to rank fixes, not to predict a bill.

**Array-format hooks get no budget findings.** That format carries no
`cache_ttl_seconds`, so it's skipped rather than guessed at.

**Top-level JSON keys get sorted** when a config is patched, because Go marshals
map keys in order. Values are untouched; your first diff will show the
reshuffle.

**Unverified:** whether a Kiro agent's `prompt` field replaces or appends to the
built-in system prompt. It determines how much base behaviour persists under a
persona. Test before relying on it.

---

## Not built yet

- `plan` / `sync` — bidirectional config sync with Claude Code
- Bilevel loops: an outer loop that improves how the inner loop searches
- `statusline uninstall`
- Additional personas beyond `tech-cofounder`

---

*Guardrails that never sleep. Thinking that knows when to stop. One command.*
