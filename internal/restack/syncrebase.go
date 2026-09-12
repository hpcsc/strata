package restack

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const newTipRefs = "refs/strata/sync/"

type syncRebase struct {
	git runner
	dir string
}

func newSyncRebase(ctx context.Context, git runner) (*syncRebase, error) {
	out, err := git.Run(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	return &syncRebase{git: git, dir: filepath.Join(strings.TrimSpace(out), "strata", "sync")}, nil
}

func (s *syncRebase) worktree() string {
	return filepath.Join(s.dir, "worktree")
}

func (s *syncRebase) recordFile() string {
	return filepath.Join(s.dir, "plan")
}

// lock lets one strata at a time run a sync rebase, which uses the sync
// worktree, the todo file and the record. The system drops the lock when the
// process ends.
func (s *syncRebase) lock() (unlock func(), err error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("another strata runs a sync rebase in this repository now: try again when it ends")
		}
		return nil, fmt.Errorf("lock %s: %w", f.Name(), err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func (s *syncRebase) commits(ctx context.Context, plan Plan, stacks [][]string) (newTips map[string]string, err error) {
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	// An interrupt cancels ctx, and the rebase that it stops must still end here.
	cleanupCtx := context.WithoutCancel(ctx)
	moving, stopped, err := s.start(ctx, plan, stacks)
	defer s.removeWorktree(cleanupCtx)
	if err != nil {
		if stopped {
			_, _ = s.git.Run(cleanupCtx, "-C", s.worktree(), "rebase", "--abort")
		}
		_ = s.deleteNewTips(cleanupCtx, moving)
		return nil, err
	}
	newTips, err = s.newTips(ctx)
	if err != nil {
		return nil, err
	}
	for _, name := range moving {
		if newTips[name] == "" {
			return nil, fmt.Errorf("make the commits of the sync: no new tip for %s", name)
		}
	}
	return newTips, nil
}

// start leaves the state of the rebase in the sync worktree when the rebase
// stops, and then reports stopped.
func (s *syncRebase) start(ctx context.Context, plan Plan, stacks [][]string) (moving []string, stopped bool, err error) {
	if s.waits() {
		return nil, false, s.waitsError()
	}
	todo, moving, err := s.todo(ctx, plan, stacks)
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, false, err
	}
	todoFile := filepath.Join(s.dir, "todo")
	if err := os.WriteFile(todoFile, []byte(todo), 0o644); err != nil {
		return nil, false, err
	}
	s.removeWorktree(ctx)
	if _, err := s.git.Run(ctx, "-c", "core.hooksPath=/dev/null", "worktree", "add", "--force", "--detach", s.worktree(), plan.TrunkTip); err != nil {
		return nil, false, err
	}
	_, err = s.git.RunEnv(ctx, []string{"GIT_SEQUENCE_EDITOR=cp " + shellQuote(todoFile)},
		"-c", "core.hooksPath=/dev/null", "-C", s.worktree(), "rebase", "--interactive", "--empty=keep", "--no-update-refs", plan.TrunkTip)
	if err != nil {
		return moving, s.waits(), fmt.Errorf("make the commits of the sync: %w", err)
	}
	return moving, false, nil
}

func (s *syncRebase) waitsError() error {
	return fmt.Errorf("a sync rebase waits in %s: finish it there with git rebase --continue, or end it with git rebase --abort", s.worktree())
}

func (s *syncRebase) waits() bool {
	admin := s.adminDir()
	if admin == "" {
		return false
	}
	for _, state := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(admin, state)); err == nil {
			return true
		}
	}
	return false
}

// adminDir returns the folder under .git/worktrees that holds the state of the
// sync worktree, which its .git file names.
func (s *syncRebase) adminDir() string {
	data, err := os.ReadFile(filepath.Join(s.worktree(), ".git"))
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(s.worktree(), dir)
	}
	return dir
}

func (s *syncRebase) todo(ctx context.Context, plan Plan, stacks [][]string) (todo string, moving []string, err error) {
	var steps []string
	onto := map[string]string{}
	for _, names := range stacks {
		for _, name := range names {
			b, o := plan.Tree.Branches[plan.Tree.Index(name)], plan.Outcomes[name]
			switch o.Kind {
			case UpToDate:
				onto[name] = b.Tip
				continue
			case Moves:
			default:
				continue
			}
			parent := plan.TrunkTip
			if o.NewParent != plan.Tree.Trunk {
				parent = onto[o.NewParent]
			}
			steps = append(steps, "reset "+parent)
			out, err := s.git.Run(ctx, "rev-list", "--reverse", b.Base+".."+b.Tip, "--")
			if err != nil {
				return "", nil, err
			}
			for _, commit := range lines(out) {
				steps = append(steps, "pick "+commit)
			}
			label := fmt.Sprintf("strata-%d", len(onto))
			steps = append(steps, "label "+label, "exec git update-ref "+shellQuote(newTipRefs+name)+" HEAD")
			onto[name] = label
			moving = append(moving, name)
		}
	}
	return strings.Join(steps, "\n") + "\n", moving, nil
}

func (s *syncRebase) newTips(ctx context.Context) (map[string]string, error) {
	out, err := s.git.Run(ctx, "for-each-ref", "--format=%(refname)%00%(objectname)", newTipRefs)
	if err != nil {
		return nil, err
	}
	tips := map[string]string{}
	for _, line := range lines(out) {
		ref, commit, _ := strings.Cut(line, "\x00")
		tips[strings.TrimPrefix(ref, newTipRefs)] = commit
	}
	return tips, nil
}

func (s *syncRebase) deleteNewTips(ctx context.Context, names []string) error {
	tips, err := s.newTips(ctx)
	if err != nil {
		return err
	}
	var deletes []string
	for _, name := range names {
		if tip, ok := tips[name]; ok {
			deletes = append(deletes, "delete "+newTipRefs+name+" "+tip)
		}
	}
	if len(deletes) == 0 {
		return nil
	}
	_, err = s.git.RunInput(ctx, "start\n"+strings.Join(deletes, "\n")+"\ncommit\n", "update-ref", "--stdin")
	return err
}

func (s *syncRebase) removeWorktree(ctx context.Context) {
	if s.waits() {
		return
	}
	if _, err := os.Stat(s.worktree()); err == nil {
		_, _ = s.git.Run(ctx, "worktree", "remove", "--force", s.worktree())
	}
}

func deleteMovedTips(ctx context.Context, git runner) error {
	out, err := git.Run(ctx, "for-each-ref", "--format=%(refname)%00%(objectname)", newTipRefs, "refs/heads/")
	if err != nil {
		return err
	}
	heads := map[string]string{}
	tips := map[string]string{}
	for _, line := range lines(out) {
		ref, commit, _ := strings.Cut(line, "\x00")
		if name, ok := strings.CutPrefix(ref, newTipRefs); ok {
			tips[name] = commit
		} else {
			heads[strings.TrimPrefix(ref, "refs/heads/")] = commit
		}
	}
	var deletes []string
	for name, commit := range tips {
		if heads[name] == commit {
			deletes = append(deletes, "delete "+newTipRefs+name+" "+commit)
		}
	}
	if len(deletes) == 0 {
		return nil
	}
	_, err = git.RunInput(ctx, "start\n"+strings.Join(deletes, "\n")+"\ncommit\n", "update-ref", "--stdin")
	return err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
