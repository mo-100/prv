// Package activity computes LastActive per the spec section "Activity":
//
//   - Git repos — no walk. LastActive = max(last commit time, newest mtime
//     among dirty/untracked paths from the porcelain State the git package
//     already collected).
//   - Non-git folders — bounded walk honouring fsutil.SkipDirs, depth cap ~10.
//     The walk never descends into symlinked directories.
package activity

import (
	"os"
	"path/filepath"
	"time"

	"github.com/mo-100/prv/internal/fsutil"
	"github.com/mo-100/prv/internal/git"
)

// walkDepthCap is the soft depth limit for the non-git mtime walk.
const walkDepthCap = 10

// ForGit computes LastActive for a git repo. It takes the porcelain State so
// the dirty paths are reused (no second git invocation beyond LastCommitTime).
func ForGit(dir string, st *git.State) time.Time {
	var latest time.Time
	if ct, ok, err := git.LastCommitTime(dir); err == nil && ok && !ct.IsZero() {
		latest = ct
	}
	if st == nil {
		return latest
	}
	for _, p := range st.DirtyPaths {
		fi, err := os.Stat(filepath.Join(dir, p))
		if err != nil {
			continue // path deleted or unreadable since porcelain — skip
		}
		if fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
	}
	return latest
}

// ForDir computes LastActive for a non-git folder: the newest non-skipped
// mtime under dir, via a bounded walk. Symlinked directories are not
// descended into (a symlink is treated as a leaf file, never recursed).
func ForDir(dir string) time.Time {
	var latest time.Time
	walk(dir, 1, &latest)
	return latest
}

func walk(dir string, depth int, latest *time.Time) {
	if depth > walkDepthCap {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if fsutil.IsSkipDir(name) {
			continue
		}
		if e.IsDir() {
			// Real directory (os.ReadDir reports symlinks as non-dirs); descend.
			walk(filepath.Join(dir, name), depth+1, latest)
			continue
		}
		// Symlink-to-dir: e.Info() is an Lstat (reports the link), we do not
		// descend. Stat the entry to honor its mtime; on a broken link, skip.
		fi, err := e.Info()
		if err != nil {
			fi, err = os.Stat(filepath.Join(dir, name))
			if err != nil {
				continue
			}
		}
		if fi.ModTime().After(*latest) {
			*latest = fi.ModTime()
		}
	}
}
