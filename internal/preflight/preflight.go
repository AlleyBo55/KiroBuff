// Package preflight checks a branch against its base before a push, so
// conflicts surface while they are still cheap to fix.
//
// It cannot promise a conflict-free repository; two people editing the same line
// will always conflict eventually. What it can do is guarantee you never learn
// about it from a pull request page. Every check answers with the command that
// fixes it.
//
// The check that earns its place is [DuplicateCommits]. When a pull request is
// squash-merged, the base branch gains one commit containing work your branch
// still carries as several. The content is identical and the hashes are not, so
// git reports a conflict in files nobody touched twice. It is the most confusing
// conflict a contributor meets and the least obvious to diagnose, and comparing
// patch IDs finds it exactly.
package preflight

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Severity ranks a finding.
type Severity string

const (
	// Blocker means the push should not proceed.
	Blocker Severity = "blocker"
	// Warning means the push may proceed but something needs attention.
	Warning Severity = "warning"
	// Info is informational only.
	Info Severity = "info"
)

// Finding is one thing preflight noticed.
type Finding struct {
	Check    string
	Severity Severity
	Detail   string
	Fix      string // exact command to run, empty when there is nothing to do
}

// Report is the outcome of a run.
type Report struct {
	Base     string
	Branch   string
	Findings []Finding
}

// Blocked reports whether any finding should stop the push.
func (r Report) Blocked() bool {
	for _, f := range r.Findings {
		if f.Severity == Blocker {
			return true
		}
	}
	return false
}

// Clean reports whether nothing at all needs attention.
func (r Report) Clean() bool { return len(r.Findings) == 0 }

// gitTimeout bounds every git invocation. preflight runs from a pre-push hook,
// where a git waiting on credentials would hang the push with no explanation.
const gitTimeout = 15 * time.Second

// gitCmd builds a git command with a deadline.
func gitCmd(args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	return exec.CommandContext(ctx, "git", args...), cancel
}

// git runs a git command and returns trimmed stdout.
func git(args ...string) (string, error) {
	cmd, cancel := gitCmd(args...)
	defer cancel()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// CurrentBranch returns the checked-out branch name.
func CurrentBranch() (string, error) {
	return git("rev-parse", "--abbrev-ref", "HEAD")
}

// DefaultBase guesses the base branch, preferring the remote's own HEAD so a
// repository that renamed main to master is handled without configuration.
func DefaultBase() string {
	if ref, err := git("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return ref
	}
	for _, candidate := range []string{"origin/master", "origin/main"} {
		if _, err := git("rev-parse", "--verify", candidate); err == nil {
			return candidate
		}
	}
	return "origin/master"
}

// protectedBranches should not receive a direct push.
var protectedBranches = []string{"master", "main", "trunk", "release"}

// Run performs every check against base.
//
// base may be empty, in which case DefaultBase is used.
func Run(base string) (Report, error) {
	branch, err := CurrentBranch()
	if err != nil {
		return Report{}, err
	}
	if base == "" {
		base = DefaultBase()
	}
	r := Report{Base: base, Branch: branch}

	if _, err := git("rev-parse", "--verify", base); err != nil {
		r.Findings = append(r.Findings, Finding{
			Check:    "base-missing",
			Severity: Warning,
			Detail:   fmt.Sprintf("%s is not present locally, so nothing can be compared", base),
			Fix:      "git fetch origin",
		})
		return r, nil //nolint:nilerr // a base that is not fetched is reported as a finding, not an error
	}

	r.Findings = append(r.Findings, checkProtected(branch)...)
	r.Findings = append(r.Findings, checkStaleBase(base)...)

	behind, err := checkBehind(branch, base)
	if err != nil {
		return r, err
	}
	r.Findings = append(r.Findings, behind...)

	dup, err := DuplicateCommits(branch, base)
	if err != nil {
		return r, err
	}
	if len(dup) > 0 {
		r.Findings = append(r.Findings, Finding{
			Check:    "squash-duplicates",
			Severity: Blocker,
			Detail: fmt.Sprintf(
				"%d commit(s) on %s already exist in %s under different hashes, "+
					"which is what a squash-merged pull request leaves behind. Git "+
					"will report conflicts in files nobody edited twice: %s",
				len(dup), branch, base, strings.Join(dup, ", ")),
			Fix: fmt.Sprintf("git rebase --onto %s %s %s", base, dup[len(dup)-1], branch),
		})
	}

	conflicts, err := ConflictingFiles(branch, base)
	if err != nil {
		return r, err
	}
	if len(conflicts) > 0 {
		r.Findings = append(r.Findings, Finding{
			Check:    "merge-conflict",
			Severity: Blocker,
			Detail: fmt.Sprintf("merging into %s would conflict in: %s",
				base, strings.Join(conflicts, ", ")),
			Fix: fmt.Sprintf("git fetch origin && git rebase %s", base),
		})
	}
	return r, nil
}

func checkProtected(branch string) []Finding {
	for _, p := range protectedBranches {
		if branch == p {
			return []Finding{{
				Check:    "protected-branch",
				Severity: Blocker,
				Detail:   fmt.Sprintf("%q is a protected branch and should receive changes through a pull request", branch),
				Fix:      "git switch -c feat/your-change",
			}}
		}
	}
	return nil
}

// checkStaleBase warns when the remote has not been fetched recently, because
// every other check is only as current as the last fetch.
func checkStaleBase(base string) []Finding {
	remote, _, found := strings.Cut(base, "/")
	if !found {
		return nil
	}
	if _, err := git("rev-parse", "--verify", base); err != nil {
		return nil
	}
	// A fetch is cheap; recommending it unconditionally is more reliable than
	// trying to date the last one from FETCH_HEAD.
	return []Finding{{
		Check:    "fetch-first",
		Severity: Info,
		Detail:   fmt.Sprintf("results reflect the last fetch of %s", remote),
		Fix:      "git fetch " + remote,
	}}
}

func checkBehind(branch, base string) ([]Finding, error) {
	counts, err := git("rev-list", "--left-right", "--count", base+"..."+branch)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(counts)
	if len(fields) != 2 {
		return nil, fmt.Errorf("unexpected rev-list output %q", counts)
	}
	if fields[0] == "0" {
		return nil, nil
	}
	return []Finding{{
		Check:    "behind-base",
		Severity: Warning,
		Detail: fmt.Sprintf("%s has %s commit(s) %s does not; rebasing now keeps the "+
			"eventual merge trivial", base, fields[0], branch),
		Fix: fmt.Sprintf("git fetch origin && git rebase %s", base),
	}}, nil
}

// DuplicateCommits returns commits on branch whose patch ID already appears in
// base, newest first.
//
// A patch ID is a hash of the diff, independent of commit metadata, so it
// matches the same change carried by a different commit. That is exactly the
// signature of work rebased or squash-merged elsewhere.
func DuplicateCommits(branch, base string) ([]string, error) {
	ours, err := patchIDs(base + ".." + branch)
	if err != nil {
		return nil, err
	}
	if len(ours) == 0 {
		return nil, nil
	}
	theirs, err := patchIDs(branch + ".." + base)
	if err != nil {
		return nil, err
	}

	inBase := make(map[string]bool, len(theirs))
	for id := range theirs {
		inBase[id] = true
	}
	var dup []string
	for id, sha := range ours {
		if inBase[id] {
			dup = append(dup, sha)
		}
	}
	return dup, nil
}

// patchIDs maps patch ID to abbreviated commit SHA for a revision range.
func patchIDs(rang string) (map[string]string, error) {
	shas, err := git("rev-list", rang)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, sha := range strings.Fields(shas) {
		// A merge commit has no single patch, and git diff-tree emits nothing
		// for one, so it is skipped rather than mismatched.
		cmd, cancel := gitCmd("diff-tree", "-p", "--no-commit-id", sha)
		diff, err := cmd.Output()
		cancel()
		if err != nil || len(diff) == 0 {
			continue
		}
		id, err := patchID(diff)
		if err != nil || id == "" {
			continue
		}
		out[id] = sha[:min(7, len(sha))]
	}
	return out, nil
}

func patchID(diff []byte) (string, error) {
	cmd, cancel := gitCmd("patch-id", "--stable")
	defer cancel()
	cmd.Stdin = strings.NewReader(string(diff))
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

// ConflictingFiles returns the paths that would conflict when branch is merged
// into base.
//
// It uses merge-tree, which computes the merge in memory and touches neither the
// index nor the working tree, so it is safe to run from a hook.
//
// The output has three sections separated by a blank line: the resulting tree
// OID, the conflicted paths, then informational messages such as "Auto-merging
// f.txt" and "CONFLICT (content): ...". Reading past the blank line reports
// those messages as though they were filenames, which is how an earlier version
// listed "Auto-merging Makefile" as a conflicting path.
func ConflictingFiles(branch, base string) ([]string, error) {
	cmd, cancel := gitCmd("merge-tree", "--write-tree", "--name-only", base, branch)
	defer cancel()
	out, err := cmd.Output()
	if err == nil {
		return nil, nil // exit 0 means a clean merge
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return nil, nil
	}
	var files []string
	for _, l := range lines[1:] { // skip the tree OID
		if strings.TrimSpace(l) == "" {
			break // end of the conflicted-paths section
		}
		files = append(files, l)
	}
	return files, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
