package stack

import (
	"context"
	"fmt"
	"strings"
)

type writer interface {
	Run(ctx context.Context, args ...string) (string, error)
	RunInput(ctx context.Context, stdin string, args ...string) (string, error)
	Try(ctx context.Context, args ...string) (string, bool, error)
}

type Deleter struct {
	git   writer
	trunk string
}

func NewDeleter(git writer, trunk string) *Deleter {
	return &Deleter{git: git, trunk: trunk}
}

func (d *Deleter) TrunkHas(ctx context.Context, branches []Branch) (map[string]bool, error) {
	out, err := d.git.Run(ctx, "rev-parse", d.trunk, d.trunk+"^{tree}")
	if err != nil {
		return nil, err
	}
	parsed := lines(out)
	if len(parsed) != 2 {
		return nil, fmt.Errorf("find the trunk %s: git rev-parse printed %q", d.trunk, out)
	}
	tip, treeOfTip := parsed[0], parsed[1]
	has := map[string]bool{}
	for _, b := range branches {
		out, clean, err := d.git.Try(ctx, "merge-tree", "--write-tree", tip, b.Tip)
		if err != nil {
			return nil, err
		}
		written := lines(out)
		has[b.Name] = clean && len(written) > 0 && written[0] == treeOfTip
	}
	return has, nil
}

func (d *Deleter) Delete(ctx context.Context, branches []Branch) error {
	if len(branches) == 0 {
		return nil
	}
	out, err := d.git.Run(ctx, "for-each-ref", "--format=%(refname)%00%(worktreepath)", "refs/heads/")
	if err != nil {
		return err
	}
	checkedOut := map[string]string{}
	for _, line := range lines(out) {
		if ref, worktree, _ := strings.Cut(line, "\x00"); worktree != "" {
			checkedOut[ref] = worktree
		}
	}
	common, err := d.git.Run(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	rebases := RebaseWorktrees(strings.TrimSpace(common))
	var deletes []string
	for _, b := range branches {
		if worktree := rebases[b.Ref]; worktree != "" {
			return rebaseError(b.Name, worktree)
		}
		if worktree := checkedOut[b.Ref]; worktree != "" {
			return checkedOutError(b.Name, worktree)
		}
		deletes = append(deletes, "delete "+b.Ref+" "+b.Tip)
	}
	transaction := "start\n" + strings.Join(deletes, "\n") + "\ncommit\n"
	if _, err := d.git.RunInput(ctx, transaction, "update-ref", "-m", "strata delete", "--stdin"); err != nil {
		return fmt.Errorf("strata deleted no branch: %w", err)
	}
	for _, b := range branches {
		_, _, _ = d.git.Try(ctx, "config", "--remove-section", "branch."+b.Name)
	}
	return nil
}
