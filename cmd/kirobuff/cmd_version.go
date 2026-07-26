package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/AlleyBo55/KiroBuff/internal/version"
)

// Build identity and the next release version.

// ---------------------------------------------------------------- version

// cmdVersionNext derives the next release version from the commits since the
// last tag, so the bump is a consequence of what changed rather than a
// judgement call at release time.
func cmdVersionNext() error {
	last, err := lastTag()
	if err != nil {
		return err
	}
	current, err := version.ParseSemver(last)
	if err != nil {
		// An unparseable or absent tag means this is the first release.
		current = version.Semver{}
		last = "(none)"
	}

	msgs, err := commitsSince(last)
	if err != nil {
		return err
	}
	bump := version.Classify(msgs)
	next := current.Apply(bump)

	fmt.Printf("last tag   %s\n", last)
	fmt.Printf("commits    %d since then\n", len(msgs))
	fmt.Printf("bump       %s\n", bump)
	fmt.Printf("next       %s\n", next)

	if bump == version.None {
		fmt.Print("\nNothing user-visible changed, so no release is needed. Only docs, " +
			"tests, chores and CI commits are present.\n")
		return nil
	}
	fmt.Printf("\n  git tag -a %s -m %q && git push origin %s\n", next, "release "+next.String(), next)
	return nil
}

func lastTag() (string, error) {
	out, err := exec.Command("git", "describe", "--tags", "--abbrev=0").Output()
	if err != nil {
		return "", nil // no tags yet is not an error
	}
	return strings.TrimSpace(string(out)), nil
}

// commitsSince reads subjects and bodies with a record separator that cannot
// appear in a commit message, so multi-line bodies parse correctly.
func commitsSince(tag string) ([]version.Message, error) {
	const sep = "\x1e"
	rangeArg := "HEAD"
	if tag != "" && tag != "(none)" {
		rangeArg = tag + "..HEAD"
	}
	out, err := exec.Command("git", "log", "--format=%s%n%b"+sep, rangeArg).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var msgs []version.Message
	for _, record := range strings.Split(string(out), sep) {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		subject, body, _ := strings.Cut(record, "\n")
		msgs = append(msgs, version.Message{
			Subject: strings.TrimSpace(subject),
			Body:    strings.TrimSpace(body),
		})
	}
	return msgs, nil
}
