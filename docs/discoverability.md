# Discoverability

The repository currently has **no description and no topics**. Verified against
the GitHub API:

```
description: None
topics: []
homepage: None
```

That is the single biggest gap. GitHub search weights the description heavily,
Google uses it as the result snippet, and topic pages are how people browse.
Neither can be set from a commit — they need repository settings or an
authenticated API call, so they are listed here rather than silently skipped.

## Set these in repository settings

**Description** (keep it under 160 characters so Google does not truncate it):

```
A buff for Kiro CLI: enforced guardrails that block unsafe AI agent edits,
composable modes, effort tuning, and token-cost budgeting.
```

**Website**: `https://pkg.go.dev/github.com/AlleyBo55/KiroBuff`

**Topics** (GitHub allows 20; these are the terms people actually search):

```
ai-coding-assistant   agentic-ai          ai-agents
llm-tools             developer-tools     cli
golang                guardrails          ai-safety
code-quality          static-analysis     git-hooks
conventional-commits  semantic-release    kiro
claude                anthropic           llm-cost-optimization
prompt-engineering    devtools
```

Or with the `gh` CLI:

```bash
gh repo edit AlleyBo55/KiroBuff \
  --description "A buff for Kiro CLI: enforced guardrails that block unsafe AI agent edits, composable modes, effort tuning, and token-cost budgeting." \
  --homepage "https://pkg.go.dev/github.com/AlleyBo55/KiroBuff" \
  --add-topic ai-coding-assistant --add-topic agentic-ai --add-topic ai-agents \
  --add-topic llm-tools --add-topic developer-tools --add-topic cli \
  --add-topic golang --add-topic guardrails --add-topic ai-safety \
  --add-topic code-quality --add-topic git-hooks --add-topic conventional-commits \
  --add-topic kiro --add-topic claude --add-topic llm-cost-optimization
```

## Why pkg.go.dev search did not find it

It was not an indexing delay. Every package lived under `internal/`, and
pkg.go.dev **never documents internal packages** — there was nothing to index
beyond a `main` package with no exported API.

Fixed by exporting the three genuinely reusable packages: `enforce`, `attest`
and `semver`, plus a root `doc.go` that gives the module a landing page.

To confirm indexing after a release, visit the module page once; that triggers a
fetch from the module proxy:

```
https://pkg.go.dev/github.com/AlleyBo55/KiroBuff
```

Search indexing follows within a day or so of the page existing with documented
packages. A tagged release helps: modules with a semantic version rank above
pseudo-versions.

## What is already done in-repo

- Root `doc.go` with a package overview and a keyword paragraph
- One-line description under the README title, matching the repo description
- pkg.go.dev, Go Report Card and licence badges
- A library section showing `enforce` used as an import, so the value is legible
  to someone who arrived looking for a Go package rather than a CLI
- A closing keyword block covering alternate spellings: `kiro buff`, `KiroBuff`,
  `kiro-buff`
- `CONTRIBUTING.md`, which GitHub surfaces in the sidebar and community profile

## What would help most next

1. Description and topics, above. Nothing in-repo substitutes for them.
2. A release with binaries attached, so the install path works without Go.
3. A short demo GIF or asciinema in the README. Terminal tools get shared on the
   strength of one screenshot more than any prose.

---

[← Back to README](../README.md)
