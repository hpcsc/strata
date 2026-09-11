# strata

strata is a terminal UI to review a stack of git branches, one branch at a time. Each branch shows only
its own changes: the diff against the branch it sits on, not the diff against the trunk.

```
┏━ Stack ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃  origin/main                                                                ┃
┃  ├─ billing-invoice    2 commits   2 files   +7 -0                          ┃
┃  │  └─ billing-pdf     1 commit    1 file    +7 -0  1 behind parent         ┃
┃  └─ orders-events      3 commits   2 files   +31 -0                         ┃
┃▌    └─ orders-handler  2 commits   2 files   +62 -0  1 behind parent        ┃
┃        └─ orders-api   1 commit    2 files   +23 -0                         ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
╭─ Files · orders-events ─╮╭─ orders/order.go · 2/2 · top ─────────────────────╮
│  A   orders/events.go   ││@@ -1,5 +1,7 @@                                    │
│▌ M   orders/order.go    ││ 1 package orders        │ 1 package orders        │
│                         ││ 2                       │ 2                       │
│                         ││                         │ 3 import "time"         │
╰─────────────────────────╯╰───────────────────────────────────────────────────╯
```

## Install

```sh
task install
```

This builds `strata` into `~/.local/bin`. strata needs Go 1.26 and git 2.41 or later, because it uses the
`ahead-behind` field of `git for-each-ref`.

## Use

Run strata in a git repository:

```sh
strata                     # the local branches
strata refs/heads/team/    # only the branches that match a for-each-ref pattern
strata --remote            # the branches on origin, for example the stack of a teammate
strata --list              # print the stack and exit
strata --trunk develop     # use a trunk other than origin/HEAD, origin/main or main
```

`task demo` opens strata on a throwaway repository with two stacks. The script deletes the repository when
you quit.

## Screen

- The **Stack** panel shows the branches as a tree under the trunk. Each row shows the commits, files and
  lines that the branch adds to its parent. "1 behind parent" tells you that the parent has a commit that
  the branch does not have, so the branch needs a restack.
- The **Files** panel shows the files that the selected branch changes.
- The **Diff** panel shows the selected file, side by side or unified, with syntax colours and marks on the
  changed words. When the Stack panel has the focus, this panel shows the commits of the branch.

## Keys

| Where | Key | Action |
| --- | --- | --- |
| Anywhere | `[` `]` | Go to the previous or next branch. The same file stays selected when that branch changes it. |
| | `tab` | Go to the next panel. `shift+tab` goes to the previous panel. |
| | `s` | Show the diff side by side or unified. |
| | `z` | Show the diff on the full screen. |
| | `v` | Mark the file viewed and go to the next file. |
| | `r` | Read the branches again. |
| | `?` | Show all keys. |
| | `q` | Quit. |
| Stack | `j` `k` `g` `G` | Move between branches. |
| | `enter` | Go to the files of the branch. |
| Files | `j` `k` `g` `G` | Move between files. |
| | `enter` | Go to the diff. |
| | `ctrl+d` `ctrl+u` | Scroll the diff. |
| Diff | `j` `k` | Scroll one line. |
| | `ctrl+d` `ctrl+u` | Scroll half a page. `space` and `b` scroll a full page. |
| | `n` `N` | Go to the next or previous hunk. |
| | `J` `K` | Go to the next or previous file. |
| | `esc` | Go back to the files. In zoom, `esc` ends the zoom first. |

## Parents

strata finds the parent of each branch in this order:

1. The parent is the nearest branch whose tip the branch contains.
2. A parent that got new commits after the branch started fails that test. strata then reads the reflogs.
   The reflog of the branch names the branch it started from, or the HEAD checkout that created it, or the
   commit it started at. strata uses the branch that had that commit as its tip.
3. When neither test finds a parent, the branch sits on the trunk.

The diff of a branch starts at the newest commit that the branch shares with its parent. Remote branches
have no reflog of their creation, so `--remote` uses only the first test.

## Viewed files

`v` marks a file viewed. strata keeps the marks in `.git/strata/viewed`. A mark names the content of the
file before and after the change. A new change to the file therefore clears the mark.

## Development

```sh
task check              # build, vet, unit tests and integration tests
task test               # unit tests only
task test:integration   # tests that build throwaway git repositories
```

GitHub Actions runs `task check` on each push to a branch. A tag such as `v0.1.0` starts the release
workflow. The workflow runs the same checks, then goreleaser builds archives for macOS and Linux (amd64
and arm64) and publishes them to a GitHub release.
