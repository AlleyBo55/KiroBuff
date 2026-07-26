package preflight

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests build real git repositories in temp directories. preflight is a
// thin wrapper over git's own merge and patch-id machinery, so mocking git would
// only test that the mock agrees with itself.

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo returns a git repository with one commit on master, and chdirs into it.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "master")
	run(t, dir, "config", "user.email", "t@example.com")
	run(t, dir, "config", "user.name", "T")
	run(t, dir, "config", "commit.gpgsign", "false")

	write(t, dir, "README.md", "start\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-qm", "initial")

	// preflight compares against a remote-style ref; a local ref of the same
	// shape keeps the tests offline.
	run(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, name, body, msg string) {
	t.Helper()
	write(t, dir, name, body)
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-qm", msg)
}

func findingFor(r Report, check string) *Finding {
	for i := range r.Findings {
		if r.Findings[i].Check == check {
			return &r.Findings[i]
		}
	}
	return nil
}

func TestCleanBranchHasNoBlockers(t *testing.T) {
	dir := repo(t)
	run(t, dir, "switch", "-qc", "feat/x")
	commit(t, dir, "new.txt", "hello\n", "feat: add a file")

	r, err := Run("origin/master")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.Blocked() {
		t.Errorf("a clean branch must not be blocked: %+v", r.Findings)
	}
	if r.Branch != "feat/x" {
		t.Errorf("branch: %q", r.Branch)
	}
}

// The conflict this package exists for: a pull request squash-merged into the
// base leaves the base holding one commit whose content the branch still carries
// separately. Git then reports conflicts in files nobody edited twice.
func TestDetectsSquashMergeDuplicates(t *testing.T) {
	dir := repo(t)
	run(t, dir, "switch", "-qc", "feat/x")
	commit(t, dir, "a.txt", "first\n", "feat: first change")
	commit(t, dir, "b.txt", "second\n", "feat: second change")

	// Simulate the squash: master gains the first change as a brand-new commit.
	run(t, dir, "switch", "-q", "master")
	commit(t, dir, "a.txt", "first\n", "feat: first change (#1)")
	run(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")
	run(t, dir, "switch", "-q", "feat/x")

	dup, err := DuplicateCommits("feat/x", "origin/master")
	if err != nil {
		t.Fatalf("DuplicateCommits: %v", err)
	}
	if len(dup) != 1 {
		t.Fatalf("expected exactly the squashed commit, got %v", dup)
	}

	r, err := Run("origin/master")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	f := findingFor(r, "squash-duplicates")
	if f == nil {
		t.Fatalf("expected a squash-duplicates finding, got %+v", r.Findings)
	}
	if f.Severity != Blocker {
		t.Errorf("severity: %s", f.Severity)
	}
	if !strings.Contains(f.Fix, "rebase --onto origin/master") {
		t.Errorf("fix should be the exact rebase command, got %q", f.Fix)
	}
	if !r.Blocked() {
		t.Error("this must block the push")
	}
}

func TestNoDuplicatesWhenNothingWasSquashed(t *testing.T) {
	dir := repo(t)
	run(t, dir, "switch", "-qc", "feat/x")
	commit(t, dir, "a.txt", "mine\n", "feat: mine")

	run(t, dir, "switch", "-q", "master")
	commit(t, dir, "other.txt", "theirs\n", "feat: theirs")
	run(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")
	run(t, dir, "switch", "-q", "feat/x")

	dup, err := DuplicateCommits("feat/x", "origin/master")
	if err != nil {
		t.Fatal(err)
	}
	if len(dup) != 0 {
		t.Errorf("different work must not be reported as duplicate: %v", dup)
	}
}

func TestDetectsRealConflict(t *testing.T) {
	dir := repo(t)
	run(t, dir, "switch", "-qc", "feat/x")
	commit(t, dir, "shared.txt", "branch version\n", "feat: edit shared")

	run(t, dir, "switch", "-q", "master")
	commit(t, dir, "shared.txt", "master version\n", "feat: also edit shared")
	run(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")
	run(t, dir, "switch", "-q", "feat/x")

	files, err := ConflictingFiles("feat/x", "origin/master")
	if err != nil {
		t.Fatalf("ConflictingFiles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected a conflicting file")
	}
	var found bool
	for _, f := range files {
		if f == "shared.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected shared.txt, got %v", files)
	}

	r, _ := Run("origin/master")
	if f := findingFor(r, "merge-conflict"); f == nil || f.Severity != Blocker {
		t.Errorf("expected a blocking merge-conflict finding, got %+v", r.Findings)
	}
}

func TestNoConflictWhenFilesAreDisjoint(t *testing.T) {
	dir := repo(t)
	run(t, dir, "switch", "-qc", "feat/x")
	commit(t, dir, "mine.txt", "mine\n", "feat: mine")

	run(t, dir, "switch", "-q", "master")
	commit(t, dir, "theirs.txt", "theirs\n", "feat: theirs")
	run(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")
	run(t, dir, "switch", "-q", "feat/x")

	files, err := ConflictingFiles("feat/x", "origin/master")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("disjoint edits must not conflict, got %v", files)
	}
}

func TestWarnsWhenBehindBase(t *testing.T) {
	dir := repo(t)
	run(t, dir, "switch", "-qc", "feat/x")
	commit(t, dir, "mine.txt", "mine\n", "feat: mine")

	run(t, dir, "switch", "-q", "master")
	commit(t, dir, "theirs.txt", "theirs\n", "feat: theirs")
	run(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")
	run(t, dir, "switch", "-q", "feat/x")

	r, err := Run("origin/master")
	if err != nil {
		t.Fatal(err)
	}
	f := findingFor(r, "behind-base")
	if f == nil {
		t.Fatalf("expected behind-base, got %+v", r.Findings)
	}
	// Being behind is not itself a failure; only an unresolvable state is.
	if f.Severity != Warning {
		t.Errorf("being behind should warn, not block: %s", f.Severity)
	}
}

func TestBlocksDirectPushToProtectedBranch(t *testing.T) {
	repo(t)
	r, err := Run("origin/master")
	if err != nil {
		t.Fatal(err)
	}
	f := findingFor(r, "protected-branch")
	if f == nil || f.Severity != Blocker {
		t.Fatalf("pushing master directly should block, got %+v", r.Findings)
	}
	if !strings.Contains(f.Fix, "switch -c") {
		t.Errorf("fix should offer a branch, got %q", f.Fix)
	}
}

func TestMissingBaseIsAWarningNotACrash(t *testing.T) {
	dir := repo(t)
	run(t, dir, "switch", "-qc", "feat/x")

	r, err := Run("origin/does-not-exist")
	if err != nil {
		t.Fatalf("a missing base must not error: %v", err)
	}
	f := findingFor(r, "base-missing")
	if f == nil {
		t.Fatalf("expected base-missing, got %+v", r.Findings)
	}
	if f.Fix != "git fetch origin" {
		t.Errorf("fix: %q", f.Fix)
	}
}

func TestDefaultBasePrefersRemoteHead(t *testing.T) {
	dir := repo(t)
	run(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/master")
	if got := DefaultBase(); got != "origin/master" {
		t.Errorf("got %q", got)
	}
}

func TestEveryFindingCarriesAnActionableFix(t *testing.T) {
	// A finding without a command is a complaint. Info findings may be
	// advisory, but blockers and warnings must tell you what to run.
	dir := repo(t)
	run(t, dir, "switch", "-qc", "feat/x")
	commit(t, dir, "shared.txt", "branch\n", "feat: edit")
	run(t, dir, "switch", "-q", "master")
	commit(t, dir, "shared.txt", "master\n", "feat: also edit")
	run(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")
	run(t, dir, "switch", "-q", "feat/x")

	r, _ := Run("origin/master")
	if len(r.Findings) == 0 {
		t.Fatal("expected findings")
	}
	for _, f := range r.Findings {
		if f.Severity != Info && strings.TrimSpace(f.Fix) == "" {
			t.Errorf("%s is %s but offers no fix", f.Check, f.Severity)
		}
		if f.Detail == "" {
			t.Errorf("%s has no detail", f.Check)
		}
	}
}

func TestConflictingFilesExcludesInformationalMessages(t *testing.T) {
	// merge-tree emits three sections separated by a blank line: the tree OID,
	// the conflicted paths, then messages like "Auto-merging f.txt" and
	// "CONFLICT (content): ...". Reading past the separator reported those
	// messages as filenames, so a two-file conflict listed nine entries.
	dir := repo(t)
	run(t, dir, "switch", "-qc", "feat/x")
	commit(t, dir, "one.txt", "branch\n", "feat: one")
	commit(t, dir, "two.txt", "branch\n", "feat: two")

	run(t, dir, "switch", "-q", "master")
	commit(t, dir, "one.txt", "master\n", "feat: one differently")
	commit(t, dir, "two.txt", "master\n", "feat: two differently")
	run(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")
	run(t, dir, "switch", "-q", "feat/x")

	files, err := ConflictingFiles("feat/x", "origin/master")
	if err != nil {
		t.Fatalf("ConflictingFiles: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected exactly the 2 conflicting files, got %d: %v", len(files), files)
	}
	for _, f := range files {
		if strings.HasPrefix(f, "Auto-merging") || strings.HasPrefix(f, "CONFLICT") {
			t.Errorf("informational message reported as a path: %q", f)
		}
		if strings.Contains(f, " ") {
			t.Errorf("%q is a message, not a path", f)
		}
	}
}
