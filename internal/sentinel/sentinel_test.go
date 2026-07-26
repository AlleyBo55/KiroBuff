package sentinel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ws(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mk(t, dir, "internal/a_test.go", `func TestA(t *testing.T){ t.Errorf("x"); t.Fatal("y") }`)
	mk(t, dir, "internal/b_test.go", `func TestB(t *testing.T){ assert.Equal(1,2) }`)
	mk(t, dir, "internal/prod.go", `func Prod() {}`) // not a test, must not count
	return dir
}

func mk(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanCountsOnlyTests(t *testing.T) {
	m, err := Scan(ws(t))
	if err != nil {
		t.Fatal(err)
	}
	if m.Files != 2 {
		t.Errorf("files: got %d want 2", m.Files)
	}
	if m.Assertions != 3 {
		t.Errorf("assertions: got %d want 3", m.Assertions)
	}
}

func TestScanPrunesDependencyDirectories(t *testing.T) {
	dir := ws(t)
	mk(t, dir, "node_modules/pkg/x_test.go", `t.Errorf("vendor noise")`)
	mk(t, dir, ".git/hooks/y_test.go", `t.Errorf("git noise")`)

	m, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Files != 2 {
		t.Errorf("dependency dirs were scanned: got %d files", m.Files)
	}
}

func TestFirstRunRecordsABaseline(t *testing.T) {
	dir := ws(t)
	v, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !v.FirstRun {
		t.Error("expected FirstRun")
	}
	if v.Regressed {
		t.Error("a baseline is not a regression")
	}
	if _, err := os.Stat(filepath.Join(dir, StateFile)); err != nil {
		t.Errorf("state not written: %v", err)
	}
}

// The reason this package exists: preToolUse blocks rm and git rm, but mv,
// unlink and find -delete all got through. The sentinel never looks at the
// command, so every one of them is caught.
func TestCatchesDeletionByAnyMethod(t *testing.T) {
	for _, method := range []string{"delete the file", "move it out of the tree", "truncate it"} {
		dir := ws(t)
		if _, err := Check(dir); err != nil { // baseline
			t.Fatal(err)
		}

		target := filepath.Join(dir, "internal", "a_test.go")
		switch method {
		case "delete the file":
			os.Remove(target)
		case "move it out of the tree":
			os.Rename(target, filepath.Join(t.TempDir(), "a_test.go"))
		case "truncate it":
			os.WriteFile(target, []byte("package internal\n"), 0o644)
		}

		v, err := Check(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Regressed {
			t.Errorf("%s: not detected (%+v)", method, v)
		}
		if v.AssertLost != 2 {
			t.Errorf("%s: lost %d assertions, want 2", method, v.AssertLost)
		}
	}
}

func TestPeakOnlyRises(t *testing.T) {
	// If a drop became the new baseline, coverage could ratchet down one turn at
	// a time without ever producing a warning.
	dir := ws(t)
	if _, err := Check(dir); err != nil {
		t.Fatal(err)
	}

	os.Remove(filepath.Join(dir, "internal", "a_test.go"))
	first, _ := Check(dir)
	if !first.Regressed {
		t.Fatal("expected the first drop to be reported")
	}

	// Check again with nothing restored: still a regression, not forgiven.
	second, _ := Check(dir)
	if !second.Regressed {
		t.Error("a drop must stay reported until it is fixed or accepted")
	}
	if second.Peak.Assertions != first.Peak.Assertions {
		t.Errorf("peak moved: %d -> %d", first.Peak.Assertions, second.Peak.Assertions)
	}
}

func TestAddingTestsRaisesThePeak(t *testing.T) {
	dir := ws(t)
	if _, err := Check(dir); err != nil {
		t.Fatal(err)
	}
	mk(t, dir, "internal/c_test.go", `t.Errorf("new"); t.Errorf("more")`)

	v, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v.Regressed {
		t.Errorf("adding tests is not a regression: %+v", v)
	}
	s, _, _ := Load(dir)
	if s.Peak.Assertions != 5 {
		t.Errorf("peak should rise to 5, got %d", s.Peak.Assertions)
	}
}

func TestAcceptLowersThePeakDeliberately(t *testing.T) {
	// Deleting a genuinely obsolete test is legitimate, so there must be a way
	// to say so explicitly.
	dir := ws(t)
	if _, err := Check(dir); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "internal", "a_test.go"))

	if v, _ := Check(dir); !v.Regressed {
		t.Fatal("expected a regression first")
	}
	if _, err := Accept(dir); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	v, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v.Regressed {
		t.Errorf("after Accept the new level is the baseline: %+v", v)
	}
}

func TestCorruptStateReBaselinesInsteadOfFailing(t *testing.T) {
	// A hook that errors on a malformed state file would break every turn.
	dir := ws(t)
	p := filepath.Join(dir, StateFile)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := Check(dir)
	if err != nil {
		t.Fatalf("corrupt state must not error: %v", err)
	}
	if !v.FirstRun {
		t.Error("corrupt state should re-baseline visibly")
	}
}

func TestDetailExplainsWhatWasLost(t *testing.T) {
	dir := ws(t)
	if _, err := Check(dir); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "internal", "a_test.go"))
	v, _ := Check(dir)

	d := v.Detail()
	for _, want := range []string{"dropped", "1 test file", "2 assertion", "peak"} {
		if !strings.Contains(d, want) {
			t.Errorf("Detail() should mention %q, got %q", want, d)
		}
	}
}

func TestStateFileIsProtectedFromTheAgent(t *testing.T) {
	// Whoever can lower the baseline can defeat the check, so the state file
	// must be in enforce's protected paths. Asserted here rather than in
	// enforce, because this package owns the path.
	if !strings.HasPrefix(StateFile, ".kiro/") {
		t.Errorf("StateFile must live under .kiro to be protectable, got %q", StateFile)
	}
}

func TestSymlinkedTestsAreNotCounted(t *testing.T) {
	// Reading through a symlink could take the scan outside the workspace
	// between the walk and the read, and a symlinked test would be counted
	// twice.
	dir := ws(t)
	outside := filepath.Join(t.TempDir(), "elsewhere_test.go")
	if err := os.WriteFile(outside, []byte(`t.Errorf("a");t.Errorf("b");t.Errorf("c")`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "internal", "linked_test.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	m, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Files != 2 || m.Assertions != 3 {
		t.Errorf("symlink was followed: got %d files / %d assertions, want 2 / 3", m.Files, m.Assertions)
	}
}

func TestDetailFirstRun(t *testing.T) {
	v := Verdict{FirstRun: true, Current: Measurement{Files: 5, Assertions: 10}}
	d := v.Detail()
	if !strings.Contains(d, "baseline") {
		t.Errorf("FirstRun detail should mention baseline, got %q", d)
	}
	if !strings.Contains(d, "5 test file") {
		t.Errorf("detail should mention file count, got %q", d)
	}
}

func TestDetailNoRegression(t *testing.T) {
	v := Verdict{
		Current: Measurement{Files: 5, Assertions: 10},
		Peak:    Measurement{Files: 5, Assertions: 10},
	}
	d := v.Detail()
	if !strings.Contains(d, "no loss") {
		t.Errorf("non-regression detail should say no loss, got %q", d)
	}
}

func TestDetailOnlyFilesLost(t *testing.T) {
	v := Verdict{
		Regressed:  true,
		FilesLost:  2,
		AssertLost: 0,
		Current:    Measurement{Files: 3, Assertions: 10},
		Peak:       Measurement{Files: 5, Assertions: 10},
	}
	d := v.Detail()
	if !strings.Contains(d, "2 test file") {
		t.Errorf("should mention files lost, got %q", d)
	}
}

func TestDetailOnlyAssertionsLost(t *testing.T) {
	v := Verdict{
		Regressed:  true,
		FilesLost:  0,
		AssertLost: 3,
		Current:    Measurement{Files: 5, Assertions: 7},
		Peak:       Measurement{Files: 5, Assertions: 10},
	}
	d := v.Detail()
	if !strings.Contains(d, "3 assertion") {
		t.Errorf("should mention assertions lost, got %q", d)
	}
	if strings.Contains(d, "test file(s)") {
		t.Errorf("should NOT mention files when none lost, got %q", d)
	}
}

func TestSaveCreatesDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep", "nested", "project")
	err := Save(dir, State{Peak: Measurement{Files: 1, Assertions: 2}})
	if err != nil {
		t.Fatalf("Save should create dirs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, StateFile)); err != nil {
		t.Errorf("state file not created: %v", err)
	}
}

func TestLoadNonExistentReturnsZero(t *testing.T) {
	dir := t.TempDir()
	s, existed, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Error("should not exist")
	}
	if s.Peak.Files != 0 || s.Peak.Assertions != 0 {
		t.Error("should return zero state")
	}
}
