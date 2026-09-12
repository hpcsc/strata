package restack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func (s *syncRebase) commits(ctx context.Context, plan Plan, stacks [][]string) (newTips map[string]string, err error) {
	todo, moving, err := s.todo(ctx, plan, stacks)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	todoFile := filepath.Join(s.dir, "todo")
	if err := os.WriteFile(todoFile, []byte(todo), 0o644); err != nil {
		return nil, err
	}
	s.removeWorktree(ctx)
	if _, err := s.git.Run(ctx, "-c", "core.hooksPath=/dev/null", "worktree", "add", "--force", "--detach", s.worktree(), plan.TrunkTip); err != nil {
		return nil, err
	}
	defer s.removeWorktree(ctx)
	_, err = s.git.Run(ctx, "-c", "core.hooksPath=/dev/null", "-c", "sequence.editor=cp "+shellQuote(todoFile),
		"-C", s.worktree(), "rebase", "--interactive", "--empty=keep", "--no-update-refs", plan.TrunkTip)
	if err != nil {
		_, _ = s.git.Run(ctx, "-C", s.worktree(), "rebase", "--abort")
		_ = s.forget(ctx, moving)
		return nil, fmt.Errorf("make the commits of the sync: %w", err)
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

func (s *syncRebase) forget(ctx context.Context, names []string) error {
	var deletes []string
	for _, name := range names {
		deletes = append(deletes, "delete "+newTipRefs+name)
	}
	if len(deletes) == 0 {
		return nil
	}
	_, err := s.git.RunInput(ctx, "start\n"+strings.Join(deletes, "\n")+"\ncommit\n", "update-ref", "--stdin")
	return err
}

func (s *syncRebase) removeWorktree(ctx context.Context) {
	if _, err := os.Stat(s.worktree()); err == nil {
		_, _ = s.git.Run(ctx, "worktree", "remove", "--force", s.worktree())
	}
}

func forgetMovedTips(ctx context.Context, git runner) error {
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
