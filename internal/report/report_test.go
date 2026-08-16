package report

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/mo-100/prv/internal/project"
	"github.com/mo-100/prv/internal/render"
)

// columnWidths mirrors RenderTable's width computation: the max runewidth of
// any cell in the column (header included). Used to peel columns back out of a
// rendered line by DISPLAY width so empty middle cells (e.g. UPDOWN when
// ahead==behind==0) and CJK names are handled correctly.
func columnWidths(rows [][]string) []int {
	widths := make([]int, len(headers))
	for _, r := range rows {
		for c, cell := range r {
			if w := runewidth.StringWidth(cell); w > widths[c] {
				widths[c] = w
			}
		}
	}
	return widths
}

// peelCells consumes widths[c] DISPLAY columns from the line for each column in
// turn, skipping the inter-column separator (render.ColGap) and trimming the
// trailing pad off each cell. This inverts RenderTable's width-based padding
// exactly, so empty cells and double-width runes survive comparison.
func peelCells(line string, widths []int) []string {
	cells := make([]string, 0, len(widths))
	rest := line
	for c := range widths {
		consumed, i := 0, 0
		for i < len(rest) {
			r, size := utf8.DecodeRuneInString(rest[i:])
			w := runewidth.RuneWidth(r)
			if consumed+w > widths[c] {
				break
			}
			consumed += w
			i += size
		}
		cells = append(cells, strings.TrimRight(rest[:i], " "))
		rest = rest[i:]
		if c < len(widths)-1 {
			rest = strings.TrimPrefix(rest, render.ColGap)
		}
	}
	return cells
}

// TestRenderTableRows exercises the assembled table end-to-end: the dirty git
// marker, clean git, non-git, the up/down combos (ahead-only, no-upstream),
// absent/present TODO, and zero/non-zero LastActive. Cell-level rendering is
// covered by internal/render; this test asserts column ORDER, assembly, and
// correct display-width padding (via peelCells).
func TestRenderTableRows(t *testing.T) {
	h3 := time.Now().Add(-3 * time.Hour)
	d21 := time.Now().Add(-21 * 24 * time.Hour)
	projects := []project.Project{
		{Name: "ross-v2", IsGit: true, Modified: 2, Branch: "main", Ahead: 2, Behind: 0, TODO: true, TODOOpen: 3, LastActive: h3},
		{Name: "mr-biggles", IsGit: true, Branch: "dev", Ahead: -1, Behind: -1, TODO: false, LastActive: d21}, // clean git, no upstream
		{Name: "scratchpad", TODO: false, LastActive: time.Time{}},                                            // non-git, zero activity
	}

	wantRows := make([][]string, len(projects))
	for i := range projects {
		wantRows[i] = render.Row(&projects[i], true)
	}
	wantRowsWithHeader := append([][]string{headers}, wantRows...)
	widths := columnWidths(wantRowsWithHeader)

	out := RenderTable(projects, true)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(wantRowsWithHeader) {
		t.Fatalf("got %d lines, want %d; out=%q", len(lines), len(wantRowsWithHeader), out)
	}
	for i, want := range wantRowsWithHeader {
		got := peelCells(lines[i], widths)
		if len(got) != len(want) {
			t.Errorf("line %d: got %d cells %v, want %d %v (line=%q)", i, len(got), got, len(want), want, lines[i])
			continue
		}
		for c := range want {
			if got[c] != want[c] {
				t.Errorf("line %d cell %d: got %q want %q (line=%q)", i, c, got[c], want[c], lines[i])
			}
		}
	}

	// Spot-check the acceptance contracts directly on the rendered lines so the
	// intent is legible without re-deriving widths.
	if !strings.Contains(lines[1], "●2") {
		t.Errorf("dirty git row should contain ●2 (Modified=2): %q", lines[1])
	}
	if !strings.Contains(lines[2], "✓") {
		t.Errorf("clean git row should contain ✓: %q", lines[2])
	}
	if !strings.HasPrefix(lines[1], "ross-v2") || !strings.Contains(lines[1], "main") ||
		!strings.Contains(lines[1], "↑2") || !strings.Contains(lines[1], " 3 ") || !strings.Contains(lines[1], "3h ago") {
		t.Errorf("ross-v2 row missing atoms: %q", lines[1])
	}
	if !strings.Contains(lines[3], "—") {
		t.Errorf("non-git scratchpad should contain —: %q", lines[3])
	}
}

// TestUpDownBoth asserts the space-separated both>0 form survives the table.
func TestUpDownBoth(t *testing.T) {
	out := RenderTable([]project.Project{
		{Name: "proto", IsGit: true, Branch: "feat/x", Ahead: 2, Behind: 1, LastActive: time.Now().Add(-5 * time.Hour)},
	}, true)
	if !strings.Contains(out, "↑2 ↓1") {
		t.Errorf("both>0 should render ↑2 ↓1 as one cell: %q", out)
	}
}

// TestCJKPadding asserts a CJK project name (double-width chars) is padded to
// the column's DISPLAY width, not its byte or rune length.
func TestCJKPadding(t *testing.T) {
	cjk := "项目"   // display width 4 (2*2)
	plain := "ab" // display width 2
	if w := runewidth.StringWidth(cjk); w != 4 {
		t.Fatalf("prerequisite: runewidth.StringWidth(%q)=%d, want 4", cjk, w)
	}

	projects := []project.Project{
		{Name: plain},
		{Name: cjk},
	}
	wantRows := [][]string{{"ab", "—", "—", "—", "—", "—", "—"}, {"项目", "—", "—", "—", "—", "—", "—"}}
	widths := columnWidths(append([][]string{headers}, wantRows...))
	if widths[0] != 4 {
		t.Fatalf("NAME column display width = %d, want 4 (max of %q width-4 and %q width-2)", widths[0], cjk, plain)
	}

	out := RenderTable(projects, true)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), out)
	}
	for i, want := range []string{plain, cjk} {
		got := peelCells(lines[i+1], widths)
		if got[0] != want {
			t.Errorf("row %d NAME cell: got %q want %q (line=%q)", i, got[0], want, lines[i+1])
		}
	}

	// The NAME field occupies widths[0]=4 DISPLAY columns on every row, even
	// for the 2-wide plain name — the only correct way that happens is width-
	// aware padding (a byte/rune-length padder would give "ab" a width-2 field
	// and misalign the CJK row). Consume widths[0] display columns raw and
	// measure them.
	takeCols := func(line string, w int) string {
		consumed, i := 0, 0
		for i < len(line) {
			r, size := utf8.DecodeRuneInString(line[i:])
			rw := runewidth.RuneWidth(r)
			if consumed+rw > w {
				break
			}
			consumed += rw
			i += size
		}
		return line[:i]
	}
	for _, line := range lines[1:] {
		nameField := takeCols(line, widths[0])
		if w := runewidth.StringWidth(nameField); w != 4 {
			t.Errorf("NAME field display width = %d, want 4 (not padded to byte/rune len): line=%q field=%q", w, line, nameField)
		}
	}
}

func TestRenderTableNoANSI(t *testing.T) {
	out := RenderTable([]project.Project{{Name: "x", IsGit: true, Modified: 1}}, true)
	if strings.ContainsAny(out, "\x1b") {
		t.Errorf("table contains ANSI escape bytes: %q", out)
	}
}

// TestRenderTableFreshFlag locks the ls stale-hedge contract: RenderTable
// with fresh=false (prv ls without --fetch, or a fetch that failed) appends
// the `?` stale suffix to local-only ahead/behind, while fresh=true (prv ls
// --fetch that succeeded) renders plain arrows. Same cue the TUI uses.
func TestRenderTableFreshFlag(t *testing.T) {
	p := []project.Project{{Name: "a", IsGit: true, Ahead: 2}}
	if stale := RenderTable(p, false); !strings.Contains(stale, "↑2?") {
		t.Errorf("fresh=false should hedge ↑2?: %q", stale)
	}
	if fresh := RenderTable(p, true); !strings.Contains(fresh, "↑2") || strings.Contains(fresh, "↑2?") {
		t.Errorf("fresh=true should render plain ↑2 (no ?): %q", fresh)
	}
}
