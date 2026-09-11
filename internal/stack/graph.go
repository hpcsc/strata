package stack

import "strings"

// graph holds the commits the listed branches have and the trunk does not.
// A commit outside the graph reaches nothing in it. Its methods memoise, so
// one goroutine at a time can use it.
type graph struct {
	ids     []string
	index   map[string]int
	parents [][]int
	// trunkParents are the parents of each commit that the trunk has.
	trunkParents [][]string
	reached      map[int]bitset
}

// newGraph reads the output of git rev-list --parents.
func newGraph(revList string) *graph {
	g := &graph{index: map[string]int{}, reached: map[int]bitset{}}
	var rows [][]string
	for _, line := range lines(revList) {
		if fields := strings.Fields(line); len(fields) > 0 {
			g.index[fields[0]] = len(g.ids)
			g.ids = append(g.ids, fields[0])
			rows = append(rows, fields)
		}
	}
	g.parents = make([][]int, len(g.ids))
	g.trunkParents = make([][]string, len(g.ids))
	for i, fields := range rows {
		for _, p := range fields[1:] {
			if j, ok := g.index[p]; ok {
				g.parents[i] = append(g.parents[i], j)
			} else {
				g.trunkParents[i] = append(g.trunkParents[i], p)
			}
		}
	}
	return g
}

func (g *graph) reach(commit string) bitset {
	start, ok := g.index[commit]
	if !ok {
		return newBitset(len(g.ids))
	}
	if r, ok := g.reached[start]; ok {
		return r
	}
	r := newBitset(len(g.ids))
	pending := []int{start}
	for len(pending) > 0 {
		n := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if !r.has(n) {
			r.set(n)
			pending = append(pending, g.parents[n]...)
		}
	}
	g.reached[start] = r
	return r
}

func (g *graph) count(commit string) int {
	return g.reach(commit).count()
}

func (g *graph) contains(from, commit string) bool {
	i, ok := g.index[commit]
	return ok && g.reach(from).has(i)
}

// only counts the commits reachable from a and not from b.
func (g *graph) only(a, b string) int {
	return g.reach(a).andNot(g.reach(b)).count()
}

// mergeBase returns the newest commit a and b share, and how many commits
// they share. ok is false when they share none, or more than one newest.
func (g *graph) mergeBase(a, b string) (base string, shared int, ok bool) {
	common := g.reach(a).and(g.reach(b))
	shared = common.count()
	if shared == 0 {
		return "", 0, false
	}
	older := newBitset(len(g.ids))
	for i := range g.ids {
		if common.has(i) {
			for _, p := range g.parents[i] {
				older.set(p)
			}
		}
	}
	newest := common.andNot(older)
	if newest.count() != 1 {
		return "", shared, false
	}
	return g.ids[newest.first()], shared, true
}

// forkPoint returns the trunk commit that commit's own history grows from.
// ok is false when that history grows from more than one.
func (g *graph) forkPoint(commit string) (string, bool) {
	r := g.reach(commit)
	var points []string
	for i := range g.ids {
		if !r.has(i) {
			continue
		}
		for _, p := range g.trunkParents[i] {
			if len(points) == 0 || points[0] != p {
				points = append(points, p)
			}
		}
	}
	if len(points) != 1 {
		return "", false
	}
	return points[0], true
}
