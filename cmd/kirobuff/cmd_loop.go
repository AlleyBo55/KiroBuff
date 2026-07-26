package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/AlleyBo55/KiroBuff/internal/loop"
)

// Loop scaffolding.

// ---------------------------------------------------------------- loop

func cmdLoop(args []string) error {
	if len(args) == 0 || args[0] != "init" {
		return errors.New("loop: only `loop init` is supported")
	}
	fs := flag.NewFlagSet("loop init", flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	goal := fs.String("goal", "", "the measurable outcome the loop pursues")
	editable := fs.String("editable", "", "glob the agent may modify (default src/**)")
	maxAttempts := fs.Int("max-attempts", 10, "stop condition")
	metric := fs.String("metric", "", "command printing one number; makes the loop a search")
	direction := fs.String("direction", "lower", "lower or higher is better")
	force := fs.Bool("force", false, "overwrite existing loop files")
	if err := fs.Parse(permute(args[1:])); err != nil {
		return err
	}
	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}

	tc := loop.DetectToolchain(ws)
	scaffold := loop.New(*goal, *editable, *maxAttempts, tc)
	if *metric != "" {
		scaffold = scaffold.WithMetric(*metric, *direction)
	}

	written, skipped, err := scaffold.Write(ws, *force)
	if err != nil {
		return err
	}

	fmt.Printf("workspace  %s\n", ws)
	fmt.Printf("toolchain  %s\n", tc.Name)
	fmt.Printf("verifier   %s\n", tc.Verify)
	fmt.Printf("editable   %s\n", scaffold.Editable)
	if scaffold.Scored() {
		fmt.Printf("metric     %s (%s is better)\n", *metric, scaffold.Direction)
	} else {
		fmt.Printf("metric     none - this is a gate, not a search\n")
	}
	fmt.Printf("stop after %d attempts\n\n", scaffold.MaxAttempts)

	for _, p := range written {
		fmt.Printf("  created  %s\n", p)
	}
	for _, p := range skipped {
		fmt.Printf("  kept     %s (already exists)\n", p)
	}

	if tc.Name == "unknown" {
		fmt.Print(`
No build system detected, so the verifier is a failing placeholder. Edit
.kiro/loop/verify.sh before running the loop - a verifier that always passes
is worse than no loop, because the agent will grade its own homework.
`)
	}

	fmt.Printf(`
Start it with:  kiro-cli chat --agent %s

The verifier runs on the stop hook, so it fires after every turn whether or
not the agent asks for it. The ledger is injected once at session start by
agentSpawn rather than declared as a resource, which would re-send it every
turn.

Set the goal in .kiro/loop/program.md before the first run. The four-point
test still applies: only build a loop when the task repeats, verification is
automated, you have budget for wasted retries, and the agent has real tools.
`, scaffold.AgentName)
	return nil
}
