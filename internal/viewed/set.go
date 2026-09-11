package viewed

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hpcsc/strata/internal/diff"
)

// Set holds the files marked viewed. A mark names the file's blobs before and
// after the change, so it stops applying once either side changes.
type Set struct {
	path string
	keys map[string]bool
}

func Load(path string) (*Set, error) {
	s := &Set{path: path, keys: map[string]bool{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			s.keys[line] = true
		}
	}
	return s, nil
}

func (s *Set) Has(f diff.File) bool {
	return s.keys[key(f)]
}

func (s *Set) Toggle(f diff.File) error {
	k := key(f)
	if s.keys[k] {
		delete(s.keys, k)
	} else {
		s.keys[k] = true
	}
	return s.save()
}

func (s *Set) save() error {
	lines := make([]string, 0, len(s.keys))
	for k := range s.keys {
		lines = append(lines, k)
	}
	sort.Strings(lines)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".viewed-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), s.path)
}

func key(f diff.File) string {
	return f.OldBlob + " " + f.NewBlob + " " + f.Path
}
