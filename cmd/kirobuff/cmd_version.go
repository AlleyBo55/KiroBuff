package main

import (
	"fmt"
	"strings"

	"github.com/AlleyBo55/KiroBuff/semver"
)

// Build identity and the next release semver.

// ---------------------------------------------------------------- version

// cmdVersionNext derives the next release version from the commits since the
// last tag, so the bump is a consequence of what changed rather than a
// judgement call at release time.
func cmdVersionNext() error {
	last := lastTag()
	current, err := semver.ParseSemver(last)
	if err != nil {
		// An unparseable or absent tag means this is the first release.
		current = semver.Semver{}
		last = "(none)"
	}

	msgs, err := commitsSince(last)
	if err != nil {
		return err
	}
	bump := semver.Classify(msgs)
	next := current.Apply(bump)

	fmt.Printf("last tag   %s\n", last)
	fmt.Printf("commits    %d since then\n", len(msgs))
	fmt.Printf("bump       %s\n", bump)
	fmt.Printf("next       %s\n", next)

	if bump == semver.None {
		fmt.Print("\nNothing user-visible changed, so no release is needed. Only docs, " +
			"tests, chores and CI commits are present.\n")
		return nil
	}
	fmt.Printf("\n  git tag -a %s -m %q && git push origin %s\n", next, "release "+next.String(), next)
	return nil
}

// lastTag returns the most recent tag, or "" when the repository has none.
//
// It returns no error: an absent tag is the normal state of a repository before
// its first release, not a failure.
func lastTag() string {
	out, err := gitOutput("describe", "--tags", "--abbrev=0")
	if err != nil {
		return ""
	}
	return out
}

// commitsSince reads subjects and bodies with a record separator that cannot
// appear in a commit message, so multi-line bodies parse correctly.
func commitsSince(tag string) ([]semver.Message, error) {
	const sep = "\x1e"
	rangeArg := "HEAD"
	if tag != "" && tag != "(none)" {
		rangeArg = tag + "..HEAD"
	}
	out, err := gitOutput("log", "--format=%s%n%b"+sep, rangeArg)
	if err != nil {
		// Outside a repository, or in one with no commits, git exits 128. That
		// is a normal situation for someone running the command in the wrong
		// directory, and deserves an explanation rather than an exit code.
		return nil, fmt.Errorf("cannot read commit history: run this inside a "+
			"git repository with at least one commit (git said: %w)", err)
	}

	var msgs []semver.Message
	for _, record := range strings.Split(out, sep) {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		subject, body, _ := strings.Cut(record, "\n")
		msgs = append(msgs, semver.Message{
			Subject: strings.TrimSpace(subject),
			Body:    strings.TrimSpace(body),
		})
	}
	return msgs, nil
}
