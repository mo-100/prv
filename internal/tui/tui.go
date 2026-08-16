// Package tui is the Bubble Tea + lipgloss full-screen frontend for prv.
//
// It owns only the interactive view: classification, git state, tags,
// todolist and activity are all produced by the engine packages; this package
// renders them and drives keyboard interaction. Sorting reuses render.Sort so
// `prv ls` and the TUI never drift apart; the column set, per-cell plain-text
// rules, and sort field set all live in internal/render — this package only
// adds lipgloss styling.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mo-100/prv/internal/project"
	"github.com/mo-100/prv/internal/render"
	"github.com/mo-100/prv/internal/scan"
)

// New returns a Bubble Tea model viewing root. cfg tunes the scanner (the
// single root-relative --depth budget for both classification and per-project
// manifest/TODO search). When autoRefresh is non-zero the model re-scans on
// that cadence; zero disables it (locked default). fetchHook, if non-nil, is
// invoked by the `f` key with the rows currently in the model so the
// implementation can fetch tracking refs across repos without a redundant
// re-scan; a nil hook makes `f` print "no fetch configured" instead of
// blocking. The caller runs the returned model, e.g.
// tea.NewProgram(tui.New(root, cfg, auto, hook), tea.WithAltScreen()).Run()
func New(root string, cfg scan.Config, autoRefresh time.Duration, fetchHook func(rows []project.Project) error) tea.Model {
	return &model{
		root:        root,
		cfg:         cfg,
		autoRefresh: autoRefresh,
		fetchHook:   fetchHook,
		width:       0,
		height:      0,
	}
}

// sortCycle is the locked cycle order for the `s` key, derived from
// render.SortCycle (the single source of truth for the sort field set) so
// `ls --sort` and the TUI `s` cycle never disagree.
var sortCycle = render.SortCycle()

// spinner frames shown beside the status line while a fetch is running.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// The window the caller renders into defaults to these dimensions until the
// first WindowSizeMsg arrives (and in headless tests that never happens).
const (
	defaultWidth  = 80
	defaultHeight = 24
)

// The staleness threshold (render.StaleDays) and the inter-cell gap
// (render.ColGap) live in internal/render — the single homes shared with
// `prv ls` — so this package reaches them there instead of restating them.

// --- messages --------------------------------------------------------------

type refreshDoneMsg struct{ rows []project.Project }
type fetchDoneMsg struct{ err error }
type tickMsg time.Time
type animMsg struct{}

func refreshCmd(root string, cfg scan.Config) tea.Cmd {
	return func() tea.Msg {
		return refreshDoneMsg{rows: scan.Run(root, cfg)}
	}
}

func scheduleTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func animTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return animMsg{} })
}

// --- model -----------------------------------------------------------------

type model struct {
	root               string
	cfg                scan.Config
	rows               []project.Project // full set, sorted in place by render.Sort
	view               []int             // indices into rows passing the active filter
	sortIdx            int               // cursor into sortCycle
	filter             string
	filtering          bool // true while the `/` filter input is capturing keys
	selected           int  // index into view
	scroll             int  // first visible row index into view
	autoRefresh        time.Duration
	fetchHook          func(rows []project.Project) error
	fetching           bool
	fetchedThisSession bool // true once a fetch completed with nil error this session
	spinner            int
	status             string
	showHelp           bool
	width              int
	height             int
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{refreshCmd(m.root, m.cfg)}
	if m.autoRefresh > 0 {
		cmds = append(cmds, scheduleTick(m.autoRefresh))
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case refreshDoneMsg:
		m.rows = msg.rows
		m.apply()
		if len(m.rows) == 0 {
			m.status = "no projects"
		} else {
			m.status = fmt.Sprintf("%d projects", len(m.rows))
		}
		return m, nil

	case tickMsg:
		// cadence tick: re-scan and reschedule.
		return m, tea.Batch(refreshCmd(m.root, m.cfg), scheduleTick(m.autoRefresh))

	case animMsg:
		if m.fetching {
			m.spinner = (m.spinner + 1) % len(spinnerFrames)
			return m, animTick()
		}
		return m, nil

	case fetchDoneMsg:
		m.fetching = false
		if msg.err == nil {
			m.fetchedThisSession = true
			m.rows = scan.Run(m.root, m.cfg)
			m.apply()
			m.status = "fetched ✓"
		} else {
			// A failed fetch leaves local-only values untouched; never claim
			// freshness. The stale cue stays in place.
			m.status = "fetch failed: " + msg.err.Error()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Help overlay swallows everything except Esc (dismiss) and quit.
	if m.showHelp {
		switch k.Type {
		case tea.KeyEsc:
			m.showHelp = false
			return m, nil
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyRunes:
			if s := string(k.Runes); s == "q" {
				return m, tea.Quit
			}
		}
		return m, nil
	}

	// Filter input captures printable keys, backspace, enter and escape.
	if m.filtering {
		switch k.Type {
		case tea.KeyEnter:
			m.filtering = false
			m.apply()
			return m, nil
		case tea.KeyEsc:
			m.filtering = false
			m.filter = ""
			m.apply()
			return m, nil
		case tea.KeyBackspace:
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.apply()
			}
			return m, nil
		case tea.KeyRunes:
			m.filter += string(k.Runes)
			m.apply()
			return m, nil
		}
		return m, nil
	}

	switch k.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyUp:
		m.move(-1)
		return m, nil
	case tea.KeyDown:
		m.move(1)
		return m, nil
	case tea.KeyEsc:
		// In normal mode Esc just clears any applied filter.
		if m.filter != "" {
			m.filter = ""
			m.apply()
		}
		return m, nil
	case tea.KeyRunes:
		switch string(k.Runes) {
		case "q":
			return m, tea.Quit
		case "j":
			m.move(1)
		case "k":
			m.move(-1)
		case "r":
			m.status = "refreshing…"
			return m, refreshCmd(m.root, m.cfg)
		case "s":
			m.sortIdx = (m.sortIdx + 1) % len(sortCycle)
			m.apply()
			m.status = "sort: " + sortCycle[m.sortIdx]
		case "f":
			return m.startFetch()
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "/":
			m.filtering = true
			m.filter = ""
			return m, nil
		}
	}
	return m, nil
}

func (m *model) startFetch() (tea.Model, tea.Cmd) {
	if m.fetching {
		return m, nil
	}
	if m.fetchHook == nil {
		m.status = "no fetch configured"
		return m, nil
	}
	hook := m.fetchHook
	// Snapshot the rows so a concurrent refresh cannot race the reader while
	// the hook inspects them; the hook is read-only by contract.
	snap := make([]project.Project, len(m.rows))
	copy(snap, m.rows)
	m.fetching = true
	m.status = "fetching…"
	return m, tea.Batch(
		func() tea.Msg { return fetchDoneMsg{err: hook(snap)} },
		animTick(),
	)
}

// move adjusts the selection within the filtered view, clamping at the edges.
func (m *model) move(delta int) {
	n := len(m.view)
	if n == 0 {
		m.selected = 0
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected > n-1 {
		m.selected = n - 1
	}
}

// apply re-sorts rows and rebuilds the filtered view, then clamps selection.
func (m *model) apply() {
	render.Sort(m.rows, sortCycle[m.sortIdx])
	m.view = m.view[:0]
	needle := strings.ToLower(m.filter)
	for i := range m.rows {
		if m.filter == "" || matchFilter(&m.rows[i], needle) {
			m.view = append(m.view, i)
		}
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if len(m.view) > 0 && m.selected > len(m.view)-1 {
		m.selected = len(m.view) - 1
	}
	if len(m.view) == 0 {
		m.selected = 0
	}
}

// matchFilter reports a substring hit across Name + Tags (joined) + the git
// state word. needle must already be lower-cased.
func matchFilter(p *project.Project, needle string) bool {
	var b strings.Builder
	b.WriteString(strings.ToLower(p.Name))
	b.WriteByte(' ')
	if len(p.Tags) > 0 {
		b.WriteString(strings.ToLower(strings.Join(p.Tags, ",")))
	}
	b.WriteByte(' ')
	b.WriteString(gitStateWord(p))
	return strings.Contains(b.String(), needle)
}

// --- TUI-only presentation ------------------------------------------------
//
// gitStateWord is the searchable git-state term (distinct from the rendered
// "●N"/"✓" glyph in internal/render.Git, so filtering matches words rather
// than symbols). The plain cell text, column set/order, and sort field set
// all come from internal/render; this package only adds lipgloss color and
// the session-time stale/fresh cue.
func gitStateWord(p *project.Project) string {
	if !p.IsGit {
		return "—"
	}
	if p.Modified > 0 || p.Untracked > 0 {
		return "dirty"
	}
	return "clean"
}

// --- palette & styles ------------------------------------------------------

var (
	colorAccent = lipgloss.Color("#7aa2f7") // selection / header / branch accent
	colorDirty  = lipgloss.Color("#ff7a93") // dirty / error accent
	colorClean  = lipgloss.Color("#9ece6a") // clean repos
	colorMuted  = lipgloss.Color("#6c7086") // non-git / clean-text / stale
	colorNormal = lipgloss.Color("#c0caf5") // default name text
	colorBar    = lipgloss.Color("#1f2335") // selection background
)

// headers is the TUI header row, derived from render.Columns (the single
// source of truth for column set/order/labels), not an independent list.
var headers = render.TUIHeaders()

// styleCell colors a single plane cell value per the spec cues.
func styleCell(col int, val string, p *project.Project) string {
	var style lipgloss.Style
	switch col {
	case render.ColName: // Name
		style = lipgloss.NewStyle().Foreground(colorNormal)
		if p.Err != nil {
			style = lipgloss.NewStyle().Foreground(colorDirty)
		}
	case render.ColTags: // Tags
		style = lipgloss.NewStyle().Foreground(colorAccent)
		if val == "—" {
			style = lipgloss.NewStyle().Foreground(colorMuted)
		}
	case render.ColGit: // Git
		switch {
		case val == "!" || strings.HasPrefix(val, "●"):
			style = lipgloss.NewStyle().Foreground(colorDirty)
		case val == "✓":
			style = lipgloss.NewStyle().Foreground(colorClean)
		default: // "—"
			style = lipgloss.NewStyle().Foreground(colorMuted)
		}
	case render.ColBranch: // Branch
		if p.IsGit {
			style = lipgloss.NewStyle().Foreground(colorAccent)
		} else {
			style = lipgloss.NewStyle().Foreground(colorMuted)
		}
	case render.ColUpDown: // ↑↓
		switch {
		case val == "—":
			style = lipgloss.NewStyle().Foreground(colorMuted)
		case val == "✓":
			style = lipgloss.NewStyle().Foreground(colorClean)
		default: // ↑N / ↓N
			style = lipgloss.NewStyle().Foreground(colorAccent)
		}
	case render.ColTODO: // TODO
		if p.TODO && p.TODOOpen > 0 {
			style = lipgloss.NewStyle().Foreground(colorNormal)
		} else {
			style = lipgloss.NewStyle().Foreground(colorMuted)
		}
	case render.ColLastActive: // LastActive
		style = lipgloss.NewStyle().Foreground(colorNormal)
		if val == "—" {
			style = lipgloss.NewStyle().Foreground(colorMuted)
		}
	}
	return style.Render(val)
}

func stale(p *project.Project) bool {
	if p.LastActive.IsZero() {
		return false
	}
	return time.Since(p.LastActive) > render.StaleDays*24*time.Hour
}

// --- view ------------------------------------------------------------------

func (m *model) View() string {
	if m.width <= 0 {
		m.width = defaultWidth
	}
	if m.height <= 0 {
		m.height = defaultHeight
	}

	if m.showHelp {
		return m.renderHelp()
	}

	title := m.renderTitle()

	avail := m.height - lipgloss.Height(title) - 1 - footerHeight(m)
	if avail < 1 {
		avail = 1
	}

	var b strings.Builder
	b.WriteString(title)
	b.WriteByte('\n')

	if len(m.view) == 0 {
		// Always show the header row so the empty state reads as "the projects
		// table, currently empty" rather than a bare placeholder. Widths come
		// from the header labels only (no rows to contribute), and the centered
		// "no projects" fills the remaining space below it.
		widths := make([]int, len(headers))
		for c, h := range headers {
			widths[c] = lipgloss.Width(h)
		}
		b.WriteString(renderHeaderRow(widths))
		b.WriteByte('\n')
		empty := lipgloss.NewStyle().Foreground(colorMuted).
			Render("no projects")
		emptyArea := m.height - lipgloss.Height(title) - 1 - footerHeight(m)
		if emptyArea < 1 {
			emptyArea = 1
		}
		b.WriteString(lipgloss.Place(m.width, emptyArea, lipgloss.Center, lipgloss.Center, empty))
		b.WriteString(m.renderFooter())
		return b.String()
	}

	body := m.renderTable(avail)
	b.WriteString(body)
	b.WriteString(m.renderFooter())
	return b.String()
}

func (m *model) renderTitle() string {
	sortLabel := "sort:" + sortCycle[m.sortIdx]
	if m.filter != "" {
		sortLabel += "  filter:" + m.filter
	}
	if m.fetchedThisSession {
		sortLabel += "  fresh"
	} else {
		sortLabel += "  local"
	}
	left := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).
		Render("prv") + lipgloss.NewStyle().Foreground(colorMuted).
		Render(" "+trimRoot(m.root))
	titleLine := lipgloss.JoinHorizontal(lipgloss.Left, left, strings.Repeat(" ", max(1, m.width-lipgloss.Width(left)-lipgloss.Width(sortLabel))), lipgloss.NewStyle().Foreground(colorMuted).Render(sortLabel))

	status := m.status
	if m.fetching {
		status = spinnerFrames[m.spinner] + " " + status
	}
	statusLine := lipgloss.NewStyle().Foreground(colorMuted).Render(status)
	return lipgloss.JoinVertical(lipgloss.Left, titleLine, statusLine)
}

func (m *model) renderTable(avail int) string {
	// Column widths come from the header labels PLUS every row in the full
	// filtered view (m.view), not just the visible window. Computing widths
	// over the visible window alone made a column shrink whenever the row
	// carrying its longest cell scrolled out of view — the "big project name
	// disappears, janky ui" jitter. Sizing from the full view keeps columns
	// stable across scroll position; the body below still emits only the
	// visible rows. Cost is O(N·cols) per render instead of O(W·cols), fine
	// for typical project counts.
	widths := make([]int, len(headers))
	for c, h := range headers {
		widths[c] = lipgloss.Width(h)
	}

	type cached struct {
		p   *project.Project
		cel []string
	}
	// all holds the plain render.Row cells for every filtered row; widths are
	// derived from these plain widths so padding matches the styled cells.
	all := make([]cached, len(m.view))
	for vi := range m.view {
		p := &m.rows[m.view[vi]]
		cel := render.Row(p, m.fetchedThisSession)
		for c, v := range cel {
			if w := lipgloss.Width(v); w > widths[c] {
				widths[c] = w
			}
		}
		all[vi] = cached{p: p, cel: cel}
	}

	// Keep selection in view.
	start := m.scroll
	if start < 0 {
		start = 0
	}
	if start > len(m.view) {
		start = len(m.view)
	}
	if m.selected < start {
		start = m.selected
	}
	if m.selected >= start+avail {
		start = m.selected - avail + 1
	}
	end := start + avail
	if end > len(m.view) {
		end = len(m.view)
	}
	m.scroll = start

	var b strings.Builder
	// header row
	b.WriteString(renderHeaderRow(widths))
	b.WriteByte('\n')

	for vi := start; vi < end; vi++ {
		r := all[vi]
		isSel := vi == m.selected
		// build colored cells, padded to column width using the plain width.
		cells := make([]string, len(headers))
		for c := range headers {
			plain := r.cel[c]
			rendered := styleCell(c, plain, r.p)
			pad := widths[c] - lipgloss.Width(plain)
			if pad < 0 {
				pad = 0
			}
			cells[c] = rendered + strings.Repeat(" ", pad)
		}
		line := strings.Join(cells, render.ColGap)
		if stale(r.p) {
			line = lipgloss.NewStyle().Faint(true).Render(line)
		}
		if isSel {
			line = lipgloss.NewStyle().Bold(true).Background(colorBar).Foreground(colorAccent).Render(" " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// headerStyle is the shared header-row style (bold accent). Used by both the
// populated table (renderTable) and the empty-state header so they never
// diverge.
var headerStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

// renderRowWidths renders a plain row with the given style and column widths.
func renderRowWidths(cells []string, widths []int, style lipgloss.Style, _ bool) string {
	parts := make([]string, len(cells))
	for c, v := range cells {
		pad := widths[c] - lipgloss.Width(v)
		if pad < 0 {
			pad = 0
		}
		parts[c] = style.Render(v) + strings.Repeat(" ", pad)
	}
	return strings.Join(parts, render.ColGap)
}

// renderHeaderRow renders the header labels padded to widths, styled once.
func renderHeaderRow(widths []int) string {
	return renderRowWidths(headers, widths, headerStyle, false)
}

func footerHeight(m *model) int {
	if m.filtering {
		return 1
	}
	return 1
}

func (m *model) renderFooter() string {
	if m.filtering {
		return lipgloss.NewStyle().Foreground(colorAccent).Render("filter: "+m.filter) + lipgloss.NewStyle().Faint(true).Render("  (enter apply · esc cancel)")
	}
	hint := "j/k move · / filter · s sort · r refresh · f fetch · ? help · q quit"
	return lipgloss.NewStyle().Faint(true).Render(hint)
}

func (m *model) renderHelp() string {
	lines := []string{
		"prv — key bindings",
		"",
		"  j / k / ↑↓   navigate",
		"  /            filter by name, tags, git state",
		"  s            cycle sort: name → activity → todo → tags → git",
		"  r            manual refresh (re-scan)",
		"  f            fetch tracking refs across repos",
		"  ?            toggle this help",
		"  q / Ctrl+C   quit",
	}
	body := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Foreground(colorNormal).
		Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func trimRoot(root string) string {
	if root == "" || root == "." {
		return "."
	}
	return root
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
