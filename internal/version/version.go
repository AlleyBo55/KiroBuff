// Package version reports the build's identity and classifies changes for
// semantic versioning.
//
// Version, Commit and Date are injected at link time. They stay unset in a
// plain `go build`, and `go install ...@vX.Y.Z` fills Version from the module
// version, so the fallback chain matters more than it looks.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// Injected via -ldflags at release time. See the Makefile.
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

// Info is the resolved build identity.
type Info struct {
	Version string
	Commit  string
	Date    string
	Go      string
	OS      string
	Arch    string
	Source  string // where Version came from, so an unexpected value is traceable
}

// Get resolves build information.
//
// Precedence: linker flags, then the module version recorded by `go install`,
// then a dev placeholder. Without the module fallback, `go install @latest`
// would report an empty version, which looks like a broken build.
func Get() Info {
	in := Info{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Source:  "ldflags",
	}
	if in.Version == "" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			in.Version = bi.Main.Version
			in.Source = "module"
		}
	}
	if in.Version == "" {
		in.Version = "dev"
		in.Source = "unset"
	}
	if in.Commit == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				if s.Key == "vcs.revision" {
					in.Commit = s.Value
				}
				if s.Key == "vcs.time" && in.Date == "" {
					in.Date = s.Value
				}
			}
		}
	}
	return in
}

// String renders a one-line summary.
func (i Info) String() string {
	s := "kirobuff " + i.Version
	if i.Commit != "" {
		c := i.Commit
		if len(c) > 7 {
			c = c[:7]
		}
		s += " (" + c + ")"
	}
	return s + fmt.Sprintf(" %s/%s %s", i.OS, i.Arch, i.Go)
}

// ---------------------------------------------------------------- semver

// Bump is the size of a version increment.
type Bump string

const (
	None  Bump = "none"
	Patch Bump = "patch"
	Minor Bump = "minor"
	Major Bump = "major"
)

// Semver is a parsed version.
type Semver struct {
	Major, Minor, Patch int
}

// ParseSemver accepts vX.Y.Z or X.Y.Z.
func ParseSemver(s string) (Semver, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	if s == "" {
		return Semver{}, fmt.Errorf("empty version")
	}
	// Discard any pre-release or build metadata before splitting.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Semver{}, fmt.Errorf("not a three-part version: %q", s)
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Semver{}, fmt.Errorf("bad component %q", p)
		}
		out[i] = n
	}
	return Semver{out[0], out[1], out[2]}, nil
}

func (v Semver) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Apply returns the version after a bump.
//
// A major bump below 1.0.0 is deliberately still a major bump rather than being
// folded into minor: the caller decides when to reach 1.0.0, and silently
// downgrading a breaking change is how a 0.x line becomes untrustworthy.
func (v Semver) Apply(b Bump) Semver {
	switch b {
	case Major:
		return Semver{v.Major + 1, 0, 0}
	case Minor:
		return Semver{v.Major, v.Minor + 1, 0}
	case Patch:
		return Semver{v.Major, v.Minor, v.Patch + 1}
	}
	return v
}

// ClassifyCommit maps one commit subject to the bump it implies, using
// Conventional Commits.
//
// Anything unrecognised counts as a patch rather than nothing, so an
// unconventional subject can never cause a release to omit a real change.
func ClassifyCommit(subject, body string) Bump {
	s := strings.TrimSpace(subject)
	lower := strings.ToLower(s)

	// A breaking change is either a ! before the colon or a BREAKING CHANGE
	// footer.
	if i := strings.Index(s, ":"); i > 0 && strings.Contains(s[:i], "!") {
		return Major
	}
	if strings.Contains(strings.ToUpper(body), "BREAKING CHANGE") {
		return Major
	}

	switch {
	case strings.HasPrefix(lower, "feat"):
		return Minor
	case strings.HasPrefix(lower, "fix"),
		strings.HasPrefix(lower, "perf"),
		strings.HasPrefix(lower, "refactor"),
		strings.HasPrefix(lower, "revert"):
		return Patch
	case strings.HasPrefix(lower, "docs"),
		strings.HasPrefix(lower, "test"),
		strings.HasPrefix(lower, "chore"),
		strings.HasPrefix(lower, "ci"),
		strings.HasPrefix(lower, "style"),
		strings.HasPrefix(lower, "build"):
		return None
	}
	return Patch
}

// Classify reduces a set of commit messages to the single largest bump they
// imply.
func Classify(commits []Message) Bump {
	rank := map[Bump]int{None: 0, Patch: 1, Minor: 2, Major: 3}
	best := None
	for _, c := range commits {
		if b := ClassifyCommit(c.Subject, c.Body); rank[b] > rank[best] {
			best = b
		}
	}
	return best
}

// Message is one commit's subject and body.
//
// Named Message rather than Commit because Commit is the injected build SHA and
// a type sharing that name shadows it inside this package.
type Message struct {
	Subject string
	Body    string
}
