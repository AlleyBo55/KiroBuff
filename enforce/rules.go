package enforce

// The built-in rules. Each is a type rather than a branch in a switch, so a
// caller can drop one, reorder them, or add their own without editing this
// file.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------- protected paths

// DefaultProtectedPaths are files an agent must never write.
//
// verify.sh is the load-bearing one: an agent that can edit its own verifier
// will eventually edit it to pass. best and score.log are the same hazard by
// another route, since whoever owns the score can win by rewriting it.
func DefaultProtectedPaths() []string {
	return []string{
		filepath.Join(".kiro", "loop", "verify.sh"),
		filepath.Join(".kiro", "loop", "program.md"),
		filepath.Join(".kiro", "loop", "best"),
		filepath.Join(".kiro", "loop", "score.log"),
	}
}

// ProtectedPathRule blocks writes to paths the agent must not control.
type ProtectedPathRule struct{ Paths []string }

// Name identifies this rule in a Decision.
func (ProtectedPathRule) Name() string { return "protect-verifier" }

// Check implements [Rule].
func (r ProtectedPathRule) Check(e Event) Decision {
	in, ok := e.Write()
	if !ok {
		return Allow
	}
	clean := filepath.ToSlash(filepath.Clean(in.Path))
	for _, p := range r.Paths {
		if strings.HasSuffix(clean, filepath.ToSlash(p)) {
			return Block(r.Name(), fmt.Sprintf(
				"Refusing to modify %s. The verifier and its score are outside "+
					"your write scope by design: a loop whose agent can edit its "+
					"own gate is a loop that grades its own homework. Change the "+
					"code under test instead.", in.Path))
		}
	}
	return Allow
}

// ---------------------------------------------------------------- DCO sign-off

var (
	// git commit -s / --signoff adds the DCO trailer automatically.
	signoffFlag = regexp.MustCompile(`\bgit\b[^|;&]*\bcommit\b[^|;&]*(\s-\w*s\w*\b|\s--signoff\b)`)
	// An explicit trailer in the message does the same thing by hand.
	signoffInline = regexp.MustCompile(`\bgit\b[^|;&]*\bcommit\b[^|;&]*Signed-off-by:`)
)

// SignOffRule blocks an agent from certifying the Developer Certificate of
// Origin, which the Linux kernel's AI policy reserves for humans.
type SignOffRule struct{}

// Name identifies this rule in a Decision.
func (SignOffRule) Name() string { return "no-agent-signoff" }

// Check implements [Rule].
func (r SignOffRule) Check(e Event) Decision {
	if in, ok := e.Shell(); ok {
		if signoffFlag.MatchString(in.Command) || signoffInline.MatchString(in.Command) {
			return r.block()
		}
		return Allow
	}
	// Writing the trailer into a commit message file is the same act by another
	// route.
	in, ok := e.Write()
	if !ok {
		return Allow
	}
	base := strings.ToLower(filepath.Base(in.Path))
	isCommitMsg := strings.Contains(base, "commit_editmsg") ||
		strings.Contains(base, "commit-msg") ||
		strings.HasSuffix(base, ".gitmessage")
	if isCommitMsg && strings.Contains(in.Content+in.NewStr, "Signed-off-by:") {
		return r.block()
	}
	return Allow
}

func (r SignOffRule) block() Decision {
	return Block(r.Name(),
		"Refusing to add a Signed-off-by trailer. Only a human can certify the "+
			"Developer Certificate of Origin, and the Linux kernel's AI policy "+
			"makes this explicit: agents must not sign off. Use an Assisted-by "+
			"trailer to record the model and tools instead, and leave the "+
			"sign-off to the human submitter.")
}

// ---------------------------------------------------------------- tests

// testPath matches the common test-file conventions across ecosystems.
var testPath = regexp.MustCompile(
	`(^|/)(test_[^/]+\.py|[^/]+_test\.(go|py|rb|rs)|[^/]+\.(test|spec)\.(js|jsx|ts|tsx)|[^/]+Test\.(java|kt|cs)|[^/]+_spec\.rb)$|(^|/)(tests?|spec)/`)

// IsTestPath reports whether a path looks like a test file or lives in a test
// directory.
func IsTestPath(path string) bool {
	return testPath.MatchString(filepath.ToSlash(path))
}

// assertionToken counts assertion-shaped constructs.
//
// The dotted forms matter more than they look: assert.Equal is the standard
// testify call and expect.that appears across JS matchers, so a pattern
// accepting only a bare identifier before the paren misses most real assertions
// and silently lets them be deleted.
var assertionToken = regexp.MustCompile(
	`\bassert\w*(?:\.\w+)*\(|\bexpect\w*(?:\.\w+)*\(|\bt\.(?:Errorf|Fatalf|Error|Fatal)\b|\brequire\.\w+\(|\bEXPECT_\w+\(|\bASSERT_\w+\(|\bshould\b`)

// CountAssertions returns how many assertion-shaped constructs appear.
//
// This runs inside the preToolUse hook, which gates every tool call, so it is
// the hottest path in the tool. FindAllString would allocate a slice holding
// every matched substring purely to take its length; scanning with
// FindStringIndex over a shrinking suffix avoids materialising the matches.
func CountAssertions(body string) int {
	count := 0
	for pos := 0; pos < len(body); {
		loc := assertionToken.FindStringIndex(body[pos:])
		if loc == nil {
			break
		}
		count++
		// A zero-width match would not advance and would spin forever.
		if loc[1] == 0 {
			pos++
			continue
		}
		pos += loc[1]
	}
	return count
}

// AssertionWeakeningRule blocks edits that reduce a test's assertion count.
type AssertionWeakeningRule struct{}

// Name identifies this rule in a Decision.
func (AssertionWeakeningRule) Name() string { return "no-assertion-weakening" }

// Check implements [Rule].
func (r AssertionWeakeningRule) Check(e Event) Decision {
	in, ok := e.Write()
	if !ok || !IsTestPath(in.Path) {
		return Allow
	}

	switch in.Command {
	case "strReplace":
		before, after := CountAssertions(in.OldStr), CountAssertions(in.NewStr)
		if after < before {
			return Block(r.Name(), fmt.Sprintf(
				"Refusing this edit to %s: it removes %d of %d assertions. A "+
					"failing test is information; deleting the assertion destroys "+
					"the information and keeps the bug. If the original "+
					"expectation was provably wrong, say why in your response and "+
					"ask before narrowing it.",
				in.Path, before-after, before))
		}

	case "create":
		full := in.Path
		if !filepath.IsAbs(full) && e.CWD != "" {
			full = filepath.Join(e.CWD, full)
		}
		existing, err := os.ReadFile(full)
		if err != nil {
			return Allow // a brand-new test file is additive, and welcome
		}
		before, after := CountAssertions(string(existing)), CountAssertions(in.Content)
		if after < before {
			return Block(r.Name(), fmt.Sprintf(
				"Refusing to overwrite %s: assertion count drops from %d to %d. "+
					"Rewriting a test file with weaker coverage is a regression "+
					"with no test left to catch it. Edit the specific case you "+
					"mean to change, or ask first.",
				in.Path, before, after))
		}
	}
	return Allow
}

var rmCommand = regexp.MustCompile(`\brm\b[^|;&]*`)

// TestDeletionRule blocks removing a test file, the most direct way to make a
// suite pass.
type TestDeletionRule struct{}

// Name identifies this rule in a Decision.
func (TestDeletionRule) Name() string { return "no-test-deletion" }

// Check implements [Rule].
func (r TestDeletionRule) Check(e Event) Decision {
	in, ok := e.Shell()
	if !ok {
		return Allow
	}
	for _, m := range rmCommand.FindAllString(in.Command, -1) {
		for _, tok := range strings.Fields(m) {
			if tok == "rm" || strings.HasPrefix(tok, "-") {
				continue
			}
			if IsTestPath(tok) {
				return Block(r.Name(), fmt.Sprintf(
					"Refusing to delete %s. A failing test is information. If the "+
						"test is genuinely obsolete, say why and ask first - "+
						"deleting it is subtractive, and subtractive changes stop "+
						"and ask.", tok))
			}
		}
	}
	return Allow
}

// ---------------------------------------------------------------- git safety

var destructiveGit = []struct {
	pattern *regexp.Regexp
	what    string
}{
	{regexp.MustCompile(`\bgit\s+reset\s+(--hard|--merge)\b`), "git reset --hard discards uncommitted work irreversibly"},
	{regexp.MustCompile(`\bgit\s+push\b[^|;&]*(--force\b|--force-with-lease\b|\s-f\b)`), "a force push can destroy commits on the remote"},
	{regexp.MustCompile(`\bgit\s+clean\b[^|;&]*-\w*[fd]`), "git clean -f deletes untracked files with no recovery"},
	{regexp.MustCompile(`\bgit\s+branch\b[^|;&]*\s-D\b`), "branch -D force-deletes a branch that may hold unmerged work"},
	{regexp.MustCompile(`\bgit\s+checkout\s+--\s`), "checkout -- overwrites local changes irreversibly"},
}

// DestructiveGitRule blocks git operations that cannot be undone.
type DestructiveGitRule struct{}

// Name identifies this rule in a Decision.
func (DestructiveGitRule) Name() string { return "no-destructive-git" }

// Check implements [Rule].
func (r DestructiveGitRule) Check(e Event) Decision {
	in, ok := e.Shell()
	if !ok {
		return Allow
	}
	for _, d := range destructiveGit {
		if d.pattern.MatchString(in.Command) {
			return Block(r.Name(), fmt.Sprintf(
				"Refusing to run this: %s. If you need it, explain what will be "+
					"lost and ask the human to run it themselves.", d.what))
		}
	}
	return Allow
}
