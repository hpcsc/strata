package refs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type runner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

var errWatchStopped = errors.New("the watch of the refs stopped")

type Watcher struct {
	git        runner
	quietTime  time.Duration
	backupPoll time.Duration
	fastPoll   time.Duration
}

func NewWatcher(git runner) *Watcher {
	return &Watcher{git: git, quietTime: 200 * time.Millisecond, backupPoll: 30 * time.Second, fastPoll: 2 * time.Second}
}

// Run calls check after each group of changes to the files that hold the refs,
// and at each poll, until ctx ends. When it cannot watch the files, it polls
// more often.
func (w *Watcher) Run(ctx context.Context, check func()) {
	if err := w.watch(ctx, check); err != nil && ctx.Err() == nil {
		w.poll(ctx, check)
	}
}

func (w *Watcher) watch(ctx context.Context, check func()) error {
	out, err := w.git.Run(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	common := strings.TrimSpace(out)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	if err := add(watcher, common, common); err != nil {
		return err
	}
	// a ref can move before the watches start
	check()

	quiet := time.NewTimer(w.quietTime)
	quiet.Stop()
	backup := time.NewTicker(w.backupPoll)
	defer backup.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return errWatchStopped
			}
			changed, err := changesRefs(watcher, common, event)
			if err != nil {
				return err
			}
			if changed {
				quiet.Reset(w.quietTime)
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return errWatchStopped
			}
			// an error, such as a full event queue, can lose a change
			quiet.Reset(w.quietTime)
		case <-quiet.C:
			check()
		case <-backup.C:
			check()
		}
	}
}

func (w *Watcher) poll(ctx context.Context, check func()) {
	ticker := time.NewTicker(w.fastPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

// changesRefs reports whether event can change the refs, and watches each
// folder that the event makes.
func changesRefs(watcher *fsnotify.Watcher, common string, event fsnotify.Event) (bool, error) {
	if event.Op == fsnotify.Chmod {
		return false, nil
	}
	rel := relative(common, event.Name)
	if event.Has(fsnotify.Create) && watched(rel) {
		// git can write a ref into the folder before the watch on it starts
		if info, err := os.Lstat(event.Name); err == nil && info.IsDir() {
			return true, add(watcher, common, event.Name)
		}
	}
	return holdsRefs(rel), nil
}

func add(watcher *fsnotify.Watcher, common, folder string) error {
	return filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if !watched(relative(common, path)) {
			return filepath.SkipDir
		}
		if err := watcher.Add(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
}

func relative(common, path string) string {
	rel, err := filepath.Rel(common, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// watched reports whether the folder at rel, under the common git folder, can
// hold a file whose change changes the refs.
func watched(rel string) bool {
	parts := strings.Split(rel, "/")
	switch {
	case rel == ".", rel == "reftable", rel == "worktrees", parts[0] == "refs":
		return true
	case parts[0] == "worktrees" && len(parts) == 2:
		return true
	case parts[0] == "worktrees" && len(parts) == 3:
		return parts[2] == "reftable"
	}
	return false
}

// holdsRefs reports whether a change to the file or folder at rel, under the
// common git folder, can change the refs. git writes a ref to a .lock file
// first, and the rename of that file changes the ref.
func holdsRefs(rel string) bool {
	if strings.HasSuffix(rel, ".lock") {
		return false
	}
	parts := strings.Split(rel, "/")
	switch {
	case rel == "HEAD", rel == "packed-refs", rel == "reftable/tables.list", parts[0] == "refs":
		return true
	case parts[0] == "worktrees" && len(parts) <= 2:
		return true
	case parts[0] == "worktrees" && len(parts) == 3:
		return parts[2] == "HEAD"
	case parts[0] == "worktrees" && len(parts) == 4:
		return parts[2] == "reftable" && parts[3] == "tables.list"
	}
	return false
}
