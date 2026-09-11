package ui

import (
	"path"
	"sort"
	"strings"

	"github.com/hpcsc/strata/internal/diff"
)

type dirNode struct {
	name  string
	dirs  map[string]*dirNode
	files []diff.File
}

func newDirNode(name string) *dirNode {
	return &dirNode{name: name, dirs: map[string]*dirNode{}}
}

func (d *dirNode) add(parts []string, f diff.File) {
	if len(parts) == 1 {
		d.files = append(d.files, f)
		return
	}
	child, ok := d.dirs[parts[0]]
	if !ok {
		child = newDirNode(parts[0])
		d.dirs[parts[0]] = child
	}
	child.add(parts[1:], f)
}

// joined follows a chain of directories that hold one directory and no files
// each, and returns the end of the chain with a label for the whole chain.
func (d *dirNode) joined() (*dirNode, string) {
	node, label := d, d.name
	for len(node.files) == 0 && len(node.dirs) == 1 {
		for _, only := range node.dirs {
			node = only
		}
		label += "/" + node.name
	}
	return node, label + "/"
}

func (d *dirNode) walk(depth int, prefix string, ordered *[]diff.File, rows *[]fileRow) {
	names := make([]string, 0, len(d.dirs))
	for name := range d.dirs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		node, label := d.dirs[name].joined()
		*rows = append(*rows, fileRow{depth: depth, label: label, folder: prefix + label, file: -1})
		node.walk(depth+1, prefix+label, ordered, rows)
	}
	files := append([]diff.File(nil), d.files...)
	sort.Slice(files, func(i, j int) bool { return path.Base(files[i].Path) < path.Base(files[j].Path) })
	for _, f := range files {
		*ordered = append(*ordered, f)
		*rows = append(*rows, fileRow{depth: depth, file: len(*ordered) - 1})
	}
}

// treeLayout orders files the way the tree shows them: in each directory, the
// directories first and then the files.
func treeLayout(files []diff.File) ([]diff.File, []fileRow) {
	root := newDirNode("")
	for _, f := range files {
		root.add(strings.Split(f.Path, "/"), f)
	}
	var ordered []diff.File
	var rows []fileRow
	root.walk(0, "", &ordered, &rows)
	return ordered, rows
}

func flatLayout(files []diff.File) ([]diff.File, []fileRow) {
	rows := make([]fileRow, len(files))
	for i := range files {
		rows[i] = fileRow{file: i}
	}
	return files, rows
}
