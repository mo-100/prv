package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mo-100/prv/internal/project"
	"github.com/mo-100/prv/internal/render"
	"github.com/mo-100/prv/internal/scan"
)

// asModel downcasts the tea.Model returned by New to the concrete *model so
// the test can poke unexported state (the documented test hook).
func asModel(t *testing.T, m tea.Model) *model {
	t.Helper()
	mm, ok := m.(*model)
	if !ok {
		t.Fatalf("New returned %T, want *model", m)
	}
	return mm
}

// TestEmptyStateAndView asserts a freshly constructed model renders without
// panicking on an empty row set and shows the centered "no projects" message.
func TestEmptyStateAndView(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init() // set up commands; we ignore them — no loadedMsg processed.

	view := m.View()
	if view == "" {
		t.Fatal("View() returned empty string for empty rows")
	}
	if !strings.Contains(view, "no projects") {
		t.Errorf("empty state missing \"no projects\" message; got:\n%s", view)
	}
	// The header row now renders even with zero projects so the empty state
	// reads as "the projects table, currently empty" (not a bare placeholder).
	for _, h := range headers {
		if !strings.Contains(view, h) {
			t.Errorf("header %q missing from empty-state view:\n%s", h, view)
		}
	}
	// footer must always be present so the user knows how to quit.
	if !strings.Contains(view, "q") {
		t.Errorf("footer key hints missing; got:\n%s", view)
	}
}

// TestViewPopulated asserts View renders the header row and one row per
// project on a hand-built slice, exercising every per-cell rendering rule.
func TestViewPopulated(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init()

	now := time.Now()
	stale := now.Add(-60 * 24 * time.Hour) // > 30d
	m.rows = []project.Project{
		{Name: "my-app", IsGit: true, Branch: "main", Ahead: 2, Tags: []string{"node"}, TODO: true, TODOOpen: 3, LastActive: now},
		{Name: "legacy-svc", IsGit: true, Branch: "dev", Modified: 1, Tags: []string{"python"}, LastActive: now.Add(-2 * 24 * time.Hour)},
		{Name: "draft-idea", Tags: nil, LastActive: time.Time{}}, // non-git
		{Name: "proto", IsGit: true, Branch: "feat/x", Behind: 1, Tags: []string{"go"}, LastActive: stale},
	}
	m.apply()

	view := m.View()
	for _, h := range headers {
		if !strings.Contains(view, h) {
			t.Errorf("header %q missing from view:\n%s", h, view)
		}
	}
	for _, want := range []string{"my-app", "legacy-svc", "draft-idea", "proto"} {
		if !strings.Contains(view, want) {
			t.Errorf("row %q missing from view:\n%s", want, view)
		}
	}
	// local-only up/down cue: no fetch this session → ↑2 should read ↑2?.
	if !strings.Contains(view, "↑2?") {
		t.Errorf("local-only cue missing; want ↑2? in view:\n%s", view)
	}
	// git glyphs: dirty renders ●<count>, clean renders ✓, non-git —.
	if !strings.Contains(view, "●1") {
		t.Errorf("dirty glyph ●1 missing (legacy-svc Modified=1):\n%s", view)
	}
	if !strings.Contains(view, "✓") {
		t.Errorf("clean glyph ✓ missing:\n%s", view)
	}
}

// TestStatusLineFreshness switches the fetched flag and asserts the cue reverts
// from stale "?" to a plain arrow.
func TestStatusLineFreshness(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init()
	now := time.Now()
	m.rows = []project.Project{
		{Name: "my-app", IsGit: true, Branch: "main", Ahead: 2, Tags: []string{"node"}, LastActive: now},
	}
	m.apply()

	if v := m.View(); !strings.Contains(v, "↑2?") {
		t.Errorf("local-only cue missing before fetch; got:\n%s", v)
	}
	m.fetchedThisSession = true
	if v := m.View(); !strings.Contains(v, "↑2 ") {
		t.Errorf("fresh cue missing after fetch; got:\n%s", v)
	}
}

// TestHeaderDistinctFromStatus is a regression guard: renderTitle() returns
// "titleLine\nstatusLine" with NO trailing newline, so View() must insert a
// delimiter before the table body — otherwise the header row concatenates onto
// the status line and the header vanishes (pushed off the right edge) whenever
// m.status is non-empty, which is always in the real app after a scan.
func TestHeaderDistinctFromStatus(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 28})
	m.status = "23 projects"
	now := time.Now()
	m.rows = make([]project.Project, 23)
	for i := range m.rows {
		m.rows[i] = project.Project{Name: fmt.Sprintf("p-%02d", i), IsGit: true, Branch: "main", Tags: []string{"py"}, LastActive: now}
	}
	m.apply()

	lines := strings.Split(m.View(), "\n")
	status, header := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "23 projects") && !strings.Contains(l, "Name") {
			status = i
		}
		if strings.Contains(l, "Name") && strings.Contains(l, "Tags") {
			header = i
		}
	}
	if status == -1 {
		t.Fatalf("status line not isolated; lines:\n%s", strings.Join(lines, "\n"))
	}
	if header == -1 {
		t.Fatalf("header line not isolated; lines:\n%s", strings.Join(lines, "\n"))
	}
	if status == header {
		t.Fatalf("status and header collided on line %d: %q", status, lines[status])
	}
	if header != status+1 {
		t.Errorf("header must be the line right after status: status=%d header=%d", status, header)
	}
}

// TestSortCycle drives the `s` key through the locked cycle and verifies the
// order wraps back to name.
func TestSortCycle(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init()
	want := []string{"name", "activity", "todo", "tags", "git", "name"}
	for i, w := range want {
		if got := sortCycle[m.sortIdx]; got != w {
			t.Fatalf("step %d: sort=%q want %q", i, got, w)
		}
		mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		m = asModel(t, mm)
	}
}

// TestFilterAppliesAndCancels asserts `/` enters filter mode, typing narrows
// rows, Enter keeps the filter, and Esc clears it.
func TestFilterAppliesAndCancels(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init()
	now := time.Now()
	m.rows = []project.Project{
		{Name: "alpha", Tags: []string{"go"}, IsGit: true, LastActive: now},
		{Name: "beta", Tags: []string{"node"}, LastActive: now},
	}
	m.apply()

	// enter filter mode and type "go"
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !m.filtering {
		t.Fatal("`/` did not enter filter mode")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g', 'o'}})
	if m.filter != "go" {
		t.Fatalf("filter=%q want %q", m.filter, "go")
	}
	// directive + apply: by tags, "go" matches "alpha" only after Enter.
	// apply() runs each keystroke; Enter simply exits typing mode.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.filtering {
		t.Fatal("Enter did not exit filter mode")
	}
	if got, want := len(m.view), 1; got != want {
		t.Fatalf("after filter view len=%d want %d (rows=%v)", got, want, m.view)
	}
	if m.rows[m.view[0]].Name != "alpha" {
		t.Fatalf("filter kept wrong row: %+v", m.rows[m.view[0]])
	}

	// Esc in normal mode clears the filter.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.filter != "" {
		t.Fatalf("Esc did not clear filter: %q", m.filter)
	}
	if len(m.view) != 2 {
		t.Fatalf("after clear view len=%d want 2", len(m.view))
	}
}

// TestFilterCancelEsc asserts `/`, type, then Esc cancels and clears rows-wide.
func TestFilterCancelEsc(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init()
	now := time.Now()
	m.rows = []project.Project{
		{Name: "alpha"}, {Name: "alpine"},
		{Name: "beta", LastActive: now},
	}
	m.apply()

	// enter filter, type "al"
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'l'}})
	if got, want := len(m.view), 2; got != want { // alpha + alpine
		t.Fatalf("filter \"al\" view len=%d want %d", got, want)
	}
	// Esc inside filter mode cancels.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.filtering {
		t.Fatal("Esc did not leave filter mode")
	}
	if m.filter != "" {
		t.Fatalf("Esc did not reset filter: %q", m.filter)
	}
	if len(m.view) != 3 {
		t.Fatalf("after cancel view len=%d want 3", len(m.view))
	}
}

// TestNavigationClamps asserts j/k move and clamp at the view edges.
func TestNavigationClamps(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init()
	now := time.Now()
	m.rows = []project.Project{
		{Name: "a", LastActive: now}, {Name: "b", LastActive: now}, {Name: "c", LastActive: now},
	}
	m.apply()
	if m.selected != 0 {
		t.Fatalf("initial selected=%d want 0", m.selected)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.selected != 1 {
		t.Fatalf("after j selected=%d want 1", m.selected)
	}
	// move past the bottom edge clamps at last row.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.selected != 2 {
		t.Fatalf("after 3×j selected=%d want 2", m.selected)
	}
	// up via arrow.
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.selected != 1 {
		t.Fatalf("after ↑ selected=%d want 1", m.selected)
	}
	// clamping at top.
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.selected != 0 {
		t.Fatalf("after clamped up selected=%d want 0", m.selected)
	}
}

// extractMsgs runs a (possibly batched) Cmd and returns all leaf messages it
// produces. tea.Batch returns a BatchMsg whose sub-commands are run here;
// tea.Tick blocks until its timer fires, so durations stay tiny in tests.
func extractMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, extractMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// TestFetchNilHook asserts `f` with nil hook shows "no fetch configured" and
// does not block.
func TestFetchNilHook(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init()
	if m.fetchHook != nil {
		t.Fatal("hook should be nil")
	}
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = asModel(t, mm)
	if cmd != nil {
		t.Errorf("nil hook must not schedule any cmd; got %v", cmd)
	}
	if !strings.Contains(m.status, "no fetch configured") {
		t.Errorf("status=%q want \"no fetch configured\"", m.status)
	}
}

// TestFetchWithHook asserts `f` invokes fetchHook with the current rows, sets
// the fetching flag, schedules a batched command, and that fetchDoneMsg flips
// fetched + re-scans.
func TestFetchWithHook(t *testing.T) {
	now := time.Now()
	seed := []project.Project{{Name: "x", IsGit: true, Ahead: 1, LastActive: now}}
	hookCalled := false
	hook := func(got []project.Project) error {
		hookCalled = true
		if len(got) != 1 || got[0].Name != "x" {
			t.Errorf("hook got unexpected rows: %+v", got)
		}
		return nil
	}
	// empty real root: the post-fetch re-scan yields 0 rows deterministically.
	m := asModel(t, New(t.TempDir(), scan.NewConfig(), 0, hook))
	m.rows = seed
	m.apply()

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = asModel(t, mm)
	if !m.fetching {
		t.Fatal("fetching flag not set")
	}
	if cmd == nil {
		t.Fatal("fetch did not schedule a command")
	}

	var done fetchDoneMsg
	for _, msg := range extractMsgs(cmd) {
		if d, ok := msg.(fetchDoneMsg); ok {
			done = d
		}
	}
	if done.err != nil {
		t.Fatalf("hook returned err: %v", done.err)
	}
	if !hookCalled {
		t.Fatal("hook was not invoked")
	}

	mm, _ = m.Update(done)
	m = asModel(t, mm)
	if !m.fetchedThisSession {
		t.Fatal("fetched flag not set after successful hook")
	}
	if m.fetching {
		t.Fatal("fetching flag still set after completion")
	}
	if len(m.rows) != 0 {
		t.Fatalf("post-fetch re-scan len=%d want 0 (empty root)", len(m.rows))
	}
}

// TestFetchFailedHook asserts a hook error leaves fetched=false and surfaces a
// failing status, so stale local values are never misread as fresh.
func TestFetchFailedHook(t *testing.T) {
	hook := func([]project.Project) error { return errBoom }
	m := asModel(t, New(t.TempDir(), scan.NewConfig(), 0, hook))
	m.rows = []project.Project{{Name: "x", IsGit: true, Ahead: 1, LastActive: time.Now()}}
	m.apply()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	var done fetchDoneMsg
	for _, msg := range extractMsgs(cmd) {
		if d, ok := msg.(fetchDoneMsg); ok {
			done = d
		}
	}
	if done.err == nil {
		t.Fatal("expected hook error, got nil")
	}
	mm, _ := m.Update(done)
	m = asModel(t, mm)
	if m.fetchedThisSession {
		t.Fatal("fetched must stay false on hook error")
	}
	if !strings.Contains(m.status, "fetch failed") {
		t.Errorf("status=%q want \"fetch failed\"", m.status)
	}
	// local-only cue must remain → ↑N? form.
	if v := m.View(); !strings.Contains(v, "↑1?") {
		t.Errorf("local-only cue missing after failed fetch; got:\n%s", v)
	}
}

// ansiRe strips SGR escape sequences so width assertions can read the plain
// text a user sees instead of the styled bytes lipgloss emits.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// TestRenderStableWidthsOnScroll is a regression test for the "big project
// name disappears, janky ui" bug: renderTable used to size column widths from
// the visible window only, so the column carrying the longest cell shrank
// when that row scrolled out of view, making every other row jitter. The fix
// sizes widths from the full filtered view (m.view), so the Name column (and
// every column) keeps a stable padded width regardless of scroll position.
func TestRenderStableWidthsOnScroll(t *testing.T) {
	m := asModel(t, New(".", scan.NewConfig(), 0, nil))
	m.Init()

	long := "this-is-a-very-long-project-name-zzz" // far wider than the short rows
	now := time.Now()
	m.rows = []project.Project{
		{Name: "a", LastActive: now},
		{Name: "b", LastActive: now},
		{Name: "c", LastActive: now},
		{Name: "d", LastActive: now},
		{Name: long, LastActive: now},
	}
	m.apply() // default sortIdx=0 → sort by name → view = [a, b, c, d, long]

	// Small window: avail = height - titleHeight(2) - 1 - footerHeight(1).
	// height=6 → only 2 rows fit, forcing a scroll that can exclude the long row.
	m.width = defaultWidth
	m.height = 6

	// nameHeaderWidth renders the view and returns the padded width of the Name
	// column, read from the header row's plain text: "Name" + pad + ColGap +
	// "Tags" + …, so width = index("Tags") - len(ColGap).
	nameHeaderWidth := func() int {
		out := m.View()
		for _, line := range strings.Split(out, "\n") {
			plain := stripANSI(line)
			if !strings.HasPrefix(plain, headers[0]) {
				continue // skip status/title/data rows (data rows carry the " " prefix)
			}
			idx := strings.Index(plain, headers[1])
			if idx < 0 {
				t.Fatalf("Tags header missing from header row: %q", plain)
			}
			return idx - len(render.ColGap)
		}
		t.Fatalf("header row not found in view:\n%s", out)
		return 0
	}

	// Case 1: long row scrolled OUT of the visible window. Selection at the top
	// pins the window to view indices [a, b]; the long row (index 4) is off-screen.
	m.selected = 0
	m.scroll = 0
	widthLongOffScreen := nameHeaderWidth()

	// Case 2: long row IN the visible window. Selection at the bottom slides the
	// window to view indices [d, long].
	m.selected = 4
	m.scroll = 3
	widthLongOnScreen := nameHeaderWidth()

	// Widths must be identical across scroll positions (the core anti-jitter fix).
	if widthLongOffScreen != widthLongOnScreen {
		t.Fatalf("Name column width jittered on scroll: off-screen=%d on-screen=%d",
			widthLongOffScreen, widthLongOnScreen)
	}
	// The width must be driven by the long row even when that row is scrolled
	// off-screen — i.e. not the header-only width (len("Name")==4).
	if widthLongOffScreen != len(long) {
		t.Errorf("Name width when long row off-screen=%d, want %d (len of long name); "+
			"widths not computed over the full filtered view", widthLongOffScreen, len(long))
	}
}

var errBoom = &boomErr{}

type boomErr struct{}

func (boomErr) Error() string { return "boom" }
