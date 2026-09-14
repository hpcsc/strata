package restack

import (
	"context"
	"os"
	"strings"

	"github.com/hpcsc/strata/internal/stack"
)

type worktreeCheck struct {
	git       runner
	rebasedIn map[string]string
}

func newWorktreeCheck(ctx context.Context, git runner) (*worktreeCheck, error) {
	out, err := git.Run(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	return &worktreeCheck{git: git, rebasedIn: stack.RebaseWorktrees(strings.TrimSpace(out))}, nil
}

func (w *worktreeCheck) outcome(ctx context.Context, b stack.Branch, o Outcome) (Outcome, error) {
	if worktree, ok := w.rebasedIn[b.Ref]; ok {
		return Outcome{Kind: Rebasing, NewParent: o.NewParent, Worktree: worktree}, nil
	}
	if b.Worktree == "" || o.Kind != Moves || o.Blocker != "" {
		return o, nil
	}
	o.Worktree = b.Worktree
	if _, err := os.Stat(b.Worktree); err != nil {
		return Outcome{Kind: WorktreeGone, NewParent: o.NewParent, Worktree: b.Worktree}, nil
	}
	out, err := w.git.Run(ctx, "-C", b.Worktree, "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return Outcome{}, err
	}
	head, changed := parseStatus(out)
	if head != (worktreeHead{branch: b.Name, commit: b.Tip}) {
		return Outcome{Kind: Stale, NewParent: o.NewParent, Worktree: b.Worktree}, nil
	}
	if len(changed) == 0 {
		return o, nil
	}
	out, err = w.git.Run(ctx, "diff", "--name-only", "-z", "--no-renames", b.Tip, o.NewTip, "--")
	if err != nil {
		return Outcome{}, err
	}
	if files := overlap(changed, strings.Split(strings.TrimRight(out, "\x00"), "\x00")); len(files) > 0 {
		return Outcome{Kind: LocalChanges, NewParent: o.NewParent, Worktree: b.Worktree, Files: files}, nil
	}
	return o, nil
}

type worktreeHead struct {
	branch string
	commit string
}

func parseStatus(out string) (worktreeHead, []string) {
	var head worktreeHead
	var changed []string
	records := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		switch {
		case strings.HasPrefix(record, "# branch.oid "):
			head.commit = strings.TrimPrefix(record, "# branch.oid ")
		case strings.HasPrefix(record, "# branch.head "):
			head.branch = strings.TrimPrefix(record, "# branch.head ")
		case strings.HasPrefix(record, "1 "):
			changed = append(changed, pathAfter(record, 8))
		case strings.HasPrefix(record, "2 "):
			changed = append(changed, pathAfter(record, 9))
			if i+1 < len(records) {
				i++
				changed = append(changed, records[i])
			}
		case strings.HasPrefix(record, "u "):
			changed = append(changed, pathAfter(record, 10))
		case strings.HasPrefix(record, "? "):
			changed = append(changed, strings.TrimPrefix(record, "? "))
		}
	}
	return head, changed
}

func pathAfter(record string, fields int) string {
	parts := strings.SplitN(record, " ", fields+1)
	if len(parts) <= fields {
		return ""
	}
	return parts[fields]
}

func overlap(changed, moved []string) []string {
	var out []string
	for _, c := range changed {
		for _, m := range moved {
			// git status shows an untracked folder as one path that ends with a slash
			if m != "" && (c == m || strings.HasSuffix(c, "/") && strings.HasPrefix(m, c)) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}
