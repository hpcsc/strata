package stack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
)

type runner interface {
	Run(ctx context.Context, args ...string) (string, error)
	Check(ctx context.Context, args ...string) (bool, error)
}

const parallelGitCommands = 8

type Reader struct {
	git      runner
	trunk    string
	patterns []string
}

func NewReader(git runner, trunk string, patterns []string) *Reader {
	return &Reader{git: git, trunk: trunk, patterns: patterns}
}

func (r *Reader) Read(ctx context.Context) (Tree, error) {
	tips, err := r.tips(ctx)
	if err != nil {
		return Tree{}, err
	}
	g, err := r.graph(ctx, tips)
	if err != nil {
		return Tree{}, err
	}
	out, err := r.git.Run(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Tree{}, err
	}
	common := strings.TrimSpace(out)

	facts := map[string]branchFacts{}
	for _, t := range tips {
		f := branchFacts{depth: g.count(t.commit), reflog: readReflog(filepath.Join(common, "logs", t.ref))}
		for _, other := range tips {
			if g.contains(t.commit, other.commit) {
				f.ancestors = append(f.ancestors, other)
			}
		}
		facts[t.name] = f
	}
	created, err := r.measure(ctx, g, tips, creatorCandidates(tips, facts, readCheckouts(common)))
	if err != nil {
		return Tree{}, err
	}
	branches := r.place(g, tips, chooseParents(tips, facts, created))

	var current string
	grp, gctx := errgroup.WithContext(ctx)
	grp.SetLimit(parallelGitCommands)
	grp.Go(func() error {
		out, err := r.git.Run(gctx, "branch", "--show-current")
		current = strings.TrimSpace(out)
		if err != nil {
			current = ""
		}
		return nil
	})
	for i := range branches {
		grp.Go(func() error {
			return r.fillDiffStat(gctx, &branches[i], tips)
		})
	}
	if err := grp.Wait(); err != nil {
		return Tree{}, err
	}
	return Tree{Trunk: r.trunk, Current: current, Branches: branches}, nil
}

func (r *Reader) Commits(ctx context.Context, b Branch) ([]Commit, error) {
	out, err := r.git.Run(ctx, "log", "--no-color", "--format=%h%x00%s%x00%ar", b.Base+".."+b.Name, "--")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range lines(out) {
		fields := strings.SplitN(line, "\x00", 3)
		if len(fields) == 3 {
			commits = append(commits, Commit{Hash: fields[0], Subject: fields[1], Age: fields[2]})
		}
	}
	return commits, nil
}

func (r *Reader) tips(ctx context.Context) ([]tip, error) {
	format := "--format=%(refname:short)%00%(refname)%00%(objectname)%00%(ahead-behind:" + r.trunk + ")"
	out, err := r.git.Run(ctx, append([]string{"for-each-ref", "--no-merged", r.trunk, format}, r.patterns...)...)
	if err != nil {
		return nil, err
	}
	var tips []tip
	for _, line := range lines(out) {
		fields := strings.SplitN(line, "\x00", 4)
		if len(fields) != 4 || fields[0] == r.trunk || fields[0] == strings.TrimPrefix(r.trunk, "origin/") {
			continue
		}
		t := tip{name: fields[0], ref: fields[1], commit: fields[2]}
		if counts := strings.Fields(fields[3]); len(counts) == 2 {
			t.behindTrunk, _ = strconv.Atoi(counts[1])
		}
		tips = append(tips, t)
	}
	return tips, nil
}

func (r *Reader) graph(ctx context.Context, tips []tip) (*graph, error) {
	if len(tips) == 0 {
		return newGraph(""), nil
	}
	args := []string{"rev-list", "--parents"}
	for _, t := range tips {
		args = append(args, t.commit)
	}
	out, err := r.git.Run(ctx, append(args, "--not", r.trunk, "--")...)
	if err != nil {
		return nil, err
	}
	return newGraph(out), nil
}

func (r *Reader) measure(ctx context.Context, g *graph, tips []tip, candidates []candidate) (map[string]link, error) {
	commits := map[string]string{}
	for _, t := range tips {
		commits[t.name] = t.commit
	}
	created := map[string]link{}
	for _, c := range candidates {
		base, shared, ok := g.mergeBase(commits[c.from], commits[c.branch])
		if shared == 0 {
			continue
		}
		if !ok {
			out, err := r.git.Run(ctx, "merge-base", commits[c.from], commits[c.branch])
			if err != nil {
				continue
			}
			base = strings.TrimSpace(out)
		}
		if shared > created[c.branch].depth {
			created[c.branch] = link{parent: c.from, base: base, depth: shared}
		}
	}
	return created, nil
}

// place builds the branches in tree order from the graph alone; their bases
// on the trunk and their diff stats still need git.
func (r *Reader) place(g *graph, tips []tip, parents map[string]link) []Branch {
	byName := map[string]tip{}
	for _, t := range tips {
		byName[t.name] = t
	}
	placements := placeInTree(tips, parents)
	branches := make([]Branch, len(placements))
	for i, p := range placements {
		t, parent := byName[p.name], parents[p.name]
		b := Branch{Name: p.name, Level: p.level}
		if parent.parent == "" {
			b.Parent, b.Behind, b.Commits = r.trunk, t.behindTrunk, g.count(t.commit)
			b.Base, _ = g.forkPoint(t.commit)
		} else {
			b.Parent, b.Base = parent.parent, parent.base
			b.Commits = g.only(t.commit, parent.base)
			b.Behind = g.only(byName[parent.parent].commit, t.commit)
		}
		branches[i] = b
	}
	return branches
}

func (r *Reader) fillDiffStat(ctx context.Context, b *Branch, tips []tip) error {
	commit := b.Name
	for _, t := range tips {
		if t.name == b.Name {
			commit = t.commit
		}
	}
	if b.Base == "" {
		out, err := r.git.Run(ctx, "merge-base", r.trunk, commit)
		if err != nil {
			return fmt.Errorf("find where %s leaves %s: %w", b.Name, r.trunk, err)
		}
		b.Base = strings.TrimSpace(out)
	}
	out, err := r.git.Run(ctx, "diff", "--no-color", "--no-ext-diff", "--shortstat", "-M", b.Base, commit)
	if err != nil {
		return err
	}
	b.Files, b.Insertions, b.Deletions = parseShortstat(out)
	return nil
}

func readReflog(path string) []reflogEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseReflog(string(data))
}

func readCheckouts(common string) []checkout {
	worktrees, _ := filepath.Glob(filepath.Join(common, "worktrees", "*", "logs", "HEAD"))
	var checkouts []checkout
	for _, path := range append([]string{filepath.Join(common, "logs", "HEAD")}, worktrees...) {
		if data, err := os.ReadFile(path); err == nil {
			checkouts = append(checkouts, parseCheckouts(string(data))...)
		}
	}
	return checkouts
}

var shortstatNumber = regexp.MustCompile(`(\d+) (file|insertion|deletion)`)

func parseShortstat(out string) (files, insertions, deletions int) {
	for _, m := range shortstatNumber.FindAllStringSubmatch(out, -1) {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "file":
			files = n
		case "insertion":
			insertions = n
		case "deletion":
			deletions = n
		}
	}
	return files, insertions, deletions
}

func lines(out string) []string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}
