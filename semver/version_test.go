package semver

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetNeverReturnsAnEmptyVersion(t *testing.T) {
	// An empty version string looks like a broken build to a user, so there must
	// always be a fallback.
	in := Get()
	if in.Version == "" {
		t.Fatal("Version must never be empty")
	}
	if in.Source == "" {
		t.Error("Source should record where the version came from")
	}
	if in.Go != runtime.Version() || in.OS != runtime.GOOS || in.Arch != runtime.GOARCH {
		t.Errorf("runtime fields wrong: %+v", in)
	}
}

func TestLdflagsTakePrecedence(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "v9.9.9"
	in := Get()
	if in.Version != "v9.9.9" || in.Source != "ldflags" {
		t.Errorf("got %q from %q", in.Version, in.Source)
	}
}

func TestStringIncludesShortCommit(t *testing.T) {
	origV, origC := Version, Commit
	t.Cleanup(func() { Version, Commit = origV, origC })

	Version, Commit = "v1.2.3", "0123456789abcdef"
	s := Get().String()
	if !strings.Contains(s, "v1.2.3") {
		t.Errorf("missing version: %q", s)
	}
	if !strings.Contains(s, "0123456") {
		t.Errorf("missing short commit: %q", s)
	}
	if strings.Contains(s, "0123456789abcdef") {
		t.Errorf("commit should be abbreviated: %q", s)
	}
}

func TestParseSemver(t *testing.T) {
	ok := map[string]Semver{
		"v1.2.3":         {1, 2, 3},
		"1.2.3":          {1, 2, 3},
		"  v0.0.1  ":     {0, 0, 1},
		"v1.2.3-rc1":     {1, 2, 3},
		"v1.2.3+build.5": {1, 2, 3},
	}
	for in, want := range ok {
		got, err := ParseSemver(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %+v want %+v", in, got, want)
		}
	}
	for _, bad := range []string{"", "v1", "v1.2", "1.2.3.4", "va.b.c", "v1.-2.3"} {
		if _, err := ParseSemver(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestApplyResetsLowerComponents(t *testing.T) {
	v := Semver{1, 4, 7}
	cases := map[Bump]Semver{
		Major: {2, 0, 0},
		Minor: {1, 5, 0},
		Patch: {1, 4, 8},
		None:  {1, 4, 7},
	}
	for b, want := range cases {
		if got := v.Apply(b); got != want {
			t.Errorf("%s: got %s want %s", b, got, want)
		}
	}
}

func TestMajorBumpIsNotSwallowedBelowOneZero(t *testing.T) {
	// Folding a breaking change into a minor bump on a 0.x line is how a
	// version series stops meaning anything.
	if got := (Semver{0, 3, 1}).Apply(Major); got != (Semver{1, 0, 0}) {
		t.Errorf("got %s want v1.0.0", got)
	}
}

func TestClassifyCommit(t *testing.T) {
	cases := []struct {
		subject string
		body    string
		want    Bump
	}{
		{"feat: add spank mode", "", Minor},
		{"feat(mode): compose fragments", "", Minor},
		{"fix: glob matcher dropped multi-segment tails", "", Patch},
		{"perf: stop walking node_modules", "", Patch},
		{"refactor: extract matcher", "", Patch},
		{"revert: undo the thing", "", Patch},
		{"docs: rewrite the hero", "", None},
		{"chore: bump deps", "", None},
		{"ci: add release workflow", "", None},
		{"test: cover the cap", "", None},
		{"feat!: rename mode install to agent install", "", Major},
		{"fix!: change the hook contract", "", Major},
		{"feat: something", "BREAKING CHANGE: config key renamed", Major},
		{"random unconventional subject", "", Patch},
	}
	for _, c := range cases {
		if got := ClassifyCommit(c.subject, c.body); got != c.want {
			t.Errorf("%q: got %s want %s", c.subject, got, c.want)
		}
	}
}

func TestUnrecognisedSubjectIsPatchNotNone(t *testing.T) {
	// Treating an unconventional subject as no change would let a real fix ship
	// without a version bump.
	if got := ClassifyCommit("made it better", ""); got != Patch {
		t.Errorf("got %s, want patch", got)
	}
}

func TestClassifyTakesTheLargestBump(t *testing.T) {
	commits := []Message{
		{Subject: "docs: tidy"},
		{Subject: "fix: a bug"},
		{Subject: "feat: a feature"},
		{Subject: "chore: noise"},
	}
	if got := Classify(commits); got != Minor {
		t.Errorf("got %s want minor", got)
	}

	commits = append(commits, Message{Subject: "feat!: breaking"})
	if got := Classify(commits); got != Major {
		t.Errorf("got %s want major", got)
	}

	if got := Classify([]Message{{Subject: "docs: only"}}); got != None {
		t.Errorf("docs-only should be none, got %s", got)
	}
	if got := Classify(nil); got != None {
		t.Errorf("no commits should be none, got %s", got)
	}
}
