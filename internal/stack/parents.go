package stack

import (
	"strconv"
	"strings"
)

type tip struct {
	name        string
	ref         string
	commit      string
	behindTrunk int
}

type reflogEntry struct {
	commit  string
	at      int64
	message string
}

type branchFacts struct {
	depth     int
	ancestors []tip
	// reflog lists the oldest entry first.
	reflog []reflogEntry
}

type checkout struct {
	from string
	to   string
	at   int64
}

type candidate struct {
	branch string
	from   string
}

type link struct {
	parent string
	base   string
	// depth counts the commits between the trunk and base.
	depth int
}

type placement struct {
	name  string
	level int
}

const createdFromPrefix = "branch: Created from "

func creatorCandidates(tips []tip, facts map[string]branchFacts, checkouts []checkout) []candidate {
	listed := map[string]bool{}
	for _, t := range tips {
		listed[t.name] = true
	}
	holders := map[string][]string{}
	for _, t := range tips {
		seen := map[string]bool{}
		for _, e := range facts[t.name].reflog {
			if !seen[e.commit] {
				seen[e.commit] = true
				holders[e.commit] = append(holders[e.commit], t.name)
			}
		}
	}

	var out []candidate
	for _, t := range tips {
		reflog := facts[t.name].reflog
		if len(reflog) == 0 {
			continue
		}
		creation := reflog[0]
		from := ""
		if strings.HasPrefix(creation.message, createdFromPrefix) {
			from = strings.TrimPrefix(creation.message, createdFromPrefix)
		}
		if from == "HEAD" {
			from = checkedOutFrom(t.name, creation.at, checkouts)
		}
		if listed[from] && from != t.name {
			out = append(out, candidate{branch: t.name, from: from})
			continue
		}
		for _, h := range holders[creation.commit] {
			if h != t.name {
				out = append(out, candidate{branch: t.name, from: h})
			}
		}
	}
	return out
}

func chooseParents(tips []tip, facts map[string]branchFacts, created map[string]link) map[string]link {
	chosen := map[string]link{}
	for _, t := range tips {
		best := link{}
		for _, a := range facts[t.name].ancestors {
			if a.name == t.name || a.commit == t.commit {
				continue
			}
			if c, ok := created[a.name]; ok && c.parent == t.name {
				continue
			}
			if d := facts[a.name].depth; d > best.depth {
				best = link{parent: a.name, base: a.commit, depth: d}
			}
		}
		if c, ok := created[t.name]; ok && c.depth > best.depth {
			best = c
		}
		if best.parent != "" {
			chosen[t.name] = best
		}
	}
	return chosen
}

func placeInTree(tips []tip, parents map[string]link) []placement {
	listed := map[string]bool{}
	for _, t := range tips {
		listed[t.name] = true
	}
	children := map[string][]string{}
	var roots []string
	for _, t := range tips {
		if p, ok := parents[t.name]; ok && listed[p.parent] {
			children[p.parent] = append(children[p.parent], t.name)
		} else {
			roots = append(roots, t.name)
		}
	}

	var out []placement
	seen := map[string]bool{}
	var walk func(name string, level int)
	walk = func(name string, level int) {
		if seen[name] {
			return
		}
		seen[name] = true
		out = append(out, placement{name: name, level: level})
		for _, c := range children[name] {
			walk(c, level+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	// branches whose parents form a cycle cannot be reached from a root
	for _, t := range tips {
		walk(t.name, 0)
	}
	return out
}

func checkedOutFrom(branch string, at int64, checkouts []checkout) string {
	for _, c := range checkouts {
		// the branch creation and the HEAD checkout can differ by a second or two
		if c.to == branch && c.at-at <= 2 && at-c.at <= 2 {
			return c.from
		}
	}
	return ""
}

// parseReflog reads a reflog file, which lists the oldest entry first.
func parseReflog(log string) []reflogEntry {
	var out []reflogEntry
	for _, line := range strings.Split(log, "\n") {
		entry, message, found := strings.Cut(line, "\t")
		fields := strings.Fields(entry)
		if !found || len(fields) < 4 {
			continue
		}
		at, err := strconv.ParseInt(fields[len(fields)-2], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, reflogEntry{commit: fields[1], at: at, message: message})
	}
	return out
}

func parseCheckouts(headLog string) []checkout {
	var out []checkout
	for _, e := range parseReflog(headLog) {
		words := strings.Fields(e.message)
		if len(words) == 6 && words[0] == "checkout:" && words[1] == "moving" && words[4] == "to" {
			out = append(out, checkout{from: words[3], to: words[5], at: e.at})
		}
	}
	return out
}
