# Safety

The guardrails, the hook that enforces them, and kernel-compliant attribution
for AI-assisted commits.

## `guardrails` — always on, every agent

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

## `enforce` — guardrails with teeth

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

## `attest` — kernel-compliant AI attribution

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

---

[← Back to README](../README.md)

## Two layers, because commands are whack-a-mole

`enforce` blocks a tool call before it runs. `sentinel` measures what actually
happened after the turn. You want both, and the reason is empirical.

Probing the `preToolUse` rules on a real install found **six working bypasses
across two rules**:

```
deleting a test:  rm ✓   git rm ✓   mv ✗   unlink ✗   find -delete ✗
signing off:      -s ✓   git config format.signOff ✗   --trailer ✗
```

All eight are blocked now. That is not the lesson. The lesson is that every
command rule invites the next variation, and the list will never be complete.

### `sentinel` — measure the outcome instead

```bash
kirobuff sentinel install .kiro/agents/mine.json
```

It counts test files and assertions across the repository after every turn and
warns when the total falls below the highest it has seen. It never looks at the
command, so it catches deletion by any route:

```
kirobuff: test coverage dropped: lost 1 test file(s) and 2 assertion(s)
          (now 1 files / 1 assertions, peak was 2 / 3)

Nothing was blocked - this runs after the turn. If a test was removed
or emptied and that was not intended, restore it now.
If it was genuinely obsolete: kirobuff sentinel accept
```

Verified against three methods the command rules originally missed: deleting the
file, moving it out of the tree, and truncating it in place. All three detected.

**The tradeoff is timing.** A `stop` hook cannot block a tool call, only warn
after the turn. So the sentinel finds the loss a minute later rather than
preventing it — which is still the difference between noticing and shipping.

**The peak only rises.** A drop stays reported until the tests come back, because
recording a drop as the new baseline would let coverage ratchet down one turn at
a time without a single warning. Deleting a genuinely obsolete test is
legitimate, so there is an explicit way to say so:

```bash
kirobuff sentinel accept
```

The baseline lives in `.kiro/kirobuff/sentinel.json` and is in `enforce`'s
protected paths. Whoever can lower it can defeat the check.
