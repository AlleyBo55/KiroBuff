// Package steering installs guardrails that apply to every agent without
// being switched on.
//
// Steering files are the only always-on surface Kiro CLI offers: by default
// every agent inherits file://~/.kiro/steering/**/*.md and the workspace
// equivalent. Nothing needs to opt in, and a new agent created tomorrow gets
// them too.
//
// The one way to lose them is chat.disableInheritingDefaultResources=true,
// which stops custom agents inheriting default resources. Verify reports that.
package steering

import (
	"os"
	"path/filepath"
	"strings"
)

// Filename is prefixed so it loads before other steering files.
const Filename = "00-kirobuff-guardrails.md"

// Marker identifies a kirobuff-managed file, so updates never clobber a file
// the user wrote themselves.
const Marker = "<!-- kirobuff:guardrails -->"

// Scope selects global or workspace installation.
type Scope string

// Installation scopes.
const (
	Global    Scope = "global"
	Workspace Scope = "workspace"
)

// Dir resolves the steering directory for a scope, honouring KIRO_HOME.
func Dir(scope Scope, home, workspace string) (string, error) {
	switch scope {
	case Global:
		kiro := os.Getenv("KIRO_HOME")
		if kiro == "" {
			kiro = filepath.Join(home, ".kiro")
		}
		return filepath.Join(kiro, "steering"), nil
	case Workspace:
		return filepath.Join(workspace, ".kiro", "steering"), nil
	}
	return "", &scopeError{scope}
}

type scopeError struct{ s Scope }

func (e *scopeError) Error() string { return "unknown scope " + string(e.s) }

// Guardrails is the always-on policy.
//
// It is written as a decision procedure rather than a set of values, because
// "do not break things" produces an agent that asks permission for everything
// or nothing. A classification the agent must perform before editing produces
// an agent that proceeds on safe work and stops on risky work.
func Guardrails() string {
	return Marker + `
# Change safety guardrails

These apply to every task. They are not advice.

## Classify the change before editing

Decide which of these the change is, and say which one when it is not obvious:

**Additive** — a new file, new function, new test, new flag that defaults to
current behaviour, or a new branch no existing caller reaches.
→ Proceed. Do not ask.

**Behaviour-preserving** — refactor, rename, extraction, or reformat where
existing tests cover the affected path and still pass unchanged.
→ Proceed. Run the tests. Report if coverage was thin.

**Behaviour-changing** — anything that alters what existing callers observe:
a signature, a default value, a return shape, an error type, output format,
a config key, a database schema, an API contract, or timing that callers
depend on.
→ Stop. State what breaks, who it affects, and the alternative. Then ask.

**Subtractive** — deleting or narrowing a function, file, flag, field,
permission, or test.
→ Stop and ask, even when it looks dead. Dead code that is actually reachable
is a regression with no test to catch it.

When a change spans categories, the riskiest category wins.

## Never do these

- Never delete or skip a test to make a suite pass. A failing test is
  information. Removing it destroys the information and keeps the bug.
- Never weaken an assertion to make it pass. Narrow the assertion only when the
  original expectation was provably wrong, and say so.
- Never widen a type, add a cast, or silence a compiler or linter error without
  understanding it. Suppression is not a fix.
- Never leave the tree in a state that does not build. If you cannot finish,
  revert to the last working state and report where you stopped.
- Never claim something works without having run it. If verification was not
  possible, say which part is unverified and why.
- Never rewrite working code because the style is unfashionable. Working code
  with an unpopular structure is not a defect.

## Bias toward proceeding

Asking about safe work is its own failure. If the change is additive or
behaviour-preserving and the tests pass, keep going without checking in. Do not
stop to summarise, confirm the plan, or request approval when nothing is at
risk. Batch the questions you genuinely need into one, at the point you
actually need them.

## Before reporting done

1. Run the project's build. If it does not run tests, run them separately.
2. State what you verified and what you could not.
3. Name anything you touched that no test covers.
4. If you changed behaviour anywhere, list it explicitly rather than leaving
   it in the diff to be discovered.

## Scope

Solve the problem that was asked about. A bug fix does not need the
surrounding code cleaned up. State what you deliberately did not do, so scope
is a decision rather than a drift.
`
}

// IsManaged reports whether a file at path was written by kirobuff.
func IsManaged(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), Marker)
}

// Install writes the guardrails into dir.
//
// A file kirobuff wrote is updated in place, since that is how users get
// policy improvements. A file it did not write is left alone unless force is
// set, so hand-edited guardrails are never silently replaced.
func Install(dir string, force bool) (path string, updated bool, err error) {
	path = filepath.Join(dir, Filename)

	if _, statErr := os.Stat(path); statErr == nil {
		if !IsManaged(path) && !force {
			return path, false, os.ErrExist
		}
		updated = true
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return path, updated, err
	}
	if err := os.WriteFile(path, []byte(Guardrails()), 0o644); err != nil {
		return path, updated, err
	}
	return path, updated, nil
}

// Verify checks that nothing prevents the guardrails from loading.
//
// The steering directory being present is not sufficient: a user who set
// chat.disableInheritingDefaultResources=true has custom agents that no longer
// inherit it, which is silent and easy to miss.
func Verify(settingsPath string) []string {
	var problems []string

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		return problems // no settings file means defaults, which inherit
	}
	// A substring check is enough here and avoids depending on the settings
	// schema, which is broader than this package needs to model.
	body := string(raw)
	if strings.Contains(body, "chat.disableInheritingDefaultResources") &&
		strings.Contains(body, "true") {
		problems = append(problems,
			"chat.disableInheritingDefaultResources is set: custom agents will NOT "+
				"inherit these guardrails. Add the steering glob to each agent's "+
				"resources, or set it back to false.")
	}
	return problems
}
