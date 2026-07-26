// Package kirobuff is a buff for Kiro CLI: enforced change-safety guardrails,
// composable agent modes, reasoning-effort tuning, and context-cost budgeting.
//
// The command lives at [github.com/AlleyBo55/KiroBuff/cmd/kirobuff]. Install it
// and run once:
//
//	go install github.com/AlleyBo55/KiroBuff/cmd/kirobuff@latest
//	kirobuff install
//
// # What it does
//
// Most of what people want from an agent harness is not more capability but
// fewer ways to be surprised. kirobuff configures Kiro CLI so that an AI coding
// agent classifies every change before touching a file: additive and
// behaviour-preserving work proceeds without asking, while behaviour-changing
// and subtractive work stops and asks.
//
// Five rules go further and are enforced mechanically rather than requested. A
// preToolUse hook exits 2, which blocks the tool call outright and tells the
// model why:
//
//   - an agent adding a Signed-off-by trailer, which only a human may certify
//   - deleting a test file
//   - an edit that reduces a test's assertion count
//   - writing to a loop's own verifier or score record
//   - git reset --hard, push --force, clean -fd, branch -D
//
// # Reusable packages
//
// Three packages are exported for use outside the command:
//
//   - [github.com/AlleyBo55/KiroBuff/enforce] evaluates agent tool calls against
//     pluggable change-safety rules. Implement enforce.Rule to add your own.
//   - [github.com/AlleyBo55/KiroBuff/attest] generates and validates Assisted-by
//     commit trailers per the Linux kernel's AI-assisted contribution policy,
//     including the rule that agents may not sign off on the Developer
//     Certificate of Origin.
//   - [github.com/AlleyBo55/KiroBuff/semver] classifies Conventional Commits
//     into major, minor and patch bumps.
//
// Everything else is under internal/ because it is specific to laying out Kiro
// CLI configuration on disk.
//
// # Keywords
//
// AI coding agent guardrails, agentic coding safety, LLM agent permissions,
// Kiro CLI configuration, Claude Code alternative tooling, agent hooks,
// preToolUse hook, prompt injection of change policy, token budget estimation,
// context window cost, reasoning effort tuning, Conventional Commits semver,
// Assisted-by trailer, Developer Certificate of Origin, Linux kernel AI policy,
// AI attribution, regression prevention, test deletion prevention,
// agent mode composition, Karpathy loop, verifier-driven agent loop.
package kirobuff
