// Package mode implements composable, toggleable modes.
//
// Agents cannot compose: /agent X gives you exactly one agent, so a mode built
// as an agent is mutually exclusive with every other mode. Steering fragments
// can compose, because Kiro CLI loads every file matching
// ~/.kiro/steering/**/*.md into every agent.
//
// So a mode is a markdown fragment that lives in a library outside the steering
// tree, and turning it on symlinks it in. Several can be on at once.
//
// The cost of that is context. Every active fragment is re-sent on every turn
// for the whole session, which is exactly what internal/budget measures and
// warns about. MaxActive exists for that reason and not as an arbitrary limit:
// six fragments is already a few thousand tokens per turn before the
// conversation starts.
package mode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxActive caps simultaneous modes. Each one costs context on every turn.
const MaxActive = 6

// Prefix orders mode fragments after the base guardrails, which use 00-.
const Prefix = "50-mode-"

// Marker identifies a kirobuff-managed steering symlink.
const Marker = "<!-- kirobuff:mode -->"

// Kind distinguishes what turning a mode on actually does.
type Kind string

const (
	// Prompt modes add a steering fragment. Composable and free of side effects.
	Prompt Kind = "prompt"
	// System modes change machine state. They compose with prompt modes but
	// cannot be applied by kirobuff itself, because they need elevated
	// privileges and have consequences beyond the session.
	System Kind = "system"
)

// Mode is one toggleable behaviour.
type Mode struct {
	Name    string
	Kind    Kind
	Summary string
	Body    string // steering fragment, for Prompt modes
}

var (
	ErrUnknown  = errors.New("unknown mode")
	ErrTooMany  = fmt.Errorf("at most %d modes can be active at once", MaxActive)
	ErrNotOn    = errors.New("mode is not active")
	ErrSystem   = errors.New("system modes cannot be toggled by kirobuff")
	ErrConflict = errors.New("a file is in the way and kirobuff did not create it")
)

// Registry returns every available mode.
func Registry() map[string]Mode {
	out := map[string]Mode{}
	for _, m := range all() {
		out[m.Name] = m
	}
	return out
}

// Names lists mode names in a stable order.
func Names() []string {
	var out []string
	for _, m := range all() {
		out = append(out, m.Name)
	}
	sort.Strings(out)
	return out
}

// Get returns a mode by name.
func Get(name string) (Mode, error) {
	m, ok := Registry()[name]
	if !ok {
		return Mode{}, fmt.Errorf("%w: %q (see kirobuff mode list)", ErrUnknown, name)
	}
	return m, nil
}

// Layout resolves the library and steering directories.
type Layout struct {
	Library  string // ~/.kiro/kirobuff/modes
	Steering string // ~/.kiro/steering
}

// DefaultLayout builds a Layout, honouring KIRO_HOME.
func DefaultLayout(home string) Layout {
	kiro := os.Getenv("KIRO_HOME")
	if kiro == "" {
		kiro = filepath.Join(home, ".kiro")
	}
	return Layout{
		Library:  filepath.Join(kiro, "kirobuff", "modes"),
		Steering: filepath.Join(kiro, "steering"),
	}
}

// libraryPath is where a mode's fragment is stored when inactive.
func (l Layout) libraryPath(name string) string {
	return filepath.Join(l.Library, name+".md")
}

// activePath is where the symlink lives when the mode is on.
func (l Layout) activePath(name string) string {
	return filepath.Join(l.Steering, Prefix+name+".md")
}

// Sync writes every prompt mode's fragment into the library, so the files
// exist whether or not the mode is on. Existing files are overwritten: the
// library is kirobuff's, and edits belong in a separate steering file.
func Sync(l Layout) error {
	if err := os.MkdirAll(l.Library, 0o755); err != nil {
		return err
	}
	for _, m := range all() {
		if m.Kind != Prompt {
			continue
		}
		body := Marker + "\n# Mode: " + m.Name + "\n\n" + strings.TrimSpace(m.Body) + "\n"
		if err := os.WriteFile(l.libraryPath(m.Name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Active lists the prompt modes currently switched on.
func Active(l Layout) []string {
	entries, err := os.ReadDir(l.Steering)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, Prefix) || !strings.HasSuffix(name, ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(name, Prefix), ".md"))
	}
	sort.Strings(out)
	return out
}

// ownedLink reports whether path is a symlink kirobuff created for this mode.
//
// Checking ownership rather than mere existence matters: a regular file the user
// wrote at the same path must not be mistaken for an active mode, or On would
// report success while changing nothing.
func (l Layout) ownedLink(name string) (owned, exists bool) {
	path := l.activePath(name)
	fi, err := os.Lstat(path)
	if err != nil {
		return false, false
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return false, true
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false, true
	}
	return target == l.libraryPath(name), true
}

// IsActive reports whether a prompt mode is on, meaning kirobuff owns a symlink
// for it.
func IsActive(l Layout, name string) bool {
	owned, _ := l.ownedLink(name)
	return owned
}

// On activates a prompt mode by symlinking its fragment into steering.
//
// Symlinking rather than copying means a later kirobuff upgrade improves an
// active mode without the user re-toggling it.
func On(l Layout, name string) error {
	m, err := Get(name)
	if err != nil {
		return err
	}
	if m.Kind == System {
		return fmt.Errorf("%w: %s changes machine state, run "+
			"`kirobuff mode explain %s` for what to do", ErrSystem, name, name)
	}

	// Ownership is checked before anything else, so a file kirobuff did not
	// create is never silently adopted or reported as already active.
	owned, exists := l.ownedLink(name)
	if owned {
		return nil // idempotent
	}
	if exists {
		return fmt.Errorf("%w: %s", ErrConflict, l.activePath(name))
	}

	if active := Active(l); len(active) >= MaxActive {
		return fmt.Errorf("%w (active: %s). Turn one off first: "+
			"each active mode is re-sent on every turn, so the cap is a context "+
			"budget rather than a preference", ErrTooMany, strings.Join(active, ", "))
	}
	if err := Sync(l); err != nil {
		return err
	}
	if err := os.MkdirAll(l.Steering, 0o755); err != nil {
		return err
	}
	return os.Symlink(l.libraryPath(name), l.activePath(name))
}

// Off deactivates a prompt mode by removing the symlink. The library copy is
// left in place.
func Off(l Layout, name string) error {
	if _, err := Get(name); err != nil {
		return err
	}
	link := l.activePath(name)
	fi, err := os.Lstat(link)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotOn, name)
	}
	// Only remove a symlink. A regular file here was not created by On.
	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%w: %s is a regular file, not a kirobuff link", ErrConflict, link)
	}
	return os.Remove(link)
}

// Remaining reports how many more modes can be switched on.
func Remaining(l Layout) int {
	n := MaxActive - len(Active(l))
	if n < 0 {
		return 0
	}
	return n
}
