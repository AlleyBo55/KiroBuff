// Package budget estimates the recurring token cost of a Kiro CLI agent
// configuration and reports where that cost can be cut without losing
// capability.
//
// The distinction that matters is per-turn versus on-demand. A `file://`
// resource is loaded into context on every request for the whole session; a
// `skill://` resource contributes only its name, description and path until
// the model actually asks for it. Tool schemas are also per-turn. Anything
// per-turn is multiplied by conversation length, so that is where the money
// goes.
//
// Token counts are estimates using bytes/4, the same approximation Kiro CLI
// itself documents for `/context`. They are for ranking fixes, not billing.
package budget

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// BytesPerToken is Kiro CLI's documented approximation for `/context`.
const BytesPerToken = 4

// Severity ranks a finding by how much recurring cost it carries.
type Severity string

const (
	High   Severity = "high"   // >2000 tokens per turn
	Medium Severity = "medium" // 500-2000 tokens per turn
	Low    Severity = "low"    // <500 tokens per turn, or advisory
)

// Finding is one identified source of recurring token cost.
type Finding struct {
	Rule          string
	Severity      Severity
	Subject       string // the config entry at fault
	TokensPerTurn int    // estimated recurring cost
	Detail        string
	Fix           string
}

// Agent is the subset of a Kiro CLI agent config that affects token cost.
// Unknown fields are ignored so this stays forward-compatible.
type Agent struct {
	Name      string            `json:"name"`
	Model     string            `json:"model"`
	Prompt    string            `json:"prompt"`
	Tools     []string          `json:"tools"`
	Resources []string          `json:"resources"`
	Hooks     map[string][]Hook `json:"-"`
	RawHooks  json.RawMessage   `json:"hooks"`
}

// Hook is the object-format hook shape.
type Hook struct {
	Command         string `json:"command"`
	CacheTTLSeconds int    `json:"cache_ttl_seconds"`
	MaxOutputSize   int    `json:"max_output_size"`
}

// Load reads an agent config from disk.
func Load(path string) (*Agent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	// Hooks accept both an object keyed by trigger and a flat array. Only the
	// object form carries cache_ttl_seconds, so the array form is skipped
	// rather than guessed at.
	if len(a.RawHooks) > 0 {
		var obj map[string][]Hook
		if err := json.Unmarshal(a.RawHooks, &obj); err == nil {
			a.Hooks = obj
		}
	}
	if a.Name == "" {
		a.Name = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	return &a, nil
}

// severityFor ranks by recurring cost.
func severityFor(tokensPerTurn int) Severity {
	switch {
	case tokensPerTurn > 2000:
		return High
	case tokensPerTurn >= 500:
		return Medium
	default:
		return Low
	}
}

// Analyze inspects an agent config against the filesystem rooted at workspace
// and returns findings ordered high severity first.
func Analyze(a *Agent, workspace string) []Finding {
	var out []Finding
	out = append(out, checkAlwaysLoaded(a, workspace)...)
	out = append(out, checkUncachedHooks(a)...)
	out = append(out, checkToolSurface(a)...)
	sortBySeverity(out)
	return out
}

// checkAlwaysLoaded is the dominant cost in practice: file:// resources are
// re-sent every turn, so a wide glob over a source tree is paid repeatedly.
func checkAlwaysLoaded(a *Agent, workspace string) []Finding {
	var out []Finding
	for _, res := range a.Resources {
		if !strings.HasPrefix(res, "file://") {
			continue
		}
		pattern := strings.TrimPrefix(res, "file://")
		matches, bytes := measure(pattern, workspace)
		if matches == 0 {
			out = append(out, Finding{
				Rule:     "dead-resource",
				Severity: Low,
				Subject:  res,
				Detail:   "pattern matches no files",
				Fix:      "remove the entry, or correct the path",
			})
			continue
		}
		tokens := int(bytes) / BytesPerToken
		if tokens < 200 {
			continue // not worth reporting
		}
		f := Finding{
			Rule:          "always-loaded",
			Severity:      severityFor(tokens),
			Subject:       res,
			TokensPerTurn: tokens,
			Detail:        plural(matches, "file") + " loaded into context on every turn",
		}
		if allSkillManifests(pattern, workspace) {
			f.Fix = "switch to skill:// so only name+description load until needed"
		} else if strings.Contains(pattern, "**") {
			f.Fix = "narrow the glob, or move the reference material into a skill"
		} else {
			f.Fix = "move into a skill if the model needs this only occasionally"
		}
		out = append(out, f)
	}
	return out
}

// checkUncachedHooks flags hooks whose stdout is injected into context on
// every turn without caching. agentSpawn runs once, so it is exempt; Kiro CLI
// never caches it anyway.
func checkUncachedHooks(a *Agent) []Finding {
	var out []Finding
	for trigger, hooks := range a.Hooks {
		if trigger != "userPromptSubmit" {
			continue
		}
		for _, h := range hooks {
			if h.CacheTTLSeconds > 0 {
				continue
			}
			size := h.MaxOutputSize
			if size == 0 {
				size = 10240 // documented default
			}
			out = append(out, Finding{
				Rule:          "uncached-hook",
				Severity:      severityFor(size / BytesPerToken),
				Subject:       trigger + ": " + h.Command,
				TokensPerTurn: size / BytesPerToken,
				Detail:        "output is added to context on every prompt, uncached (worst case at max_output_size)",
				Fix:           "set cache_ttl_seconds, or lower max_output_size",
			})
		}
	}
	return out
}

// checkToolSurface flags a wide tool list. Tool schemas ship with every
// request, so unused tools are a permanent tax and they also widen the space
// of things the model may attempt.
func checkToolSurface(a *Agent) []Finding {
	for _, t := range a.Tools {
		if t == "*" {
			return []Finding{{
				Rule:     "wide-tool-surface",
				Severity: Medium,
				Subject:  `tools: ["*"]`,
				Detail:   "every tool schema is sent on every request",
				Fix:      "list only the tools this agent needs",
			}}
		}
	}
	return nil
}

// measure resolves a glob or plain path under workspace and totals its size.
func measure(pattern, workspace string) (count int, total int64) {
	walkMatches(pattern, workspace, func(_ string, size int64) {
		count++
		total += size
	})
	return count, total
}

// prunedDirs are never part of a meaningful context budget. Walking them turns
// a single glob into tens of thousands of stats on a normal JavaScript or Rust
// repository.
//
// A directory is only pruned when the pattern does not mention it, so an
// explicit `file://dist/**/*.js` still resolves rather than being reported as
// matching nothing.
var prunedDirs = []string{
	".git", "node_modules", "vendor", "target", "dist", "build",
	".venv", "venv", "__pycache__", ".cache", ".next", ".terraform",
}

// walkMatches streams the files matching pattern, calling fn for each.
//
// Streaming rather than returning a slice keeps memory flat on large trees, and
// the size comes from the directory entry that the walk already read, so no
// second stat is needed.
func walkMatches(pattern, workspace string, fn func(path string, size int64)) {
	full := pattern
	if !filepath.IsAbs(full) {
		full = filepath.Join(workspace, full)
	}

	// No ** means filepath.Glob handles it, and it does not walk.
	if !strings.Contains(full, "**") {
		matches, err := filepath.Glob(full)
		if err != nil {
			return
		}
		for _, p := range matches {
			fi, err := os.Lstat(p)
			if err != nil || fi.IsDir() {
				continue
			}
			fn(p, fi.Size())
		}
		return
	}

	// Walk from the deepest fixed ancestor so the ** does not start at /.
	idx := strings.Index(full, "**")
	root := filepath.Dir(full[:idx])
	if root == "" {
		root = "."
	}

	patternMentions := func(dir string) bool {
		return strings.Contains(full, string(filepath.Separator)+dir) ||
			strings.HasSuffix(full, dir)
	}

	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			name := d.Name()
			for _, skip := range prunedDirs {
				if name == skip && !patternMentions(skip) {
					return fs.SkipDir
				}
			}
			return nil
		}
		if !matchGlob(full, p) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fn(p, info.Size())
		return nil
	})
}

// matchGlob matches a path against a pattern where ** spans any number of path
// segments.
//
// filepath.Match has no ** support and matches a single segment only, so an
// earlier version compared the pattern tail against the basename. That silently
// failed for any pattern with path separators after the **, for example
// a/**/b/*.go, which matched nothing while reporting the resource as dead.
func matchGlob(pattern, path string) bool {
	// Both sides must be normalised the same way. Trimming the leading
	// separator from only one of them leaves an empty first segment on that
	// side, so the very first comparison fails and nothing ever matches.
	split := func(s string) []string {
		return strings.Split(strings.TrimPrefix(filepath.ToSlash(s), "/"), "/")
	}
	return matchSegments(split(pattern), split(path))
}

func matchSegments(pattern, path []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			// ** matches zero or more segments: try every split point.
			rest := pattern[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(path); i++ {
				if matchSegments(rest, path[i:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 {
			return false
		}
		ok, err := filepath.Match(pattern[0], path[0])
		if err != nil || !ok {
			return false
		}
		pattern, path = pattern[1:], path[1:]
	}
	return len(path) == 0
}

// allSkillManifests reports whether every match is a SKILL.md, meaning the
// entry is a skill set declared with the wrong URI scheme.
func allSkillManifests(pattern, workspace string) bool {
	var seen, manifests int
	walkMatches(pattern, workspace, func(p string, _ int64) {
		seen++
		if filepath.Base(p) == "SKILL.md" {
			manifests++
		}
	})
	return seen > 0 && seen == manifests
}

// Total sums the estimated recurring cost of all findings.
func Total(fs []Finding) int {
	n := 0
	for _, f := range fs {
		n += f.TokensPerTurn
	}
	return n
}

func sortBySeverity(fs []Finding) {
	rank := map[Severity]int{High: 0, Medium: 1, Low: 2}
	sort.SliceStable(fs, func(i, j int) bool {
		if rank[fs[i].Severity] != rank[fs[j].Severity] {
			return rank[fs[i].Severity] < rank[fs[j].Severity]
		}
		return fs[i].TokensPerTurn > fs[j].TokensPerTurn
	})
}

func plural(n int, noun string) string {
	s := strconv.Itoa(n) + " " + noun
	if n != 1 {
		s += "s"
	}
	return s
}
