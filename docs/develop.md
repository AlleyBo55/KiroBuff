# Develop

Layout, conventions, and how a version gets decided.

## Develop

```bash
make build      # ./bin/kirobuff
make test       # go test ./...
make check      # gofmt -l, go vet, go test
make install    # build + copy to ~/.local/bin
make clean
```

Layout:

```
cmd/kirobuff/          CLI: flag parsing, output, nothing else
internal/budget/       recurring token-cost analysis
internal/discover/      locate config artifacts in both harnesses
internal/enforce/       preToolUse rules that block, not advise
internal/guard/        hook installation into agent configs
internal/loop/         Karpathy loop scaffolding
internal/persona/      switchable agent modes
internal/statusline/   OSC 0 terminal-title indicator
internal/steering/     always-on guardrails
internal/tune/         per-model effort defaults
```

Conventions:

- **Read-only until asked.** Nothing writes without an explicit subcommand.
- **Never clobber.** Anything kirobuff didn't write is left alone unless
  `-force`. Managed files carry a marker.
- **Preserve unknown fields.** Configs are patched through
  `map[string]json.RawMessage`, so fields kirobuff doesn't model survive.
- **Refuse rather than guess.** An unknown model family gets an error, not a
  plausible JSON path that silently does nothing.
- **Zero dependencies.** Standard library only. No `go.sum`.

## On clean architecture

Packages are organised by feature, not by architectural layer. There are no
ports, no interfaces, and no dependency inversion: `internal/*` calls `os` and
`os/exec` directly.

That is a considered tradeoff rather than compliance. For a CLI this size,
layering would be ceremony, and the tests exercise real temporary directories
instead of a mocked filesystem port - which is stronger evidence that the code
works than a satisfied interface would be.

What clean-code practice does apply here: one responsibility per package, small
functions, comments that explain why rather than what, and tests read as
documentation. `cmd/kirobuff` is deliberately thin - it parses arguments, calls
one package, and formats output. It holds no logic worth testing in isolation,
which is why the only test there covers argument handling.

If you are adding a package, the bar is: could someone delete every other
package and still understand this one.

Set the module path in `go.mod` before publishing — it is currently the bare
name `kirobuff`.

---

## Versioning and releases

The version follows from what changed, rather than being decided at release
time.

```bash
kirobuff version        # build identity
kirobuff version next   # what the next release should be, and why
```

```
last tag   v0.2.1
commits    7 since then
bump       minor
next       v0.3.0

  git tag -a v0.3.0 -m "release v0.3.0" && git push origin v0.3.0
```

Commit subjects map to bumps via Conventional Commits:

| Subject | Bump |
|---|---|
| `feat!:` or a `BREAKING CHANGE:` footer | **major** |
| `feat:` | **minor** |
| `fix:` `perf:` `refactor:` `revert:` | **patch** |
| `docs:` `test:` `chore:` `ci:` `style:` `build:` | none |
| anything unrecognised | **patch** |

That last row is deliberate. Treating an unconventional subject as *no change*
would let a real fix ship without a version bump, so the default errs toward
releasing.

A breaking change on a `0.x` line still bumps major. Folding it into a minor is
how a version series stops meaning anything.

Pushing a `v*` tag runs GoReleaser: cross-compiled binaries for macOS, Linux and
Windows on amd64 and arm64, sha256 checksums, a grouped changelog, and a
Homebrew formula. CI reports what the next version would be on every merge, so
the bump is visible before you tag.

Version is injected at link time and falls back to the module version recorded
by `go install`, then to `dev`. It is never empty, because an empty version
looks like a broken build.

---

---

[← Back to README](../README.md)
