# No conflict rules

```bash
kirobuff preflight install
```

Every push now checks your branch against its base before git talks to the
remote. Blockers stop the push and print the command that fixes them.

## What it checks

| Check | Severity | Means |
|---|---|---|
| `protected-branch` | blocker | a direct push to `master`, `main`, `trunk` or `release` |
| `squash-duplicates` | blocker | your branch carries commits the base already has under different hashes |
| `merge-conflict` | blocker | the exact files that would conflict, computed in memory |
| `behind-base` | warning | the base moved; rebasing now keeps the eventual merge trivial |
| `fetch-first` | info | results are only as current as your last fetch |

## The one nobody diagnoses

`squash-duplicates` is why this exists.

When a pull request is **squash-merged**, the base branch gains a single new
commit containing work your branch still carries as several separate ones. The
content is identical; the hashes are not. Git then reports conflicts in files
nobody edited twice, and the diff looks like nonsense.

This project hit it on PR #5. Master had `c8dff35`, one squashed commit. The
branch still had the three commits that went into it. Four files conflicted, none
of them genuinely touched on both sides.

Detection compares **patch IDs** — a hash of the diff itself, independent of
commit metadata — so the same change under a different SHA is recognised as the
same change:

```
[blocker] squash-duplicates
          1 commit(s) on feat/x already exist in origin/master under
          different hashes, which is what a squash-merged pull request
          leaves behind. Git will report conflicts in files nobody
          edited twice: cd89ba7
          run: git rebase --onto origin/master cd89ba7 feat/x
```

That command is the fix, with the drop point already worked out.

## Honest scope

This does **not** make conflicts impossible. Two people editing the same line
will always conflict, and no tool changes that.

What it guarantees is that you never find out from a pull request page. The
conflict surfaces on your machine, before the push, while the fix is one command
and not a conversation.

## Bypassing and removing

```bash
git push --no-verify                            # once
rm "$(git rev-parse --git-path hooks/pre-push)" # permanently
```

The hook is a one-line shim that calls the binary, so upgrading kirobuff upgrades
the check without anyone reinstalling anything. It also exits 0 silently when
`kirobuff` is not on `PATH`, so a contributor who has not installed it is never
blocked by a hook they cannot run.

## Use it without the hook

```bash
kirobuff preflight                    # compares against the remote's HEAD
kirobuff preflight -base origin/dev   # or an explicit base
kirobuff preflight -quiet             # silent unless something blocks
```

---

[← Back to README](../README.md)
