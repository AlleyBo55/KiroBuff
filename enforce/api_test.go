package enforce_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AlleyBo55/KiroBuff/enforce"
)

// This file is an external test package on purpose: it can only touch the
// exported API, so it fails to compile if the public surface stops being usable
// from outside. An internal test would not catch that.

func event(tool string, input any) enforce.Event {
	raw, _ := json.Marshal(input)
	return enforce.Event{HookEventName: "preToolUse", ToolName: tool, ToolInput: raw}
}

// noVendorEdits is the example from the package documentation. If this stops
// compiling, the documented extension point is broken.
type noVendorEdits struct{}

func (noVendorEdits) Name() string { return "no-vendor-edits" }

func (noVendorEdits) Check(e enforce.Event) enforce.Decision {
	in, ok := e.Write()
	if !ok || !strings.HasPrefix(in.Path, "vendor/") {
		return enforce.Allow
	}
	return enforce.Block("no-vendor-edits", "vendor/ is generated; edit go.mod instead")
}

func TestCallerCanAddARule(t *testing.T) {
	e := event("write", enforce.WriteInput{Command: "create", Path: "vendor/x/y.go"})

	// Not blocked by the defaults.
	if d := enforce.Evaluate(e); d.Blocked {
		t.Fatalf("default rules should allow this, got %+v", d)
	}
	// Blocked once the caller's rule is appended.
	rules := append(enforce.DefaultRules(), noVendorEdits{})
	d := enforce.Evaluate(e, rules...)
	if !d.Blocked || d.Rule != "no-vendor-edits" {
		t.Fatalf("custom rule not applied: %+v", d)
	}
}

func TestCallerCanDropARule(t *testing.T) {
	// Reordering and subsetting is the other half of open/closed: a caller who
	// wants force pushes allowed should not have to fork the package.
	e := event("shell", enforce.ShellInput{Command: "git push --force origin main"})
	if d := enforce.Evaluate(e); !d.Blocked {
		t.Fatal("defaults should block a force push")
	}

	var kept []enforce.Rule
	for _, r := range enforce.DefaultRules() {
		if r.Name() != "no-destructive-git" {
			kept = append(kept, r)
		}
	}
	if d := enforce.Evaluate(e, kept...); d.Blocked {
		t.Errorf("dropping the rule should permit the call, got %+v", d)
	}
}

func TestEvaluateWithNoRulesUsesDefaults(t *testing.T) {
	// The zero-configuration call has to do the expected thing, or every caller
	// has to remember to pass DefaultRules.
	e := event("shell", enforce.ShellInput{Command: "git commit -s -m x"})
	if d := enforce.Evaluate(e); !d.Blocked {
		t.Error("Evaluate with no rules should apply the defaults")
	}
}

func TestEveryDefaultRuleIsNamedAndAllowsUnrelatedEvents(t *testing.T) {
	unrelated := event("read", map[string]string{"path": "README.md"})
	seen := map[string]bool{}
	for _, r := range enforce.DefaultRules() {
		if r.Name() == "" {
			t.Errorf("%T has no name", r)
		}
		if seen[r.Name()] {
			t.Errorf("duplicate rule name %q", r.Name())
		}
		seen[r.Name()] = true

		// A rule must tolerate any event, including tools it does not handle.
		if d := r.Check(unrelated); d.Blocked {
			t.Errorf("%s blocked an unrelated read: %+v", r.Name(), d)
		}
		if d := r.Check(enforce.Event{}); d.Blocked {
			t.Errorf("%s blocked a zero event: %+v", r.Name(), d)
		}
	}
}

func TestRuleNamesReportsTheSet(t *testing.T) {
	got := enforce.RuleNames(enforce.DefaultRules())
	if len(got) != len(enforce.DefaultRules()) {
		t.Fatalf("got %v", got)
	}
	for _, want := range []string{"no-agent-signoff", "no-test-deletion",
		"no-assertion-weakening", "protect-verifier", "no-destructive-git"} {
		var found bool
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q from %v", want, got)
		}
	}
}

func TestAttributionFallsBackToTheRuleName(t *testing.T) {
	// A rule that forgets to set Rule must still be attributable, otherwise a
	// block appears from nowhere.
	d := enforce.Evaluate(event("read", nil), anonymousBlocker{})
	if d.Rule != "anonymous" {
		t.Errorf("expected the rule name to be filled in, got %q", d.Rule)
	}
}

type anonymousBlocker struct{}

func (anonymousBlocker) Name() string { return "anonymous" }
func (anonymousBlocker) Check(enforce.Event) enforce.Decision {
	return enforce.Decision{Blocked: true, Reason: "no rule field set"}
}

func TestToolAliasNormalisationIsExported(t *testing.T) {
	for _, name := range []string{"write", "fs_write", "fsWrite"} {
		if got := (enforce.Event{ToolName: name}).Tool(); got != "write" {
			t.Errorf("%s -> %s", name, got)
		}
	}
	for _, name := range []string{"shell", "execute_bash", "execute_cmd"} {
		if got := (enforce.Event{ToolName: name}).Tool(); got != "shell" {
			t.Errorf("%s -> %s", name, got)
		}
	}
}

func TestWriteAndShellFailClosedOnBadPayloads(t *testing.T) {
	bad := enforce.Event{ToolName: "write", ToolInput: json.RawMessage(`"not an object"`)}
	if _, ok := bad.Write(); ok {
		t.Error("unparseable payload should report false")
	}
	if _, ok := (enforce.Event{ToolName: "read"}).Write(); ok {
		t.Error("wrong tool should report false")
	}
}
