package budget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workspace builds a project with a large source tree and a skill directory.
func workspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()

	for _, d := range []string{
		filepath.Join(ws, "src", "deep"),
		filepath.Join(ws, ".kiro", "skills", "one"),
		filepath.Join(ws, ".kiro", "skills", "two"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// ~12 KB of source => ~3000 tokens, high severity.
	big := strings.Repeat("x", 4000)
	for _, p := range []string{
		filepath.Join(ws, "src", "a.go"),
		filepath.Join(ws, "src", "b.go"),
		filepath.Join(ws, "src", "deep", "c.go"),
	} {
		if err := os.WriteFile(p, []byte(big), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Small README => under the reporting floor.
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two skill manifests, each ~1.2 KB.
	manifest := "---\nname: s\ndescription: d\n---\n" + strings.Repeat("y", 1200)
	for _, n := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(ws, ".kiro", "skills", n, "SKILL.md"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func find(fs []Finding, rule string) *Finding {
	for i := range fs {
		if fs[i].Rule == rule {
			return &fs[i]
		}
	}
	return nil
}

func TestAlwaysLoadedGlobIsHighSeverity(t *testing.T) {
	ws := workspace(t)
	a := &Agent{Resources: []string{"file://src/**/*.go"}}

	got := Analyze(a, ws)
	f := find(got, "always-loaded")
	if f == nil {
		t.Fatalf("expected an always-loaded finding, got %+v", got)
	}
	if f.Severity != High {
		t.Errorf("severity: got %s, want high", f.Severity)
	}
	// 3 files x 4000 bytes / 4 = 3000 tokens.
	if f.TokensPerTurn != 3000 {
		t.Errorf("tokens: got %d, want 3000", f.TokensPerTurn)
	}
	if !strings.Contains(f.Detail, "3 files") {
		t.Errorf("detail should count matches, got %q", f.Detail)
	}
}

func TestSkillManifestsLoadedAsFilesSuggestSkillScheme(t *testing.T) {
	ws := workspace(t)
	a := &Agent{Resources: []string{"file://.kiro/skills/**/SKILL.md"}}

	f := find(Analyze(a, ws), "always-loaded")
	if f == nil {
		t.Fatal("expected a finding for skill manifests loaded as files")
	}
	if !strings.Contains(f.Fix, "skill://") {
		t.Errorf("fix should recommend skill://, got %q", f.Fix)
	}
}

func TestSkillSchemeIsNotCharged(t *testing.T) {
	ws := workspace(t)
	a := &Agent{Resources: []string{"skill://.kiro/skills/**/SKILL.md"}}

	if got := Analyze(a, ws); len(got) != 0 {
		t.Errorf("skill:// resources are on-demand and must not be charged, got %+v", got)
	}
}

func TestSmallResourceBelowFloorIsIgnored(t *testing.T) {
	ws := workspace(t)
	a := &Agent{Resources: []string{"file://README.md"}}

	if f := find(Analyze(a, ws), "always-loaded"); f != nil {
		t.Errorf("a 3-byte file should be below the reporting floor, got %+v", f)
	}
}

func TestDeadResourceIsReported(t *testing.T) {
	ws := workspace(t)
	a := &Agent{Resources: []string{"file://does/not/exist/*.rs"}}

	f := find(Analyze(a, ws), "dead-resource")
	if f == nil {
		t.Fatal("expected a dead-resource finding")
	}
	if f.TokensPerTurn != 0 {
		t.Errorf("dead resource costs nothing, got %d", f.TokensPerTurn)
	}
}

func TestUncachedUserPromptHookIsCharged(t *testing.T) {
	a := &Agent{Hooks: map[string][]Hook{
		"userPromptSubmit": {{Command: "git status"}},
	}}

	f := find(Analyze(a, ""), "uncached-hook")
	if f == nil {
		t.Fatal("expected an uncached-hook finding")
	}
	// Default max_output_size 10240 / 4 = 2560 tokens per turn.
	if f.TokensPerTurn != 2560 {
		t.Errorf("tokens: got %d, want 2560", f.TokensPerTurn)
	}
	if !strings.Contains(f.Fix, "cache_ttl_seconds") {
		t.Errorf("fix should mention caching, got %q", f.Fix)
	}
}

func TestCachedHookIsNotCharged(t *testing.T) {
	a := &Agent{Hooks: map[string][]Hook{
		"userPromptSubmit": {{Command: "git status", CacheTTLSeconds: 60}},
	}}
	if f := find(Analyze(a, ""), "uncached-hook"); f != nil {
		t.Errorf("cached hook must not be charged, got %+v", f)
	}
}

func TestAgentSpawnHookIsExempt(t *testing.T) {
	// agentSpawn runs once per session and is never cached by Kiro CLI, so
	// charging it per turn would be wrong.
	a := &Agent{Hooks: map[string][]Hook{
		"agentSpawn": {{Command: "git status"}},
	}}
	if f := find(Analyze(a, ""), "uncached-hook"); f != nil {
		t.Errorf("agentSpawn is a one-shot hook, got %+v", f)
	}
}

func TestWildcardToolSurfaceIsFlagged(t *testing.T) {
	a := &Agent{Tools: []string{"*"}}
	if f := find(Analyze(a, ""), "wide-tool-surface"); f == nil {
		t.Error("expected wide-tool-surface finding")
	}
}

func TestFindingsSortHighestCostFirst(t *testing.T) {
	ws := workspace(t)
	a := &Agent{
		Resources: []string{"file://does/not/exist/*.rs", "file://src/**/*.go"},
		Tools:     []string{"*"},
	}
	got := Analyze(a, ws)
	if len(got) < 3 {
		t.Fatalf("expected at least 3 findings, got %d", len(got))
	}
	if got[0].Severity != High {
		t.Errorf("first finding should be high severity, got %s", got[0].Severity)
	}
	if got[len(got)-1].Severity != Low {
		t.Errorf("last finding should be low severity, got %s", got[len(got)-1].Severity)
	}
}

func TestLoadParsesObjectFormatHooks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "reviewer.json")
	cfg := `{
	  "name": "reviewer",
	  "tools": ["read"],
	  "resources": ["file://README.md"],
	  "hooks": {"userPromptSubmit": [{"command": "date", "cache_ttl_seconds": 30}]}
	}`
	if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if a.Name != "reviewer" {
		t.Errorf("name: got %q", a.Name)
	}
	if len(a.Hooks["userPromptSubmit"]) != 1 {
		t.Fatalf("hooks not parsed: %+v", a.Hooks)
	}
	if a.Hooks["userPromptSubmit"][0].CacheTTLSeconds != 30 {
		t.Errorf("cache_ttl_seconds not parsed")
	}
}

func TestLoadToleratesArrayFormatHooks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "arr.json")
	cfg := `{"name":"arr","hooks":[{"trigger":"agentSpawn","action":{"type":"command","command":"date"}}]}`
	if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := Load(p)
	if err != nil {
		t.Fatalf("array-format hooks must not fail the load: %v", err)
	}
	if len(a.Hooks) != 0 {
		t.Errorf("array format carries no cache metadata, expected no parsed hooks")
	}
}

func TestTotalSumsRecurringCost(t *testing.T) {
	fs := []Finding{{TokensPerTurn: 100}, {TokensPerTurn: 250}, {TokensPerTurn: 0}}
	if got := Total(fs); got != 350 {
		t.Errorf("Total: got %d, want 350", got)
	}
}

// ------------------------------------------------- regressions

func TestMultiSegmentTailAfterDoubleStar(t *testing.T) {
	// An earlier matcher compared the pattern tail against the basename only, so
	// any pattern with separators after ** silently matched nothing and was
	// reported as a dead resource while the files existed.
	ws := t.TempDir()
	deep := filepath.Join(ws, "a", "x", "y", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "f.go"), []byte(strings.Repeat("z", 800)), 0o644); err != nil {
		t.Fatal(err)
	}

	n, bytes := measure("a/**/b/*.go", ws)
	if n != 1 {
		t.Fatalf("expected 1 match for a/**/b/*.go, got %d", n)
	}
	if bytes != 800 {
		t.Errorf("size: got %d want 800", bytes)
	}
}

func TestDoubleStarMatchesZeroSegments(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	// src/**/*.go must match src/a.go, not only src/deeper/a.go.
	os.WriteFile(filepath.Join(ws, "src", "a.go"), []byte("x"), 0o644)
	if n, _ := measure("src/**/*.go", ws); n != 1 {
		t.Errorf("** should match zero segments, got %d matches", n)
	}
}

func TestDependencyDirectoriesArePruned(t *testing.T) {
	ws := t.TempDir()
	for _, d := range []string{"node_modules/pkg", ".git/objects", "vendor/lib", "src"} {
		os.MkdirAll(filepath.Join(ws, d), 0o755)
		os.WriteFile(filepath.Join(ws, d, "f.go"), []byte("x"), 0o644)
	}
	n, _ := measure("**/*.go", ws)
	if n != 1 {
		t.Errorf("expected only src/f.go, got %d matches (dependency dirs not pruned)", n)
	}
}

func TestExplicitlyNamedDirectoryIsNotPruned(t *testing.T) {
	// Pruning must not create a false "matches no files" for someone who asked
	// for a pruned directory by name.
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "dist", "sub"), 0o755)
	os.WriteFile(filepath.Join(ws, "dist", "sub", "a.js"), []byte(strings.Repeat("q", 400)), 0o644)

	if n, _ := measure("dist/**/*.js", ws); n != 1 {
		t.Errorf("an explicitly named dist/ must still resolve, got %d", n)
	}
}

func TestPluralHandlesNegativeAndZero(t *testing.T) {
	// The previous hand-rolled itoa returned "" for any negative number.
	for in, want := range map[int]string{0: "0 files", 1: "1 file", -5: "-5 files"} {
		if got := plural(in, "file"); got != want {
			t.Errorf("plural(%d): got %q want %q", in, got, want)
		}
	}
}
