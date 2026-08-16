// Package todolist detects a project's TODO file (TODO, TODO.md, TODO.txt) and
// reports how many checkbox items are still open.
//
// [IsTODOName] is the single source of truth for which filenames count as a
// TODO file; project.isSignalName delegates to it so project detection and
// the TODO render never disagree on what a TODO file is (AGENTS.md #6: one
// fact, one home). Add an extension there, not anywhere else.
//
// The parser is isolated behind the [Source] interface so additional TODO
// origins can be plugged in later — AGENTS.md / CLAUDE.md checklist sections,
// `grep TODO` comments, or GitHub issues — without the scanner knowing about
// each origin. Today the only implementation is [fileSource], the TODO file
// parser; no other source is added yet.
package todolist

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mo-100/prv/internal/fsutil"
)

// Source reports whether a TODO source was found in dir and how many open
// (- [ ]) items it contains. New origins implement this; the scanner would
// hold a Source rather than calling the parser directly so origins can grow
// without touching call sites.
type Source interface {
	Scan(dir string, depth int) (present bool, open int, err error)
}

// Scan reports whether a TODO file was found within dir up to depth levels of
// directories (depth=1 → dir only; depth=2 → dir plus its immediate
// subdirectories; and so on), and how many open (- [ ]) items it has. The
// first match wins, deterministically: dir itself is checked first, then
// subdirectory names are sorted and searched depth-first. Filenames are
// matched case-insensitively via [IsTODOName] — the single source of truth
// shared with project detection; all accepted names use one parser. Hidden and
// skip-listed subdirectories (fsutil.IsHidden / fsutil.IsSkipDir — the same
// pruning tag detection uses) are never descended into, so a TODO.md inside
// node_modules/.vscode cannot inject a false signal. A TODO
// file with no checkbox lines still reports present=true, open=0. A read error is
// returned as err so the scanner can attach it to the row.
func Scan(dir string, depth int) (present bool, open int, err error) {
	if depth < 1 {
		depth = 1
	}
	return fileSource{}.Scan(dir, depth)
}

// fileSource is the TODO file parser: the single Source implementation for now.
type fileSource struct{}

func (fileSource) Scan(dir string, depth int) (present bool, open int, err error) {
	path, ok := findTODO(dir, depth)
	if !ok {
		return false, 0, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, 0, err
	}
	defer f.Close()
	open, err = parseTODO(f)
	if err != nil {
		return false, 0, err
	}
	return true, open, nil
}

// todoNames are the accepted file basenames, matched case-insensitively. This
// is the single source of truth for "what is a TODO file": project.isSignalName
// delegates to [IsTODOName] so project detection and the TODO render agree.
// Bare "todo" is included — a TODO file with no extension is a documented
// project signal (see README). Add an extension only here.
var todoNames = []string{"todo", "todo.md", "todo.txt"}

// IsTODOName reports whether name is a recognized TODO file basename, matched
// case-insensitively. It is the single authority for which filenames count as
// a TODO file; project.isSignalName asks it instead of keeping its own copy.
func IsTODOName(name string) bool {
	for _, t := range todoNames {
		if strings.EqualFold(name, t) {
			return true
		}
	}
	return false
}

// findTODO returns the path to the first TODO file found searching dir up to
// depth levels: dir itself first, then its subdirectories in sorted
// (deterministic) depth-first order, pruning hidden and skip-listed
// subdirectories (fsutil.IsHidden / fsutil.IsSkipDir) exactly like the
// manifest walk. A read failure of dir is treated as "no
// TODO here" (the scanner owns per-project error reporting). ok is false if
// none is found within the depth bound.
func findTODO(dir string, depth int) (path string, ok bool) {
	if p, ok := findTODOInDir(dir); ok {
		return p, true
	}
	if depth <= 1 {
		return "", false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var subdirs []string
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
		subdirs = append(subdirs, name)
	}
	sort.Strings(subdirs)
	for _, name := range subdirs {
		if p, ok := findTODO(filepath.Join(dir, name), depth-1); ok {
			return p, true
		}
	}
	return "", false
}

// findTODOInDir looks for a TODO file directly inside dir. If multiple
// accepted names are present the lexicographically smaller one wins, keeping
// selection deterministic regardless of filesystem ordering.
func findTODOInDir(dir string) (path string, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var candidates []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if IsTODOName(e.Name()) {
			candidates = append(candidates, e.Name())
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	sort.Strings(candidates)
	return filepath.Join(dir, candidates[0]), true
}

// parseTODO counts open checkbox items in a TODO file's contents. A line is
// open if, after optional leading whitespace, it begins with "- [ ]"; done
// lines ("- [x]" / "- [X]") and non-checkbox lines are ignored. A file with no
// checkbox lines yields open=0.
func parseTODO(r io.Reader) (int, error) {
	s := bufio.NewScanner(r)
	open := 0
	for s.Scan() {
		line := strings.TrimLeft(s.Text(), " \t")
		if strings.HasPrefix(line, "- [ ]") {
			open++
		}
	}
	if err := s.Err(); err != nil {
		return 0, err
	}
	return open, nil
}
