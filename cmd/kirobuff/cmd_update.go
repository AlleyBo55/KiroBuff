package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/AlleyBo55/KiroBuff/internal/release"
	"github.com/AlleyBo55/KiroBuff/semver"
)

// Self-update: check what is published, and replace this binary with it.

// ---------------------------------------------------------------- update

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	checkOnly := fs.Bool("check", false, "report whether an update exists, change nothing")
	force := fs.Bool("force", false, "update even from a development build")
	if err := fs.Parse(permute(args)); err != nil {
		return err
	}

	in := semver.Get()
	home, _ := os.UserHomeDir()

	self, err := release.Self()
	if err != nil {
		return fmt.Errorf("cannot locate the running binary: %w", err)
	}

	src := release.Default()
	latest, err := src.Latest()
	if err != nil {
		return err
	}

	fmt.Println("kirobuff update")
	fmt.Println()
	fmt.Printf("  installed   %s\n", in.Version)
	fmt.Printf("  latest      %s", latest.Tag)
	if latest.PublishedAt != "" {
		fmt.Printf("   published %s", shortDate(latest.PublishedAt))
	}
	fmt.Println()
	fmt.Printf("  binary      %s\n", display(self, home))
	fmt.Println()

	newer, err := release.Newer(in.Version, latest.Tag)
	switch {
	case errors.Is(err, release.ErrDevBuild):
		// A locally built binary reports "dev". Overwriting one by default would
		// replace a build someone is in the middle of testing.
		if !*force {
			fmt.Printf("This is a %s build (version source: %s), so there is nothing to compare.\n",
				in.Version, in.Source)
			fmt.Println("Re-run with -force to install " + latest.Tag + " over it anyway.")
			return nil
		}
		fmt.Println("Development build, -force given: installing " + latest.Tag + " over it.")
	case err != nil:
		return err
	case !newer:
		fmt.Println("Already up to date.")
		return nil
	default:
		fmt.Printf("Update available: %s -> %s\n", in.Version, latest.Tag)
	}

	if *checkOnly {
		fmt.Println("\n  kirobuff update            install it")
		if latest.URL != "" {
			fmt.Printf("  %s\n", latest.URL)
		}
		return nil
	}

	goos, goarch := release.Platform()
	asset := release.AssetName(latest.Tag, goos, goarch)
	fmt.Printf("\nDownloading %s\n", asset)

	payload, err := src.Fetch(latest.Tag, goos, goarch)
	if err != nil {
		return err
	}
	fmt.Printf("Checksum verified (%s, %s)\n", asset, byteSize(len(payload)))

	if err := release.Replace(self, payload); err != nil {
		// The most common cause is a binary in a root-owned directory.
		return fmt.Errorf("%w\n\nIf %s needs elevated permissions, either re-run with sudo or "+
			"reinstall with:\n  curl -fsSL https://raw.githubusercontent.com/%s/master/install.sh | sh",
			err, display(self, home), release.Repo)
	}

	fmt.Printf("Replaced %s\n", display(self, home))
	fmt.Printf("\nUpdated to %s. Run `kirobuff install` if the release notes mention new config.\n", latest.Tag)
	return nil
}

// byteSize renders a size in the largest unit that keeps it readable.
func byteSize(n int) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f TiB", value/unit)
}

// shortDate keeps the date half of an RFC 3339 timestamp. The API returns
// 2026-07-26T08:15:00Z; the time of day is noise in this output.
func shortDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}
