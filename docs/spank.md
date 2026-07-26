# Spank mode

Close the laptop, walk away, come back to finished work. Platform-specific,
and one of the three needs your explicit go-ahead.

## `mode explain spank` — work with the lid shut

```bash
kirobuff mode explain spank              # AC only, the safe default
kirobuff mode explain spank -scope all   # battery too
```

Closing the lid suspends your agent. It isn't killed — it resumes when you open
the laptop — but nothing progresses. Turning that off is a system change, and
the mechanism is different on every platform.

| Platform | Mechanism | Scoped? |
|---|---|---|
| macOS | `sudo pmset -c disablesleep 1` | no, persists across reboots |
| Linux | `systemd-inhibit --what=handle-lid-switch:sleep:idle -- <cmd>` | **yes**, released on exit |
| Windows | `powercfg /setacvalueindex SCHEME_CURRENT SUB_BUTTONS LIDACTION 0` | no, per power scheme |

**`caffeinate` does not work for this.** It holds a prevent-idle-sleep assertion
and has no effect on the lid-close path, and `caffeinate -s` is void on battery
by design — your own man page says so. Linux is the only platform with a scoped
mechanism; there is nothing to remember to undo.

`kirobuff mode explain` prints the exact enable and revert commands for your
platform, and **does not run them**. Disabling lid sleep is persistent and
system-wide, with thermal and battery consequences. That isn't a tool's call to
make on someone's laptop.

`mode status` reports whether it's currently on:

```
spank    off - closing the lid suspends the agent (pmset does not report disablesleep, so it is unset)
```

Detection is implemented for macOS only. Windows and Linux report *unknown*
rather than guessing — claiming the lid is safe when it isn't costs you a night.

**The trap nobody mentions:** if the agent hits a tool-approval prompt with the
lid shut, it waits forever. You return to zero progress and a blinking cursor.
Configure trust up front — and this is where `enforce` earns its keep. You can
trust broadly *because* the five hard rules block the calls that matter:

```bash
kiro-cli chat --trust-tools=read,grep,glob,code "your task"
```

---

[← Back to README](../README.md)
