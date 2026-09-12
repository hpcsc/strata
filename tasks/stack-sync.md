# strata sync: task breakdown

## Contents

1. Task 1: Plan a sync with `strata sync --dry-run` behind the git version gate
2. Task 2: Find the merged branches in the plan
3. Task 3: Replay each branch in memory and keep a stack whole when one branch stays
4. Task 4: Keep a stack when a worktree rebases or changes its branch
5. Task 5: Move the stacks that no worktree holds with `strata sync`
6. Task 6: Move checked-out branches in their worktrees
7. Task 7: Sign the moved commits with a sync rebase
8. Task 8: Show the plan in the Stack panel with `S`
9. Task 9: Move the stacks from the plan with `enter`
10. Task 10: Resolve a conflict with `strata sync --resolve`
11. Task 11: Resolve a conflict from the terminal UI with `c`
12. Task 12: Document `strata sync` for users

## Story Reference

The request: implement `strata sync` from the design doc, and follow its "Order of work": (1) the git version
gate and `strata sync --dry-run`, (2) `strata sync` moves stacks (replay commits, or signed commits through a
sync rebase when `commit.gpgsign` is true), deletes merged branches, and applies the move safely in worktrees,
(3) the terminal UI `S` preview and `enter`, (4) conflicts: `c`, `--resolve`, the shell and the record. Make
logical commits. The work goes on the branch `stack-sync` of this worktree.

The specification is `/Users/davidnguyen/Personal/Code/strata/.scratches/stack-sync/design.md`. It is
git-ignored, so it is not in this worktree. Its sections "Git version", "The steps of a sync" (1-7), "One
stack moves as one", "Conflicts", "Worktrees", "Outcomes", "Code", "Tests" and "Order of work" define the
behaviour. The git behaviour in its appendix is settled, so no task re-investigates it.

The caller's coverage list: (1) the git version gate, (2) `strata sync --dry-run`, (3) `strata sync` applies
the plan, (4) the TUI `S` / `enter` / `esc`, (5) conflicts, (6) user documentation and e2e tests. Each task
is one logical commit that builds and passes `task check`.

## Boundaries

**Out of scope** (the design's "Out of scope", verbatim):

- **Push:** after a sync, each branch that moved needs `git push --force-with-lease`.
- **`--remote`:** the branches of a teammate are not yours to move.
- **Merge commits:** a stack stays when a branch has a merge commit in its own commits.

**Deferred** (the design's "Open questions"; this work builds the design as written):

- The sync worktree stays at `.git/strata/sync/worktree`. This work does not build a folder next to the
  repository.
- One way to make commits: an unsigned sync keeps the replay's commits. This work does not build a sync
  rebase for every sync.
- git 2.41 for all of strata: `git.Version` gates only the sync. The rest of strata keeps its current
  behaviour on old git.
- Push: this work does not build a key that pushes the moved branches with `--force-with-lease`.

**Added by this breakdown:**

- `scripts/demo.sh` and `task demo` do not change. The demo repository gets no sync scenario.
- Windows is not a target (`.goreleaser.yaml` builds linux and darwin), so the TUI's fetch in a new process
  session needs no Windows variant.
- Where the design is silent, these points are decided here. The caller can overrule them:
  - The "N new commits" on the first line of the plan is the number of commits that the fetch added to the
    trunk. The design's Goal says "get the new commits of the trunk from the remote".
  - `strata sync --dry-run` exits 1 when a stack stays because of a conflict, as `strata sync` does. The
    design gives that exit status for the command whose output is the plan, and `--dry-run` only stops
    before the move.
  - The design names no outcome text for a merged branch that `--keep-merged` keeps. The only requirement
    here is that the plan does not say that strata deletes it.
  - The design names no outcome text for a worktree whose HEAD is no longer on the old tip. The only
    requirement here is that the stack stays and the outcome names the worktree.

## Codebase Context

- **`internal/git`**: `Repo` (`repo.go`) runs git in a directory, with `GIT_OPTIONAL_LOCKS=0` and
  `GIT_PAGER=cat`. `Run` captures stdout and wraps each failure in `*git.Error` with the stderr text. `Check`
  reads exit status 1 as "no". Nothing gives git stdin, extra environment for one call, or the terminal. The
  design's Code table puts `(*Repo).Version` in `version.go`, but the repository rule (and
  `clerk lint --rule type-methods-split`) keeps a method in the file of its type. So `(*Repo).Version` goes
  in `repo.go`, and `version.go` holds the new `Version` type.
- **`internal/stack`**: `Reader.Read` runs `for-each-ref --no-merged <trunk>` (fields: short name, ref,
  `%(objectname)`, `%(ahead-behind:<trunk>)`), `rev-list --parents`, reads the reflog files, and runs
  `diff --shortstat`. It returns a `Tree` in tree order, with parents before children. The reader knows each
  tip commit, but `Branch` does not carry it. `graph` keeps the parents of each commit, inside the graph and
  on the trunk, so merge commits are visible with no extra process. `readReflog` and `readCheckouts` read
  files under the common dir and `worktrees/*` with no process, which is the precedent for the rebase
  `head-name` check. `Tree.Connectors` draws the tree lines that `--list` and the Stack panel use.
- **`cmd/strata/main.go`**: the urfave/cli v3.11.0 root has the flags `--remote`, `--trunk`, `--list` and
  `--theme`, and the subcommands `version` and `update`. `newCommand()` takes no argument, and no command is
  `Hidden` (v3.11.0 has the field). `printTree` prints the `--list` layout. `run` gets the common dir with
  `rev-parse --git-common-dir`, and strata already keeps `.git/strata/viewed` there, so `.git/strata/sync/`
  goes beside it.
- **`internal/ui`**: `Sources` holds interfaces that `ui` defines (`TreeReader`, `DiffLoader`, `Highlighter`,
  `ViewedMarks`). A background load is a `tea.Cmd` that returns a message (`reloadTree` returns
  `treeLoaded`). `m.status` shows an error in the footer until the next key. The footer has one hint line
  for each focus. The keys screen is the static `helpLines`. The method `Model.sync()` means "point the
  Files and Diff panels at the selection", so it clashes with the stack sync under the rule of one word for
  one concept. Nothing in the repo uses `tea.Exec*` yet, and bubbletea v1.3.10 has `tea.ExecProcess`.
- **Unit tests of the UI**: `TestModel` drives the model with memory sources and a `settle` loop that runs
  each command in the test. The program handles the message that `tea.ExecProcess` returns, not the model,
  so only an end-to-end test can see the shell of `c`.
- **Test repositories**: `gittest.New` makes a bare `origin.git` and a clone, with one pushed commit and
  `origin/HEAD`. gittest sets the identity, `commit.gpgsign=false` and `core.hooksPath` only for its own git
  commands, through the environment. strata's `git.Repo` gets the environment of the test process, which
  includes the user's global config. On this machine the global config sets `commit.gpgsign=true`, and the
  e2e Docker image has no git identity. So when strata starts to make commits (`git replay` and the sync
  rebase), each test repository needs its own config. The same applies to `e2e/testUtils.ts`, where
  `gitEnv` covers only the helpers' git calls.
- **e2e**: `runStrata(cwd, args, env)` runs commands, and `openStrata(cwd, args)` opens the screen, with no
  `env` argument yet. Most tests use `ordersRepo()`, where `orders-handler` is checked out and
  `orders-events` has a commit that `orders-handler` does not have. The Docker image has git 2.47 (Debian
  trixie). CI runs `task check` on `ubuntu-latest`. `task check` does not run the e2e tests.
- **Process budget**: `docs/stack.md` gives the cost of each git process. The design's process table is the
  budget for each step.
- **macOS temp paths**: `t.TempDir()` is under `/var`, which is a symlink to `/private/var`. Paths that git
  prints, such as `%(worktreepath)` and `git worktree list`, can differ from `gittest`'s `Dir`.

## Tasks

### Task 1: Plan a sync with `strata sync --dry-run` behind the git version gate

**Behavior:** On git 2.44 or later, `strata sync --dry-run` does these things:

- It fetches the remote of the trunk.
- It reads the stack against the new trunk.
- It prints the plan in the tree layout of `strata --list`. Each branch shows `up to date` or
  `moves onto <parent>`.

It changes no branch. On older git, or when strata cannot read the git version, `strata --help` does not
list `sync`, and `strata sync` prints the error and exits 1. This task ships the gate with its first
consumer. Stacks whose replay stops come in Task 2 to Task 4.

**Acceptance Criteria:**
- [x] `git.ParseVersion` reads the first two numbers of each form that the design lists:
  `git version 2.47.1`, `git version 2.39.5 (Apple Git-154)`, `git version 2.44.0.windows.1` and
  `git version 2.44.0.rc2`. It gives an error for text that is not a version (`TestVersion`, group
  `parse`).
- [x] A comparison puts 2.43.9 below 2.44, and 2.44.0 at 2.44 (`TestVersion`, group `compare`).
- [x] `restack.Available` gives these results (`TestAvailable`):
  - nil for git 2.44.
  - the error `sync needs git 2.44 or later, and this is git 2.43.0` for git 2.43.0.
  - an error that gives the reason, for a version that strata could not read.
- [x] strata runs `git version` one time at start, before it reads the command line.
- [x] When the sync is not available:
  - `strata --help` does not list `sync`.
  - `strata sync` prints `strata: sync needs git 2.44 or later, and this is git 2.43.0` on stderr and exits
    1.
- [x] When the sync is available, `strata --help` lists `sync`.
- [x] `strata --remote sync` exits 1 with the error that `internal/restack` gives for `--remote`, and fetches
  nothing.
- [x] `strata sync --dry-run` runs `git fetch --prune` for the remote of the trunk (`origin` for
  `origin/main`), with the terminal attached to git, so a password prompt works. With a local trunk
  (`--trunk main`), it fetches nothing.
- [x] In a test repository, a commit that only the remote's `main` had before the sync is in the trunk of
  the plan.
- [x] The first line of the plan names the trunk and the number of commits that the fetch added to it.
- [x] Each branch shows one time, in the tree order and with the connectors of `strata --list`, followed by
  its outcome.
- [x] A branch shows `up to date` when it is on the tip of its parent and its parent does not move.
- [x] Every other branch shows `moves onto <parent>`. This includes a child that is behind its parent while
  the trunk has no new commit.
- [x] Each ref under `refs/heads/` points at the same commit before and after `strata sync --dry-run`.
- [x] `strata sync` without `--dry-run` exits 1 with an error that names `--dry-run`, and moves no branch.
- [x] The output of `strata --list` does not change. The `strata --list` e2e test passes without edits.
- [x] e2e, old git: a folder first in `PATH` holds a `git` script that prints `git version 2.43.0` for
  `git version` and runs the real git for all other commands. With it, `strata --help` has no `sync`, and
  `strata sync` exits 1 with the error.
- [x] e2e, dry run: after the remote's `main` gets a commit, `strata sync --dry-run` on `ordersRepo()` prints
  the plan with an outcome for each branch.
- [x] `docs/e2e-tests.md` gives the git version that the local e2e run needs now that it includes the sync
  tests.

**Affected Files/Modules:**
- `internal/git/version.go` (new): the `Version` type, `ParseVersion`, and the methods of `Version`.
- `internal/git/version_test.go` (new, `unit`): `TestVersion`.
- `internal/git/repo.go`: `(*Repo).Version`, and a run that gives git the terminal for the command-line fetch.
- `internal/restack/` (new package): `Available`, the `--remote` error, and the plan with its first two
  outcomes. A `unit` test holds `TestAvailable`, and an `integration` test holds the plan.
- `internal/stack/tree.go`, `internal/stack/reader.go`: `Branch.Tip` from `%(objectname)`.
- `internal/stack/reader_test.go`: a scenario for `Tip`.
- `internal/gittest/repo.go`: a helper that gives the remote's `main` a commit, which the clone gets only
  through a fetch.
- `cmd/strata/main.go`: `git version` at start, `newCommand` with the gate error, the `sync` command with
  `Hidden` and `--dry-run`, and the plan printer.
- `e2e/testUtils.ts`: the old-git `PATH` folder, and a helper that moves the remote trunk.
- `e2e/tests/`: the sync command-line tests.
- `docs/e2e-tests.md`: the git version of the local e2e run.

**Patterns to Follow:**
- `internal/version/version.go:11-54`
- `internal/version/version_test.go:1-56`
- `cmd/strata/main.go:39-75`
- `cmd/strata/main.go:64-72`
- `cmd/strata/main.go:172-195`
- `internal/stack/reader.go:15-18`
- `internal/stack/reader.go:100-119`
- `internal/stack/reader_test.go:15-37`
- `internal/gittest/repo.go:18-30`
- `e2e/tests/cli.test.ts:4-23`
- `e2e/testUtils.ts:155-172`

**Testable:** Yes

**Certainty:** medium. The precedents are `cmd/strata/main.go:64-72` for a subcommand with flags and
`internal/version/version.go:11-54` for parsing version text. The variation: no command here is hidden by a
check that runs before strata reads the command line, and no git run here gives git the terminal.

**Blast radius:** low. The task reads refs and fetches. The fetch prunes only remote-tracking refs that the
remote deleted, and no branch moves or gets deleted.

**Verification:** `task check` passes. `task test:e2e:local` (or `task test:e2e`) passes. With
`GIT_TRACE`, the dry run adds only the fetch to the processes of a read. A manual `strata sync --dry-run`
against a remote that needs a password shows the git prompt.

**Depends on:** None

---

### Task 2: Find the merged branches in the plan

**Behavior:** The plan tests each branch on the trunk with `git merge-tree --write-tree` against the new
trunk. When the new trunk already has all the changes of a branch, for example after a squash merge on
GitHub, the branch is merged:

- The plan marks the branch for deletion.
- The children of the branch go onto the trunk.
- The children get the same test.

A merged branch that a worktree has checked out stays. A branch whose remote branch is gone, but whose
changes the trunk does not have, gets a note and moves as usual.

**Acceptance Criteria:**
- [x] A branch on the trunk whose changes the new trunk has, after a squash merge in the remote, shows
  `merged: strata deletes it`.
- [x] A merged branch that a worktree has checked out shows `merged, checked out in <worktree>` with the
  path of that worktree. The plan does not mark it for deletion.
- [x] The children of a merged branch sit on the trunk in the plan, and show `moves onto <trunk>`. The
  commits that they move are only their own commits, not those of the merged branch.
- [x] When the new trunk also has the changes of a child of a merged branch (two branches of one stack
  merged between two syncs), that child is merged too, and its children go onto the trunk.
- [x] Some branches show `[gone]` in `%(upstream:track)` after the fetch, because the remote deleted the
  branch and `fetch --prune` removed its remote-tracking ref. When the trunk does not have the changes of
  such a branch, it gets the note `remote branch gone, not merged` and still shows its move.
- [x] `stack.Branch` carries `Worktree` from `%(worktreepath)` and `Gone` from `%(upstream:track)`. The
  `for-each-ref` that the reader runs now fills both, so the read adds no process.
- [x] The existing `TestReader` scenarios pass without edits.
- [x] `gittest` has a helper that squash merges a branch into the remote's `main`.
- [x] `gittest` has a helper that checks out an existing branch in a new worktree.

**Affected Files/Modules:**
- `internal/stack/tree.go`, `internal/stack/reader.go`: `Worktree` and `Gone`.
- `internal/stack/reader_test.go`: scenarios for the new fields.
- `internal/restack/`: the merge test, the children that go onto the trunk, the two merged outcomes and
  the gone note, with integration tests.
- `internal/gittest/repo.go`: the squash-merge helper and the worktree-for-a-branch helper.
- `cmd/strata/main.go`: the plan printer shows the note.

**Patterns to Follow:**
- `internal/stack/reader.go:100-119`
- `internal/stack/reader_test.go:123-136`
- `internal/gittest/repo.go:61-67`
- `task:1`

**Testable:** Yes

**Certainty:** medium. The precedents are `internal/stack/reader.go:100-119` for new `for-each-ref` fields,
and Task 1 for the plan. The variation: a result from `git merge-tree` changes the parent of a branch after
the parent rules have run, and nothing here does that.

**Blast radius:** high. This test decides which branches a sync deletes (Task 5). A false "merged" result
deletes a branch that has unmerged work, and the reflog of that branch goes with it.

**Verification:** `task check` passes. With `GIT_TRACE`, the test runs one `git merge-tree` for each branch
that it tests.

**Depends on:** Task 1

---

### Task 3: Replay each branch in memory and keep a stack whole when one branch stays

**Behavior:** The plan replays each branch that moves with `git replay`, in tree order, onto the new tip of
its parent. So the plan knows each conflict before anything changes. A branch stays when its replay stops,
or when it has a merge commit in its own commits. Every other branch of its stack then stays too. The test
repositories get their own git config in this task, because this is the first task where strata makes
commits.

**Acceptance Criteria:**
- [x] Each branch that moves gets a new tip. The new tip is its own commits (`<base>..<tip>`), replayed onto
  one of these:
  - the new trunk, for a branch on the trunk or a child of a merged branch.
  - the new tip of its parent, for any other child.
- [x] A branch with no own commits gets the new tip of its parent. The plan does not show it as a conflict.
- [x] A branch whose replay stops shows `stays: conflict in <files>`. The files come from one merge of the
  full branch onto the new tip of its parent.
- [x] A branch with a merge commit in `<base>..<tip>` shows `stays: merge commit`.
- [x] `stack.Branch.HasMerge` comes from the commit graph that the reader already builds, so it adds no
  process.
- [x] When a branch stays, every other branch of its stack shows `stays: <branch> cannot move`, where
  `<branch>` is a branch that stays for its own reason.
- [x] The plan does not mark a merged branch of a stack that stays for deletion.
- [x] Other stacks keep their outcomes.
- [x] `strata sync --dry-run` prints the plan, then exits 1 when a stack stays because of a conflict.
- [x] After a plan with a conflict, each ref under `refs/heads/` and each worktree is as it was before.
- [x] The integration tests and the dry-run e2e test pass on a machine whose global git config sets
  `commit.gpgsign=true` and gives no user identity. For this, the repositories that `gittest` and
  `e2e/testUtils.ts` make carry their own committer identity and `commit.gpgsign=false`.

**Affected Files/Modules:**
- `internal/stack/tree.go`, `internal/stack/graph.go`, `internal/stack/reader.go`: `HasMerge`.
- `internal/restack/`: the replay, the conflict files, the merge-commit outcome and the rule that a stack
  stays whole, with integration tests for each outcome.
- `internal/gittest/repo.go`: the clone's own identity and `commit.gpgsign=false`.
- `e2e/testUtils.ts`: the same for `Repo.create()`.
- `cmd/strata/main.go`: the exit status of `--dry-run`.

**Patterns to Follow:**
- `internal/stack/graph.go:64-76`
- `internal/git/repo.go:32-46`
- `internal/stack/reader_test.go:15-37`
- `task:2`

**Testable:** Yes

**Certainty:** low. This is the first use of `git replay` here. No code in this repo passes the result of
the git run for one branch to the runs for its children, or makes the state of one branch apply to its
whole stack.

**Blast radius:** low. The task computes new tips only. A later move records each tip in the reflog of the
branch, so a wrong tip can be undone.

**Verification:** `task check` passes. `task test:e2e:local` passes. With `GIT_TRACE`, a branch with no own
commits runs no `git replay`, and each stack with a conflict runs one `git merge-tree --name-only`.

**Depends on:** Task 2

---

### Task 4: Keep a stack when a worktree rebases or changes its branch

**Behavior:** The plan runs the worktree checks of the design's step 5. A stack stays when a rebase in any
worktree uses one of its branches. A stack also stays when a worktree has one of its branches checked out
and has uncommitted changes that touch the files that the branch's move changes. The plan names each
worktree whose branch the sync moves.

**Acceptance Criteria:**
- [x] A rebase in any worktree can use a branch. Its `rebase-merge/head-name` or `rebase-apply/head-name`
  names the branch, in the git dir of the main worktree or of a linked worktree. Such a branch shows
  `stays: a rebase in <worktree> uses it`, and its stack stays. This also applies when `%(worktreepath)` is
  empty for the branch.
- [x] strata reads those files with no git process, and does the check for every branch.
- [x] A worktree can have a branch checked out and uncommitted changes (staged, unstaged or untracked, as
  `git status` lists them) in a file that the move changes between the tip and the new tip. Such a branch
  shows `stays: changes in <worktree>`, and its stack stays.
- [x] A branch still moves when its worktree has changes only in files that the move does not change.
- [x] The plan names the worktree of each branch that moves while a worktree has it checked out. This
  includes the worktree that strata runs in.
- [x] A worktree whose HEAD is not on the tip that the plan read makes its stack stay, and the outcome names
  the worktree. Task 6 tests this at move time, where it can occur.
- [x] The checks add at most two git processes for each branch in a worktree: `git status --porcelain=v2
  --branch` and `git diff --name-only`.

**Affected Files/Modules:**
- `internal/restack/`: the three checks and their outcomes, with integration tests.
- `internal/gittest/repo.go`: a helper that leaves a rebase stopped in a worktree, if the tests need one.
- `cmd/strata/main.go`: the plan printer names the worktrees.

**Patterns to Follow:**
- `internal/stack/reader.go:208-225`
- `internal/stack/reader.go:41-45`
- `internal/diff/loader.go:28-46`
- `task:3`

**Testable:** Yes

**Certainty:** medium. The precedent is `internal/stack/reader.go:208-225`, which reads files of each
worktree with no process. The variation: those files and the output of `git status --porcelain=v2` decide
whether a stack stays. No code here parses porcelain v2, or finds the worktree path of a
`.git/worktrees/<name>` folder.

**Blast radius:** high. These checks stop a later move from moving a branch under a rebase or under
uncommitted work. A missed case corrupts the index and the files of another worktree.

**Verification:** `task check` passes. `task test:e2e:local` passes.

**Depends on:** Task 3

---

### Task 5: Move the stacks that no worktree holds with `strata sync`

**Behavior:** `strata sync` prints the plan, then moves each stack that moves and that no worktree holds.
For each stack, strata first runs the checks of Task 4 again. It then runs one `git update-ref --stdin`
transaction with the old tip of each branch, and the same transaction deletes the merged branches of the
stack.

- A branch that changed after the plan makes the transaction fail, and that stack does not move.
- `--keep-merged` keeps the merged branches.
- A stack where a worktree has checked out a branch that must move stays until Task 6.
- When `commit.gpgsign` is true, strata moves nothing until Task 7.

**Acceptance Criteria:**
- [x] `strata sync` prints the plan as `--dry-run` does, then moves the stacks.
- [x] After the move, each branch of a moved stack points at the new tip in the plan. `strata --list` then
  shows each branch on its parent, with no branch behind its parent.
- [x] The transaction that moves the other branches of a stack also deletes its merged branch.
- [x] With `--keep-merged`, the merged branch stays, and the plan does not say that strata deletes it.
- [x] A merged branch that a worktree has checked out stays.
- [x] A branch of a stack can be off its old tip when the stack moves, for example because a commit came
  after the plan. Then no branch of that stack moves and no merged branch of it is deleted, and
  `strata sync` shows the error and exits 1.
- [x] strata runs the checks of Task 4 again before each stack moves. A rebase that started on a branch of
  the stack after the plan makes the stack stay.
- [x] A stack that stays because of a conflict does not move, the other stacks move, and `strata sync` exits
  1.
- [x] A stack stays when a worktree has checked out one of its branches that must move, and the plan names
  that worktree.
- [x] When `git config --type=bool commit.gpgsign` is true, `strata sync` moves no branch, and exits 1 with
  an error that names `commit.gpgsign`.
- [x] `strata sync` without `--dry-run` runs the move and does not refuse.
- [x] `internal/git` can give text to a git command on its standard input (`RunInput`). A failure gives the
  same `*git.Error` as `Run`.

**Affected Files/Modules:**
- `internal/git/repo.go`: `RunInput`.
- `internal/restack/`: the move (the second run of the checks, the transaction, the deletes) and the read
  of `commit.gpgsign`, with integration tests.
- `cmd/strata/main.go`: `strata sync` without `--dry-run`, `--keep-merged`, and the exit status.

**Patterns to Follow:**
- `internal/git/repo.go:21-53`
- `internal/git/repo.go:55-70`
- `cmd/strata/main.go:64-72`
- `task:4`

**Testable:** Yes

**Certainty:** low. This is the first ref write in the repository. No code here gives git a transaction on
stdin, or deletes a branch.

**Blast radius:** high. The task moves and deletes the user's local branches, and a deleted branch loses its
reflog.

**Verification:** `task check` passes. With `GIT_TRACE`, each stack that moves runs one `git update-ref
--stdin`, and the sync reads `commit.gpgsign` one time.

**Depends on:** Task 4

---

### Task 6: Move checked-out branches in their worktrees

**Behavior:** After the transaction of its stack, strata moves each branch that a worktree has checked out
with `git -C <worktree> reset --keep <new tip>`. The HEAD, the index and the files of that worktree move
together, and uncommitted changes in other files stay. A worktree that is no longer on the old tip of its
branch keeps its stack where it is. When `reset --keep` fails, strata keeps the new tip in
`refs/strata/sync/<branch>` and prints the command that finishes the move.

**Acceptance Criteria:**
- [x] A stack with a branch checked out in a linked worktree moves.
- [x] A stack with a branch checked out in the worktree that strata runs in moves.
- [x] After the move, each such worktree is on the new tip. `git status` there shows no staged or unstaged
  change that undoes the move.
- [x] Uncommitted changes in files that the move does not change are still in the worktree after the move.
- [x] A worktree can be off the old tip of its branch when the stack moves, for example after a commit in
  that worktree after the plan. Then the stack stays, and no branch of it moves.
- [x] Uncommitted changes that touch the move can appear in a worktree after the plan. Then the stack
  stays, and no branch of it moves.
- [x] When `git reset --keep` fails in a worktree, the new tip of the branch is in
  `refs/strata/sync/<branch>`, and `strata sync` prints a command that finishes the move and exits 1.

**Affected Files/Modules:**
- `internal/restack/`: the move in worktrees, the worktree HEAD check at move time, and the ref that a
  failed move keeps, with integration tests.
- `internal/gittest/repo.go`: worktree helpers, if the tests need more than Task 2 gives.
- `cmd/strata/main.go`: prints the command that finishes a move.

**Patterns to Follow:**
- `internal/git/repo.go:17-19`
- `internal/gittest/repo.go:61-67`
- `task:5`

**Testable:** Yes

**Certainty:** low. No code here changes the files of another worktree, or leaves a ref for the user to
finish a move.

**Blast radius:** high. The task resets the HEAD, the index and the files of the user's worktrees, including
worktrees where an agent is at work.

**Verification:** `task check` passes. `task test:e2e:local` passes. With `GIT_TRACE`, each branch in a
worktree runs one `git reset --keep`.

**Depends on:** Task 5

---

### Task 7: Sign the moved commits with a sync rebase

**Behavior:** When `commit.gpgsign` is true, strata makes the commits to keep again with a sync rebase, so
that each commit gets a signature. The sync rebase has these properties:

- It runs in `.git/strata/sync/worktree`, which is a worktree with a detached HEAD.
- Hooks are off in it.
- strata writes its todo list: `reset`, `pick`, `label` and `exec git update-ref refs/strata/sync/<branch>
  HEAD`.
- It runs with `--empty=keep`.

The move of Task 5 and Task 6 then uses the tips in `refs/strata/sync/`. After the move, strata removes the
sync worktree and the refs.

**Acceptance Criteria:**
- [x] A test sets `commit.gpgsign=true` and a fake `gpg.program` that writes `[GNUPG:] SIG_CREATED` and a
  fake signature. Each commit that `strata sync` puts on a moved branch then has a signature, and the tree
  and the message that the replay of the plan gave.
- [x] The sync rebase moves no branch. The branches move only in the move step, with the checks and the
  transaction of Task 5 and Task 6.
- [x] A child that was behind its parent moves onto the new tip of its parent.
- [x] The children of a merged branch move onto the new trunk with only their own commits.
- [x] A tree of branches (two children of one parent) moves in one sync rebase.
- [x] A commit whose change the new trunk already has stays as an empty commit, as the replay keeps it.
- [x] Hooks of the repository, for example a `post-checkout` hook, do not run in the sync worktree.
- [x] After `strata sync`, `git worktree list` shows no sync worktree, and `refs/strata/sync/` holds no ref,
  except the ref that Task 6 keeps for a branch whose `reset --keep` failed.
- [x] A sync worktree that a stopped sync left behind does not stop the next `strata sync`.
- [x] When `commit.gpgsign` is false or not set, `strata sync` keeps the replay's commits and adds no
  worktree.
- [x] When `commit.gpgsign` is true, `strata sync` moves the stacks. The refusal that Task 5 added is gone.

**Affected Files/Modules:**
- `internal/restack/`: the path for `commit.gpgsign`, the todo list, the sync worktree, and the read and
  removal of `refs/strata/sync/`, with integration tests.
- `internal/git/repo.go`: a run with extra environment for `GIT_SEQUENCE_EDITOR`, if `Run` cannot do it.
- `internal/gittest/repo.go`: a helper for the fake `gpg.program`, if the tests share one.

**Patterns to Follow:**
- `cmd/strata/main.go:108-115`
- `internal/viewed/set.go:52-73`
- `task:6`

**Testable:** Yes

**Certainty:** low. No code here runs an interactive rebase, writes a todo list, or adds and removes a
worktree.

**Blast radius:** high. The sync rebase makes the commits that the move writes to the user's branches, and it
adds and removes a worktree and refs under `.git`.

**Verification:** `task check` passes. With `GIT_TRACE`, a signed sync adds `git worktree add`,
`git rebase -i` and `git worktree remove`, and an unsigned sync adds none of them.

**Depends on:** Task 6

---

### Task 8: Show the plan in the Stack panel with `S`

**Behavior:** In the terminal UI, `S` fetches the trunk in the background and shows the plan in the Stack
panel: the tree, with the outcome of each branch. The fetch runs in a new process session with no terminal,
and with `GIT_TERMINAL_PROMPT=0`. `esc` closes the plan and changes nothing.

The sync is not available on old git, on a version that strata cannot read, and with `--remote`. Then the
footer and the keys screen do not show `S`, and `S` shows the error in the status line.

**Acceptance Criteria:**
- [x] When the sync is available, the footer of the Stack panel shows `S sync`, and the keys screen lists
  `S`.
- [x] `S` shows the plan in the Stack panel, and no branch moves. The plan has the trunk line with its count
  of new commits, and each branch with its outcome in place of its counts.
- [x] While the plan shows, the footer shows `esc close`.
- [x] `esc` closes the plan, and the Stack panel shows the tree as it was before.
- [x] When the sync is not available, the footer and the keys screen do not show `S`. `S` then shows the
  error in the status line: the error of `restack.Available`, or the `--remote` error (`TestModel`).
- [x] A plan that fails shows its error in the status line (`TestModel`).
- [x] In the TUI, a fetch from a remote that needs a password fails with the git error. Nothing asks for
  the password on the terminal, and the screen stays intact.
- [x] `ui.Sources` has a `Sync` source. Its type is an interface that `internal/ui` defines, with
  `Available()`. `cmd/strata/main.go` gives it the restack implementation.
- [x] In `internal/ui`, the word "sync" names only the stack sync. The method `Model.sync`, which points the
  Files and Diff panels at the selection, gets a name that says what it does.
- [x] `openStrata` in `e2e/testUtils.ts` takes an `env` argument, as `runStrata` does.
- [x] e2e, old git: with the old-git `PATH` folder from Task 1, the footer has no `S`, and `S` shows the
  error.
- [x] e2e, current git: after the remote's `main` gets a commit, `S` on `ordersRepo()` shows the plan.
- [x] `docs/e2e-tests.md` shows the `env` argument of `openStrata`.

**Affected Files/Modules:**
- `internal/ui/sources.go`: the `Sync` source and its interface.
- `internal/ui/model.go`: the `S` key, `esc` in the plan, the footer and the keys screen by availability,
  the status line, and the new name for `Model.sync`.
- `internal/ui/stackpanel.go`: the plan in the Stack panel.
- `internal/ui/model_test.go`: a memory fake of the sync source, and the `TestModel` scenarios.
- `internal/git/repo.go`: the fetch in a new process session with no terminal.
- `internal/restack/`: the fetch mode that the TUI asks for, if the plan runs the fetch.
- `cmd/strata/main.go`: sets up the sync source from the gate error and `--remote`.
- `e2e/testUtils.ts`, `e2e/tests/`: the `env` argument of `openStrata`, and the screen tests of `S`.
- `docs/e2e-tests.md`: the `env` argument of `openStrata`.

**Patterns to Follow:**
- `internal/ui/sources.go:11-36`
- `internal/ui/model.go:355-361`
- `internal/ui/model.go:77-87`
- `internal/ui/model.go:571-589`
- `internal/ui/model.go:605-636`
- `internal/ui/stackpanel.go:158-203`
- `internal/ui/model_test.go:20-72`
- `internal/ui/model_test.go:144-186`
- `e2e/testUtils.ts:134-147`
- `e2e/tests/screen.test.ts:5-13`

**Testable:** Yes

**Certainty:** medium. The precedents are `internal/ui/model.go:355-361` for a background load that returns
a message, and `internal/ui/model_test.go:20-72` for memory sources. The variation: none of these exist
here yet:

- a second mode of the Stack panel that replaces its counts.
- keys that show only when a source allows them.
- a git run in a new process session with no terminal.

**Blast radius:** low. The task fetches and shows the plan, and no branch moves.

**Verification:** `task check` passes. `task test:e2e:local` passes. A manual check in a private tmux server
(`docs/e2e-tests.md`) with a remote that needs a password shows the git error in the status line, with no
prompt.

**Depends on:** Task 4

---

### Task 9: Move the stacks from the plan with `enter`

**Behavior:** While the plan shows, `enter` moves each stack that has no conflict, with the move of Task 5
to Task 7. strata then reads the branches again and closes the plan. The footer tells how many stacks
`enter` moves.

**Acceptance Criteria:**
- [x] While the plan shows, the footer shows `enter move <n> stack` (or `stacks`) beside `esc close`, where
  `<n>` is the number of stacks that move.
- [x] `enter` moves those stacks, reads the branches again, and closes the plan.
- [x] After `enter`, the Stack panel shows the new tree: no branch that moved is behind its parent, and the
  children of a deleted merged branch sit on the trunk.
- [x] When the plan is closed, `enter` keeps its current meaning in each panel.
- [x] `enter` in a plan with no stack to move moves nothing.
- [x] A move that fails, for example on a branch that changed after the plan, shows the error in the status
  line.
- [x] A branch whose `reset --keep` failed shows the command that finishes its move.
- [x] The keys screen lists `enter` and `esc` for the plan.
- [x] e2e: after the remote's `main` gets a commit, `S` and then `enter` on `ordersRepo()` make the Stack
  panel show the new tree, with no `behind parent` on `orders-handler`.

**Affected Files/Modules:**
- `internal/ui/model.go`: `enter` in the plan, the footer count, and the message after a move.
- `internal/ui/sources.go`: the move in the sync interface.
- `internal/ui/model_test.go`: `TestModel` scenarios for `enter`.
- `cmd/strata/main.go`: the restack implementation of the move for the TUI.
- `e2e/tests/`: the `S` and `enter` screen test.

**Patterns to Follow:**
- `internal/ui/model.go:355-361`
- `internal/ui/model.go:77-87`
- `internal/ui/model_test.go:144-186`
- `task:8`

**Testable:** Yes

**Certainty:** high. The precedents are `internal/ui/model.go:355-361` and `internal/ui/model.go:77-87`: a
key starts a background job, and the message from that job reads the tree again.

**Blast radius:** high. `enter` moves and deletes the user's branches. If the task handles the key in the
wrong mode, `enter` moves branches when the plan is not on the screen.

**Verification:** `task check` passes. `task test:e2e:local` passes.

**Depends on:** Task 7, Task 8

---

### Task 10: Resolve a conflict with `strata sync --resolve`

**Behavior:** `strata sync --resolve <branch>` starts a sync rebase for the stack of `<branch>`, with the
todo list of Task 7. The rebase stops at the conflict. strata writes a record of the sync rebase at
`.git/strata/sync/plan`, prints the path of the sync worktree, and exits 1. After that, `strata sync` finds
one of three states:

- **The rebase waits:** strata tells you to finish the rebase first.
- **The rebase is done:** strata moves the resolved stack, then does a full sync.
- **The rebase stopped:** strata removes the record, the refs and the sync worktree. No branch moved.

**Acceptance Criteria:**
- [x] `strata sync --resolve <branch>`, for a branch of a stack with a conflict, stops at the conflict,
  prints the path of the sync worktree, exits 1, and moves no branch. `git status` in the sync worktree
  shows the conflict.
- [x] strata resolves no conflict by itself, and uses no `-X ours` or `-X theirs`.
- [x] The record at `.git/strata/sync/plan` holds the branches of the stack, the old tip of each, and their
  worktrees.
- [x] While the rebase waits, these commands move no branch, tell you to finish the rebase in the sync
  worktree first, and exit 1:
  - `strata sync`, with or without `--dry-run`.
  - `strata sync --resolve`.
- [x] After `git rebase --continue` in the sync worktree, `strata sync` moves the resolved stack to the tips
  in `refs/strata/sync/`. The move runs the checks of Task 4, and a transaction on the old tips of the
  record.
- [x] strata then removes the record, the refs and the sync worktree, and syncs the other stacks.
- [x] A branch of the resolved stack can move after `--resolve`. Then the move of that stack fails, and no
  branch of the stack moves.
- [x] After `git rebase --abort`, `strata sync` removes the record, the sync worktree and every ref in
  `refs/strata/sync/`. The sync rebase moved no branch, and the sync then runs as usual.
- [x] With `commit.gpgsign=true`, the commits of the resolved stack have signatures.

**Affected Files/Modules:**
- `internal/restack/`: the sync rebase for one stack, the record, and the three states, with integration
  tests.
- `cmd/strata/main.go`: `--resolve`, the messages for each state, and the exit status.
- `internal/gittest/repo.go`: helpers to resolve a conflict in the sync worktree, if the tests share them.

**Patterns to Follow:**
- `internal/viewed/set.go:21-36`
- `internal/viewed/set.go:52-73`
- `task:7`

**Testable:** Yes

**Certainty:** low. The design decides a state machine across separate runs of strata. Each run reads it
back from a record and from the state of a rebase in another worktree. Nothing like it exists here.

**Blast radius:** high. The task moves the branches of the resolved stack. Its "stopped" state removes the
sync worktree, and while the rebase waits, that worktree holds the user's resolution.

**Verification:** `task check` passes.

**Depends on:** Task 7

---

### Task 11: Resolve a conflict from the terminal UI with `c`

**Behavior:** While the plan shows, `c` on a branch of a stack with a conflict starts the sync rebase for
that stack. It then opens `$SHELL` in the sync worktree with `tea.ExecProcess`. When the shell exits,
strata reads the state again:

- **Done:** the Stack panel shows the plan for that stack, and `enter` moves it.
- **Waits:** the status line tells you to finish the rebase.
- **Stopped:** strata cleans up, and no branch moved.

`S` handles the same three states.

**Acceptance Criteria:**
- [ ] While the plan shows, the footer shows `c resolve the conflict` when the selected branch is in a
  stack with a conflict. On other branches, it does not.
- [ ] `c` starts the sync rebase for the stack of the selected branch, and opens `$SHELL` in the sync
  worktree. The strata screen comes back when the shell exits.
- [ ] When the shell exits with the rebase done, the Stack panel shows the plan for that stack, and `enter`
  moves it.
- [ ] When the shell exits with the rebase still waiting, the status line tells you to finish the rebase in
  the sync worktree. `S` does the same while the rebase waits.
- [ ] When the shell exits after `git rebase --abort`, strata removes the record, the refs and the sync
  worktree, and no branch moved. `S` does the same.
- [ ] `S` with a done rebase shows the plan for the resolved stack.
- [ ] The keys screen lists `c`.
- [ ] e2e: with `SHELL` set to `sh` through `openStrata`, the test does these steps:
  1. Press `S`.
  2. Select the branch with the conflict and press `c`.
  3. Resolve the conflict in the shell, run `git rebase --continue`, and exit the shell.
  The plan then shows that the stack moves, and `enter` moves it.

**Affected Files/Modules:**
- `internal/ui/model.go`: the `c` key, the shell, and the three states on shell exit and on `S`.
- `internal/ui/sources.go`: the resolve and state operations in the sync interface.
- `internal/ui/model_test.go`: `TestModel` scenarios for the footer and the three states.
- `cmd/strata/main.go`: the restack implementation of these operations for the TUI.
- `e2e/testUtils.ts`: a repository with a conflict after the remote trunk moves.
- `e2e/tests/`: the `c` screen test.

**Patterns to Follow:**
- `internal/ui/model.go:355-361`
- `internal/ui/sources.go:11-36`
- `e2e/tests/screen.test.ts:93-109`
- `task:9`
- `task:10`

**Testable:** Yes

**Certainty:** low. No code here gives the terminal to another program (`tea.ExecProcess`). The command
loop of `TestModel` cannot see the shell, so only the e2e test can test that part.

**Blast radius:** high. The keys of this task lead to the clean-up of the "stopped" state, which removes the
sync worktree, and to the move of the resolved stack.

**Verification:** `task check` passes. `task test:e2e:local` passes.

**Depends on:** Task 9, Task 10

---

### Task 12: Document `strata sync` for users

**Behavior:** `README.md` and `docs/sync.md` tell users these things about `strata sync`:

- what it does.
- how to use it from the command line and from the terminal UI.
- what each outcome means.
- how to resolve a conflict.
- its limits.

`docs/stack.md` agrees with the new fields of the read.

**Acceptance Criteria:**
- [ ] `README.md` has a section for `strata sync` with the four command forms and a link to `docs/sync.md`.
- [ ] `README.md` gives the git 2.44 need of the sync beside the git 2.41 need of strata.
- [ ] The Keys table of `README.md` lists `S`, and `enter`, `c` and `esc` in the plan.
- [ ] `docs/sync.md` covers these subjects for users:
  - the goal, and what is out of scope.
  - the steps of a sync.
  - the plan and its outcomes.
  - why a stack moves as one.
  - conflicts: `c`, `--resolve`, and the three states.
  - worktrees.
  - signed commits.
  - the git version.
  - the limits.
- [ ] `docs/sync.md` uses the terms of `docs/stack.md` (trunk, parent, base, tip), and the design's terms
  (stack, move, replay, new tip, merged branch, plan, sync worktree, sync rebase).
- [ ] The data table of `docs/stack.md` names the fields that the `for-each-ref` of step 1 now also reads.
- [ ] The Limits entry of `docs/stack.md` on squash merges says that `strata sync` finds and deletes merged
  branches.
- [ ] Each command, key, flag, outcome text and path in the documents is the same as what the code prints
  and accepts.
- [ ] The documents read as first versions, with plain words, `must` and `can`, and no metaphors.

**Affected Files/Modules:**
- `README.md`: the sync section, the git version, and the keys.
- `docs/sync.md` (new): the user documentation of the sync.
- `docs/stack.md`: the new fields of the read, and the squash-merge limit.

**Patterns to Follow:**
- `docs/stack.md:1-45`
- `README.md:32-45`
- `README.md:73-98`

**Testable:** No

**Certainty:** high. The precedents are `docs/stack.md:1-45` for the style of a user document, and
`README.md:73-98` for the Keys table.

**Blast radius:** low. The task changes documentation only.

**Verification:** Read the documents against `strata sync --help`, the plan output, and the keys screen.
`task check` passes.

**Depends on:** Task 11

## Summary

- **Total:** 12 tasks.
- **Order:** the tasks follow the design's "Order of work": the dry run, then the move, then the terminal
  UI, then conflicts, then the user documentation.
  - Inside the dry run, the tasks follow the design's steps (fetch and read, merged branches, replay, worktree
    checks). Each step then builds on the plan of the step before it.
  - The move lands in three parts: the ref transaction, then worktrees, then signed commits. So each
    destructive mechanism gets its own commit and its own review.
  - Where a part is not there yet, the commit before it is safe: it refuses, or keeps a stack where it is,
    and never moves a branch by a wrong rule. Task 1 refuses `strata sync` without `--dry-run`, and Task 5
    lifts that. Task 5 keeps a stack with a checked-out branch, and Task 6 lifts that. Task 5 refuses
    `commit.gpgsign=true`, and Task 7 lifts that.
- **Where the e2e tests go:** each e2e test goes in the task of the behaviour that it tests, not in a
  separate task:
  - Task 1: the old-git `PATH` folder for the command line, and the dry run.
  - Task 8: the old-git footer and `S`, and `openStrata` with an `env` argument.
  - Task 9: `S` then `enter`.
  - Task 11: `c` through a real shell.
- **Coverage of the caller's list:**
  1. The git version gate: Task 1 (`internal/git`, `restack.Available`, the hidden command and its error),
     and Task 8 (the footer, the keys screen and the status line).
  2. `strata sync --dry-run`:
     - Task 1: the fetch, the read, `Tip`, and the plan printer.
     - Task 2: the merge test, `Worktree` and `Gone`.
     - Task 3: the replay, the conflict files, `HasMerge`, and the rule that a stack moves as one.
     - Task 4: the worktree checks.
  3. `strata sync` applies the plan:
     - Task 5: the transaction, the deletes, `--keep-merged`, the second run of the checks, and the exit
       status.
     - Task 6: `reset --keep` in worktrees.
     - Task 7: the sync rebase for signed commits.
  4. The TUI: Task 8 (`S`, `esc`, the gate) and Task 9 (`enter`).
  5. Conflicts: Task 10 (`--resolve`, the record, and the three states on the command line) and Task 11
     (`c`, the shell, and the three states on `S`).
  6. User documentation: Task 12. The e2e tests are in the tasks listed above.
- **The design's test list:**
  - `TestVersion` and `TestAvailable` are in Task 1. `TestModel` for the gate is in Task 8.
  - There is one integration scenario for each outcome: `up to date` and `moves onto` in Task 1, the merged
    outcomes in Task 2, conflict, merge commit and "cannot move" in Task 3, and the rebase and changes
    outcomes in Task 4.
  - A branch that moves after the plan is in Task 5. Signed commits are in Task 7. `--continue` and
    `--abort` are in Task 10.
  - The three `gittest` helpers are in Task 1 and Task 2.
- **Deferred:** nothing from the request. The design's open questions stay open (see Boundaries).
