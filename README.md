# kirobuff

![A Go gopher in a wizard hat casting a spell on the Kiro ghost, which glows
with DEFENSE UP, SPEED UP, CODE UP and FOCUS UP
buffs](assets/kirobuff-hero.png)

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
go install github.com/AlleyBo55/KiroBuff/cmd/kirobuff@latest   # get the tool
kirobuff install                                               # configure Kiro CLI
```

Two commands, once. Nothing to run again.

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

**Active** — stack up to six
`focus` `paranoid` `perf` `debug` `tech-cofounder` `ship-it` `terse` `teacher` — they
compose, because they're steering fragments rather than agents. Six is the cap
and it isn't arbitrary: every active one costs context on every turn.

**Party** — one buff each, working in parallel
Give the reviewer `paranoid` and the optimiser `perf`. Separate agents, separate
worktrees, same project. Each pays only for the lens it carries.

**Spank** — keep going with the lid shut
Close the laptop, walk away, come back to finished work. `caffeinate` can't do
this. `kirobuff mode explain spank` tells you exactly what can, on macOS, Linux,
or Windows — and refuses to run it for you, because that one's your call.

**Stats** — tuned instead of maxed
`tune` Opus ships at `xhigh` effort and deliberates over renames. Drop the floor,
raise it per task. Faster answers, fewer credits, same result.

**No Conflict Rules** — never learn about a conflict from a PR page
```bash
kirobuff preflight install
```
Every push checks your branch against its base first. Behind? It says so.
Would conflict? It names the files. Carrying commits the base already
squash-merged? It spots that too, which is the one nobody diagnoses. Each line
ends in the command that fixes it.

**HUD** — see the state you're in
`statusline` active modes and live context cost, in your tab title

**Utility** — for when it matters
`budget` finds the tokens you burn every single turn · `loop` runs scored
experiments without you in the middle · `attest` emits Linux-kernel-compliant AI
attribution

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

### Step 1 — get the binary

Pick one. All three verified working against `v0.1.1`.

**Go**
```bash
go install github.com/AlleyBo55/KiroBuff/cmd/kirobuff@latest
```

**Script** — no Go toolchain needed
```bash
curl -fsSL https://raw.githubusercontent.com/AlleyBo55/KiroBuff/master/install.sh | sh
```
Verifies the release checksum, installs to `~/.local/bin`, and warns if that is
not on your `PATH`. Override with `PREFIX=`, pin with `KIROBUFF_VERSION=`.

**Source**
```bash
git clone https://github.com/AlleyBo55/KiroBuff && cd KiroBuff
make install
```

Homebrew is **not** available. That is a gap rather than a plan: it needs a tap
repository and a token that do not exist yet. See [known limits](docs/limits.md).

`kirobuff`'s hooks re-invoke it **by name**, so it must be on `PATH` when
`kiro-cli` starts. Every install path warns you if it is not.

### Step 2 — configure Kiro CLI

```bash
kirobuff install
```

Idempotent and non-destructive. It never overwrites a file you wrote yourself;
it says so and moves on. What it touches:

```
~/.kiro/steering/00-kirobuff-guardrails.md   always-on change safety
~/.kiro/agents/tech-cofounder.json           off until you switch to it
~/.kiro/settings/cli.json                    effort default for Opus
```

Respects `KIRO_HOME`. Nothing else on your system is modified.

To skip the effort change: `kirobuff install -no-tune`

Optional, once per repository — catch merge conflicts before you push:

```bash
kirobuff preflight install
```

---

## Capabilities

Nine buffs. One command installs the ones that should always be on.

| | What it does | More |
|---|---|---|
| `install` | guardrails, personas and effort defaults, in one shot | — |
| `focus` | **stop it overthinking** — no adjacent problems, no detours | [modes](docs/modes.md#focus--stop-it-overthinking) |
| `guardrails` | classify every change before touching a file | [safety](docs/safety.md) |
| `enforce` | five rules that **block** the tool call, not warn | [safety](docs/safety.md#enforce--guardrails-with-teeth) |
| `mode` | stack up to six lenses, globally or per agent | [modes](docs/modes.md) |
| `spank` | keep working with the laptop lid shut | [spank](docs/spank.md) |
| `tune` | reasoning volume: stop deliberating over renames | [cost](docs/cost.md#tune--reasoning-volume-not-reasoning-aim) |
| `budget` | find the tokens you burn on *every single turn* | [cost](docs/cost.md) |
| `guard` | warn automatically when an agent gets expensive | [cost](docs/cost.md#guard--make-the-budget-check-automatic) |
| `statusline` | active modes and live cost, in your tab title | [statusline](docs/statusline.md) |
| `loop` | scored experiments that run without you in the middle | [loops](docs/loop.md) |
| `attest` | Linux-kernel-compliant AI attribution on commits | [safety](docs/safety.md#attest--kernel-compliant-ai-attribution) |
| `preflight` | **no conflict rules** — catch them before you push | [conflicts](docs/conflicts.md) |
| `version next` | the next release, derived from what changed | [develop](docs/develop.md#versioning-and-releases) |

## The two that answer "it costs too much"

They're different problems, and mixing them up wastes money.

**It thinks about the wrong thing.** Reads six files to change one. Answers the
question next to yours. Fixes something unrelated on the way past. Every detour
is tokens and latency you didn't ask for.

```bash
kirobuff mode on focus
```

**It thinks too long.** Opus ships at `xhigh` effort and will deliberate over a
rename.

```bash
kirobuff tune
```

Lower effort makes it think *less*; `focus` makes it think *nearer the point*.
You usually want both — and before either, `kirobuff budget` will tell you how
much irrelevant context it's reasoning over in the first place. That's the
biggest lever and nobody looks at it.

→ [Cost and speed](docs/cost.md)

## Use it as a library

Three packages are exported. The rest is under `internal/` because it is
specific to laying out Kiro CLI config on disk.

```go
import "github.com/AlleyBo55/KiroBuff/enforce"

// Add your own rule to the built-in set.
d := enforce.Evaluate(event, append(enforce.DefaultRules(), noVendorEdits{})...)
if d.Blocked {
    fmt.Fprintln(os.Stderr, d.Reason)
    os.Exit(2)
}
```

| Package | What it does |
|---|---|
| [`enforce`](https://pkg.go.dev/github.com/AlleyBo55/KiroBuff/enforce) | evaluate agent tool calls against pluggable change-safety rules |
| [`attest`](https://pkg.go.dev/github.com/AlleyBo55/KiroBuff/attest) | `Assisted-by` commit trailers and DCO validation, per the Linux kernel AI policy |
| [`semver`](https://pkg.go.dev/github.com/AlleyBo55/KiroBuff/semver) | classify Conventional Commits into major, minor and patch |

## Read more

| | |
|---|---|
| [Modes](docs/modes.md) | all nine lenses, composing them, per-agent scoping, parallel agents |
| [Safety](docs/safety.md) | guardrails, the enforcement hook, commit attribution |
| [Cost and speed](docs/cost.md) | token budget, effort tuning, where the money goes |
| [Loops](docs/loop.md) | verifier, scored ledger, stop condition |
| [Spank mode](docs/spank.md) | lid-closed work on macOS, Linux and Windows |
| [Status line](docs/statusline.md) | the tab-title HUD |
| [Develop](docs/develop.md) | layout, conventions, versioning and releases |
| [Known limits](docs/limits.md) | what doesn't work, stated plainly |
| [No conflict rules](docs/conflicts.md) | pre-push checks, and the squash-merge trap |
| [Contributing](CONTRIBUTING.md) | code and test guidelines, adding modes and rules |
| [Discoverability](docs/discoverability.md) | repo metadata still to set, and why pkg.go.dev was empty |

---

## Use it, fork it, ship it

Use it. Fork it. Ship it. No attribution ritual, no CLA, no permission to ask.

And feel free to contribute more buffs — that's the cheapest thing in here to
add. A mode is a steering fragment plus a catalogue entry, not a new agent and
not new plumbing. If you keep re-explaining the same lens to your agent every
session, `security-review`, `sre`, `frontend`, `migration`, then that lens is
already a buff. It just isn't in the catalogue yet.

Also welcome:

- tuning defaults as new models land, since the effort path is per-family
- enforcement rules decidable from tool input alone — anything needing judgment
  belongs in steering instead
- the platform gaps under Known limits: Linux and Windows need someone who
  actually runs them

Fork it, add the buff, open a PR. Buffs stack; so should the catalogue.

## What this is for

Search terms that led you here, or should have:

AI coding agent guardrails · agentic coding safety · stop an AI agent breaking
existing features · prevent LLM regressions · agent tool-call permissions ·
Kiro CLI configuration · Kiro CLI modes and hooks · `preToolUse` hook ·
reduce Claude Opus token usage · LLM context window cost · reasoning effort
tuning · Conventional Commits automatic versioning · `Assisted-by` trailer ·
Linux kernel AI contribution policy · Developer Certificate of Origin and AI ·
keep an AI agent running with the laptop lid closed · Karpathy loop ·
verifier-driven agent loop · agent persona and mode composition

Also written as **kiro buff**, **KiroBuff**, **kiro-buff**.

## License

[MIT](LICENSE).

---

*Guardrails that never sleep. Thinking that knows when to stop. One command.*
