package enforce

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ev(tool string, input any) Event {
	raw, _ := json.Marshal(input)
	return Event{HookEventName: "preToolUse", ToolName: tool, ToolInput: raw}
}

func evIn(tool string, cwd string, input any) Event {
	e := ev(tool, input)
	e.CWD = cwd
	return e
}

// ---------------------------------------------------------------- DCO

func TestBlocksGitCommitSignoffFlag(t *testing.T) {
	for _, cmd := range []string{
		`git commit -s -m "fix"`,
		`git commit --signoff -m "fix"`,
		`git commit -sm "fix"`,
	} {
		d := Evaluate(ev("shell", shellInput{Command: cmd}))
		if !d.Blocked || d.Rule != "no-agent-signoff" {
			t.Errorf("%q should be blocked, got %+v", cmd, d)
		}
	}
}

func TestBlocksInlineSignoffTrailer(t *testing.T) {
	cmd := `git commit -m "fix

Signed-off-by: Agent <bot@example.com>"`
	d := Evaluate(ev("shell", shellInput{Command: cmd}))
	if !d.Blocked || d.Rule != "no-agent-signoff" {
		t.Errorf("expected a block, got %+v", d)
	}
}

func TestBlocksSignoffWrittenIntoCommitMessageFile(t *testing.T) {
	d := Evaluate(ev("write", writeInput{
		Command: "create",
		Path:    ".git/COMMIT_EDITMSG",
		Content: "fix\n\nSigned-off-by: Agent <bot@x>\n",
	}))
	if !d.Blocked || d.Rule != "no-agent-signoff" {
		t.Errorf("expected a block, got %+v", d)
	}
}

func TestAllowsPlainCommitAndAssistedBy(t *testing.T) {
	for _, cmd := range []string{
		`git commit -m "fix the thing"`,
		`git commit -am "fix"`,
		`git commit -m "fix

Assisted-by: Claude:claude-opus-4.7 kiro-cli"`,
	} {
		if d := Evaluate(ev("shell", shellInput{Command: cmd})); d.Blocked {
			t.Errorf("%q should be allowed, got %+v", cmd, d)
		}
	}
}

// ---------------------------------------------------------------- verifier

func TestBlocksVerifierEdits(t *testing.T) {
	for _, p := range []string{
		".kiro/loop/verify.sh",
		"/abs/project/.kiro/loop/verify.sh",
		".kiro/loop/program.md",
	} {
		d := Evaluate(ev("write", writeInput{Command: "create", Path: p, Content: "exit 0"}))
		if !d.Blocked || d.Rule != "protect-verifier" {
			t.Errorf("%s should be protected, got %+v", p, d)
		}
	}
}

func TestAllowsLedgerWrites(t *testing.T) {
	// The agent must be able to record attempts.
	d := Evaluate(ev("write", writeInput{
		Command: "create", Path: ".kiro/loop/state.json", Content: "{}",
	}))
	if d.Blocked {
		t.Errorf("the ledger must remain writable, got %+v", d)
	}
}

// ---------------------------------------------------------------- test paths

func TestIsTestPathRecognisesConventions(t *testing.T) {
	yes := []string{
		"internal/budget/budget_test.go",
		"tests/test_thing.py",
		"test_thing.py",
		"src/foo.test.ts",
		"src/foo.spec.tsx",
		"src/FooTest.java",
		"spec/models/user_spec.rb",
		"tests/anything.txt",
		"test/helper.js",
	}
	for _, p := range yes {
		if !IsTestPath(p) {
			t.Errorf("%s should be a test path", p)
		}
	}
	no := []string{
		"internal/budget/budget.go",
		"src/latest.go",
		"contested/thing.go",
		"README.md",
		"src/protest.py",
	}
	for _, p := range no {
		if IsTestPath(p) {
			t.Errorf("%s should NOT be a test path", p)
		}
	}
}

func TestBlocksTestDeletion(t *testing.T) {
	for _, cmd := range []string{
		`rm internal/budget/budget_test.go`,
		`rm -f tests/test_api.py`,
		`rm -rf spec/`,
	} {
		d := Evaluate(ev("shell", shellInput{Command: cmd}))
		if !d.Blocked || d.Rule != "no-test-deletion" {
			t.Errorf("%q should be blocked, got %+v", cmd, d)
		}
	}
}

func TestAllowsUnrelatedRemovals(t *testing.T) {
	for _, cmd := range []string{
		`rm /tmp/scratch.txt`,
		`rm -rf bin`,
		`rm coverage.out`,
	} {
		if d := Evaluate(ev("shell", shellInput{Command: cmd})); d.Blocked {
			t.Errorf("%q should be allowed, got %+v", cmd, d)
		}
	}
}

// ------------------------------------------------------- assertion weakening

func TestCountAssertions(t *testing.T) {
	body := `
	if got != want { t.Errorf("x") }
	t.Fatal("y")
	assert.Equal(a, b)
	expect(thing).toBe(1)
	require.NoError(err)
	EXPECT_EQ(1, 2);
	`
	if got := CountAssertions(body); got < 6 {
		t.Errorf("expected at least 6 assertions, got %d", got)
	}
	if got := CountAssertions("func main() {}"); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestBlocksAssertionRemovalViaStrReplace(t *testing.T) {
	d := Evaluate(ev("write", writeInput{
		Command: "strReplace",
		Path:    "internal/x/x_test.go",
		OldStr:  `if a != b { t.Errorf("no") }` + "\n" + `if c != d { t.Fatal("no") }`,
		NewStr:  `if a != b { t.Errorf("no") }`,
	}))
	if !d.Blocked || d.Rule != "no-assertion-weakening" {
		t.Fatalf("expected a block, got %+v", d)
	}
	if !strings.Contains(d.Reason, "1 of 2") {
		t.Errorf("reason should quantify the loss, got %q", d.Reason)
	}
}

func TestAllowsAddingAssertions(t *testing.T) {
	d := Evaluate(ev("write", writeInput{
		Command: "strReplace",
		Path:    "internal/x/x_test.go",
		OldStr:  `t.Errorf("a")`,
		NewStr:  `t.Errorf("a")` + "\n" + `t.Errorf("b")`,
	}))
	if d.Blocked {
		t.Errorf("strengthening a test must be allowed, got %+v", d)
	}
}

func TestBlocksOverwritingTestFileWithWeakerCoverage(t *testing.T) {
	ws := t.TempDir()
	rel := filepath.Join("internal", "x_test.go")
	full := filepath.Join(ws, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	strong := `t.Errorf("a"); t.Errorf("b"); t.Fatal("c")`
	if err := os.WriteFile(full, []byte(strong), 0o644); err != nil {
		t.Fatal(err)
	}

	d := Evaluate(evIn("write", ws, writeInput{
		Command: "create", Path: rel, Content: `t.Errorf("a")`,
	}))
	if !d.Blocked || d.Rule != "no-assertion-weakening" {
		t.Fatalf("expected a block, got %+v", d)
	}
	if !strings.Contains(d.Reason, "3 to 1") {
		t.Errorf("reason should show the drop, got %q", d.Reason)
	}
}

func TestAllowsBrandNewTestFile(t *testing.T) {
	ws := t.TempDir()
	d := Evaluate(evIn("write", ws, writeInput{
		Command: "create", Path: "internal/new_test.go", Content: `t.Errorf("a")`,
	}))
	if d.Blocked {
		t.Errorf("a new test file is additive and must be allowed, got %+v", d)
	}
}

func TestNonTestFilesAreNotSubjectToAssertionRules(t *testing.T) {
	d := Evaluate(ev("write", writeInput{
		Command: "strReplace",
		Path:    "internal/x/x.go",
		OldStr:  `t.Errorf("a")` + "\n" + `t.Errorf("b")`,
		NewStr:  ``,
	}))
	if d.Blocked {
		t.Errorf("production code is not governed by this rule, got %+v", d)
	}
}

// ---------------------------------------------------------------- git safety

func TestBlocksDestructiveGit(t *testing.T) {
	for _, cmd := range []string{
		`git reset --hard HEAD~3`,
		`git push --force origin main`,
		`git push -f`,
		`git clean -fd`,
		`git branch -D feature`,
		`git checkout -- .`,
	} {
		d := Evaluate(ev("shell", shellInput{Command: cmd}))
		if !d.Blocked || d.Rule != "no-destructive-git" {
			t.Errorf("%q should be blocked, got %+v", cmd, d)
		}
	}
}

func TestAllowsSafeGit(t *testing.T) {
	for _, cmd := range []string{
		`git status`,
		`git diff`,
		`git log --oneline -5`,
		`git add -A`,
		`git push origin feature-branch`,
		`git branch -d merged-branch`,
		`git checkout feature`,
	} {
		if d := Evaluate(ev("shell", shellInput{Command: cmd})); d.Blocked {
			t.Errorf("%q should be allowed, got %+v", cmd, d)
		}
	}
}

// ---------------------------------------------------------------- robustness

func TestUnknownToolsAndBadInputAreAllowed(t *testing.T) {
	// Failing open on unparseable input matters: a hook that blocks on a shape
	// it does not recognise would break every session after a schema change.
	if d := Evaluate(ev("read", map[string]string{"path": "x"})); d.Blocked {
		t.Errorf("unrelated tools should pass, got %+v", d)
	}
	bad := Event{ToolName: "write", ToolInput: json.RawMessage(`"not an object"`)}
	if d := Evaluate(bad); d.Blocked {
		t.Errorf("unparseable input should fail open, got %+v", d)
	}
}

func TestToolAliasesAreNormalised(t *testing.T) {
	for _, name := range []string{"write", "fs_write", "fsWrite"} {
		d := Evaluate(ev(name, writeInput{Command: "create", Path: ".kiro/loop/verify.sh"}))
		if !d.Blocked {
			t.Errorf("alias %q not recognised", name)
		}
	}
	for _, name := range []string{"shell", "execute_bash", "execute_cmd"} {
		d := Evaluate(ev(name, shellInput{Command: "git commit -s -m x"}))
		if !d.Blocked {
			t.Errorf("alias %q not recognised", name)
		}
	}
}

func TestBlockReasonsAddressTheModel(t *testing.T) {
	// stderr goes to the model on exit 2, so the text has to tell it what to do
	// instead, not just refuse.
	cases := []Event{
		ev("shell", shellInput{Command: "git commit -s -m x"}),
		ev("shell", shellInput{Command: "rm internal/x_test.go"}),
		ev("write", writeInput{Command: "create", Path: ".kiro/loop/verify.sh"}),
		ev("shell", shellInput{Command: "git reset --hard"}),
	}
	for _, e := range cases {
		d := Evaluate(e)
		if !d.Blocked {
			t.Fatalf("expected block for %s", e.ToolName)
		}
		if len(d.Reason) < 60 {
			t.Errorf("%s: reason too terse to be actionable: %q", d.Rule, d.Reason)
		}
		if !strings.Contains(strings.ToLower(d.Reason), "instead") &&
			!strings.Contains(strings.ToLower(d.Reason), "ask") {
			t.Errorf("%s: reason should offer a path forward: %q", d.Rule, d.Reason)
		}
	}
}
