package main

// Sentinel: detect test coverage disappearing, whatever route it took.

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/AlleyBo55/KiroBuff/internal/guard"
	"github.com/AlleyBo55/KiroBuff/internal/sentinel"
)

func cmdSentinel(args []string) error {
	sub := ""
	if len(args) > 0 && (args[0] == "install" || args[0] == "accept") {
		sub, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("sentinel "+sub, flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	quiet := fs.Bool("quiet", false, "print nothing unless coverage dropped")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}

	switch sub {
	case "install":
		return sentinelInstall(fs.Arg(0))
	case "accept":
		m, err := sentinel.Accept(ws)
		if err != nil {
			return err
		}
		fmt.Printf("Baseline reset to %d test file(s) and %d assertion(s).\n", m.Files, m.Assertions)
		fmt.Println("Use this only when a test was genuinely obsolete.")
		return nil
	}

	v, err := sentinel.Check(ws)
	if err != nil {
		// Never fail a turn over a scan problem: this runs from a hook on
		// every turn, and an unreadable file must not become a warning storm.
		fmt.Fprintf(os.Stderr, "sentinel: skipped (%v)\n", err)
		return nil //nolint:nilerr
	}

	if !v.Regressed {
		if !*quiet {
			fmt.Println(v.Detail())
		}
		return nil
	}

	// A stop hook cannot block the call; a non-zero exit shows stderr to the
	// user as a warning, which is the strongest signal available at this point.
	fmt.Fprintf(os.Stderr, "\n  kirobuff: %s\n\n", v.Detail())
	fmt.Fprint(os.Stderr,
		"  Nothing was blocked - this runs after the turn. If a test was removed\n"+
			"  or emptied and that was not intended, restore it now.\n"+
			"  If it was genuinely obsolete: kirobuff sentinel accept\n\n")
	os.Exit(1)
	return nil
}

func sentinelInstall(configPath string) error {
	if configPath == "" {
		return errors.New("sentinel install: need a path to an agent config")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	// stop, not preToolUse: this measures the state of the repository after a
	// turn rather than inspecting a command, so it has nothing to look at until
	// the turn is over.
	command := "kirobuff sentinel -quiet"
	patched, err := guard.InstallOn(raw, "stop", command, 0)
	if err != nil {
		if errors.Is(err, guard.ErrAlreadyInstalled) {
			fmt.Printf("Already installed in %s - nothing to do.\n", configPath)
			return nil
		}
		return err
	}
	if err := writeAgentConfig(configPath, patched); err != nil {
		return err
	}

	fmt.Printf("Installed the coverage sentinel in %s\n\n  %s\n\n", configPath, command)
	fmt.Print(`It counts test files and assertions across the repository after every turn
and warns when the total drops below the highest it has seen.

Why this exists: blocking commands is whack-a-mole. Probing the preToolUse
rules found rm and git rm caught, but mv, unlink and find -delete all passed
straight through. This never looks at the command, so it catches deletion by
any method, including ones nobody has thought of.

The tradeoff is timing. A stop hook cannot block a tool call, only warn after
the turn, so this finds the loss a minute later rather than preventing it. Use
both: preToolUse stops the obvious move, the sentinel catches the rest.

The peak only rises. A drop stays reported until the tests come back or you
accept the new level deliberately:

  kirobuff sentinel accept

The baseline lives in .kiro/kirobuff/sentinel.json and is in enforce's
protected paths, because whoever can lower it can defeat the check.
`)
	return nil
}
