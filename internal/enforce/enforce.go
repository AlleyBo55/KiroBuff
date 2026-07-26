// Package enforce blocks tool calls that violate the guardrails, rather than
// asking the model not to make them.
//
// The steering file states a policy. A policy in a prompt is a hope. Kiro CLI's
// preToolUse hook contract makes a subset of it mechanical: exit code 2 blocks
// the tool call and returns stderr to the model, so the model learns why it was
// stopped and can choose differently.
//
// Only rules that can be decided from the tool input belong here. "Prefer the
// smallest change" is judgment and stays in steering. "Do not delete a test
// file" is a string match and belongs in a wall.
package enforce

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Event is the preToolUse payload delivered on stdin.
type Event struct {
	HookEventName string          `json:"hook_event_name"`
	CWD           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// Decision is the outcome of evaluating an event.
type Decision struct {
	Blocked bool
	Rule    string
	Reason  string // returned to the model on a block
}

// Allow is the default outcome.
var Allow = Decision{}

// block builds a refusal whose text is aimed at the model, since that is who
// reads it.
func block(rule, reason string) Decision {
	return Decision{Blocked: true, Rule: rule, Reason: reason}
}

// writeInput is the shape of the write tool's parameters.
type writeInput struct {
	Command string `json:"command"`
	Path    string `json:"path"`
	Content string `json:"content"`
	OldStr  string `json:"oldStr"`
	NewStr  string `json:"newStr"`
}

// shellInput is the shape of the shell tool's parameters.
type shellInput struct {
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir"`
}

// normalizeTool collapses the documented aliases to a canonical name.
func normalizeTool(name string) string {
	switch strings.ToLower(name) {
	case "write", "fs_write", "fswrite":
		return "write"
	case "shell", "execute_bash", "executebash", "execute_cmd", "executecmd":
		return "shell"
	}
	return strings.ToLower(name)
}

// Evaluate applies every rule to an event and returns the first block.
func Evaluate(e Event) Decision {
	switch normalizeTool(e.ToolName) {
	case "write":
		var in writeInput
		if json.Unmarshal(e.ToolInput, &in) != nil {
			return Allow // unparseable input is not evidence of a violation
		}
		return evaluateWrite(in, e.CWD)
	case "shell":
		var in shellInput
		if json.Unmarshal(e.ToolInput, &in) != nil {
			return Allow
		}
		return evaluateShell(in)
	}
	return Allow
}

// ---------------------------------------------------------------- write rules

// protectedPaths may never be written by an agent. verify.sh is the load-
// bearing one: an agent that can edit its own verifier will eventually edit it
// to pass.
var protectedPaths = []string{
	filepath.Join(".kiro", "loop", "verify.sh"),
	filepath.Join(".kiro", "loop", "program.md"),
}

func evaluateWrite(in writeInput, cwd string) Decision {
	if d := checkProtectedPath(in.Path); d.Blocked {
		return d
	}
	if d := checkTestWeakening(in, cwd); d.Blocked {
		return d
	}
	return checkSignOffInContent(in)
}

func checkProtectedPath(path string) Decision {
	clean := filepath.ToSlash(filepath.Clean(path))
	for _, p := range protectedPaths {
		if strings.HasSuffix(clean, filepath.ToSlash(p)) {
			return block("protect-verifier", fmt.Sprintf(
				"Refusing to modify %s. The verifier and its constraints are "+
					"outside your write scope by design: a loop whose agent can "+
					"edit its own gate is a loop that grades its own homework. "+
					"Change the code under test instead.", path))
		}
	}
	return Allow
}

// testPath matches the common test-file conventions across ecosystems.
var testPath = regexp.MustCompile(
	`(^|/)(test_[^/]+\.py|[^/]+_test\.(go|py|rb|rs)|[^/]+\.(test|spec)\.(js|jsx|ts|tsx)|[^/]+Test\.(java|kt|cs)|[^/]+_spec\.rb)$|(^|/)(tests?|spec)/`)

// IsTestPath reports whether a path looks like a test file or lives in a test
// directory.
func IsTestPath(path string) bool {
	return testPath.MatchString(filepath.ToSlash(path))
}

// assertionToken counts the assertion-shaped constructs in a body. Counting is
// deliberately crude: the goal is to notice assertions disappearing, not to
// parse every test framework.
//
// The dotted forms matter more than they look. assert.Equal is the standard
// testify call and expect.that appears across JS matchers, so a pattern that
// only accepts a bare identifier before the paren misses the majority of real
// assertions and silently lets them be deleted.
var assertionToken = regexp.MustCompile(
	`\bassert\w*(\.\w+)*\(|\bexpect\w*(\.\w+)*\(|\bt\.(Errorf|Fatalf|Error|Fatal)\b|\brequire\.\w+\(|\bEXPECT_\w+\(|\bASSERT_\w+\(|\bshould\b`)

// CountAssertions returns how many assertion-shaped constructs appear.
func CountAssertions(body string) int {
	return len(assertionToken.FindAllString(body, -1))
}

func checkTestWeakening(in writeInput, cwd string) Decision {
	if !IsTestPath(in.Path) {
		return Allow
	}

	switch in.Command {
	case "strReplace":
		before, after := CountAssertions(in.OldStr), CountAssertions(in.NewStr)
		if after < before {
			return block("no-assertion-weakening", fmt.Sprintf(
				"Refusing this edit to %s: it removes %d of %d assertions. A "+
					"failing test is information; deleting the assertion destroys "+
					"the information and keeps the bug. If the original "+
					"expectation was provably wrong, say why in your response and "+
					"ask before narrowing it.",
				in.Path, before-after, before))
		}

	case "create":
		// Overwriting an existing test file with fewer assertions is the same
		// harm by another route.
		full := in.Path
		if !filepath.IsAbs(full) && cwd != "" {
			full = filepath.Join(cwd, full)
		}
		existing, err := os.ReadFile(full)
		if err != nil {
			return Allow // a brand-new test file is additive, and welcome
		}
		before, after := CountAssertions(string(existing)), CountAssertions(in.Content)
		if after < before {
			return block("no-assertion-weakening", fmt.Sprintf(
				"Refusing to overwrite %s: assertion count drops from %d to %d. "+
					"Rewriting a test file with weaker coverage is a regression "+
					"with no test left to catch it. Edit the specific case you "+
					"mean to change, or ask first.",
				in.Path, before, after))
		}
	}
	return Allow
}

// checkSignOffInContent catches an agent writing a DCO trailer into a commit
// message file, which is how it would sneak past the shell rule.
func checkSignOffInContent(in writeInput) Decision {
	base := strings.ToLower(filepath.Base(in.Path))
	isCommitMsg := strings.Contains(base, "commit_editmsg") ||
		strings.Contains(base, "commit-msg") ||
		strings.HasSuffix(base, ".gitmessage")
	if !isCommitMsg {
		return Allow
	}
	body := in.Content + in.NewStr
	if strings.Contains(body, "Signed-off-by:") {
		return dcoBlock()
	}
	return Allow
}

// ---------------------------------------------------------------- shell rules

var (
	// git commit -s / --signoff adds the DCO trailer automatically.
	signoffFlag = regexp.MustCompile(`\bgit\b[^|;&]*\bcommit\b[^|;&]*(\s-\w*s\w*\b|\s--signoff\b)`)
	// An explicit trailer in the message does the same thing by hand.
	signoffInline = regexp.MustCompile(`\bgit\b[^|;&]*\bcommit\b[^|;&]*Signed-off-by:`)

	destructiveGit = []struct {
		pattern *regexp.Regexp
		what    string
	}{
		{regexp.MustCompile(`\bgit\s+reset\s+(--hard|--merge)\b`), "git reset --hard discards uncommitted work irreversibly"},
		{regexp.MustCompile(`\bgit\s+push\b[^|;&]*(--force\b|--force-with-lease\b|\s-f\b)`), "a force push can destroy commits on the remote"},
		{regexp.MustCompile(`\bgit\s+clean\b[^|;&]*-\w*[fd]`), "git clean -f deletes untracked files with no recovery"},
		{regexp.MustCompile(`\bgit\s+branch\b[^|;&]*\s-D\b`), "branch -D force-deletes a branch that may hold unmerged work"},
		{regexp.MustCompile(`\bgit\s+checkout\s+--\s`), "checkout -- overwrites local changes irreversibly"},
	}

	// rm targeting something test-shaped.
	rmCommand = regexp.MustCompile(`\brm\b[^|;&]*`)
)

func evaluateShell(in shellInput) Decision {
	cmd := in.Command

	if signoffFlag.MatchString(cmd) || signoffInline.MatchString(cmd) {
		return dcoBlock()
	}

	for _, d := range destructiveGit {
		if d.pattern.MatchString(cmd) {
			return block("no-destructive-git", fmt.Sprintf(
				"Refusing to run this: %s. If you need it, explain what will be "+
					"lost and ask the human to run it themselves.", d.what))
		}
	}

	// Deleting tests is the most direct way to make a suite pass.
	for _, m := range rmCommand.FindAllString(cmd, -1) {
		for _, tok := range strings.Fields(m) {
			if strings.HasPrefix(tok, "-") || tok == "rm" {
				continue
			}
			if IsTestPath(tok) {
				return block("no-test-deletion", fmt.Sprintf(
					"Refusing to delete %s. A failing test is information. If the "+
						"test is genuinely obsolete, say why and ask first - "+
						"deleting it is subtractive, and subtractive changes stop "+
						"and ask.", tok))
			}
		}
	}
	return Allow
}

func dcoBlock() Decision {
	return block("no-agent-signoff",
		"Refusing to add a Signed-off-by trailer. Only a human can certify the "+
			"Developer Certificate of Origin, and the Linux kernel's AI policy "+
			"makes this explicit: agents must not sign off. Use an Assisted-by "+
			"trailer to record the model and tools instead, and leave the "+
			"sign-off to the human submitter.")
}
