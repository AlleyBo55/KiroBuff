package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/AlleyBo55/KiroBuff/internal/enforce"
	"github.com/AlleyBo55/KiroBuff/internal/guard"
)

// Enforcement hook and its installer.

// ---------------------------------------------------------------- enforce

func cmdEnforce(args []string) error {
	if len(args) > 0 && args[0] == "install" {
		return enforceInstall(args[1:])
	}

	// Hook mode: a preToolUse event arrives on stdin. Exit 2 blocks the call and
	// returns stderr to the model; any other non-zero code only warns the user
	// and lets the call through, so the distinction matters.
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil // fail open: a broken pipe must not block every tool call
	}
	var event enforce.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil // fail open on an unrecognised payload shape
	}

	d := enforce.Evaluate(event)
	if !d.Blocked {
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s [%s]\n", d.Reason, d.Rule)
	os.Exit(2)
	return nil
}

func enforceInstall(args []string) error {
	fs := flag.NewFlagSet("enforce install", flag.ExitOnError)
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("enforce install: need a path to an agent config")
	}
	configPath := fs.Arg(0)

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	// No matcher: the rules decide which tools they apply to, and a matcher here
	// would silently exempt any tool alias not listed.
	patched, err := guard.InstallOn(raw, "preToolUse", "kirobuff enforce", 0)
	if err != nil {
		if errors.Is(err, guard.ErrAlreadyInstalled) {
			fmt.Printf("Already installed in %s - nothing to do.\n", configPath)
			return nil
		}
		return err
	}
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(configPath); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(configPath, patched, mode); err != nil {
		return err
	}

	fmt.Printf("Installed enforcement hook in %s\n\n", configPath)
	fmt.Print(`These are no longer requests. The hook exits 2, which blocks the tool call
and returns the reason to the model:

  no-agent-signoff        git commit -s, or a Signed-off-by written by hand
  no-test-deletion        rm on a test file or test directory
  no-assertion-weakening  an edit that reduces a test's assertion count
  protect-verifier        writes to .kiro/loop/verify.sh, program.md, best
  no-destructive-git      reset --hard, push --force, clean -fd, branch -D

Everything else passes. Unknown tools and unparseable input fail open on
purpose: a hook that blocks a shape it does not recognise would break every
session after a schema change.

Judgment stays in steering. Only rules decidable from the tool input are here.
`)
	return nil
}
