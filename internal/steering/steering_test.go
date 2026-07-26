package steering

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardrailsCoverTheRequiredPolicy(t *testing.T) {
	g := strings.ToLower(Guardrails())
	// Each of these is a behaviour the guardrails exist to produce. A vague
	// "be careful" prompt yields an agent that either asks about everything or
	// nothing, so the decision procedure has to be explicit.
	required := []string{
		"additive",             // proceed without asking
		"behaviour-preserving", // proceed, run tests
		"behaviour-changing",   // stop and ask
		"subtractive",          // stop and ask
		"never delete or skip a test",
		"bias toward proceeding",
		"deliberately did not do",
	}
	for _, want := range required {
		if !strings.Contains(g, want) {
			t.Errorf("guardrails should cover %q", want)
		}
	}
}

func TestGuardrailsStartWithMarker(t *testing.T) {
	if !strings.HasPrefix(Guardrails(), Marker) {
		t.Error("marker must lead the file so IsManaged is reliable")
	}
}

func TestFilenameSortsEarly(t *testing.T) {
	// Steering files all load; the numeric prefix keeps ordering predictable
	// against whatever else the user has.
	if !strings.HasPrefix(Filename, "00-") {
		t.Errorf("expected a sort-early prefix, got %q", Filename)
	}
}

func TestInstallCreatesFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "steering")

	path, updated, err := Install(dir, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if updated {
		t.Error("first install is not an update")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), Marker) {
		t.Error("installed file is missing the marker")
	}
}

func TestInstallUpdatesItsOwnFileInPlace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "steering")
	path, _, err := Install(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an older kirobuff version's content, marker intact.
	if err := os.WriteFile(path, []byte(Marker+"\nold policy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, updated, err := Install(dir, false)
	if err != nil {
		t.Fatalf("managed file should update without force: %v", err)
	}
	if !updated {
		t.Error("expected updated=true")
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "old policy") {
		t.Error("managed file was not refreshed")
	}
}

func TestInstallRefusesToClobberHandWrittenFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "steering")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, Filename)
	if err := os.WriteFile(mine, []byte("# my own rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Install(dir, false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected ErrExist for an unmanaged file, got %v", err)
	}
	body, _ := os.ReadFile(mine)
	if string(body) != "# my own rules\n" {
		t.Error("hand-written file was overwritten")
	}

	if _, _, err := Install(dir, true); err != nil {
		t.Fatalf("force should overwrite: %v", err)
	}
}

func TestIsManaged(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "a.md")
	other := filepath.Join(dir, "b.md")
	os.WriteFile(managed, []byte(Marker+"\nx"), 0o644)
	os.WriteFile(other, []byte("# mine"), 0o644)

	if !IsManaged(managed) {
		t.Error("marker file should be managed")
	}
	if IsManaged(other) {
		t.Error("plain file should not be managed")
	}
	if IsManaged(filepath.Join(dir, "missing.md")) {
		t.Error("missing file should not be managed")
	}
}

func TestDirResolvesScopes(t *testing.T) {
	t.Setenv("KIRO_HOME", "")
	got, err := Dir(Global, "/home/u", "/ws")
	if err != nil || got != filepath.Join("/home/u", ".kiro", "steering") {
		t.Errorf("global: got %s err=%v", got, err)
	}
	got, err = Dir(Workspace, "/home/u", "/ws")
	if err != nil || got != filepath.Join("/ws", ".kiro", "steering") {
		t.Errorf("workspace: got %s err=%v", got, err)
	}
	if _, err := Dir("bogus", "/h", "/w"); err == nil {
		t.Error("unknown scope should error")
	}
}

func TestDirHonoursKiroHome(t *testing.T) {
	t.Setenv("KIRO_HOME", "/opt/kiro")
	got, _ := Dir(Global, "/home/u", "/ws")
	if got != filepath.Join("/opt/kiro", "steering") {
		t.Errorf("got %s", got)
	}
}

func TestVerifyFlagsDisabledInheritance(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cli.json")
	os.WriteFile(p, []byte(`{"chat.disableInheritingDefaultResources": true}`), 0o644)

	problems := Verify(p)
	if len(problems) == 0 {
		t.Fatal("disabled inheritance means custom agents silently lose the guardrails")
	}
	if !strings.Contains(problems[0], "will NOT") {
		t.Errorf("problem text should be unambiguous, got %q", problems[0])
	}
}

func TestVerifyQuietWhenInheritanceIsOn(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cli.json")
	os.WriteFile(p, []byte(`{"chat.defaultModel":"claude-opus-4.7"}`), 0o644)

	if problems := Verify(p); len(problems) != 0 {
		t.Errorf("expected no problems, got %v", problems)
	}
	// A missing settings file means defaults apply, which inherit.
	if problems := Verify(filepath.Join(dir, "absent.json")); len(problems) != 0 {
		t.Errorf("missing settings should be fine, got %v", problems)
	}
}
