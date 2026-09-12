package restack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hpcsc/strata/internal/stack"
)

type State int

const (
	NoRebase State = iota
	RebaseWaits
	RebaseDone
	RebaseStopped
)

type Pending struct {
	State    State
	Stack    string
	Worktree string
	// Plan holds the resolved stack when State is RebaseDone.
	Plan Plan
}

func (p Pending) WaitsError() error {
	return fmt.Errorf("the sync rebase for the stack of %s waits in %s: finish it there with git rebase --continue, or stop it with git rebase --abort",
		p.Stack, p.Worktree)
}

type Resolver struct {
	git   runner
	mover *Mover
}

func NewResolver(git runner, mover *Mover) *Resolver {
	return &Resolver{git: git, mover: mover}
}

func (r *Resolver) Start(ctx context.Context, plan Plan, branch string) (Pending, error) {
	rebase, err := newSyncRebase(ctx, r.git)
	if err != nil {
		return Pending{}, err
	}
	pending, record, err := r.pending(ctx, rebase)
	if err != nil {
		return Pending{}, err
	}
	switch pending.State {
	case RebaseWaits:
		return Pending{}, pending.WaitsError()
	case RebaseDone:
		return Pending{}, fmt.Errorf("the sync rebase for the stack of %s is done: run strata sync to move the stack", pending.Stack)
	case RebaseStopped:
		if err := r.forgetStopped(ctx, rebase, record); err != nil {
			return Pending{}, err
		}
	}

	names := stackOf(plan, branch)
	if names == nil {
		return Pending{}, fmt.Errorf("the plan has no branch %s", branch)
	}
	resolve, err := planToResolve(plan, names)
	if err != nil {
		return Pending{}, err
	}
	if err := writeRecord(rebase.recordFile(), resolve); err != nil {
		return Pending{}, err
	}
	if err := rebase.forget(ctx, names); err != nil {
		return Pending{}, err
	}
	if _, stopped, err := rebase.start(ctx, resolve, [][]string{names}); err != nil && !stopped {
		_ = r.forgetStopped(ctx, rebase, resolve)
		return Pending{}, err
	}
	pending, _, err = r.pending(ctx, rebase)
	return pending, err
}

func (r *Resolver) Pending(ctx context.Context) (Pending, error) {
	rebase, err := newSyncRebase(ctx, r.git)
	if err != nil {
		return Pending{}, err
	}
	pending, _, err := r.pending(ctx, rebase)
	return pending, err
}

// Finish moves the stack of a sync rebase that is done, or removes what a
// sync rebase that stopped left behind.
func (r *Resolver) Finish(ctx context.Context) (Result, error) {
	rebase, err := newSyncRebase(ctx, r.git)
	if err != nil {
		return Result{}, err
	}
	pending, record, err := r.pending(ctx, rebase)
	if err != nil {
		return Result{}, err
	}
	switch pending.State {
	case NoRebase:
		return Result{}, nil
	case RebaseWaits:
		return Result{}, pending.WaitsError()
	case RebaseStopped:
		return Result{}, r.forgetStopped(ctx, rebase, record)
	}
	newTips, err := rebase.newTips(ctx)
	if err != nil {
		return Result{}, err
	}
	result, err := r.mover.moveToNewTips(ctx, record.withNewTips(newTips), stacksOf(record.Tree))
	if err != nil {
		return result, err
	}
	rebase.removeWorktree(ctx)
	return result, removeRecord(rebase.recordFile())
}

func (r *Resolver) pending(ctx context.Context, rebase *syncRebase) (Pending, Plan, error) {
	record, found, err := readRecord(rebase.recordFile())
	if err != nil || !found {
		return Pending{State: NoRebase}, Plan{}, err
	}
	pending := Pending{Stack: record.Tree.Branches[0].Name, Worktree: rebase.worktree()}
	if rebase.waits() {
		pending.State = RebaseWaits
		return pending, record, nil
	}
	newTips, err := rebase.newTips(ctx)
	if err != nil {
		return Pending{}, Plan{}, err
	}
	for name, o := range record.Outcomes {
		if o.Kind == Moves && newTips[name] == "" {
			pending.State = RebaseStopped
			return pending, record, nil
		}
	}
	pending.State, pending.Plan = RebaseDone, record.withNewTips(newTips)
	return pending, record, nil
}

func (r *Resolver) forgetStopped(ctx context.Context, rebase *syncRebase, record Plan) error {
	rebase.removeWorktree(ctx)
	var names []string
	for _, b := range record.Tree.Branches {
		names = append(names, b.Name)
	}
	if err := rebase.forget(ctx, names); err != nil {
		return err
	}
	return removeRecord(rebase.recordFile())
}

func stackOf(plan Plan, branch string) []string {
	for _, names := range stacksOf(plan.Tree) {
		for _, name := range names {
			if name == branch {
				return names
			}
		}
	}
	return nil
}

func planToResolve(plan Plan, names []string) (Plan, error) {
	conflict := false
	outcomes := map[string]Outcome{}
	var branches []stack.Branch
	for _, name := range names {
		b, o := plan.Tree.Branches[plan.Tree.Index(name)], plan.Outcomes[name]
		switch {
		case o.Kind == Conflict:
			conflict = true
			o.Kind = Moves
		case o.Kind.keepsStack():
			return Plan{}, fmt.Errorf("the stack of %s stays because %s %s, and a rebase does not resolve that", names[0], name, o.Text())
		}
		if o.Kind == Moves && b.HasMergeCommit {
			return Plan{}, fmt.Errorf("the stack of %s stays because %s has a merge commit, and a sync rebase does not replay merge commits", names[0], name)
		}
		o.Blocker, o.NewTip = "", ""
		outcomes[name] = o
		branches = append(branches, b)
	}
	if !conflict {
		return Plan{}, fmt.Errorf("the stack of %s has no conflict", names[0])
	}
	tree := stack.Tree{Trunk: plan.Tree.Trunk, Current: plan.Tree.Current, Branches: branches}
	return Plan{Tree: tree, NewCommits: plan.NewCommits, TrunkTip: plan.TrunkTip, Outcomes: outcomes}, nil
}

func writeRecord(path string, plan Plan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readRecord(path string) (Plan, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Plan{}, false, nil
	}
	if err != nil {
		return Plan{}, false, err
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return Plan{}, false, fmt.Errorf("read the record of the sync rebase %s: %w", path, err)
	}
	if len(plan.Tree.Branches) == 0 {
		return Plan{}, false, fmt.Errorf("read the record of the sync rebase %s: it has no branch", path)
	}
	return plan, true, nil
}

func removeRecord(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
