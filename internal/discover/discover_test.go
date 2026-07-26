package discover

import (
	"os"
	"path/filepath"
	"testing"
)

// buildTree lays out a miniature version of both harnesses inside a temp dir,
// including the shared-symlink arrangement that already exists in the wild.
func buildTree(t *testing.T) Layout {
	t.Helper()
	home := t.TempDir()
	ws := filepath.Join(home, "project")

	mkdirs := []string{
		filepath.Join(home, ".claude", "commands"),
		filepath.Join(home, ".claude", "agents"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".kiro", "prompts"),
		filepath.Join(home, ".kiro", "skills"),
		filepath.Join(home, ".kiro", "settings"),
		filepath.Join(home, ".agents", "skills", "find-skills"),
		filepath.Join(ws, ".claude", "commands"),
		filepath.Join(ws, ".kiro", "agents"),
	}
	for _, d := range mkdirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(home, ".claude", "CLAUDE.md"):                         "# user memory\n",
		filepath.Join(home, ".claude", "settings.json"):                     `{"permissions":{"deny":["Read(./.env)"]}}`,
		filepath.Join(home, ".claude.json"):                                 `{"projects":{}}`,
		filepath.Join(home, ".claude", "commands", "review.md"):             "Review $ARGUMENTS\n",
		filepath.Join(home, ".claude", "commands", "ship.md"):               "Ship it\n",
		filepath.Join(home, ".claude", "commands", "notes.txt"):             "ignored, wrong extension\n",
		filepath.Join(home, ".claude", "agents", "reviewer.md"):             "---\nname: reviewer\n---\n",
		filepath.Join(home, ".kiro", "settings", "cli.json"):                `{"chat.defaultModel":"claude-opus-4.7"}`,
		filepath.Join(home, ".kiro", "prompts", "carbon-review.md"):         "Review\n",
		filepath.Join(home, ".agents", "skills", "find-skills", "SKILL.md"): "---\nname: find-skills\n---\n",
		filepath.Join(ws, "CLAUDE.md"):                                      "# project memory\n",
		filepath.Join(ws, "AGENTS.md"):                                      "# shared memory\n",
		filepath.Join(ws, ".claude", "commands", "deploy.md"):               "Deploy\n",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	// The shared-skill arrangement: one authored skill, linked into both trees.
	for _, link := range []string{
		filepath.Join(home, ".claude", "skills", "find-skills"),
		filepath.Join(home, ".kiro", "skills", "find-skills"),
	} {
		if err := os.Symlink(filepath.Join(home, ".agents", "skills", "find-skills"), link); err != nil {
			t.Fatalf("symlink %s: %v", link, err)
		}
	}

	return Layout{
		Home:       home,
		Workspace:  ws,
		ClaudeHome: filepath.Join(home, ".claude"),
		KiroHome:   filepath.Join(home, ".kiro"),
		SharedRoot: filepath.Join(home, ".agents"),
	}
}

func count(arts []Artifact, h Harness, k Kind) int {
	n := 0
	for _, a := range arts {
		if a.Harness == h && a.Kind == k {
			n++
		}
	}
	return n
}

func TestScanFindsBothHarnesses(t *testing.T) {
	l := buildTree(t)
	arts, err := Scan(l)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	cases := []struct {
		name string
		h    Harness
		k    Kind
		want int
	}{
		// review.md + ship.md at user scope, deploy.md at workspace scope.
		// notes.txt must be excluded by the *.md glob.
		{"claude commands", ClaudeCode, KindCommand, 3},
		{"claude agents", ClaudeCode, KindAgent, 1},
		{"claude memory", ClaudeCode, KindMemory, 2}, // user + workspace CLAUDE.md
		{"claude settings", ClaudeCode, KindSettings, 1},
		{"claude mcp", ClaudeCode, KindMCP, 1}, // ~/.claude.json
		{"kiro prompts", KiroCLI, KindCommand, 1},
		{"kiro settings", KiroCLI, KindSettings, 1},
		{"shared memory", Shared, KindMemory, 1}, // AGENTS.md
	}
	for _, c := range cases {
		if got := count(arts, c.h, c.k); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSkillSymlinksAreDetectedAsShared(t *testing.T) {
	l := buildTree(t)
	arts, err := Scan(l)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var linked int
	for _, a := range arts {
		if a.Kind != KindSkill || a.Harness == Shared {
			continue
		}
		if !a.IsSymlink {
			t.Errorf("%s: expected a symlink", a.Path)
			continue
		}
		if !a.SharedLink(l.SharedRoot) {
			t.Errorf("%s: target %q not recognised as shared", a.Path, a.LinkTarget)
			continue
		}
		linked++
	}
	if linked != 2 {
		t.Errorf("expected 2 harness-side shared skill links, got %d", linked)
	}
}

func TestSkillDirWithoutManifestIsIgnored(t *testing.T) {
	l := buildTree(t)
	// A bare directory is not a skill until it contains SKILL.md.
	if err := os.MkdirAll(filepath.Join(l.ClaudeHome, "skills", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	arts, err := Scan(l)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, a := range arts {
		if filepath.Base(a.Path) == "empty" {
			t.Fatalf("directory without SKILL.md was reported as a skill: %s", a.Path)
		}
	}
}

func TestMissingPathsAreNotErrors(t *testing.T) {
	l := Layout{
		Home:       filepath.Join(t.TempDir(), "nonexistent"),
		ClaudeHome: filepath.Join(t.TempDir(), "nonexistent", ".claude"),
		KiroHome:   filepath.Join(t.TempDir(), "nonexistent", ".kiro"),
		SharedRoot: filepath.Join(t.TempDir(), "nonexistent", ".agents"),
	}
	arts, err := Scan(l)
	if err != nil {
		t.Fatalf("Scan on empty layout should not error: %v", err)
	}
	if len(arts) != 0 {
		t.Errorf("expected no artifacts, got %d", len(arts))
	}
}

func TestDefaultLayoutUsesKIROHOME(t *testing.T) {
	t.Setenv("KIRO_HOME", "/custom/kiro")
	l, err := DefaultLayout("/some/workspace")
	if err != nil {
		t.Fatal(err)
	}
	if l.KiroHome != "/custom/kiro" {
		t.Errorf("KiroHome=%q, want /custom/kiro", l.KiroHome)
	}
	if l.Workspace != "/some/workspace" {
		t.Errorf("Workspace=%q, want /some/workspace", l.Workspace)
	}
}

func TestDefaultLayoutFallsBackToHome(t *testing.T) {
	t.Setenv("KIRO_HOME", "")
	l, err := DefaultLayout("")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if l.KiroHome != filepath.Join(home, ".kiro") {
		t.Errorf("KiroHome=%q, want %q", l.KiroHome, filepath.Join(home, ".kiro"))
	}
}

func TestSharedLinkReturnsFalseForNonSymlink(t *testing.T) {
	a := Artifact{
		Path:      "/some/path",
		IsSymlink: false,
	}
	if a.SharedLink("/some/root") {
		t.Error("non-symlink should return false")
	}
}

func TestSharedLinkReturnsFalseForEmptyTarget(t *testing.T) {
	a := Artifact{
		Path:       "/some/path",
		IsSymlink:  true,
		LinkTarget: "",
	}
	if a.SharedLink("/some/root") {
		t.Error("empty LinkTarget should return false")
	}
}

func TestSharedLinkReturnsFalseWhenTargetOutsideRoot(t *testing.T) {
	a := Artifact{
		Path:       "/home/user/.kiro/skills/x",
		IsSymlink:  true,
		LinkTarget: "/home/user/elsewhere/x",
	}
	if a.SharedLink("/home/user/.agents") {
		t.Error("target outside shared root should return false")
	}
}

func TestSharedLinkReturnsTrueWhenTargetInsideRoot(t *testing.T) {
	tmp := t.TempDir()
	shared := filepath.Join(tmp, "agents", "skills", "x")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	a := Artifact{
		Path:       filepath.Join(tmp, "kiro", "skills", "x"),
		IsSymlink:  true,
		LinkTarget: shared,
	}
	if !a.SharedLink(filepath.Join(tmp, "agents")) {
		t.Error("target inside shared root should return true")
	}
}

func TestWithinRejectsParentPaths(t *testing.T) {
	if within("/a/b", "/a") {
		t.Error("/a should not be within /a/b")
	}
	if within("/a/b", "/a/c") {
		t.Error("/a/c should not be within /a/b")
	}
}

func TestWithinAcceptsSelfAndChild(t *testing.T) {
	if !within("/a/b", "/a/b") {
		t.Error("/a/b should be within /a/b (self)")
	}
	if !within("/a/b", "/a/b/c") {
		t.Error("/a/b/c should be within /a/b")
	}
}

func TestCanonicalResolvesCleanPath(t *testing.T) {
	tmp := t.TempDir()
	// canonical should at minimum clean the path
	got := canonical(filepath.Join(tmp, ".", "sub"))
	if filepath.Base(got) != "sub" {
		t.Errorf("canonical did not clean: %q", got)
	}
}

func TestScanWithWorkspaceFindsWorkspaceArtifacts(t *testing.T) {
	l := buildTree(t)
	arts, err := Scan(l)
	if err != nil {
		t.Fatal(err)
	}
	var wsArts int
	for _, a := range arts {
		if a.Scope == ScopeWorkspace {
			wsArts++
		}
	}
	if wsArts == 0 {
		t.Error("expected some workspace-scoped artifacts")
	}
}

func TestScanWithoutWorkspaceSkipsWorkspaceProbes(t *testing.T) {
	l := buildTree(t)
	l.Workspace = ""
	arts, err := Scan(l)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range arts {
		if a.Scope == ScopeWorkspace {
			t.Errorf("found workspace artifact %s with empty workspace", a.Path)
		}
	}
}
