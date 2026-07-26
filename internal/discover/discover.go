// Package discover locates agent-configuration artifacts belonging to
// Claude Code and Kiro CLI, in both the user (home) and workspace scopes.
//
// It only reads metadata: paths, kinds, and whether an entry is a symlink.
// Translation and writing live in other packages so that discovery stays
// safe to run anywhere, including against directories you do not own.
package discover

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Harness identifies which tool an artifact belongs to.
type Harness string

// The harnesses kirobuff knows how to read.
const (
	ClaudeCode Harness = "claude-code"
	KiroCLI    Harness = "kiro-cli"
	Shared     Harness = "shared"
)

// Kind classifies an artifact by the role it plays, independent of harness.
// Two artifacts with the same Kind are candidates for translation.
type Kind string

// The artifact roles, independent of harness.
const (
	KindMemory   Kind = "memory"   // CLAUDE.md, AGENTS.md, steering files
	KindCommand  Kind = "command"  // .claude/commands/*.md, .kiro/prompts/*.md
	KindAgent    Kind = "agent"    // subagent / agent definitions
	KindSkill    Kind = "skill"    // SKILL.md with YAML frontmatter
	KindSettings Kind = "settings" // settings.json, cli.json
	KindMCP      Kind = "mcp"      // MCP server declarations
)

// Scope distinguishes user-level from workspace-level configuration.
type Scope string

// Configuration scopes.
const (
	ScopeUser      Scope = "user"
	ScopeWorkspace Scope = "workspace"
)

// Artifact is a single configuration file discovered on disk.
type Artifact struct {
	Path       string
	Harness    Harness
	Kind       Kind
	Scope      Scope
	IsSymlink  bool
	LinkTarget string // resolved target when IsSymlink is true
	SizeBytes  int64
}

// SharedLink reports whether the artifact is a symlink pointing into the
// shared root (~/.agents). Shared artifacts need no translation: both
// harnesses already read the same bytes.
//
// Both sides are canonicalised first. On macOS a home or temp directory may
// itself sit behind a symlink (/var -> /private/var), in which case the
// resolved link target and the raw shared root would otherwise never match.
func (a Artifact) SharedLink(sharedRoot string) bool {
	if !a.IsSymlink || a.LinkTarget == "" {
		return false
	}
	return within(canonical(sharedRoot), canonical(a.LinkTarget))
}

// canonical resolves symlinks where possible, falling back to a cleaned
// absolute path so that a missing directory still compares sensibly.
func canonical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// within reports whether child is root itself or lives underneath it.
func within(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Layout describes where each harness keeps its files. Overridable so tests
// can run against a temp dir and so KIRO_HOME is respected.
type Layout struct {
	Home       string // user home
	Workspace  string // current project root, may be empty
	ClaudeHome string // default: <Home>/.claude
	KiroHome   string // default: <Home>/.kiro, or $KIRO_HOME
	SharedRoot string // default: <Home>/.agents
}

// DefaultLayout builds a Layout from the environment.
func DefaultLayout(workspace string) (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	kiro := os.Getenv("KIRO_HOME")
	if kiro == "" {
		kiro = filepath.Join(home, ".kiro")
	}
	return Layout{
		Home:       home,
		Workspace:  workspace,
		ClaudeHome: filepath.Join(home, ".claude"),
		KiroHome:   kiro,
		SharedRoot: filepath.Join(home, ".agents"),
	}, nil
}

// probe is one place to look, and what to call whatever is found there.
type probe struct {
	path    string
	harness Harness
	kind    Kind
	scope   Scope
	dir     bool // when true, walk the directory instead of stat-ing a file
	glob    string
}

func (l Layout) probes() []probe {
	var p []probe

	// --- user scope -------------------------------------------------------
	p = append(p,
		probe{path: filepath.Join(l.ClaudeHome, "CLAUDE.md"), harness: ClaudeCode, kind: KindMemory, scope: ScopeUser},
		probe{path: filepath.Join(l.ClaudeHome, "settings.json"), harness: ClaudeCode, kind: KindSettings, scope: ScopeUser},
		probe{path: filepath.Join(l.Home, ".claude.json"), harness: ClaudeCode, kind: KindMCP, scope: ScopeUser},
		probe{path: filepath.Join(l.ClaudeHome, "commands"), harness: ClaudeCode, kind: KindCommand, scope: ScopeUser, dir: true, glob: "*.md"},
		probe{path: filepath.Join(l.ClaudeHome, "agents"), harness: ClaudeCode, kind: KindAgent, scope: ScopeUser, dir: true, glob: "*.md"},
		probe{path: filepath.Join(l.ClaudeHome, "skills"), harness: ClaudeCode, kind: KindSkill, scope: ScopeUser, dir: true},

		probe{path: filepath.Join(l.KiroHome, "agents"), harness: KiroCLI, kind: KindAgent, scope: ScopeUser, dir: true, glob: "*.json"},
		probe{path: filepath.Join(l.KiroHome, "prompts"), harness: KiroCLI, kind: KindCommand, scope: ScopeUser, dir: true, glob: "*.md"},
		probe{path: filepath.Join(l.KiroHome, "skills"), harness: KiroCLI, kind: KindSkill, scope: ScopeUser, dir: true},
		probe{path: filepath.Join(l.KiroHome, "steering"), harness: KiroCLI, kind: KindMemory, scope: ScopeUser, dir: true, glob: "*.md"},
		probe{path: filepath.Join(l.KiroHome, "settings", "cli.json"), harness: KiroCLI, kind: KindSettings, scope: ScopeUser},

		probe{path: filepath.Join(l.SharedRoot, "skills"), harness: Shared, kind: KindSkill, scope: ScopeUser, dir: true},
	)

	// --- workspace scope --------------------------------------------------
	if l.Workspace != "" {
		w := l.Workspace
		p = append(p,
			probe{path: filepath.Join(w, "CLAUDE.md"), harness: ClaudeCode, kind: KindMemory, scope: ScopeWorkspace},
			probe{path: filepath.Join(w, "AGENTS.md"), harness: Shared, kind: KindMemory, scope: ScopeWorkspace},
			probe{path: filepath.Join(w, ".mcp.json"), harness: ClaudeCode, kind: KindMCP, scope: ScopeWorkspace},
			probe{path: filepath.Join(w, ".claude", "settings.json"), harness: ClaudeCode, kind: KindSettings, scope: ScopeWorkspace},
			probe{path: filepath.Join(w, ".claude", "commands"), harness: ClaudeCode, kind: KindCommand, scope: ScopeWorkspace, dir: true, glob: "*.md"},
			probe{path: filepath.Join(w, ".claude", "agents"), harness: ClaudeCode, kind: KindAgent, scope: ScopeWorkspace, dir: true, glob: "*.md"},
			probe{path: filepath.Join(w, ".claude", "skills"), harness: ClaudeCode, kind: KindSkill, scope: ScopeWorkspace, dir: true},

			probe{path: filepath.Join(w, ".kiro", "agents"), harness: KiroCLI, kind: KindAgent, scope: ScopeWorkspace, dir: true, glob: "*.json"},
			probe{path: filepath.Join(w, ".kiro", "prompts"), harness: KiroCLI, kind: KindCommand, scope: ScopeWorkspace, dir: true, glob: "*.md"},
			probe{path: filepath.Join(w, ".kiro", "skills"), harness: KiroCLI, kind: KindSkill, scope: ScopeWorkspace, dir: true},
			probe{path: filepath.Join(w, ".kiro", "steering"), harness: KiroCLI, kind: KindMemory, scope: ScopeWorkspace, dir: true, glob: "*.md"},
		)
	}
	return p
}

// Scan walks every known location and returns the artifacts that exist.
// Missing paths are not errors; they are the normal case.
func Scan(l Layout) ([]Artifact, error) {
	var out []Artifact
	for _, pr := range l.probes() {
		if pr.dir {
			found, err := scanDir(pr)
			if err != nil {
				return nil, err
			}
			out = append(out, found...)
			continue
		}
		a, ok, err := inspect(pr.path, pr.harness, pr.kind, pr.scope)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, a)
		}
	}
	return out, nil
}

func scanDir(pr probe) ([]Artifact, error) {
	entries, err := os.ReadDir(pr.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Artifact
	for _, e := range entries {
		full := filepath.Join(pr.path, e.Name())

		// Skills are directories containing SKILL.md, not flat files.
		if pr.kind == KindSkill {
			target := filepath.Join(full, "SKILL.md")
			a, ok, err := inspect(full, pr.harness, pr.kind, pr.scope)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if _, statErr := os.Stat(target); statErr != nil {
				continue // directory without a SKILL.md is not a skill
			}
			out = append(out, a)
			continue
		}

		if pr.glob != "" {
			match, err := filepath.Match(pr.glob, e.Name())
			if err != nil {
				return nil, err
			}
			if !match {
				continue
			}
		}
		a, ok, err := inspect(full, pr.harness, pr.kind, pr.scope)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, a)
		}
	}
	return out, nil
}

// inspect stats a path without following symlinks, so that shared links are
// reported as links rather than as copies of their target.
func inspect(path string, h Harness, k Kind, s Scope) (Artifact, bool, error) {
	li, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Artifact{}, false, nil
		}
		return Artifact{}, false, err
	}
	a := Artifact{Path: path, Harness: h, Kind: k, Scope: s, SizeBytes: li.Size()}
	if li.Mode()&os.ModeSymlink != 0 {
		a.IsSymlink = true
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			a.LinkTarget = resolved
		}
	}
	return a, true, nil
}
