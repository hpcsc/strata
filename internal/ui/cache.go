package ui

import (
	"context"
	"maps"

	tea "charm.land/bubbletea/v2"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/diffview"
	"github.com/hpcsc/strata/internal/stack"
)

type fileList struct {
	files []diff.File
	err   error
}

type patchResult struct {
	source diffview.Source
	err    error
}

type commitList struct {
	commits []stack.Commit
	err     error
}

type filesLoaded struct {
	key  string
	list fileList
}

type patchLoaded struct {
	key    string
	result patchResult
}

type commitsLoaded struct {
	key  string
	list commitList
}

// cache remembers what has been loaded for the panels. Its maps are touched
// only from Update and View; the load commands return messages instead.
type cache struct {
	ctx      context.Context
	sources  Sources
	files    map[string]fileList
	patches  map[string]patchResult
	commits  map[string]commitList
	inFlight map[string]bool
}

func newCache(ctx context.Context, sources Sources) *cache {
	c := &cache{ctx: ctx, sources: sources}
	c.clear()
	return c
}

func (c *cache) clear() {
	c.files = map[string]fileList{}
	c.patches = map[string]patchResult{}
	c.commits = map[string]commitList{}
	c.inFlight = map[string]bool{}
}

// A commit list loads again because the ages in it change.
func (c *cache) clearCommitsAndErrors() {
	c.commits = map[string]commitList{}
	maps.DeleteFunc(c.files, func(_ string, list fileList) bool { return list.err != nil })
	maps.DeleteFunc(c.patches, func(_ string, result patchResult) bool { return result.err != nil })
}

func (c *cache) fileList(b stack.Branch) (fileList, bool) {
	list, ok := c.files[branchKey(b)]
	return list, ok
}

func (c *cache) requestFiles(b stack.Branch) tea.Cmd {
	key := branchKey(b)
	if _, ok := c.files[key]; ok || !c.start("files", key) {
		return nil
	}
	ctx, diffs := c.ctx, c.sources.Diffs
	return func() tea.Msg {
		files, err := diffs.Files(ctx, b.Base, b.Name)
		return filesLoaded{key: key, list: fileList{files: files, err: err}}
	}
}

func (c *cache) patch(b stack.Branch, f diff.File) (patchResult, bool) {
	result, ok := c.patches[fileKey(b, f)]
	return result, ok
}

func (c *cache) requestPatch(b stack.Branch, f diff.File) tea.Cmd {
	key := fileKey(b, f)
	if _, ok := c.patches[key]; ok || !c.start("patch", key) {
		return nil
	}
	ctx, diffs, highlighter := c.ctx, c.sources.Diffs, c.sources.Highlighter
	return func() tea.Msg {
		patch, err := diffs.Patch(ctx, b.Base, b.Name, f)
		if err != nil {
			return patchLoaded{key: key, result: patchResult{err: err}}
		}
		src := diffview.Source{Patch: patch}
		if len(patch.Hunks) > 0 {
			if before, after, err := diffs.Contents(ctx, f); err == nil {
				oldPath := f.Path
				if f.OldPath != "" {
					oldPath = f.OldPath
				}
				src.Before = highlighter.Lines(oldPath, before)
				src.After = highlighter.Lines(f.Path, after)
			}
		}
		return patchLoaded{key: key, result: patchResult{source: src}}
	}
}

func (c *cache) commitList(b stack.Branch) (commitList, bool) {
	list, ok := c.commits[branchKey(b)]
	return list, ok
}

func (c *cache) requestCommits(b stack.Branch) tea.Cmd {
	key := branchKey(b)
	if _, ok := c.commits[key]; ok || !c.start("commits", key) {
		return nil
	}
	ctx, tree := c.ctx, c.sources.Tree
	return func() tea.Msg {
		commits, err := tree.Commits(ctx, b)
		return commitsLoaded{key: key, list: commitList{commits: commits, err: err}}
	}
}

// store records a loaded message and reports whether msg was one.
func (c *cache) store(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case filesLoaded:
		delete(c.inFlight, "files"+msg.key)
		c.files[msg.key] = msg.list
	case patchLoaded:
		delete(c.inFlight, "patch"+msg.key)
		c.patches[msg.key] = msg.result
	case commitsLoaded:
		delete(c.inFlight, "commits"+msg.key)
		c.commits[msg.key] = msg.list
	default:
		return false
	}
	return true
}

func (c *cache) start(kind, key string) bool {
	if c.inFlight[kind+key] {
		return false
	}
	c.inFlight[kind+key] = true
	return true
}

func branchKey(b stack.Branch) string {
	return b.Base + ".." + b.Name + "@" + b.Tip
}

// The tip stays out of the key, so a diff stays loaded when its branch moves
// and the file does not change.
func fileKey(b stack.Branch, f diff.File) string {
	return b.Base + ".." + b.Name + "\x00" + f.OldBlob + "\x00" + f.NewBlob + "\x00" + f.Path
}
