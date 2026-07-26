// Package sentinel detects test coverage disappearing, whatever route it took.
//
// Blocking commands is whack-a-mole. Probing kirobuff's own preToolUse rules
// found six working bypasses for two rules: a test can be removed with rm, git
// rm, mv, unlink or find -delete, and only the first two were caught. Every rule
// added invites the next variation.
//
// This measures the outcome instead. It counts test files and assertions across
// the repository, remembers the highest figures it has seen, and reports when
// the current count is lower. That catches deletion by any method, including
// ones nobody has thought of, because it never looks at the command.
//
// The tradeoff is timing. Kiro CLI's stop hook cannot block a tool call, only
// warn after the turn, so this finds the loss a minute later rather than
// preventing it. A minute is still the difference between noticing and shipping.
package sentinel

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlleyBo55/KiroBuff/enforce"
)

// StateFile is where the high-water mark lives, relative to the workspace.
//
// It sits outside .kiro/loop so a repository can use the sentinel without a
// loop, and enforce protects it from being written by the agent: whoever can
// lower the baseline can defeat the check.
const StateFile = ".kiro/kirobuff/sentinel.json"

// prunedDirs are skipped when scanning. Walking node_modules on a JavaScript
// repository would dominate the runtime of a hook that runs every turn.
var prunedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "target": true,
	"dist": true, "build": true, ".venv": true, "venv": true,
	"__pycache__": true, ".cache": true, ".next": true, ".terraform": true,
}

// Measurement is one scan of the repository.
type Measurement struct {
	Files      int `json:"files"`
	Assertions int `json:"assertions"`
}

// State is the persisted high-water mark.
type State struct {
	// Peak is the best measurement seen, which is what current results are
	// compared against. A transient drop therefore stays visible rather than
	// quietly becoming the new normal.
	Peak      Measurement `json:"peak"`
	Last      Measurement `json:"last"`
	UpdatedAt string      `json:"updated_at"`
}

// Verdict is the result of a check.
type Verdict struct {
	Current    Measurement
	Peak       Measurement
	Regressed  bool
	FirstRun   bool
	FilesLost  int
	AssertLost int
}

// Detail renders a human-readable explanation.
func (v Verdict) Detail() string {
	switch {
	case v.FirstRun:
		return fmt.Sprintf("baseline recorded: %d test file(s), %d assertion(s)",
			v.Current.Files, v.Current.Assertions)
	case !v.Regressed:
		return fmt.Sprintf("%d test file(s), %d assertion(s); no loss against the peak of %d/%d",
			v.Current.Files, v.Current.Assertions, v.Peak.Files, v.Peak.Assertions)
	}
	var parts []string
	if v.FilesLost > 0 {
		parts = append(parts, fmt.Sprintf("%d test file(s)", v.FilesLost))
	}
	if v.AssertLost > 0 {
		parts = append(parts, fmt.Sprintf("%d assertion(s)", v.AssertLost))
	}
	return fmt.Sprintf("test coverage dropped: lost %s (now %d files / %d assertions, "+
		"peak was %d / %d)",
		strings.Join(parts, " and "),
		v.Current.Files, v.Current.Assertions, v.Peak.Files, v.Peak.Assertions)
}

// Scan measures the test surface of a workspace.
//
// Assertion counting reuses [enforce.CountAssertions], so the sentinel and the
// preToolUse rule can never disagree about what an assertion is.
func Scan(workspace string) (Measurement, error) {
	var m Measurement
	err := filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if prunedDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		// Skip symlinks. WalkDir does not follow them for traversal, but reading
		// through one could take the scan outside the workspace between the walk
		// and the read. Counting a symlinked test twice would also be wrong.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, relErr := filepath.Rel(workspace, path)
		if relErr != nil {
			rel = path
		}
		if !enforce.IsTestPath(rel) {
			return nil
		}
		// gosec G122 flags any filesystem operation on a path from a WalkDir
		// callback as a symlink TOCTOU risk. The symlink skip above removes the
		// route, and the only complete fix is os.Root, which needs Go 1.24 while
		// this module targets 1.21. The exposure if it were reachable is a
		// changed count, never file contents: nothing read here is emitted.
		body, readErr := os.ReadFile(path) //nolint:gosec // see above
		if readErr != nil {
			// Unreadable mid-scan: skip the file rather than fail a hook that
			// runs on every turn.
			return nil //nolint:nilerr
		}
		m.Files++
		m.Assertions += enforce.CountAssertions(string(body))
		return nil
	})
	return m, err
}

// Load reads the state, returning a zero State when none exists yet.
func Load(workspace string) (State, bool, error) {
	raw, err := os.ReadFile(filepath.Join(workspace, StateFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{}, false, nil
		}
		return State{}, false, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		// A corrupt state file must not wedge the hook. Treat it as absent and
		// re-baseline, which is visible in the output rather than silent.
		return State{}, false, nil //nolint:nilerr
	}
	return s, true, nil
}

// Save writes the state.
func Save(workspace string, s State) error {
	path := filepath.Join(workspace, StateFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}

// Check scans the workspace and compares against the stored peak.
//
// The peak only ever rises. Recording a drop as the new baseline would let
// coverage ratchet down one turn at a time without a single warning, which is
// exactly the failure this package exists to catch.
func Check(workspace string) (Verdict, error) {
	current, err := Scan(workspace)
	if err != nil {
		return Verdict{}, err
	}
	state, existed, err := Load(workspace)
	if err != nil {
		return Verdict{}, err
	}

	v := Verdict{Current: current, Peak: state.Peak, FirstRun: !existed}
	if v.FirstRun {
		v.Peak = current
		return v, Save(workspace, State{Peak: current, Last: current})
	}

	if current.Files < state.Peak.Files {
		v.FilesLost = state.Peak.Files - current.Files
	}
	if current.Assertions < state.Peak.Assertions {
		v.AssertLost = state.Peak.Assertions - current.Assertions
	}
	v.Regressed = v.FilesLost > 0 || v.AssertLost > 0

	next := State{Peak: state.Peak, Last: current}
	if current.Files > next.Peak.Files {
		next.Peak.Files = current.Files
	}
	if current.Assertions > next.Peak.Assertions {
		next.Peak.Assertions = current.Assertions
	}
	return v, Save(workspace, next)
}

// Accept lowers the peak to the current measurement.
//
// Deleting a genuinely obsolete test is legitimate, so there has to be a way to
// say so. It is deliberately a separate, explicit command rather than a flag on
// the check: an agent cannot run it as part of the same turn that removed the
// test, because the state file is in enforce's protected paths.
func Accept(workspace string) (Measurement, error) {
	current, err := Scan(workspace)
	if err != nil {
		return Measurement{}, err
	}
	return current, Save(workspace, State{Peak: current, Last: current})
}
