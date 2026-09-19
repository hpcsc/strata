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
`ahead-behind` field of `git for-each-ref`. `strata sync` needs git 2.44 or later, because it uses
`git replay`.

## Use

Run strata in a git repository:

```sh
strata                     # the local branches
strata refs/heads/team/    # only the branches that match a for-each-ref pattern
strata --remote            # the branches on origin, for example the stack of a teammate
strata --list              # print the stack and exit
strata --trunk develop     # use a trunk other than origin/HEAD, origin/main or main
strata --theme github      # colour the code and the diff with a chroma style other than nord
```

`task demo` opens strata on a throwaway repository with two stacks. The script deletes the repository when
you quit.

## Sync

`strata sync` gets the new trunk from the remote and moves each stack of local branches onto it. A stack
with a conflict stays where it is until you resolve the conflict. [docs/sync.md](docs/sync.md) tells how
the sync works.

```sh
strata sync                     # fetch the trunk and move each stack that has no conflict
strata sync --dry-run           # print the plan and change nothing
strata sync --resolve <branch>  # start a sync rebase for the stack of <branch>
strata sync --keep-merged       # do not delete the merged branches
```

In the terminal UI, `S` shows the plan of a sync in the Stack panel, and `enter` moves the stack of the
selected branch. On git older than 2.44, strata does not show the sync.

## Delete

`d` in the Stack panel deletes the selected branch. To delete more than one branch, mark each one with
`space`, and then `d` deletes the marked branches. A marked branch stays in the Stack panel when a search
hides the other branches.

Before strata deletes a branch, the Stack panel shows what the delete loses, and only `y` deletes:

| The Stack panel shows | Meaning |
| --- | --- |
| `loses 2 commits` | The branch has 2 commits of its own. |
| `the trunk has its changes` | The trunk has all the changes of the branch, for example after a squash merge. |
| `removes worktree strata-api` | Another worktree has the branch checked out. strata removes that worktree. |
| `removes worktree strata-api and loses 2 changed files: a.go, notes.txt` | The worktree has modified or untracked files, and the delete loses them. |
| `forgets worktree strata-api, whose folder is gone` | The folder of the worktree is gone. strata removes the record that git keeps of it. |
| `closes 2 tmux panes in work:strata-api` | strata runs in tmux, and 2 panes of the tmux window `strata-api` in the session `work` are in the folder of the worktree. strata closes them, and stops the programs in them. |

The Stack panel shows `in <folder>` beside each branch that another worktree has checked out, and
`rebase in <folder>` beside each branch that a rebase uses.

strata does not delete these branches:

- The branch that strata runs on, and a branch that the main worktree has checked out.
- A branch whose worktree is locked. `git worktree unlock` unlocks it.
- A branch that a rebase uses, in any worktree.
- A branch that another branch sits on, unless you delete that branch too. strata finds each parent from
  the commits, so the branch that stays then shows the commits of the deleted branch as its own.

strata removes the worktrees with `git worktree remove` first. A worktree with modified or untracked files
needs `--force`, and strata uses it only for the files that the Stack panel showed. When the worktree
changes after strata shows the delete, strata removes nothing and asks you to press `d` again. git also
removes the ignored files of a worktree, such as build output.

When strata runs in tmux, it closes each pane whose folder is in a removed worktree, after it removes the
worktree. A window closes with its last pane. strata closes only the panes that the Stack panel showed, and
only when they are still in the worktree. A pane that goes into the worktree after the Stack panel shows the
delete, and a shell outside tmux, stay open in a folder that is gone.

Then strata deletes the branches in one transaction, with the tip that it read for each branch. When a
branch moved after strata read it, strata deletes no branch. When a step fails, the status line names the
worktrees that strata already removed. When the delete succeeds, the footer names each deleted branch with
its tip, for example `Deleted billing (was 1a2b3c4).` `git branch billing 1a2b3c4` brings the branch back.

strata deletes only local branches. With `--remote`, `space` and `d` do nothing.

## Version and update

```sh
strata version                # the tag of a release or a prerelease, or the commit of any other build
strata update                 # install the latest release
strata update --prerelease    # install the latest prerelease, a build of main
strata update --check         # only tell you whether this build is the latest
```

`strata update` downloads the archive for your platform from a GitHub release, checks it against the
`checksums.txt` of that release, and then replaces the strata binary. It names each step on stderr, and in a
terminal it shows how much of the download has arrived. `GITHUB_TOKEN` or `GH_TOKEN` gives access to a
private repository.

Releases and prereleases are two channels:

- `strata update` installs the latest release.
- `strata update --prerelease` installs the latest prerelease.

Each command installs the latest build of its channel when this build is a different one. So
`strata update` on a prerelease goes back to the latest release. A build from a commit, for example from
`task install`, is not a release or a prerelease, so `strata update` does not replace it unless you add
`--force`.

## Screen

- The **Stack** panel shows the branches as a tree under the trunk. Each row shows the commits, files and
  lines that the branch adds to its parent. "1 behind parent" tells you that the parent has a commit that
  the branch does not have, so the branch needs a restack.
- The **Files** panel shows the files that the selected branch changes, under their folders. A chain of
  folders that each hold only one folder shows on one row. `o` folds a folder, and a folder folds by itself
  when you have viewed every file in it. A folder that you unfold with `o` stays open. `t` shows the files
  as a list of paths.
- The **Diff** panel shows the selected file, side by side or unified, with syntax colours and marks on the
  changed words. `w` adds the lines that the diff leaves out, so you read the change in the code around it.
  The hunk that was at the top of the panel stays there, and `n` and `p` still go from change to change. A
  file over 1 MB keeps only its changed lines, and the panel says so. When the Stack panel has the focus,
  this panel shows the commits of the branch. When a folder is selected, it shows the files in the folder
  and their line counts.

## Keys

These are the default keys. The [config file](#config-file) can change them.

| Where | Key | Action |
| --- | --- | --- |
| Anywhere | `[` `]` | Go to the previous or next branch. The same file stays selected when that branch changes it. |
| | `tab` | Go to the next panel. `shift+tab` goes to the previous panel. |
| | `s` | Show the diff side by side or unified. |
| | `w` | Show the whole file, or only the changed lines. |
| | `z` | Show the diff on the full screen. |
| | `v` | Mark the file viewed and go to the next file. |
| | `t` | Show the files as a tree or as a list of paths. |
| | `/` | Search the panel that has the focus. See [Search](#search). |
| | `r` | Read the branches again. See [Refresh](#refresh). |
| | `S` | Get the trunk from the remote and show the plan of a sync. See [Sync](#sync). |
| | `?` | Show all keys. The keys of the Diff panel scroll them when they do not all fit. |
| | `Q` | Quit. |
| Stack | `j` `k` `g` `G` | Move between branches. |
| | `enter` | Go to the files of the branch. |
| | `space` | Mark the branch to delete, or unmark it. |
| | `d` | Delete the marked branches, or the selected branch. See [Delete](#delete). |
| Sync plan | `enter` | Move the stack of the selected branch. The plan stays open with the other stacks. |
| | `c` | Resolve the conflict of the stack of the branch in a shell. |
| | `q` `esc` | Close the plan. |
| Files | `j` `k` `g` `G` | Move between files and folders. |
| | `o` | Fold or unfold the folder. On a file, fold the folder that holds it. |
| | `enter` | Go to the diff. |
| | `q` `h` `esc` | Go back to the stack. |
| | `ctrl+d` `ctrl+u` | Scroll the diff. |
| Diff | `j` `k` | Scroll one line. |
| | `ctrl+d` `ctrl+u` | Scroll half a page. `space` and `b` scroll a full page. |
| | `n` `p` | Go to the next or previous hunk. While a search is on, `n` and `N` go to the next or previous match. |
| | `J` `K` | Go to the next or previous file. The files in a folded folder do not count. |
| | `q` `h` `esc` | Go back to the files. In zoom, these keys end the zoom first. |

## Refresh

`r` reads the branches again. strata keeps the files and diffs that it loaded for each branch that did not
move. It loads again the commit lists, so their ages change, and each file list or diff whose load failed.

With `auto_refresh = true` in the [config file](#config-file), strata also refreshes by itself when git
changes a branch, the trunk or the branch that a worktree checks out. For example, a commit, an amend or a
fetch in another terminal shows in strata with no key press.

- strata watches the files in `.git` that hold the refs, and checks the refs each 30 seconds too. When it
  cannot watch these files, it checks the refs each 2 seconds.
- A refresh keeps the selected branch and file, the viewed counts, and the scroll position of a diff whose
  file did not change.
- strata does not refresh by itself while the sync plan shows, while a delete waits for `y`, or while a move
  or a delete runs. It refreshes after that.
- strata does not refresh by itself with `--remote`, because a read of the branches of a remote can take
  many seconds.

[docs/auto-refresh.md](docs/auto-refresh.md) tells how the automatic refresh works.

## Search

`/` opens a search line at the bottom of the screen. The search applies to the panel that has the focus,
and the panel changes as you type:

| Panel | Search |
| --- | --- |
| Stack | Shows only the branches that match, and the branches they sit on. |
| Files | Shows only the files whose paths match, and their folders. The filter stays when you go to another branch. |
| Diff | Marks the text that matches and goes to the first match. `n` and `N` go to the next and previous match. |

`enter` keeps the search and `esc` clears it. When the search line is closed, `esc` in the panel clears its
search. A search in lower case ignores case, and a search with a capital letter matches case exactly.

## Config file

strata reads options and keys from `~/.config/strata/config.toml`. When `XDG_CONFIG_HOME` is set, strata
reads `$XDG_CONFIG_HOME/strata/config.toml`. `--config <path>` gives a different file. When the file does
not exist, strata uses its defaults.

```toml
theme = "github"
split = false
auto_refresh = true

[keys]
quit = ["Q", "ctrl+q"]

[keys.diff]
next_hunk = ["n", "ctrl+n"]
previous_hunk = ["p", "ctrl+p"]
next_file = "L"
previous_file = "H"
```

- `theme` is the chroma style of the code and the diff. `--theme` overrides it.
- `split = false` starts the diff unified, not side by side.
- `auto_refresh = true` makes strata read the branches again when git changes them. See
  [Refresh](#refresh). It is off by default.
- An action takes one key, a list of keys, or `[]` for no key. The keys replace the default keys of the
  action. In this example, `J` and `K` do nothing in the diff.

`strata config` prints a config file with all the options and all the actions, with their default keys.
Use it to start your own file:

```sh
mkdir -p ~/.config/strata
strata config > ~/.config/strata/config.toml
```

A key name is the name that Bubble Tea gives the key. It is a character, such as `n`, `N` or `?`, or a
name, such as `enter`, `esc`, `space`, `tab`, `shift+tab`, `up`, `pgdown`, `home` or `ctrl+d`. `ctrl+c`
always quits, so no action can have it.

Each table applies at a different time. strata looks for a key in the tables in this order:

| Table | When it applies |
| --- | --- |
| `[keys.plan]` | While the sync plan is open. Its actions apply only in the Stack panel. |
| `[keys.search]` | While the panel with the focus has a search. `next_match` and `previous_match` apply only in the Diff panel. |
| `[keys]` | In all panels. |
| `[keys.stack]`, `[keys.files]`, `[keys.diff]` | In the panel with the focus. |

strata checks the file before it opens the screen. It stops with an error for these problems:

- The file is not correct TOML.
- An option, a table, an action or a key has a name that strata does not know.
- Two actions in one table have the same key.
- An action in `[keys]` and an action in a panel table have the same key. The action in the panel table
  can never run, because strata looks in `[keys]` first.

## Parents

strata finds the parent of each branch in this order:

1. The parent is the nearest branch whose tip the branch contains.
2. A parent that got new commits after the branch started fails that test. strata then reads the reflogs.
   The reflog of the branch names the branch it started from, or the HEAD checkout that created it, or the
   commit it started at. strata uses the branch that had that commit as its tip.
3. When neither test finds a parent, the branch sits on the trunk.

The diff of a branch starts at the newest commit that the branch shares with its parent. Remote branches
have no reflog of their creation, so `--remote` uses only the first test. [docs/stack.md](docs/stack.md)
gives the rules in full, with diagrams.

## Viewed files

`v` marks a file viewed. strata keeps the marks in `.git/strata/viewed`. A mark names the content of the
file before and after the change. A new change to the file therefore clears the mark.

## Development

```sh
task check              # build, vet, unit tests and integration tests
task test               # unit tests only
task test:integration   # tests that build throwaway git repositories
task test:e2e           # end-to-end tests in Docker, as CI runs them
task test:e2e:local     # the same tests on this machine, without Docker
```

The end-to-end tests in `e2e/` run the strata binary in a terminal with
[tuistory](https://www.npmjs.com/package/tuistory). Each test builds its own git repository with a stack,
presses keys, and reads the screen. [docs/e2e-tests.md](docs/e2e-tests.md) tells how they work.

GitHub Actions runs `task check` and `task test:e2e` on each push to a branch. A tag such as `v0.1.0` starts the release
workflow. The workflow runs the same checks, then goreleaser builds archives for macOS and Linux (amd64
and arm64) and publishes them to a GitHub release.

Each push to main starts the prerelease workflow. It runs the same checks, then tags the commit with the
next patch after the latest release, the run number and the commit, for example `v0.2.1-42.g4829f92`.
goreleaser then publishes the archives to a GitHub prerelease. The workflow keeps the 5 newest prereleases
of main and deletes the older ones with their tags.
