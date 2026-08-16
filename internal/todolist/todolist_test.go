package todolist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestScanNoTODO(t *testing.T) {
	dir := t.TempDir()
	present, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if present || open != 0 {
		t.Fatalf("got present=%v open=%d, want false/0", present, open)
	}
}

func TestScanRootMarkdown(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "TODO.md", "# Tasks\n- [ ] one\n- [ ] two\n- [x] done\n")
	present, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !present {
		t.Fatal("present=false, want true")
	}
	if open != 2 {
		t.Fatalf("open=%d, want 2", open)
	}
}

func TestScanSubdirText(t *testing.T) {
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, docs, "TODO.txt", "- [ ] docs task\n- [ ] another\n- [X] finished\n")
	present, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !present {
		t.Fatal("present=false, want true")
	}
	if open != 2 {
		t.Fatalf("open=%d, want 2", open)
	}
}

func TestScanUppercaseXIsDone(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "TODO.md", "- [ ] open\n- [X] done-upper\n- [x] done-lower\n")
	_, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if open != 1 {
		t.Fatalf("open=%d, want 1 (uppercase X is done)", open)
	}
}

func TestScanNoCheckboxes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "TODO.md", "# Notes\njust prose, no tasks\n")
	present, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !present {
		t.Fatal("present=false, want true")
	}
	if open != 0 {
		t.Fatalf("open=%d, want 0", open)
	}
}

func TestScanEmptyIsPresentZeroOpen(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "TODO.md", "")
	present, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !present || open != 0 {
		t.Fatalf("got present=%v open=%d, want true/0", present, open)
	}
}

func TestScanCaseInsensitiveNames(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Todo.TXT", "- [ ] a\n- [ ] b\n- [ ] c\n")
	present, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !present {
		t.Fatal("present=false, want true")
	}
	if open != 3 {
		t.Fatalf("open=%d, want 3", open)
	}
}

func TestScanRootBeatsSubdir(t *testing.T) {
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "TODO.md", "- [ ] root-only\n")             // 1 open
	write(t, docs, "TODO.txt", "- [ ] a\n- [ ] b\n- [ ] c\n") // 3 open
	_, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if open != 1 {
		t.Fatalf("open=%d, want 1 (root must win over subdir)", open)
	}
}

func TestScanDeterministicSubdirOrder(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"docs", "aaa"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// "aaa" sorts before "docs", so its TODO must win regardless of creation order.
	write(t, filepath.Join(dir, "aaa"), "TODO.md", "- [ ] in-aaa\n")                                 // 1 open
	write(t, filepath.Join(dir, "docs"), "TODO.md", "- [ ] a\n- [ ] b\n- [ ] c\n- [ ] d\n- [ ] e\n") // 5 open
	_, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if open != 1 {
		t.Fatalf("open=%d, want 1 (aaa, sorted first, must win)", open)
	}
}

func TestScanSubdirOnlyOneLevel(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "work", "client")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	// A TODO two levels down must NOT be found at depth=2.
	write(t, inner, "TODO.md", "- [ ] deep\n")
	present, _, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if present {
		t.Fatal("present=true, want false (two levels down is out of scope at depth 2)")
	}
	// ...but it IS found once the depth bound is raised.
	present, open, err := Scan(dir, 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !present || open != 1 {
		t.Fatalf("depth=3: got present=%v open=%d, want true/1", present, open)
	}
}

func TestScanDepth1StopsAtRoot(t *testing.T) {
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, docs, "TODO.md", "- [ ] a\n")
	// depth=1 must look at dir only; the subdir TODO is invisible.
	present, _, err := Scan(dir, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if present {
		t.Fatal("present=true, want false (subdir out of scope at depth 1)")
	}
}

// TestScanPrunesSkipAndHiddenDirs locks the regression where the TODO walk
// descended into EVERY subdirectory while tag detection prunes the shared skip
// list and hidden dirs: a TODO.md inside node_modules/.vscode injected a false
// TODO signal + open count into the row. The walk now prunes identically to
// the manifest walk via fsutil.IsHidden / fsutil.IsSkipDir.
func TestScanPrunesSkipAndHiddenDirs(t *testing.T) {
	for _, tt := range []struct {
		name string
		sub  string
	}{
		{"skip dir node_modules", "node_modules"},
		{"hidden dir .vscode", ".vscode"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			sub := filepath.Join(dir, tt.sub)
			if err := os.Mkdir(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, sub, "TODO.md", "- [ ] dependency task\n")
			present, open, err := Scan(dir, 2)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if present || open != 0 {
				t.Fatalf("got present=%v open=%d, want false/0 (TODO under %s must be pruned)", present, open, tt.sub)
			}
		})
	}
}

func TestScanParseErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	// A single line longer than bufio's MaxScanTokenSize triggers a scanner
	// error; the contract is that read/parse errors surface as err.
	write(t, dir, "TODO.md", strings.Repeat("a", 1<<17))
	present, _, err := Scan(dir, 2)
	if err == nil {
		t.Fatal("err=nil, want a parse error for an oversized line")
	}
	if present {
		t.Fatal("present=true on error, want false")
	}
}

// TestScanBareTODO locks the contract that a bare `TODO` (no extension) is a
// recognized TODO file. It used to classify as a project signal via
// project.isSignalName but never rendered (todoNames lacked it) - detection
// and render disagreed. Now both stem from IsTODOName.
func TestScanBareTODO(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "TODO", "- [ ] one\n- [x] two\n")
	present, open, err := Scan(dir, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !present {
		t.Fatal("present=false for bare TODO, want true")
	}
	if open != 1 {
		t.Fatalf("open=%d, want 1", open)
	}
}

// TestScanRejectsOtherExtensions locks the accepted set to TODO/TODO.md/TODO.txt.
// TODO.org (and other TODO.*) previously classified as a project via the old
// isSignalName prefix but never rendered; it must now do neither. Guards
// against re-broadening detection past the single source of truth.
func TestScanRejectsOtherExtensions(t *testing.T) {
	for _, name := range []string{"TODO.org", "TODO.bak", "TODO.swp", "TODO.txt.md"} {
		dir := t.TempDir()
		write(t, dir, name, "- [ ] x\n")
		present, open, err := Scan(dir, 2)
		if err != nil {
			t.Fatalf("%s: err: %v", name, err)
		}
		if present || open != 0 {
			t.Errorf("%s: got present=%v open=%d, want false/0 (not in IsTODOName)", name, present, open)
		}
	}
}

func TestIsTODONameCaseInsensitive(t *testing.T) {
	for _, name := range []string{"TODO", "todo", "Todo", "TODO.md", "todo.txt", "Todo.MD"} {
		if !IsTODOName(name) {
			t.Errorf("IsTODOName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"TODO.org", "TODO.bak", "TODO.", "todolist", "README.md", ""} {
		if IsTODOName(name) {
			t.Errorf("IsTODOName(%q) = true, want false", name)
		}
	}
}
