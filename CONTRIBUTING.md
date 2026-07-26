# Contributing

## Before you open a PR

```bash
make check    # gofmt, go vet, go test -race
make cover    # library coverage must stay at or above the floor
make lint     # golangci-lint
```

CI runs all three. `make lint` needs a one-time install:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

## Code guidelines

**Refuse rather than guess.** An unknown model family gets an error, not a
plausible JSON path that silently does nothing. A wrong value written to a
config Kiro CLI ignores is indistinguishable from a broken feature, and costs
someone an afternoon.

**Preserve unknown fields.** Config files are patched through
`map[string]json.RawMessage`, so keys this tool does not model survive a write.
Never unmarshal into a struct and marshal it back — that silently deletes
whatever the struct omits.

**Never clobber what you did not write.** Managed files carry a marker; anything
without one is left alone unless `-force`. Check ownership *before* checking
idempotency, or a user's own file gets reported as already installed.

**Fail open in hooks, fail closed in verifiers.** A `preToolUse` hook that
blocks a payload shape it does not recognise breaks every session after a
schema change, so unparseable input allows the call. A loop verifier with no
configured metric fails, because a verifier that always passes is worse than no
loop.

**Comments say why, not what.** `// increment i` is noise. `// Ownership is
checked before idempotency, or a file the user wrote is silently adopted` is the
reason someone will need in six months.

**No dependencies without a strong reason.** Standard library only, and there is
no `go.sum`. Every dependency is a supply-chain surface on a tool that writes to
your home directory.

**cmd/ stays thin.** Parse arguments, call one package, format output. Logic
that deserves a test belongs in a package, not in a command handler.

## Test guidelines

**Test the behaviour, not the implementation.** Assert on the outcome a user
would notice. A test that breaks when you rename a private function was testing
the wrong thing.

**Name the failure, not the function.** `TestOnIsIdempotent` beats `TestOn`.
`TestRefusesToTouchAFileItDidNotCreate` beats `TestConflict`. The name should
tell you what broke without opening the file.

**Say why in the test, when the why is not obvious.** Several tests here carry a
comment explaining the hazard they guard, because the assertion alone does not
convey that `agentSpawn` runs once and must not be charged per turn.

**Prove the negative too.** For every rule that blocks something, test that it
allows the adjacent legitimate case. `git commit -s` is blocked; `git commit -m`
must not be. Rules without a negative test become false positives nobody
notices.

**Use real temp directories.** `t.TempDir()`, real files, real symlinks. This
project deliberately has no filesystem abstraction, so tests exercise the actual
syscalls. A mocked filesystem would have hidden the macOS `/var` to
`/private/var` symlink resolution bug that a real one caught.

**External test packages for public API.** `package enforce_test` can only touch
exported identifiers, so it fails to compile if the public surface stops being
usable from outside. An internal test cannot catch that.

**Never delete or weaken a test to make a suite pass.** This is enforced at the
tool level by `kirobuff enforce`, and it applies to the humans too. A failing
test is information.

## Adding a mode

Cheapest contribution in the project. A mode is a catalogue entry plus a
steering fragment — no new agent, no new plumbing.

1. Add an entry to `all()` in `internal/mode/catalogue.go`
2. Keep the fragment under about 2000 bytes: it is re-sent on **every turn**,
   and a test enforces the ceiling
3. Write it as a lens that changes what gets noticed, not as a personality

If you keep re-explaining the same perspective to your agent every session,
that perspective is already a mode. It just is not in the catalogue yet.

## Adding an enforcement rule

Implement `enforce.Rule`. Two hard requirements:

- **Decidable from the tool input alone.** Anything needing judgment belongs in
  steering, not in a wall.
- **Tolerate any event.** Return `enforce.Allow` for tools you do not handle,
  including the zero `Event`. A test asserts this for every default rule.

Write the block reason for the *model*, not a human: it is returned on exit 2
and should say what to do instead.

## Commits

Conventional Commits, because the release version is derived from them:

| Prefix | Bump |
|---|---|
| `feat!:` or a `BREAKING CHANGE:` footer | major |
| `feat:` | minor |
| `fix:` `perf:` `refactor:` `revert:` | patch |
| `docs:` `test:` `chore:` `ci:` `style:` | none |

Anything unrecognised counts as a patch, so an unconventional subject can never
cause a real change to ship unversioned.

Check what your branch implies:

```bash
kirobuff version next
```

**Squash-merge titles matter.** The release version is derived from commit
subjects on the default branch, and GitHub's default squash title is the branch
name, not your commit subject: `Fix/ci lint config (#7)` has no colon, so it
falls through to `patch` regardless of what the branch actually did. One release
here was classified `major` only because the squash body happened to retain a
`BREAKING CHANGE` footer.

Either set the squash title to a conventional subject by hand, or enable
"Default to pull request title and commit details" in repository settings and
title pull requests conventionally. Otherwise `kirobuff version next` slowly
stops reflecting reality.

**Do not push to the default branch.** It is protected, and
`kirobuff preflight install` blocks it locally before git ever contacts the
remote:

```
[blocker] protected-branch
          "master" is a protected branch and should receive changes through
          a pull request
          run: git switch -c feat/your-change
```

This happened during development: a release fix went straight to `master`,
bypassing branch protection and leaving an unsigned commit on a branch that
requires signatures. The check existed and was ignored.

Commits touching AI-generated code should carry an `Assisted-by` trailer, and
must not carry an agent's `Signed-off-by` — only a human can certify the DCO:

```bash
kirobuff attest -model <model-id> -agent kiro-cli -f .git/COMMIT_EDITMSG -w
```
