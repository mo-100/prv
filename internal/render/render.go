// Package render is the single source of truth for the project table's
// column schema, the plain-text formatting of each cell, and the sortable
// field set. Both frontends — the pipe-friendly `prv ls` table
// (internal/report, which adds width-aware padding) and the lipgloss-styled
// TUI (internal/tui, which adds color) — build their headers and rows from
// here, and both sort through here. Adding a column or a sort field is one
// edit to this package; no frontend re-states the set.
//
// What does NOT live here: ANSI color, the TUI's searchable git-state word
// (internal/tui.gitStateWord), session-time "fresh/stale" decisions beyond
// the UpDown `fresh` flag, and the fixed-width table assembly
// (internal/report.RenderTable). Those are frontend-specific presentation.
package render

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mo-100/prv/internal/project"
)

// Column indices are the locked order shared by both frontends. Reordering or
// adding a column means editing these consts + Columns below, nowhere else.
const (
	ColName = iota
	ColTags
	ColGit
	ColBranch
	ColUpDown
	ColTODO
	ColLastActive
)

// Column describes one table column: its locked position and the header label
// each frontend uses (`prv ls` prints uppercase, the TUI prints a friendlier
// label / glyph for the ↑↓ column). The order of Columns IS the column order.
type Column struct {
	ID     int
	Report string // prv ls header (uppercase, pipe-friendly)
	TUI    string // TUI header (may use a glyph, e.g. ↑↓)
}

// Columns is the single source of truth for the table's column set and order.
// Both frontends project their header slice from it; Row emits cells in this
// order. Add a column here and both frontends pick it up.
var Columns = []Column{
	{ColName, "NAME", "Name"},
	{ColTags, "TAGS", "Tags"},
	{ColGit, "GIT", "Git"},
	{ColBranch, "BRANCH", "Branch"},
	{ColUpDown, "UPDOWN", "↑↓"},
	{ColTODO, "TODO", "TODO"},
	{ColLastActive, "LASTACTIVE", "LastActive"},
}

// StaleDays is the locked staleness threshold (in days): a project whose
// LastActive is older than this is "stale". It is the single home for the
// threshold — the TUI's `stale()` predicate reads it, and any future `prv ls`
// column / fetch-aware grace reads it too, so both frontends agree on one
// definition (AGENTS.md #6). Kept here (not in internal/tui) because it is a
// shared output-time fact, not a TUI-only presentation detail.
const StaleDays = 30

// ColGap is the inter-cell separator both frontends place between columns of
// a rendered row ("  ", two spaces). It is the single home for the gap so
// internal/report and internal/tui never drift on column spacing; change it
// once here and both frontends pick it up.
const ColGap = "  "

// ReportHeaders returns the prv ls header labels in column order.
func ReportHeaders() []string {
	out := make([]string, len(Columns))
	for i, c := range Columns {
		out[i] = c.Report
	}
	return out
}

// TUIHeaders returns the TUI header labels in column order.
func TUIHeaders() []string {
	out := make([]string, len(Columns))
	for i, c := range Columns {
		out[i] = c.TUI
	}
	return out
}

// --- plain-text cell formatters -------------------------------------------
//
// These turn one Project field into the plain string the user reads. Both
// frontends call them: report pipes them out verbatim, the TUI colors them
// via styleCell. No ANSI lives here.

// Name renders the project name with a trailing "!" error marker when the
// per-project scan failed (corrupt .git, permissions, etc.) — per spec
// line 130.
func Name(name string, err error) string {
	if err != nil {
		return name + "!"
	}
	return name
}

// Tags renders the catalog-ordered tag list comma-separated, or `—` when
// none were detected.
func Tags(tags []string) string {
	if len(tags) == 0 {
		return "—"
	}
	return strings.Join(tags, ",")
}

// Git renders the git-state glyph: `—` non-git, `!` git error,
// `●N` dirty (N = modified + untracked), `✓` clean.
func Git(p *project.Project) string {
	if !p.IsGit {
		return "—"
	}
	if p.Err != nil {
		return "!"
	}
	if p.Modified > 0 || p.Untracked > 0 {
		return "●" + strconv.Itoa(p.Modified+p.Untracked)
	}
	return "✓"
}

// Branch renders the branch name (or short OID when detached), or `—` for a
// non-git / branchless row.
func Branch(branch string) string {
	if branch == "" {
		return "—"
	}
	return branch
}

// UpDown formats the ahead/behind cell. Non-git, or either value == -1
// (no upstream), renders `—`. In sync (both zero) renders `✓`. Otherwise
// `↑N` and/or `↓N`, space-separated.
//
// fresh controls the local-vs-fresh cue: when the TUI has not fetched this
// session, ahead/behind come from local tracking refs and may be stale, so
// each numeric arrow gets a `?` suffix so stale data never reads as fresh.
// `prv ls` is synchronous (it fetches up front if --fetch, else renders
// local-only values that are as-fresh-as-asked) so it always passes fresh.
func UpDown(p *project.Project, fresh bool) string {
	if !p.IsGit || p.Ahead == -1 || p.Behind == -1 {
		return "—"
	}
	var parts []string
	suf := ""
	if !fresh {
		suf = "?"
	}
	if p.Ahead > 0 {
		parts = append(parts, "↑"+strconv.Itoa(p.Ahead)+suf)
	}
	if p.Behind > 0 {
		parts = append(parts, "↓"+strconv.Itoa(p.Behind)+suf)
	}
	if len(parts) == 0 {
		return "✓"
	}
	return strings.Join(parts, " ")
}

// TODO renders the open-checkbox count, or `—` when no TODO file is present.
func TODO(present bool, open int) string {
	if !present {
		return "—"
	}
	return strconv.Itoa(open)
}

// LastActive formats LastActive as a coarse relative time:
//
//	zero time → "—"
//	<1m → "just now"
//	<1h → "<m>m ago"
//	<1d → "<h>h ago"
//	else → "<d>d ago"
func LastActive(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d/time.Minute)) + "m ago"
	}
	if d < 24*time.Hour {
		return strconv.Itoa(int(d/time.Hour)) + "h ago"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d ago"
}

// Row builds the plain cell values for a single project in column order. Both
// frontends build their rows through this so the column set/order lives in
// one place. fresh is the UpDown local-vs-fresh flag (see UpDown).
func Row(p *project.Project, fresh bool) []string {
	return []string{
		Name(p.Name, p.Err),
		Tags(p.Tags),
		Git(p),
		Branch(p.Branch),
		UpDown(p, fresh),
		TODO(p.TODO, p.TODOOpen),
		LastActive(p.LastActive),
	}
}

// --- sort -----------------------------------------------------------------

// fieldDef is one sortable field: its --sort key and its comparator. The sort
// field set ($prv ls --sort` and the TUI `s` cycle) is this one list — Sort,
// ValidSort, and SortCycle all read from it, so adding a field is a single
// edit, never a sync across a switch + a slice + a validator + test literals.
type fieldDef struct {
	key  string
	less func(a, b *project.Project) bool
}

// sortable is the locked set + cycle order: name -> activity -> todo -> tags ->
// git. Directions are fixed per field, most-interesting-first: activity =
// newest first, todo = most open first, git = dirtiest first, name/tags =
// A→Z (tags compared element-wise in lexical order). An unrecognized key
// leaves the slice untouched (see Sort).
var sortable = []fieldDef{
	{"name", func(a, b *project.Project) bool { return a.Name < b.Name }},
	{"activity", func(a, b *project.Project) bool { return a.LastActive.After(b.LastActive) }},
	{"todo", func(a, b *project.Project) bool { return a.TODOOpen > b.TODOOpen }},
	{"tags", tagsLess},
	{"git", func(a, b *project.Project) bool { return dirtyCount(a) > dirtyCount(b) }},
}

// Sort reorders projects by the given field. An unrecognized field leaves the
// slice untouched. Both frontends sort through this so `prv ls` and the TUI
// never drift apart.
func Sort(projects []project.Project, by string) {
	for _, f := range sortable {
		if f.key == by {
			sort.SliceStable(projects, func(i, j int) bool {
				return f.less(&projects[i], &projects[j])
			})
			return
		}
	}
}

// ValidSort reports whether by is a known sort field.
func ValidSort(by string) bool {
	for _, f := range sortable {
		if f.key == by {
			return true
		}
	}
	return false
}

// SortCycle returns the sort keys in the locked cycle order used by the TUI
// `s` key. It is the same set accepted by Sort / --sort.
func SortCycle() []string {
	out := make([]string, len(sortable))
	for i, f := range sortable {
		out[i] = f.key
	}
	return out
}

// DefaultSort is the locked default sort field — the first entry of the sort
// cycle (sortable[0], "name"). It is the single home for the `--sort` default:
// main's flag default and the usage/README field list read from here /
// SortCycle, never a hand-typed "name". Changing the default is one edit to
// the sortable order.
func DefaultSort() string { return sortable[0].key }

// tagsLess reports whether a is ordered before b, compared element-wise in
// lexical (alphabetical) order. A shorter tag list sorts before a longer one
// when the shared prefix is equal (e.g. ["go"] < ["go","node"]).
func tagsLess(a, b *project.Project) bool {
	ta, tb := a.Tags, b.Tags
	n := len(ta)
	if len(tb) < n {
		n = len(tb)
	}
	for k := range n {
		if ta[k] != tb[k] {
			return ta[k] < tb[k]
		}
	}
	return len(ta) < len(tb)
}

// dirtyCount is the "dirtiness" metric that powers the git sort: tracked
// modified/unmerged plus untracked entries. Non-git projects yield 0.
func dirtyCount(p *project.Project) int {
	if !p.IsGit {
		return 0
	}
	return p.Modified + p.Untracked
}
