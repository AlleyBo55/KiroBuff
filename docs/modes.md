# Modes

Nine lenses you switch on and off. Eight are prompt fragments that compose;
one changes your machine.

## `mode` — stack them, up to six

```bash
kirobuff mode list
kirobuff mode on paranoid              # every agent
kirobuff mode on perf                  # both now active
kirobuff mode status
```

Nine lenses. Eight are prompt fragments; one changes your machine.

| Mode | Lens |
|---|---|
| `tech-cofounder` | argue with the premise: cost, reversibility, build-at-all |
| `paranoid` | trust boundaries, injection, secrets, blast radius |
| `perf` | measure before changing, name the cost model |
| `focus` | stay on the question: no adjacent problems, no extra exploration |
| `debug` | reproduce, bisect, one hypothesis at a time |
| `ship-it` | smallest change that produces signal |
| `terse` | answers only, no preamble |
| `teacher` | show the reasoning, name the rejected alternative |
| `spank` | keep working with the lid closed *(system)* |

**Why fragments, not agents.** `/agent X` gives you exactly one agent, so a mode
built as an agent is mutually exclusive with every other mode. Steering
fragments all load together, so they compose. Turning a mode on symlinks its
fragment into `~/.kiro/steering/`; turning it off removes the link.

**Why the cap is six.** Every active fragment is re-sent on every turn. Six is
already a few thousand tokens before the conversation starts — the same cost
`budget` measures. It's a context budget, not a preference, and the error says so:

```
at most 6 modes can be active at once (active: paranoid, perf, ship-it,
teacher, tech-cofounder, terse). Turn one off first: each active mode is
re-sent on every turn, so the cap is a context budget rather than a preference
```

## Per-agent modes, and running agents in parallel

Global modes apply to every agent, which is the wrong default for specialists —
a security reviewer shouldn't pay context for the performance lens.

```bash
kirobuff mode on paranoid -agent .kiro/agents/secreview.json
kirobuff mode on perf     -agent .kiro/agents/optimiser.json
```

```
secreview  -> paranoid.md
optimiser  -> perf.md
```

Two agents, one project, each carrying only what it needs. Both pass
`kiro-cli agent validate`. This is cheaper than one agent wearing six hats and
it specialises the tool surface too — the reviewer gets `read`/`grep`, the
optimiser gets `code`/`shell`.

**Before you run them at the same time:** Kiro CLI has no file locking between
sessions. Two agents writing the same working tree will lose each other's edits
and race on the git index. Give each one its own tree:

```bash
git worktree add ../proj-sec  -b sec-review
git worktree add ../proj-perf -b perf-work
# then run one agent in each, and merge
```

Parallel agents in *one* tree is the failure mode. Parallel agents in separate
worktrees is the point.

## `focus` — stop it overthinking

This is the one people mean when they say the agent overthinks. It is a different
problem from `tune`, and the more expensive one in practice: the model doesn't
think *too much*, it thinks about the wrong thing. reads six files to
change one, answers the adjacent question, enumerates alternatives it won't take,
fixes something unrelated on the way past. Every detour is tokens, credits, and
latency spent on work you didn't ask for.

```bash
kirobuff mode on focus
```

It names the mechanisms rather than saying "stay on topic", because the general
instruction doesn't survive contact with an interesting tangent:

- Name what you expect to find *before* reading a file; if the list grows, say why
- Don't answer the adjacent question — "why does this fail" is not "redesign this"
- One recommendation, not a survey of options you're not taking
- Notice an unrelated problem? One line, then move on. Don't investigate it
- No abstraction before the second caller exists
- State the stopping condition up front, then stop there

Composes with `terse`, which trims *output*. Two different layers: `focus` cuts
what gets thought about, `terse` cuts what gets written.

**The bigger lever is mechanical.** Drift scales with how much irrelevant
material is in context — the model reasons about what it can see. Before reaching
for a prompt fragment:

```bash
kirobuff budget .kiro/agents/mine.json
```

Every `file://` glob is re-sent on every turn, so a wide one gives the model a
whole subsystem to wander into. Narrowing globs, moving reference material to
`skill://` so it loads on demand, and setting
`chat.disableInheritingDefaultResources` for focused agents all cut drift at the
source. A prompt asking for focus while 8,000 tokens of unrelated code sit in
context is fighting the wrong end of the problem.

## `agent install` — an agent carrying the enforcement hook

---

[← Back to README](../README.md)
