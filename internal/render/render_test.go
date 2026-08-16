package render

import (
	"strings"
	"testing"
	"time"

	"github.com/mo-100/prv/internal/project"
)

func TestGit(t *testing.T) {
	cases := []struct {
		name string
		p    project.Project
		want string
	}{
		{"non-git", project.Project{}, "—"},
		{"git error", project.Project{IsGit: true, Err: errBoom}, "!"},
		{"dirty modified", project.Project{IsGit: true, Modified: 2}, "●2"},
		{"dirty untracked", project.Project{IsGit: true, Untracked: 3}, "●3"},
		{"dirty modified+untracked", project.Project{IsGit: true, Modified: 1, Untracked: 4}, "●5"},
		{"clean", project.Project{IsGit: true}, "✓"},
	}
	for _, c := range cases {
		if got := Git(&c.p); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestUpDownFresh(t *testing.T) {
	// fresh=true (prv ls / post-fetch TUI): plain arrows.
	cases := []struct {
		name string
		p    project.Project
		want string
	}{
		{"ahead", project.Project{IsGit: true, Ahead: 2}, "↑2"},
		{"behind", project.Project{IsGit: true, Behind: 1}, "↓1"},
		{"both", project.Project{IsGit: true, Ahead: 2, Behind: 1}, "↑2 ↓1"},
		{"synced", project.Project{IsGit: true}, "✓"},
		{"no-upstream-ahead", project.Project{IsGit: true, Ahead: -1}, "—"},
		{"no-upstream-behind", project.Project{IsGit: true, Behind: -1}, "—"},
		{"non-git", project.Project{}, "—"},
	}
	for _, c := range cases {
		if got := UpDown(&c.p, true); got != c.want {
			t.Errorf("%s (fresh): got %q want %q", c.name, got, c.want)
		}
	}
}

func TestUpDownStale(t *testing.T) {
	// fresh=false (TUI pre-fetch): numeric arrows get a `?` suffix so stale
	// local-only values never read as fresh. The non-applicable and in-sync
	// cases are unaffected (no number to question).
	cases := []struct {
		name string
		p    project.Project
		want string
	}{
		{"ahead", project.Project{IsGit: true, Ahead: 2}, "↑2?"},
		{"behind", project.Project{IsGit: true, Behind: 1}, "↓1?"},
		{"both", project.Project{IsGit: true, Ahead: 2, Behind: 1}, "↑2? ↓1?"},
		{"synced", project.Project{IsGit: true}, "✓"},
		{"no-upstream", project.Project{IsGit: true, Ahead: -1}, "—"},
		{"non-git", project.Project{}, "—"},
	}
	for _, c := range cases {
		if got := UpDown(&c.p, false); got != c.want {
			t.Errorf("%s (stale): got %q want %q", c.name, got, c.want)
		}
	}
}

func TestTODO(t *testing.T) {
	if got := TODO(false, 0); got != "—" {
		t.Errorf("absent: got %q want —", got)
	}
	if got := TODO(true, 3); got != "3" {
		t.Errorf("present open=3: got %q want 3", got)
	}
	if got := TODO(true, 0); got != "0" {
		t.Errorf("present open=0: got %q want 0", got)
	}
}

func TestLastActive(t *testing.T) {
	if got := LastActive(time.Time{}); got != "—" {
		t.Errorf("zero: got %q want —", got)
	}
	if got := LastActive(time.Now().Add(-3 * time.Hour)); got != "3h ago" {
		t.Errorf("3h ago: got %q want 3h ago", got)
	}
	if got := LastActive(time.Now().Add(-2 * time.Minute)); got != "2m ago" {
		t.Errorf("2m ago: got %q want 2m ago", got)
	}
	if got := LastActive(time.Now().Add(-21 * 24 * time.Hour)); got != "21d ago" {
		t.Errorf("21d ago: got %q want 21d ago", got)
	}
	if got := LastActive(time.Now().Add(-10 * time.Second)); got != "just now" {
		t.Errorf("just now: got %q want just now", got)
	}
}

func TestName(t *testing.T) {
	if got := Name("x", nil); got != "x" {
		t.Errorf("no error: got %q want x", got)
	}
	if got := Name("x", errBoom); got != "x!" {
		t.Errorf("with error: got %q want x!", got)
	}
}

func TestTags(t *testing.T) {
	if got := Tags(nil); got != "—" {
		t.Errorf("nil tags: got %q want —", got)
	}
	if got := Tags([]string{"go", "node"}); got != "go,node" {
		t.Errorf("two tags: got %q want go,node", got)
	}
}

func TestBranch(t *testing.T) {
	if got := Branch(""); got != "—" {
		t.Errorf("empty: got %q want —", got)
	}
	if got := Branch("main"); got != "main" {
		t.Errorf("main: got %q want main", got)
	}
}

func TestRow(t *testing.T) {
	p := project.Project{Name: "app", IsGit: true, Branch: "main", Ahead: 2,
		Tags: []string{"go"}, TODO: true, TODOOpen: 5,
		LastActive: time.Now().Add(-2 * time.Hour)}
	cells := Row(&p, false)
	if len(cells) != len(Columns) {
		t.Fatalf("row cell count = %d, want %d (one per column)", len(cells), len(Columns))
	}
	if cells[ColName] != "app" || cells[ColUpDown] != "↑2?" || cells[ColTODO] != "5" {
		t.Errorf("row cells out of order/stale: %v", cells)
	}
}

func TestSort(t *testing.T) {
	t.Run("git dirtier first", func(t *testing.T) {
		ps := []project.Project{
			{Name: "clean-only", IsGit: true, Modified: 0, Untracked: 0},
			{Name: "a-lot", IsGit: true, Modified: 2, Untracked: 3},
			{Name: "a-bit", IsGit: true, Modified: 1, Untracked: 0},
			{Name: "no-git", IsGit: false},
		}
		Sort(ps, "git")
		want := []string{"a-lot", "a-bit", "clean-only", "no-git"}
		for i := range want {
			if ps[i].Name != want[i] {
				t.Errorf("pos %d: got %q want %q", i, ps[i].Name, want[i])
			}
		}
	})

	t.Run("activity newest first", func(t *testing.T) {
		old := time.Now().Add(-30 * 24 * time.Hour)
		newer := time.Now().Add(-1 * time.Hour)
		zero := time.Time{}
		ps := []project.Project{
			{Name: "stale", LastActive: old},
			{Name: "fresh", LastActive: newer},
			{Name: "never", LastActive: zero},
		}
		Sort(ps, "activity")
		want := []string{"fresh", "stale", "never"}
		for i := range want {
			if ps[i].Name != want[i] {
				t.Errorf("pos %d: got %q want %q", i, ps[i].Name, want[i])
			}
		}
	})

	t.Run("name A to Z", func(t *testing.T) {
		ps := []project.Project{
			{Name: "zeta"},
			{Name: "alpha"},
			{Name: "mid"},
		}
		Sort(ps, "name")
		want := []string{"alpha", "mid", "zeta"}
		for i := range want {
			if ps[i].Name != want[i] {
				t.Errorf("pos %d: got %q want %q", i, ps[i].Name, want[i])
			}
		}
	})

	t.Run("todo most open first", func(t *testing.T) {
		ps := []project.Project{
			{Name: "few", TODOOpen: 1},
			{Name: "many", TODOOpen: 9},
			{Name: "none", TODOOpen: 0},
		}
		Sort(ps, "todo")
		want := []string{"many", "few", "none"}
		for i := range want {
			if ps[i].Name != want[i] {
				t.Errorf("pos %d: got %q want %q", i, ps[i].Name, want[i])
			}
		}
	})

	t.Run("tags element-wise A to Z", func(t *testing.T) {
		ps := []project.Project{
			{Name: "node-only", Tags: []string{"node"}},
			{Name: "go-node", Tags: []string{"go", "node"}},
			{Name: "go-only", Tags: []string{"go"}},
		}
		Sort(ps, "tags")
		want := []string{"go-only", "go-node", "node-only"}
		for i := range want {
			if ps[i].Name != want[i] {
				t.Errorf("pos %d: got %q want %q", i, ps[i].Name, want[i])
			}
		}
	})

	t.Run("unknown field is no-op", func(t *testing.T) {
		ps := []project.Project{{Name: "x"}, {Name: "y"}}
		orig := []string{ps[0].Name, ps[1].Name}
		Sort(ps, "bogus")
		for i := range orig {
			if ps[i].Name != orig[i] {
				t.Errorf("pos %d mutated: got %q want %q", i, ps[i].Name, orig[i])
			}
		}
	})
}

func TestValidSortAndCycle(t *testing.T) {
	for _, key := range SortCycle() {
		if !ValidSort(key) {
			t.Errorf("SortCycle key %q not ValidSort", key)
		}
	}
	if ValidSort("bogus") {
		t.Errorf("ValidSort(bogus) = true, want false")
	}
	wantCycle := strings.Join([]string{"name", "activity", "todo", "tags", "git"}, ",")
	if got := strings.Join(SortCycle(), ","); got != wantCycle {
		t.Errorf("SortCycle = %q want %q", got, wantCycle)
	}
}

func TestDefaultSortIsCycleHead(t *testing.T) {
	cycle := SortCycle()
	if len(cycle) == 0 {
		t.Fatal("SortCycle is empty")
	}
	if got := DefaultSort(); got != cycle[0] {
		t.Errorf("DefaultSort = %q, want cycle head %q", got, cycle[0])
	}
	if DefaultSort() != "name" {
		t.Errorf(`DefaultSort = %q, want "name"`, DefaultSort())
	}
}

func TestHeadersDeriveFromColumns(t *testing.T) {
	rh := ReportHeaders()
	th := TUIHeaders()
	if len(rh) != len(Columns) || len(th) != len(Columns) {
		t.Fatalf("header len %d/%d != columns %d", len(rh), len(th), len(Columns))
	}
	for i, c := range Columns {
		if rh[i] != c.Report {
			t.Errorf("ReportHeaders[%d] = %q want %q", i, rh[i], c.Report)
		}
		if th[i] != c.TUI {
			t.Errorf("TUIHeaders[%d] = %q want %q", i, th[i], c.TUI)
		}
	}
	// The order in Columns must match the iota indices.
	for i, c := range Columns {
		if c.ID != i {
			t.Errorf("Columns[%d].ID = %d, want %d (index drift)", i, c.ID, i)
		}
	}
}

var errBoom = errSentinel{}

type errSentinel struct{}

func (errSentinel) Error() string { return "boom" }
