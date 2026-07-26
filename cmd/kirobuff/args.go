package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Argument handling and output formatting shared by every command.

// ---------------------------------------------------------------- helpers

func resolveWorkspace(flagValue string) (string, error) {
	if flagValue == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		flagValue = wd
	}
	return filepath.Abs(flagValue)
}

// permute moves flags ahead of positional arguments.
//
// Go's flag package stops parsing at the first non-flag argument, so
// `budget path -max 2000` would silently discard the threshold. Reordering
// makes flag placement irrelevant, matching what users expect from most CLIs.
func permute(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			return append(flags, positional...)
		case valueFlags[a] && i+1 < len(args):
			flags = append(flags, a, args[i+1])
			i++
		case strings.HasPrefix(a, "-") && a != "-":
			flags = append(flags, a)
		default:
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

// display shortens a path to ~/... for readability.
func display(path, home string) string {
	if home != "" && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

// withCommas groups thousands for readability.
func withCommas(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// gitTimeout bounds every git invocation. A pre-push hook that inherits a git
// waiting on credentials would otherwise hang the push with no explanation.
const gitTimeout = 15 * time.Second

// gitOutput runs a git command and returns trimmed stdout.
func gitOutput(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// writeAgentConfig persists a patched agent config, preserving its file mode.
func writeAgentConfig(path string, body []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(path, body, mode)
}
