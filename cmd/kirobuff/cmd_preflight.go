package main

// Preflight: catch conflicts before a push, not on a pull request page.

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AlleyBo55/KiroBuff/internal/preflight"
)

func cmdPreflight(args []string) error {
	if len(args) > 0 && args[0] == "install" {
		return preflightInstall(args[1:])
	}

	fs := flag.NewFlagSet("preflight", flag.ExitOnError)
	base := fs.String("base", "", "base branch to compare against (default: the remote's HEAD)")
	quiet := fs.Bool("quiet", false, "print nothing when there is nothing to fix")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}

	// git hands a pre-push hook the refs on stdin. Reading them is what
	// distinguishes a tag push from a branch push; without it, tagging from
	// master trips the protected-branch check.
	var refs []preflight.PushRef
	if fi, statErr := os.Stdin.Stat(); statErr == nil && fi.Mode()&os.ModeCharDevice == 0 {
		refs = preflight.ParsePushRefs(os.Stdin)
	}

	report, err := preflight.Run(*base, refs...)
	if err != nil {
		// A repository preflight cannot read is not a reason to block a push.
		fmt.Fprintf(os.Stderr, "preflight: skipped (%v)\n", err)
		return nil
	}

	if report.Clean() {
		if !*quiet {
			fmt.Printf("%s -> %s: nothing to fix.\n", report.Branch, report.Base)
		}
		return nil
	}

	if *quiet && !report.Blocked() {
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n  %s -> %s\n\n", report.Branch, report.Base)
	for _, f := range report.Findings {
		fmt.Fprintf(os.Stderr, "  [%-7s] %s\n", f.Severity, f.Check)
		fmt.Fprintf(os.Stderr, "            %s\n", f.Detail)
		if f.Fix != "" {
			fmt.Fprintf(os.Stderr, "            run: %s\n", f.Fix)
		}
		fmt.Fprintln(os.Stderr)
	}

	if report.Blocked() {
		fmt.Fprint(os.Stderr,
			"  Push stopped. Every line above ends in the command that fixes it.\n"+
				"  To push anyway: git push --no-verify\n\n")
		os.Exit(1)
	}
	return nil
}

// hookScript is the pre-push hook. It stays a one-liner on purpose: the logic
// lives in the binary, so upgrading kirobuff upgrades the check without anyone
// reinstalling a hook.
const hookScript = `#!/usr/bin/env sh
# Installed by kirobuff. Catches conflicts before they reach a pull request.
# Remove this file, or push with --no-verify, to bypass.
command -v kirobuff >/dev/null 2>&1 || exit 0
exec kirobuff preflight -quiet
`

func preflightInstall(args []string) error {
	fs := flag.NewFlagSet("preflight install", flag.ExitOnError)
	force := fs.Bool("force", false, "replace an existing pre-push hook")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}

	dir, err := gitHooksDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "pre-push")

	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == hookScript {
			fmt.Printf("Already installed at %s\n", path)
			return nil
		}
		if !*force {
			return fmt.Errorf("%s exists and was not written by kirobuff; "+
				"inspect it and re-run with -force to replace it", path)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(hookScript), 0o755); err != nil {
		return err
	}
	// WriteFile honours the mode only when creating the file.
	if err := os.Chmod(path, 0o755); err != nil {
		return err
	}

	fmt.Printf("Installed the no-conflict check at %s\n\n", path)
	fmt.Print(`Every push now checks the branch against its base first:

  protected-branch    a direct push to master or main
  squash-duplicates   commits the base already has under different hashes,
                      which is what a squash-merged PR leaves behind
  merge-conflict      the exact files that would conflict
  behind-base         the base moved and rebasing now keeps it trivial

Blockers stop the push and print the command that fixes them. Warnings do not.

This does not make conflicts impossible - two people editing one line always
will. It makes sure you never find out from a pull request page.

  git push --no-verify     bypass once
  rm ` + "`git rev-parse --git-path hooks/pre-push`" + `   remove it
`)
	return nil
}

// gitHooksDir resolves the hooks directory, respecting core.hooksPath and
// working correctly inside a worktree, where .git is a file rather than a
// directory.
func gitHooksDir() (string, error) {
	if out, err := gitOutput("rev-parse", "--git-path", "hooks"); err == nil && out != "" {
		abs, err := filepath.Abs(out)
		if err != nil {
			// A relative hooks path is still usable from the repository root,
			// which is where git invokes hooks from.
			return out, nil //nolint:nilerr
		}
		return abs, nil
	}
	return "", errors.New("not a git repository")
}
