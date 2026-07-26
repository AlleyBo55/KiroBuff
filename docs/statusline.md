# Status line

Active modes and live context cost, in your terminal tab title.

## `statusline` — mode and cost in your tab title

```bash
kirobuff statusline install .kiro/agents/mine.json -max 2000
```

```
kirobuff · myproject · tech-cofounder · 3.0k/2.0k! tok
```

Kiro CLI has **no** `statusLine` setting. The terminal window title is the only
surface available, so kirobuff writes an OSC 0 sequence straight to `/dev/tty`,
bypassing the hook's captured stdout. Nothing goes to stdout, so it costs
nothing per turn.

**Credits are not shown.** Account usage is reachable only through the
in-session `/usage` command — `kiro-cli user` has no usage subcommand. A number
here would be invented or stale, so there isn't one.

## `status` — what's installed where

```bash
kirobuff status
```

Scans both Claude Code and Kiro CLI across user and workspace scope, and reports
which artifacts are already shared via symlink.

---

---

[← Back to README](../README.md)
