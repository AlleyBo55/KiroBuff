# Known limits

Properties of the tools, not bugs to file. Listed so nobody discovers them
the hard way.

## Known limits

Honest list. These are properties of the tools, not bugs to file.

**No real status line.** Kiro CLI has no `statusLine` setting. The tab title is
all there is.

**Status line is UNVERIFIED.** Whether a Kiro CLI hook subprocess inherits a
controlling terminal has not been confirmed — the shell used to develop this had
none. The escape sequence and the graceful fallback are tested; the `/dev/tty`
write is not. Start a session and check your tab title.

**No credit balance anywhere.** In-session `/usage` only.

**spank detection is macOS only.** `pmset` gives a reliable answer there.
Windows and Linux report *unknown* rather than guessing. The command sequences
for those platforms follow their documented interfaces but have not been
executed here.

**macOS only, in practice.** `filepath` is used throughout so paths should
hold, but nothing has been run on Linux or Windows, where symlink semantics
differ.

**Hardcoded paths.** `KIRO_HOME` is honoured. `~/.claude` and `~/.agents` are
not configurable.

**No Homebrew.** GoReleaser v2 deprecated `brews`, and either that or its
replacement needs an `AlleyBo55/homebrew-tap` repository plus a
`HOMEBREW_TAP_TOKEN` secret. Neither exists, so `brew install
AlleyBo55/tap/kirobuff` fails with "No available formula or cask". `install.sh`
and `go install` cover every supported platform in the meantime, and both are
verified against the current release.

**pkg.go.dev will not match `kiro buff` as two words.** It tokenises the module
name as one, so `kirobuff`, `KiroBuff`, `kiro` and `kiro cli` all find it and
the spaced form returns nothing. That is how their index works; nothing in this
repository changes it.

**pkg.go.dev documents the latest tag, not the default branch.** A change to the
public API is invisible there until it is tagged. Every version before `v0.1.0`
kept all packages under `internal/`, which pkg.go.dev never documents, so the
module appeared to have no API at all.

**bytes/4 is an estimate.** It matches Kiro CLI's own `/context`
approximation. Use it to rank fixes, not to predict a bill.

**Array-format hooks get no budget findings.** That format carries no
`cache_ttl_seconds`, so it's skipped rather than guessed at.

**Top-level JSON keys get sorted** when a config is patched, because Go marshals
map keys in order. Values are untouched; your first diff will show the
reshuffle.

**Unverified:** whether a Kiro agent's `prompt` field replaces or appends to the
built-in system prompt. It determines how much base behaviour persists under a
persona. Test before relying on it.

---

## Not built yet

- `plan` / `sync` — bidirectional config sync with Claude Code
- Bilevel loops: an outer loop that improves how the inner loop searches
- `statusline uninstall`
- Additional personas beyond `tech-cofounder`

---

---

[← Back to README](../README.md)
