package stack

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
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

type Deletion struct {
	Branch          Branch
	TrunkHas        bool
	RemovesWorktree string
	WorktreeGone    bool
	LostFiles       []string
}

func (d *Deleter) Check(ctx context.Context, branches []Branch) ([]Deletion, error) {
	s, err := d.readState(ctx)
	if err != nil {
		return nil, err
	}
	out, err := d.git.Run(ctx, "rev-parse", d.trunk, d.trunk+"^{tree}")
	if err != nil {
		return nil, err
	}
	parsed := lines(out)
	if len(parsed) != 2 {
		return nil, fmt.Errorf("find the trunk %s: git rev-parse printed %q", d.trunk, out)
	}
	tip, treeOfTip := parsed[0], parsed[1]
	deletions := make([]Deletion, 0, len(branches))
	for _, b := range branches {
		del, err := s.deletion(ctx, d.git, b)
		if err != nil {
			return nil, err
		}
		out, clean, err := d.git.Try(ctx, "merge-tree", "--write-tree", tip, b.Tip)
		if err != nil {
			return nil, err
		}
		written := lines(out)
		del.TrunkHas = clean && len(written) > 0 && written[0] == treeOfTip
		deletions = append(deletions, del)
	}
	return deletions, nil
}

func (d *Deleter) Delete(ctx context.Context, deletions []Deletion) error {
	if len(deletions) == 0 {
		return nil
	}
	s, err := d.readState(ctx)
	if err != nil {
		return err
	}
	for _, want := range deletions {
		got, err := s.deletion(ctx, d.git, want.Branch)
		if err != nil {
			return err
		}
		if got.RemovesWorktree != want.RemovesWorktree || !isSubset(got.LostFiles, want.LostFiles) {
			return fmt.Errorf("the worktree of %s changed after strata showed the delete: press d again", want.Branch.Name)
		}
	}
	var removed []string
	for _, del := range deletions {
		if del.RemovesWorktree == "" {
			continue
		}
		args := []string{"worktree", "remove"}
		if len(del.LostFiles) > 0 {
			args = append(args, "--force")
		}
		if _, err := d.git.Run(ctx, append(args, del.RemovesWorktree)...); err != nil {
			return noBranchDeleted(removed, err)
		}
		removed = append(removed, del.RemovesWorktree)
	}
	deletes := make([]string, len(deletions))
	for i, del := range deletions {
		deletes[i] = "delete " + del.Branch.Ref + " " + del.Branch.Tip
	}
	transaction := "start\n" + strings.Join(deletes, "\n") + "\ncommit\n"
	if _, err := d.git.RunInput(ctx, transaction, "update-ref", "-m", "strata delete", "--stdin"); err != nil {
		return noBranchDeleted(removed, err)
	}
	for _, del := range deletions {
		_, _, _ = d.git.Try(ctx, "config", "--remove-section", "branch."+del.Branch.Name)
	}
	return nil
}

func noBranchDeleted(removed []string, err error) error {
	if len(removed) == 0 {
		return fmt.Errorf("strata deleted no branch: %w", err)
	}
	return fmt.Errorf("strata removed %s and deleted no branch: %w", strings.Join(removed, ", "), err)
}

func isSubset(files, of []string) bool {
	for _, f := range files {
		if !slices.Contains(of, f) {
			return false
		}
	}
	return true
}

type worktree struct {
	path       string
	main       bool
	locked     bool
	lockReason string
	prunable   bool
}

type repoState struct {
	here       string
	checkedOut map[string]worktree
	rebases    map[string]string
}

func (d *Deleter) readState(ctx context.Context) (repoState, error) {
	s := repoState{checkedOut: map[string]worktree{}}
	if out, err := d.git.Run(ctx, "rev-parse", "--show-toplevel"); err == nil {
		s.here = strings.TrimSpace(out)
	}
	common, err := d.git.Run(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return repoState{}, err
	}
	s.rebases = RebaseWorktrees(strings.TrimSpace(common))
	out, err := d.git.Run(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return repoState{}, err
	}
	for i, record := range strings.Split(strings.Trim(out, "\x00"), "\x00\x00") {
		w, ref := worktree{main: i == 0}, ""
		for _, line := range strings.Split(record, "\x00") {
			key, value, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				w.path = value
			case "branch":
				ref = value
			case "locked":
				w.locked, w.lockReason = true, value
			case "prunable":
				w.prunable = true
			}
		}
		if ref != "" {
			s.checkedOut[ref] = w
		}
	}
	return s, nil
}

func (s repoState) deletion(ctx context.Context, git writer, b Branch) (Deletion, error) {
	if worktree := s.rebases[b.Ref]; worktree != "" {
		return Deletion{}, rebaseError(b.Name, worktree)
	}
	del := Deletion{Branch: b}
	w, ok := s.checkedOut[b.Ref]
	switch {
	case !ok:
		return del, nil
	case samePath(w.path, s.here):
		return Deletion{}, fmt.Errorf("%s is checked out here: switch to another branch first", b.Name)
	case w.main:
		return Deletion{}, fmt.Errorf("%s is checked out in the main worktree %s: switch it to another branch first", b.Name, w.path)
	case w.locked && w.lockReason != "":
		return Deletion{}, fmt.Errorf("the worktree %s of %s is locked (%s): unlock it with git worktree unlock first", w.path, b.Name, w.lockReason)
	case w.locked:
		return Deletion{}, fmt.Errorf("the worktree %s of %s is locked: unlock it with git worktree unlock first", w.path, b.Name)
	case w.prunable:
		del.RemovesWorktree, del.WorktreeGone = w.path, true
		return del, nil
	}
	out, err := git.Run(ctx, "-C", w.path, "status", "--porcelain", "-z")
	if err != nil {
		return Deletion{}, err
	}
	del.RemovesWorktree, del.LostFiles = w.path, changedFiles(out)
	return del, nil
}

func changedFiles(status string) []string {
	var files []string
	records := strings.Split(strings.TrimRight(status, "\x00"), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 {
			continue
		}
		files = append(files, record[3:])
		// a rename or a copy names its source in the next record
		if record[0] == 'R' || record[0] == 'C' {
			i++
		}
	}
	return files
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if real, err := filepath.EvalSymlinks(a); err == nil {
		a = real
	}
	if real, err := filepath.EvalSymlinks(b); err == nil {
		b = real
	}
	return a == b
}
