// Package report renders a plain, fixed-width, pipe-friendly table of projects
// for `prv ls`. It owns ONLY the table assembly: header row + cell rows padded
// to a display width (runewidth-aware so CJK and emoji names align), cells
// separated by two spaces, trailing newline, and no ANSI color. The column
// set, per-cell plain-text rules, and the sort field set all live in
// internal/render; this package builds the table on top of them.
//
// The TUI (internal/tui) draws from the same internal/render rules and adds
// lipgloss styling separately; the two frontends never re-state the column or
// sort sets, so `prv ls` and the TUI cannot drift apart.
package report

import (
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/mo-100/prv/internal/project"
	"github.com/mo-100/prv/internal/render"
)

// headers is the prv ls header row, in locked column order. It is a projection
// of render.Columns (the single source of truth for column set/order/labels),
// not an independent list.
var headers = render.ReportHeaders()

// RenderTable returns the plain fixed-width table, headers first then one row
// per project. Columns are padded to a display width (runewidth-aware so CJK
// and emoji names align); cells are separated by two spaces; the table ends
// with a trailing newline. No ANSI color. fresh is the up/down local-vs-
// fetched cue passed to render.Row: `prv ls --fetch` passes true (a fetch
// actually ran and succeeded); without --fetch it passes false so local-only
// ahead/behind render with the `?` stale suffix instead of reading as
// authoritative.
func RenderTable(projects []project.Project, fresh bool) string {
	rows := make([][]string, 0, len(projects)+1)
	rows = append(rows, headers)
	for i := range projects {
		rows = append(rows, render.Row(&projects[i], fresh))
	}

	widths := make([]int, len(headers))
	for _, r := range rows {
		for c, cell := range r {
			if w := runewidth.StringWidth(cell); w > widths[c] {
				widths[c] = w
			}
		}
	}

	var b strings.Builder
	for _, r := range rows {
		var line strings.Builder
		for c, cell := range r {
			if c > 0 {
				line.WriteString(render.ColGap)
			}
			line.WriteString(cell)
			line.WriteString(strings.Repeat(" ", widths[c]-runewidth.StringWidth(cell)))
		}
		b.WriteString(line.String())
		b.WriteByte('\n')
	}
	return b.String()
}
