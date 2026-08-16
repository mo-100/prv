// Package scan classifies a directory tree into rows and fills each row's
// Project record, returning a slice both frontends render from.
//
// Two phases per docs/project.md §Scanner:
//  1. Classify (cheap, serial): os.ReadDir(root) → per child a signal check
//     (file-existence only). A signal-less child gets one deeper ReadDir to
//     test the container rule. Hard cap Config.Depth (default 4).
//  2. Full scan (concurrent): a bounded worker pool scans project rows
//     (git state, TODO parse, tags, activity). Raw rows need only the cheap
//     non-git activity walk.
//
// Errors are per-project: a failing scan sets Project.Err and the row still
// renders. An empty root or zero dirs is a valid state returning an empty slice.
package scan

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/mo-100/prv/internal/activity"
	"github.com/mo-100/prv/internal/fsutil"
	"github.com/mo-100/prv/internal/git"
	"github.com/mo-100/prv/internal/manifest"
	"github.com/mo-100/prv/internal/project"
	"github.com/mo-100/prv/internal/todolist"
)

// DefaultDepth is the locked default root-relative depth (4 = the sum of the
// former classification and search defaults, 2+2). One --depth flag budgets
// both classification (container expansion) and per-row manifest/TODO search.
// It is the single home for the --depth default: NewConfig, the zero-value
// normalizer, and main's flag default all read it (AGENTS.md #6 — one fact,
// one home).
const DefaultDepth = 4

// Config tunes the scanner. Build it with NewConfig to get the locked defaults;
// the zero value is valid too (it normalizes to the defaults).
//
//   - Depth: the single root-relative budget for both classification and
//     per-row manifest/TODO search. Containers expand up to Depth from the
//     root (1 = root children only, no containers expanded); a container at
//     the cap produces no row. Each project row at root-depth k searches its
//     own subtree for manifests and TODO files with budget max(1, Depth-k+1),
//     so the deepest contributing manifest/TODO sits at root-depth ≤ Depth.
//     A root-with-own-signal counts as k=1.
type Config struct {
	Depth int
}

// NewConfig returns the default Config: Depth from DefaultDepth (the single
// home for the --depth default). Both frontends build their scan config
// through here or set the zero value, which normalizes to the same default.
func NewConfig() Config {
	return Config{Depth: DefaultDepth}
}

func (c Config) normalized() int {
	if c.Depth < 1 {
		return DefaultDepth
	}
	return c.Depth
}

// Run scans root and returns one row per leaf / container-child, expanded per
// the container rule up to cfg.Depth. Each row records its root-depth (k: 1 =
// root child) so the full scan can derive the per-row search budget. If root
// itself carries project signals, it is scanned as a single project row
// (locked, counting as k=1).
func Run(root string, cfg Config) []project.Project {
	abs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return []project.Project{{Name: root, Path: root, Err: err}}
	}
	depth := cfg.normalized()

	// Locked: a root with its own signals is a single project row (k=1).
	if project.HasSignal(abs) {
		row := project.Project{
			Name:  filepath.Base(abs),
			Path:  abs,
			Kind:  project.KindProject,
			Depth: 1, // root-with-own-signal counts as root-depth 1
		}
		fullScan(&row, rowBudget(depth, row.Depth))
		return []project.Project{row}
	}

	children, err := dirEntries(abs)
	if err != nil {
		return []project.Project{{Name: filepath.Base(abs), Path: abs, Err: err}}
	}

	s := &scanner{
		depth: depth,
		seen:  make(map[string]bool), // resolved-path dedup for symlink aliases
	}
	s.collect(abs, "", 1, children, nil)

	// Phase 2: concurrent full scan over all rows (slots are independent).
	scanAll(s.rows, depth)
	return s.rows
}

// scanner accumulates rows while walking the tree, sharing the depth config and
// the resolved-path dedup set across the recursive container expansion.
type scanner struct {
	depth int
	seen  map[string]bool
	rows  []project.Project
}

// collect appends rows for the classified entries of one directory. childDepth
// is the root-depth at which these entries live (1 = root's children): project
// rows record it so the full scan can derive their search budget. A container
// at depth < s.depth is expanded by recursing; at the cap it produces no row.
// relPrefix is the path of dir relative to the scan root ("" for root's
// children), in forward-slash form; row names are built from it.
func (s *scanner) collect(dir, relPrefix string, childDepth int, entries []entry, err error) {
	if err != nil {
		name := relPrefix
		if name == "" {
			name = filepath.Base(dir)
		}
		s.rows = append(s.rows, project.Project{
			Name: filepath.ToSlash(name), Path: dir, Kind: project.KindRaw, Err: err,
		})
		return
	}
	for _, c := range entries {
		if c.skip {
			continue
		}
		if s.seen[c.resolved] {
			continue
		}
		s.seen[c.resolved] = true

		relName := filepath.ToSlash(c.rel)
		if relPrefix != "" {
			relName = relPrefix + "/" + relName
		}

		switch c.kind {
		case project.KindProject:
			s.rows = append(s.rows, project.Project{
				Name: relName, Path: c.abs, Kind: c.kind, Depth: childDepth,
			})
		case project.KindContainer:
			if childDepth < s.depth {
				// Expand: descend into the container; its children live one
				// level deeper and are classified (and possibly expanded) too.
				grandkids, gerr := dirEntries(c.abs)
				s.collect(c.abs, relName, childDepth+1, grandkids, gerr)
			}
			// At the depth cap the container produces no row — its signalled
			// children are too deep to surface, and an empty placeholder adds
			// no value.
		}
	}
}

// entry is a classified directory under the scan root.
type entry struct {
	abs      string // resolved absolute path
	rel      string // path relative to root, used as Name
	resolved string // EvalSymlinks-resolved path (dedup key)
	kind     project.Kind
	skip     bool // broken symlink, file target, or unreadable
}

// dirEntries lists directories under dir, classifying each per the spec rules.
func dirEntries(dir string) ([]entry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, e := range entries {
		// Follow symlinks uniformly (works for junctions too): os.Stat.
		abs := filepath.Join(dir, e.Name())
		fi, err := os.Stat(abs)
		if err != nil {
			// Broken symlink, unreadable, or link to a file: skip silently.
			out = append(out, entry{abs: abs, resolved: abs, skip: true})
			continue
		}
		if !fi.IsDir() {
			out = append(out, entry{abs: abs, resolved: abs, skip: true})
			continue
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			resolved = abs
		}

		rel, err := filepath.Rel(dir, abs)
		if err != nil {
			rel = e.Name()
		}

		name := e.Name()
		switch {
		// Hidden/skip wins over signals: a hidden or skip-listed dir is never a
		// row even if it carries a project signal (e.g. .git/.prv), and it is
		// never descended into.
		case fsutil.IsHidden(name) || fsutil.IsSkipDir(name):
			out = append(out, entry{abs: abs, rel: rel, resolved: resolved, kind: project.KindRaw})
		case project.HasSignal(abs):
			out = append(out, entry{abs: abs, rel: rel, resolved: resolved, kind: project.KindProject})
		default:
			// One deeper ReadDir to test the container rule; depth cap handled
			// by the caller (we only expand container children once).
			if hasSignalledChild(abs) {
				out = append(out, entry{abs: abs, rel: rel, resolved: resolved, kind: project.KindContainer})
			} else {
				out = append(out, entry{abs: abs, rel: rel, resolved: resolved, kind: project.KindRaw})
			}
		}
	}
	return out, nil
}

// hasSignalledChild reports whether any immediate directory child of dir has a
// project signal (file-existence only).
func hasSignalledChild(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		// Hidden and skip-listed children never surface, so they can't make a
		// parent a container (mirrors dirEntries precedence). A dir whose only
		// signalled child is hidden/skip (e.g. node_modules/.git) must not
		// become a container that expands to nothing.
		if fsutil.IsHidden(e.Name()) || fsutil.IsSkipDir(e.Name()) {
			continue
		}
		if !e.IsDir() {
			// A symlink-to-dir counts; os.Stat to confirm it resolves to a dir.
			if fi, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && fi.IsDir() {
				if project.HasSignal(filepath.Join(dir, e.Name())) {
					return true
				}
			}
			continue
		}
		if project.HasSignal(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

// rowBudget is a row's manifest/TODO search budget: max(1, depth-k+1) where k
// is the row's root-depth (project.Depth). A root child (k=1) searches to the
// full config depth; rows deeper in the tree get a proportionally smaller
// budget, so the deepest contributing manifest/TODO always sits at
// root-depth ≤ depth. Uniform search is intentionally relaxed — search scope
// depends on the row's depth (user-confirmed when the two depth flags merged).
func rowBudget(depth, k int) int {
	if b := depth - k + 1; b > 1 {
		return b
	}
	return 1
}

// scanAll runs the bounded full-scan pool over rows. Each goroutine fills its
// own pre-allocated slot, so no locking is needed on the slice. depth is the
// config's root-relative --depth; each row's search budget is derived from
// its recorded root-depth via rowBudget.
func scanAll(rows []project.Project, depth int) {
	conc := runtime.NumCPU() * 2
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(p *project.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fullScan(p, rowBudget(depth, p.Depth))
		}(&rows[i])
	}
	wg.Wait()
}

// fullScan fills a single Project row's git/todo/tags/activity fields. searchDepth
// is the row's own-subtree search budget for manifests (union) and TODO files
// (first match): 1 = the row's directory only, 2 = plus immediate subdirs, etc.
// The budget is derived per row from the merged config depth and the row's
// root-depth (see rowBudget), so search scope intentionally depends on row
// depth; `.prv`-marked and unmarked rows at the same depth search identically.
func fullScan(p *project.Project, searchDepth int) {
	if p.Err != nil {
		return // classification failed; keep the error, fill nothing
	}

	if git.HasGitEntry(p.Path) {
		p.IsGit = true
		if st, err := git.Scan(p.Path); err != nil {
			p.Err = err
		} else {
			p.Branch = st.Branch
			p.Modified = st.Modified
			p.Untracked = st.Untracked
			p.Ahead = st.Ahead
			p.Behind = st.Behind
			p.LastActive = activity.ForGit(p.Path, st)
		}
		// Activity may still be zero on a git error; leave it.
	}
	if p.LastActive.IsZero() && p.IsGit {
		// Fall back without dirty paths if Scan failed; keeps commit-time signal.
		p.LastActive = activity.ForGit(p.Path, nil)
	}

	// Tags are the union of every manifest found up to searchDepth, in catalog
	// order. A manifest at the root never suppresses one in a subdirectory, so
	// a `.prv`-marked monorepo root and an unmarked project both roll up child
	// manifests uniformly.
	p.Tags = manifest.Tags(p.Path, searchDepth)

	if present, open, err := todolist.Scan(p.Path, searchDepth); err != nil {
		if p.Err == nil {
			p.Err = err
		}
		p.TODO = present
		p.TODOOpen = open
	} else {
		p.TODO = present
		p.TODOOpen = open
	}

	if !p.IsGit {
		p.LastActive = activity.ForDir(p.Path)
	}
}

// fetchPool caps concurrent fetches (spec: ~4-8), separate from the scan pool.
const fetchPool = 6

// FetchRows runs `git fetch` on every git row concurrently (own bounded pool),
// then re-derives each row's state from the now-fresh tracking refs. Per-repo
// network failures are swallowed: callers keep last-known values (graceful
// degradation per spec). Non-git rows are untouched. cfg supplies the
// root-relative --depth; each row's search budget is derived from its recorded
// root-depth via rowBudget.
func FetchRows(rows []project.Project, cfg Config) error {
	depth := cfg.normalized()
	sem := make(chan struct{}, fetchPool)
	var wg sync.WaitGroup
	for i := range rows {
		if !rows[i].IsGit {
			continue
		}
		wg.Add(1)
		go func(p *project.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_ = git.Fetch(p.Path)                  // graceful: ignore network errors
			fullScan(p, rowBudget(depth, p.Depth)) // re-derive ahead/behind from fresh refs
		}(&rows[i])
	}
	wg.Wait()
	return nil
}
