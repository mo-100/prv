// Package manifest detects ecosystem tags from project manifest files.
//
// It walks a directory (optionally recursively) and reports tags in a FIXED
// catalog order (deterministic), never filesystem order. The catalog is not a
// first-match cascade: every present manifest contributes a tag, and a
// manifest at one depth never suppresses one found at another.
package manifest

import (
	"os"
	"path/filepath"

	"github.com/mo-100/prv/internal/fsutil"
	"github.com/mo-100/prv/internal/project"
)

// project.Catalog (internal/project/project.go) is the single source of truth
// for the manifest→tag mapping and its order. Tags iterates it so a new
// ecosystem added to project.Catalog is picked up here with no second list to
// keep in sync.

// Tags returns the union of ecosystem tags detected in dir and, recursively,
// its subdirectories up to depth levels, in catalog order.
//
//   - depth=1 → dir only (its own manifests).
//   - depth=2 → dir plus each immediate subdirectory.
//   - depth>2 → descend further.
//
// A manifest found at any level contributes its tag; a manifest at depth 1
// never suppresses one at depth 2 (the union is taken across all levels).
// This makes the search uniform for `.prv`-marked and unmarked projects alike.
//
// Cheap - no subprocess, no file contents; entry-name checks only. The shared
// skip list (node_modules, .git, build, …) and hidden directories are pruned
// during recursion so dependency/vendored trees never inject false tags and
// the walk stays bounded. A read failure yields no tags from that branch.
func Tags(dir string, depth int) []string {
	if depth < 1 {
		depth = 1
	}
	fired := make([]bool, len(project.Catalog))
	collectTags(dir, depth, 1, fired)
	var tags []string
	for i, c := range project.Catalog {
		if fired[i] {
			tags = append(tags, c.Tag)
		}
	}
	return tags
}

// collectTags fires catalog slots for every manifest in dir, then recurses
// into each non-skipped subdirectory as long as level < depth. fired is
// shared across the whole walk so a tag fires once regardless of how many
// files or directories contribute it.
func collectTags(dir string, depth, level int, fired []bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		for i, c := range project.Catalog {
			if !fired[i] && c.Match(name) {
				fired[i] = true
			}
		}
	}
	if level >= depth {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if fsutil.IsHidden(name) || fsutil.IsSkipDir(name) {
			continue
		}
		// A symlinked subdir: e.IsDir() reports the link, not its target.
		// os.Stat to follow it; broken links and link-to-file just skip.
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !fi.IsDir() {
			continue
		}
		collectTags(filepath.Join(dir, name), depth, level+1, fired)
	}
}
