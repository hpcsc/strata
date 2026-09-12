package restack

import (
	"context"
	"fmt"
	"strings"

	"github.com/hpcsc/strata/internal/stack"
)

type Mover struct {
	git runner
}

func NewMover(git runner) *Mover {
	return &Mover{git: git}
}

func (m *Mover) Move(ctx context.Context, plan Plan) (Result, error) {
	rebase, err := newSyncRebase(ctx, m.git)
	if err != nil {
		return Result{}, err
	}
	if rebase.waits() {
		return Result{}, rebase.waitsError()
	}
	var moving [][]string
	for _, names := range stacksOf(plan.Tree) {
		if plan.stackMoves(names) {
			moving = append(moving, names)
		}
	}
	signs, err := m.signs(ctx)
	if err != nil {
		return Result{}, err
	}
	if signs && len(moving) > 0 {
		// git replay cannot sign commits, so a sync that signs makes them again with git rebase
		newTips, err := rebase.commits(ctx, plan, moving)
		if err != nil {
			return Result{}, err
		}
		plan = plan.withNewTips(newTips)
	}
	return m.moveToNewTips(ctx, plan, moving)
}

func (m *Mover) moveToNewTips(ctx context.Context, plan Plan, stacks [][]string) (Result, error) {
	check, err := newWorktreeCheck(ctx, m.git)
	if err != nil {
		return Result{}, err
	}
	checkedOut, err := m.checkedOut(ctx)
	if err != nil {
		return Result{}, err
	}
	var result Result
	for _, names := range stacks {
		reason, err := m.moveStack(ctx, plan, names, check, checkedOut)
		if err != nil {
			return result, err
		}
		if reason != "" {
			result.Stayed = append(result.Stayed, Stay{Stack: names[0], Reason: reason})
		} else {
			result.Moved++
		}
	}
	return result, forgetMovedTips(ctx, m.git)
}

func (m *Mover) signs(ctx context.Context) (bool, error) {
	out, set, err := m.git.Try(ctx, "config", "--type=bool", "--get", "commit.gpgsign")
	return set && strings.TrimSpace(out) == "true", err
}

func (m *Mover) checkedOut(ctx context.Context) (map[string]string, error) {
	out, err := m.git.Run(ctx, "for-each-ref", "--format=%(refname)%00%(worktreepath)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	worktrees := map[string]string{}
	for _, line := range lines(out) {
		if ref, worktree, _ := strings.Cut(line, "\x00"); worktree != "" {
			worktrees[ref] = worktree
		}
	}
	return worktrees, nil
}

func (m *Mover) moveStack(ctx context.Context, plan Plan, names []string, check *worktreeCheck, checkedOut map[string]string) (stayReason string, err error) {
	var updates []string
	var deleted, inWorktrees []stack.Branch
	for _, name := range names {
		b, o := plan.Tree.Branches[plan.Tree.Index(name)], plan.Outcomes[name]
		b.Worktree = checkedOut[b.Ref]
		checked, err := check.outcome(ctx, b, o)
		if err != nil {
			return "", err
		}
		if checked.Kind.blocksStack() {
			return name + " " + checked.Text(), nil
		}
		switch {
		case o.Kind == Moves && b.Worktree != "":
			inWorktrees = append(inWorktrees, b)
		case o.Kind == Moves:
			updates = append(updates, fmt.Sprintf("update %s %s %s", b.Ref, o.NewTip, b.Tip))
		case o.Kind == Merged && !o.Keep && b.Worktree == "":
			updates = append(updates, fmt.Sprintf("delete %s %s", b.Ref, b.Tip))
			deleted = append(deleted, b)
		}
	}
	if len(updates) > 0 {
		transaction := "start\n" + strings.Join(updates, "\n") + "\ncommit\n"
		if _, err := m.git.RunInput(ctx, transaction, "update-ref", "-m", "strata sync", "--stdin"); err != nil {
			return "no branch of the stack moved: " + err.Error(), nil
		}
	}
	for _, b := range deleted {
		_, _, _ = m.git.Try(ctx, "config", "--remove-section", "branch."+b.Name)
	}
	var unfinished []string
	for _, b := range inWorktrees {
		reason, err := m.moveInWorktree(ctx, b, plan.Outcomes[b.Name].NewTip)
		if err != nil {
			return "", err
		}
		if reason != "" {
			unfinished = append(unfinished, reason)
		}
	}
	return strings.Join(unfinished, "; "), nil
}

func (m *Mover) moveInWorktree(ctx context.Context, b stack.Branch, newTip string) (unfinished string, err error) {
	_, resetErr := m.git.Run(ctx, "-C", b.Worktree, "reset", "--keep", newTip)
	if resetErr == nil {
		return "", nil
	}
	ref := newTipRefs + b.Name
	if _, err := m.git.Run(ctx, "update-ref", ref, newTip); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s did not move in %s (%v). To finish the move, run: git -C %s reset --keep %s",
		b.Name, b.Worktree, resetErr, shellQuote(b.Worktree), shellQuote(ref)), nil
}

type Result struct {
	Moved int
	// Stayed holds only the stacks that the plan moved.
	Stayed []Stay
}

type Stay struct {
	Stack  string
	Reason string
}
