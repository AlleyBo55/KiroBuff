// Package statusline renders a session indicator into the terminal's window
// title.
//
// Kiro CLI has no statusLine setting: there is no supported way to draw a
// custom row inside its UI. What it does have is OSC 0, the escape sequence
// that sets the terminal window or tab title, exposed through /title and the
// chat.terminalTitle setting.
//
// This package writes that sequence directly to /dev/tty from a hook. Writing
// to /dev/tty rather than stdout matters twice over: the harness captures a
// hook's stdout and feeds it to the model on exit 0, so printing there would
// both fail to reach the terminal and cost tokens. /dev/tty bypasses the
// capture and reaches the terminal the user is looking at.
//
// UNVERIFIED: whether a Kiro CLI hook subprocess inherits a controlling
// terminal has not been confirmed. If it does not, WriteTTY returns
// ErrNoTTY and the caller should exit 0 silently rather than fail the hook.
package statusline

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrNoTTY means no controlling terminal was available.
var ErrNoTTY = errors.New("no controlling terminal: /dev/tty is not writable")

// Separator between fields. Middle dot degrades to "." under KIRO_ASCII_MODE,
// matching Kiro CLI's own ASCII substitution table.
func separator() string {
	if os.Getenv("KIRO_ASCII_MODE") != "" {
		return " . "
	}
	return " · "
}

// Status is what the indicator shows.
//
// Credits are deliberately absent. Account usage is only reachable through the
// in-session /usage command; kiro-cli exposes no usage subcommand, so a credit
// figure here would have to be invented or stale.
type Status struct {
	Mode          string // active persona or agent, empty when default
	TokensPerTurn int    // measured recurring context cost
	Budget        int    // configured ceiling, 0 when unset
	Workspace     string // short project name
}

// Render builds the title text.
func (s Status) Render() string {
	parts := []string{"kirobuff"}

	if s.Workspace != "" {
		parts = append(parts, s.Workspace)
	}
	if s.Mode != "" {
		parts = append(parts, s.Mode)
	}
	if s.TokensPerTurn > 0 {
		token := compact(s.TokensPerTurn)
		if s.Budget > 0 {
			over := ""
			if s.TokensPerTurn > s.Budget {
				over = "!"
			}
			token = fmt.Sprintf("%s/%s%s", token, compact(s.Budget), over)
		}
		parts = append(parts, token+" tok")
	}
	return strings.Join(parts, separator())
}

// compact shortens large counts: 3000 -> 3.0k.
func compact(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// OSC wraps text in the escape sequence that sets a terminal title.
//
// ESC ] 0 ; <text> BEL is the widely supported form, and the same one Kiro CLI
// documents for its own /title command.
func OSC(text string) string {
	// A title containing BEL or ESC would terminate the sequence early and
	// leak bytes onto the screen.
	clean := strings.Map(func(r rune) rune {
		if r == '\a' || r == 0x1b || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, text)
	return "\033]0;" + clean + "\007"
}

// Write emits the sequence to w. Used for tests and for piping.
func Write(w io.Writer, s Status) error {
	_, err := io.WriteString(w, OSC(s.Render()))
	return err
}

// WriteTTY emits the sequence to the controlling terminal.
//
// Returns ErrNoTTY when there is none, which is the normal case in CI, in a
// pipe, or anywhere the process was started without a terminal.
func WriteTTY(s Status) error {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return ErrNoTTY
	}
	defer f.Close()
	if _, err := io.WriteString(f, OSC(s.Render())); err != nil {
		return ErrNoTTY
	}
	return nil
}

// Available reports whether a controlling terminal can be written to, so a
// caller can decide to stay silent instead of erroring.
func Available() bool {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// HookCommand is the shell one-liner to install as a userPromptSubmit hook.
//
// userPromptSubmit is the right trigger because the indicator should refresh as
// the session progresses, and this costs nothing: the command prints nothing to
// stdout, so the empty result added to context on exit 0 is free.
//
// It always exits 0. A status indicator that fails a hook and shows the user a
// warning every turn would be worse than no indicator.
func HookCommand(agentConfig string) string {
	return fmt.Sprintf("kirobuff statusline emit %s || true", agentConfig)
}
