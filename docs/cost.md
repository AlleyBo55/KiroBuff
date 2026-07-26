# Cost and speed

What context actually costs per turn, how to see it, and how to stop paying
for reasoning you did not ask for.

## `budget` — measure what context actually costs

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

## `guard` — make the budget check automatic

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

## `tune` — reasoning volume, not reasoning aim

```bash
kirobuff tune                                  # Opus -> medium
kirobuff tune -model openai-gpt-5.4 -effort high
kirobuff tune -show                            # what's configured now
```

Kiro CLI's built-in default for the Opus family is `xhigh`. On mechanical work
that buys nothing and costs both latency and credits. This sets a floor, not a
ceiling — raise it for one session with `/effort high` when a task earns it.

**This is not the fix for wandering off-topic.** Effort controls how *long* the
model deliberates, not how *close to the question* it stays. A lower floor makes
it think less; it does not make it think nearer the point. For drift, see
`focus` mode below and the context section under it — the biggest single cause is
having irrelevant material loaded in the first place.

The JSON path is model-specific and unforgiving: Claude reads
`output_config.effort`, GPT reads `reasoning.effort`, and a value at the wrong
path is silently ignored at bootstrap. kirobuff writes the right one and
**refuses** to guess for models with no documented effort field, rather than
writing a setting that quietly does nothing.

---

[← Back to README](../README.md)
