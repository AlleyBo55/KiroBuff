// Package loop scaffolds a Karpathy-style agent loop: a verifier the agent
// cannot grade itself against, a state ledger so the next iteration resumes
// instead of restarts, and a stop condition.
//
// The mapping onto Kiro CLI primitives is what makes it work without a runner:
//
//	verifier       stop hook, which fires after every assistant turn
//	state          agentSpawn hook that cats the ledger into context once
//	constraints    program.md, loaded as an always-on resource
//	stop condition max_attempts in the ledger, enforced by the verifier
//
// Putting the verifier on the stop hook is the load-bearing choice. The agent
// does not decide whether to run the tests; the harness runs them on its
// behalf after every response, and a failure is reported back. That is the
// difference between a loop and an agent agreeing with itself on repeat.
package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Dir is the workspace-relative home for loop artifacts.
const Dir = ".kiro/loop"

// Toolchain describes how to verify a project.
type Toolchain struct {
	Name   string
	Verify string // command that exits non-zero on failure
}

// knownToolchains maps a marker file to its verification command, most
// specific first.
var knownToolchains = []struct {
	marker string
	tc     Toolchain
}{
	{"go.mod", Toolchain{"go", "go build ./... && go test ./..."}},
	{"Cargo.toml", Toolchain{"cargo", "cargo test"}},
	{"pyproject.toml", Toolchain{"python", "python -m pytest -q"}},
	{"package.json", Toolchain{"node", "npm test --silent"}},
	{"Makefile", Toolchain{"make", "make test"}},
}

// DetectToolchain inspects a workspace for a recognised build system. The
// fallback is deliberately a failing placeholder rather than a guess: a loop
// with a verifier that always passes is worse than no loop.
func DetectToolchain(workspace string) Toolchain {
	for _, k := range knownToolchains {
		if _, err := os.Stat(filepath.Join(workspace, k.marker)); err == nil {
			return k.tc
		}
	}
	return Toolchain{
		Name:   "unknown",
		Verify: `echo "no verifier configured - edit .kiro/loop/verify.sh" >&2; exit 1`,
	}
}

// Scaffold describes a loop to be generated.
type Scaffold struct {
	Goal        string
	Editable    string // glob the agent is permitted to modify
	MaxAttempts int
	Toolchain   Toolchain
	AgentName   string

	// Metric is a command printing a single number to stdout. When set, the
	// loop becomes a search: a change is kept only if the number improved.
	// When empty the loop is a gate, which stops bad changes but does not find
	// good ones.
	Metric string
	// Direction is "lower" or "higher" - which way is better.
	Direction string
}

// New builds a Scaffold with defaults filled in.
func New(goal, editable string, maxAttempts int, tc Toolchain) Scaffold {
	if editable == "" {
		editable = "src/**"
	}
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	if goal == "" {
		goal = "Describe the measurable outcome here."
	}
	return Scaffold{
		Goal:        goal,
		Editable:    editable,
		MaxAttempts: maxAttempts,
		Toolchain:   tc,
		AgentName:   "loop",
		Direction:   "lower",
	}
}

// WithMetric turns the gate into a search.
func (s Scaffold) WithMetric(metric, direction string) Scaffold {
	s.Metric = metric
	if direction == "higher" {
		s.Direction = "higher"
	} else {
		s.Direction = "lower"
	}
	return s
}

// Scored reports whether a metric is configured.
func (s Scaffold) Scored() bool { return strings.TrimSpace(s.Metric) != "" }

// File is a generated artifact.
type File struct {
	Path string // workspace-relative
	Body string
	Mode os.FileMode
}

// Files returns everything the loop needs, in write order.
func (s Scaffold) Files() []File {
	return []File{
		{Path: filepath.Join(Dir, "program.md"), Body: s.program(), Mode: 0o644},
		{Path: filepath.Join(Dir, "verify.sh"), Body: s.verify(), Mode: 0o755},
		{Path: filepath.Join(Dir, "state.json"), Body: s.state(), Mode: 0o644},
		{Path: filepath.Join(".kiro/agents", s.AgentName+".json"), Body: s.agent(), Mode: 0o644},
	}
}

func (s Scaffold) program() string {
	return fmt.Sprintf(`# Loop program

## Goal

%s

## Constraints

- You may modify only: %s
- You may not modify: .kiro/loop/verify.sh, .kiro/loop/program.md
- One change per attempt. A large diff cannot be attributed to a cause.
- If an attempt does not improve the verifier result, revert it and record why.

## Protocol

1. Read .kiro/loop/state.json. Do not repeat an attempt already listed there.
2. Form one hypothesis about what will improve the result.
3. Make the smallest change that tests it.
4. The stop hook runs the verifier automatically after your turn. Wait for it.
5. Read the verifier's verdict. It decides keep or revert, not you.
6. Append the attempt to state.json: hypothesis, diff summary, score, verdict.
7. Stop when the goal is met or attempts reach max_attempts.

## Scoring

%s

## Why the verifier is off-limits

The verifier is the only thing standing between a loop and an agent grading its
own homework. Editing it to pass is the single failure mode that makes the
entire exercise worthless. The same applies to .kiro/loop/best: whoever owns
the score can win by rewriting it, so the verifier owns it and you cannot.
`, s.Goal, s.Editable, s.scoringNote())
}

// scoringNote explains to the agent how keep/revert is decided.
func (s Scaffold) scoringNote() string {
	if !s.Scored() {
		return "No metric is configured, so the verifier is a pass/fail gate. It can\n" +
			"reject a broken change but cannot tell an improvement from a no-op. Do not\n" +
			"claim an improvement you cannot measure."
	}
	better := "lower is better"
	if s.Direction == "higher" {
		better = "higher is better"
	}
	return fmt.Sprintf(
		"The verifier runs `%s` after the correctness gate passes and compares the\n"+
			"result against the best score so far (%s).\n\n"+
			"- Improved: the change is kept and the new best is recorded.\n"+
			"- Not improved: the verifier exits 1. Revert the change and form a\n"+
			"  different hypothesis. A change that does not move the number is not\n"+
			"  progress, however reasonable it looked.\n\n"+
			"Correctness is checked first. A faster wrong answer is not an improvement.",
		s.Metric, better)
}

func (s Scaffold) verify() string {
	head := fmt.Sprintf(`#!/usr/bin/env bash
# Deterministic verifier. Detected toolchain: %s
#
# Contract:
#   exit 0  -> keep the change
#   exit 1  -> reject; stderr explains why and is shown to the user
#
# Keep this deterministic. An LLM judge doubles the cost of every iteration and
# reintroduces the self-grading problem this file exists to prevent.
#
# This script owns .kiro/loop/best. The agent cannot write it, which is the
# point: whoever owns the score can win by editing it.
set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

STATE=".kiro/loop/state.json"
BEST=".kiro/loop/best"
LOG=".kiro/loop/score.log"

# Stop condition: refuse to keep going past the attempt cap.
if [ -f "$STATE" ]; then
  attempts=$(grep -c '"hypothesis"' "$STATE" 2>/dev/null || echo 0)
  max=$(sed -n 's/.*"max_attempts"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "$STATE" | head -1)
  max=${max:-%d}
  if [ "$attempts" -ge "$max" ] 2>/dev/null; then
    echo "loop: attempt cap reached ($attempts/$max). Stop and report." >&2
    exit 1
  fi
fi

# Correctness gate first. A faster wrong answer is not an improvement.
if ! %s; then
  echo "loop: verifier failed. Revert the change or fix it before the next attempt." >&2
  exit 1
fi
`, s.Toolchain.Name, s.MaxAttempts, s.Toolchain.Verify)

	if !s.Scored() {
		return head + `
# No metric configured, so this is a gate rather than a search: it rejects bad
# changes but cannot tell an improvement from a no-op. Add one with
#   kirobuff loop init -metric "CMD" -direction lower|higher
exit 0
`
	}

	// A scored loop needs float comparison, which shell cannot do portably.
	// awk is the smallest dependency that can, and is present everywhere bash is.
	return head + fmt.Sprintf(`
# --- score ------------------------------------------------------------------
score=$(%s 2>/dev/null | tr -d '[:space:]')

if ! printf '%%s' "$score" | grep -Eq '^-?[0-9]+([.][0-9]+)?$'; then
  echo "loop: metric command produced no number (got '$score'). Fix the metric before continuing." >&2
  exit 1
fi

printf '%%s\t%%s\n' "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)" "$score" >> "$LOG"

if [ ! -f "$BEST" ]; then
  printf '%%s' "$score" > "$BEST"
  echo "loop: baseline established at $score. Next attempt must improve on it." >&2
  exit 0
fi

best=$(tr -d '[:space:]' < "$BEST")

# %s is better.
improved=$(awk -v s="$score" -v b="$best" 'BEGIN { print (s %s b) ? 1 : 0 }')

if [ "$improved" = "1" ]; then
  printf '%%s' "$score" > "$BEST"
  echo "loop: improved $best -> $score. Keep this change and record it." >&2
  exit 0
fi

echo "loop: no improvement ($score vs best $best). Revert this change and try a different hypothesis." >&2
exit 1
`, s.Metric, s.Direction, comparator(s.Direction))
}

// comparator maps a direction to the awk operator that means "better".
func comparator(direction string) string {
	if direction == "higher" {
		return ">"
	}
	return "<"
}

func (s Scaffold) state() string {
	metric := "null"
	if s.Scored() {
		metric = fmt.Sprintf(`{"command": %q, "direction": %q}`, s.Metric, s.Direction)
	}
	// best is intentionally absent here: the verifier owns it, in
	// .kiro/loop/best, where the agent cannot write it.
	return fmt.Sprintf(`{
  "goal": %q,
  "max_attempts": %d,
  "metric": %s,
  "attempts": []
}
`, s.Goal, s.MaxAttempts, metric)
}

// agent wires the loop into a Kiro CLI agent.
//
// The state ledger is injected by agentSpawn rather than declared as a
// resource, because a resource is re-sent every turn while agentSpawn runs
// once per session.
func (s Scaffold) agent() string {
	return fmt.Sprintf(`{
  "name": %q,
  "description": "Karpathy-style loop: propose, verify, keep or revert",
  "prompt": "You are running a verification loop. Read .kiro/loop/program.md and follow its protocol exactly. Make one small change per attempt. You do not decide whether the change is good - the verifier does, and it runs automatically after your turn. Never edit .kiro/loop/verify.sh. Record every attempt in .kiro/loop/state.json before proposing the next one.",
  "tools": ["read", "write", "shell", "grep", "glob", "code"],
  "allowedTools": ["read", "grep", "glob", "code"],
  "toolsSettings": {
    "write": {
      "allowedPaths": [%q, ".kiro/loop/state.json"],
      "deniedPaths": [".kiro/loop/verify.sh", ".kiro/loop/program.md", ".kiro/loop/best", ".kiro/loop/score.log"]
    },
    "shell": {
      "autoAllowReadonly": true,
      "deniedCommands": ["rm -rf *", "git push *", "git reset --hard *"]
    }
  },
  "resources": ["file://.kiro/loop/program.md"],
  "hooks": {
    "preToolUse": [
      {
        "command": "kirobuff enforce"
      }
    ],
    "agentSpawn": [
      {
        "command": "cat .kiro/loop/state.json"
      }
    ],
    "stop": [
      {
        "command": ".kiro/loop/verify.sh"
      }
    ]
  }
}
`, s.AgentName, s.Editable)
}

// Write creates the scaffold under workspace. Existing files are left alone
// and reported, so re-running never destroys an in-progress ledger.
func (s Scaffold) Write(workspace string, force bool) (written, skipped []string, err error) {
	for _, f := range s.Files() {
		full := filepath.Join(workspace, f.Path)
		if _, statErr := os.Stat(full); statErr == nil && !force {
			skipped = append(skipped, f.Path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return written, skipped, err
		}
		if err := os.WriteFile(full, []byte(f.Body), f.Mode); err != nil {
			return written, skipped, err
		}
		// WriteFile honours the mode only when creating, so set it explicitly.
		if err := os.Chmod(full, f.Mode); err != nil {
			return written, skipped, err
		}
		written = append(written, f.Path)
	}
	return written, skipped, nil
}
