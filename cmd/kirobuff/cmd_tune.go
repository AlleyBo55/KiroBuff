package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/AlleyBo55/KiroBuff/internal/tune"
)

// Per-model reasoning effort defaults.

// ---------------------------------------------------------------- tune

func cmdTune(args []string) error {
	fs := flag.NewFlagSet("tune", flag.ExitOnError)
	model := fs.String("model", "claude-opus-4.7", "model ID to set a default for")
	effort := fs.String("effort", tune.Recommended, "low, medium, high, xhigh, max")
	show := fs.Bool("show", false, "print current defaults and exit")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := tune.SettingsPath(home)

	if *show {
		raw, _ := os.ReadFile(path)
		models := tune.ConfiguredModels(raw)
		if len(models) == 0 {
			fmt.Printf("No per-model effort defaults set in %s\n", display(path, home))
			return nil
		}
		fmt.Printf("%s\n\n", display(path, home))
		for _, m := range models {
			if e, ok := tune.Current(raw, m); ok {
				fmt.Printf("  %-28s %s\n", m, e)
			}
		}
		return nil
	}

	if err := tune.Write(path, *model, *effort); err != nil {
		return err
	}
	fmt.Printf("Set %s effort to %s in %s\n\n", *model, *effort, display(path, home))
	fmt.Printf(`Kiro CLI's built-in default for the Opus family is xhigh, which is more
deliberation than a rename or a test run needs. %s is enough for real work
and noticeably faster.

This is a floor, not a ceiling. Raise it for one session with /effort high
when the task actually warrants it.
`, *effort)
	return nil
}
