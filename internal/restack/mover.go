package restack

import (
	"context"
	"errors"
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

type Result struct {
	Moved int
	// Stayed holds only the stacks that the plan moved.
	Stayed []Stay
}

type Stay struct {
	Stack  string
	Reason string
}

func (m *Mover) Move(ctx context.Context, plan Plan) (Result, error) {
	signs, err := m.signs(ctx)
	if err != nil {
		return Result{}, err
	}
	if signs {
		return Result{}, errors.New("strata sync cannot sign commits yet, and commit.gpgsign is true")
	}
	check, err := newWorktreeCheck(ctx, m.git)
	if err != nil {
		return Result{}, err
	}
	var result Result
	for _, names := range stacksOf(plan.Tree) {
		if !plan.stackMoves(names) {
			continue
		}
		reason, err := m.moveStack(ctx, plan, names, check)
		if err != nil {
			return result, err
		}
		if reason != "" {
			result.Stayed = append(result.Stayed, Stay{Stack: names[0], Reason: reason})
		} else {
			result.Moved++
		}
	}
	return result, nil
}

func (m *Mover) signs(ctx context.Context) (bool, error) {
	out, set, err := m.git.Try(ctx, "config", "--type=bool", "--get", "commit.gpgsign")
	return set && strings.TrimSpace(out) == "true", err
}

func (m *Mover) moveStack(ctx context.Context, plan Plan, names []string, check *worktreeCheck) (stayReason string, err error) {
	var updates []string
	var deleted, checkedOut []stack.Branch
	for _, name := range names {
		b, o := plan.Tree.Branches[plan.Tree.Index(name)], plan.Outcomes[name]
		checked, err := check.outcome(ctx, b, o)
		if err != nil {
			return "", err
		}
		if checked.Kind.keepsStack() {
			return name + " " + checked.Text(), nil
		}
		switch {
		case o.Kind == Moves && b.Worktree != "":
			checkedOut = append(checkedOut, b)
		case o.Kind == Moves:
			updates = append(updates, fmt.Sprintf("update %s %s %s", b.Ref, o.NewTip, b.Tip))
		case o.Kind == Merged && !o.Keep && o.Worktree == "":
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
	for _, b := range checkedOut {
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
	ref := "refs/strata/sync/" + b.Name
	if _, err := m.git.Run(ctx, "update-ref", ref, newTip); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s did not move in %s (%v). To finish the move, run: git -C %s reset --keep %s",
		b.Name, b.Worktree, resetErr, b.Worktree, ref), nil
}
