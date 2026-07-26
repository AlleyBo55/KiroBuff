package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/AlleyBo55/KiroBuff/internal/discover"
)

// Status: what each harness has on disk.

// ---------------------------------------------------------------- status

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	wsFlag := fs.String("C", "", "workspace root")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}
	ws, err := resolveWorkspace(*wsFlag)
	if err != nil {
		return err
	}

	layout, err := discover.DefaultLayout(ws)
	if err != nil {
		return err
	}
	artifacts, err := discover.Scan(layout)
	if err != nil {
		return err
	}

	fmt.Printf("workspace   %s\n", layout.Workspace)
	fmt.Printf("shared root %s\n", layout.SharedRoot)
	fmt.Printf("kiro home   %s\n\n", layout.KiroHome)

	if len(artifacts) == 0 {
		fmt.Println("No agent configuration found in either harness.")
		return nil
	}

	byKind := map[discover.Kind][]discover.Artifact{}
	for _, a := range artifacts {
		byKind[a.Kind] = append(byKind[a.Kind], a)
	}

	order := []discover.Kind{
		discover.KindMemory, discover.KindCommand, discover.KindAgent,
		discover.KindSkill, discover.KindSettings, discover.KindMCP,
	}

	var shared, unshared int
	for _, kind := range order {
		group := byKind[kind]
		if len(group) == 0 {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			if group[i].Harness != group[j].Harness {
				return group[i].Harness < group[j].Harness
			}
			return group[i].Path < group[j].Path
		})

		fmt.Printf("%s\n", strings.ToUpper(string(kind)))
		for _, a := range group {
			note := "authored here"
			switch {
			case a.SharedLink(layout.SharedRoot):
				note = "-> shared"
				shared++
			case a.IsSymlink:
				note = "-> " + a.LinkTarget
			default:
				if a.Harness != discover.Shared {
					unshared++
				}
			}
			fmt.Printf("  %-12s %-10s %-14s %s\n",
				a.Harness, a.Scope, note, display(a.Path, layout.Home))
		}
		fmt.Println()
	}

	fmt.Printf("%d artifact(s) already shared, %d still harness-specific\n", shared, unshared)
	return nil
}
