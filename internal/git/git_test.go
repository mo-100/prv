package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// gitRun runs git in dir, failing the test on error; returns trimmed stdout.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// initRepo creates a git repo at dir with a deterministic identity.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "init", "-b", "main", dir)
	gitRun(t, dir, "config", "user.email", "t@example.com")
	gitRun(t, dir, "config", "user.name", "Tester")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
}

// commitFile writes, adds, and commits a file with the given content.
func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", name)
	gitRun(t, dir, "commit", "-m", msg)
}

// commitAt commits with a deterministic committer+author date (unix seconds),
// set via env vars git honors for the commit process.
func commitAt(t *testing.T, dir, name, content, msg string, unix int64) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", name)

	ts := strconv.FormatInt(unix, 10) + " +0000"
	cmd := exec.Command("git", "-C", dir, "commit", "-m", msg)
	cmd.Env = append(os.Environ(),
		"GIT_COMMITTER_DATE="+ts,
		"GIT_AUTHOR_DATE="+ts,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func TestScanCleanRepo(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "a.txt", "hello\n", "init")

	s, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if s.Branch != "main" {
		t.Errorf("Branch = %q, want %q", s.Branch, "main")
	}
	if s.Detached {
		t.Errorf("Detached = true, want false")
	}
	if s.Modified != 0 || s.Untracked != 0 {
		t.Errorf("counts = (%d, %d), want (0, 0)", s.Modified, s.Untracked)
	}
	if s.Ahead != -1 || s.Behind != -1 {
		t.Errorf("ahead/behind = (%d, %d), want (-1, -1): no upstream", s.Ahead, s.Behind)
	}
	if len(s.DirtyPaths) != 0 {
		t.Errorf("DirtyPaths = %v, want empty", s.DirtyPaths)
	}
}

func TestScanModifiedFile(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "a.txt", "hello\n", "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if s.Modified != 1 {
		t.Errorf("Modified = %d, want 1", s.Modified)
	}
	if s.Untracked != 0 {
		t.Errorf("Untracked = %d, want 0", s.Untracked)
	}
	if len(s.DirtyPaths) != 1 || s.DirtyPaths[0] != "a.txt" {
		t.Errorf("DirtyPaths = %v, want [a.txt]", s.DirtyPaths)
	}
}

func TestScanUntrackedFile(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "a.txt", "hello\n", "init")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if s.Untracked != 1 {
		t.Errorf("Untracked = %d, want 1", s.Untracked)
	}
	if s.Modified != 0 {
		t.Errorf("Modified = %d, want 0", s.Modified)
	}
	if len(s.DirtyPaths) != 1 || s.DirtyPaths[0] != "new.txt" {
		t.Errorf("DirtyPaths = %v, want [new.txt]", s.DirtyPaths)
	}
}

func TestScanRenameCountedOnce(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "old.txt", "content\n", "init")
	// git mv stages a rename → a single "2 " entry.
	gitRun(t, dir, "mv", "old.txt", "new.txt")

	s, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if s.Modified != 1 {
		t.Errorf("Modified = %d, want 1 (rename counted once)", s.Modified)
	}
	if len(s.DirtyPaths) != 1 || s.DirtyPaths[0] != "new.txt" {
		t.Errorf("DirtyPaths = %v, want [new.txt]", s.DirtyPaths)
	}
}

func TestScanAheadBehind(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "a.txt", "1\n", "A")
	commitFile(t, dir, "a.txt", "12\n", "B")

	// Bare upstream + push -u sets the tracking ref at B (+0 -0).
	bare := t.TempDir()
	gitRun(t, dir, "init", "--bare", bare)
	gitRun(t, dir, "remote", "add", "origin", bare)
	gitRun(t, dir, "push", "-u", "origin", "main")

	// Move local back to A (origin/main stays at B) → behind 1.
	gitRun(t, dir, "reset", "--hard", "HEAD~1")
	// New divergent commit C → ahead 1, behind 1.
	commitFile(t, dir, "a.txt", "13\n", "C")

	s, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if s.Branch != "main" {
		t.Errorf("Branch = %q, want main", s.Branch)
	}
	if s.Ahead != 1 || s.Behind != 1 {
		t.Errorf("ahead/behind = (%d, %d), want (1, 1)", s.Ahead, s.Behind)
	}
	if s.Detached {
		t.Error("Detached = true, want false")
	}
}

func TestScanDetachedHEAD(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "a.txt", "1\n", "A")
	commitFile(t, dir, "a.txt", "12\n", "B")

	// Checkout the first commit → detached HEAD, no upstream.
	gitRun(t, dir, "checkout", "HEAD~1")

	full := gitRun(t, dir, "rev-parse", "HEAD")
	wantShort := full
	if len(wantShort) > shortOIDLen {
		wantShort = wantShort[:shortOIDLen]
	}

	s, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !s.Detached {
		t.Fatalf("Detached = false, want true")
	}
	if s.Branch != wantShort {
		t.Errorf("Branch = %q, want %q", s.Branch, wantShort)
	}
	if s.Ahead != -1 || s.Behind != -1 {
		t.Errorf("ahead/behind = (%d, %d), want (-1, -1): no upstream on detached", s.Ahead, s.Behind)
	}
	if s.Modified != 0 || s.Untracked != 0 {
		t.Errorf("counts = (%d, %d), want (0, 0)", s.Modified, s.Untracked)
	}
}

func TestScanNoCommitsYet(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	// No commits; an untracked file still shows as ? .
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// A freshly-init'd repo reports branch.head main even with no commits.
	if s.Branch != "main" {
		t.Errorf("Branch = %q, want main", s.Branch)
	}
	if s.Modified != 0 || s.Untracked != 1 {
		t.Errorf("counts = (%d, %d), want (0, 1)", s.Modified, s.Untracked)
	}
	if s.Ahead != -1 || s.Behind != -1 {
		t.Errorf("ahead/behind = (%d, %d), want (-1, -1)", s.Ahead, s.Behind)
	}
}

func TestLastCommitTime(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	// No commits yet → zero time, ok=false, no error.
	ct, ok, err := LastCommitTime(dir)
	if err != nil || ok {
		t.Fatalf("before commit: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
	if !ct.IsZero() {
		t.Errorf("zero-time before commit = %v, want zero", ct)
	}

	// Commit with a deterministic committer date.
	want := int64(1_700_000_000)
	commitAt(t, dir, "a.txt", "1\n", "A", want)

	ct, ok, err = LastCommitTime(dir)
	if err != nil {
		t.Fatalf("LastCommitTime: %v", err)
	}
	if !ok {
		t.Fatal("ok = false after a commit, want true")
	}
	if got := ct.Unix(); got != want {
		t.Errorf("commit time = %d, want %d", got, want)
	}
}

func TestHasGitEntry(t *testing.T) {
	dir := t.TempDir()
	if HasGitEntry(dir) {
		t.Fatal("HasGitEntry true before init")
	}
	initRepo(t, dir)
	if !HasGitEntry(dir) {
		t.Fatal("HasGitEntry false after init")
	}
}

func TestScanMissingGitEntryReturnsTypedError(t *testing.T) {
	dir := t.TempDir()
	if HasGitEntry(dir) {
		t.Fatal("temp dir unexpectedly has .git")
	}
	_, err := Scan(dir)
	if err == nil {
		t.Fatal("Scan returned nil error on non-repo; want typed *Err")
	}
	var ge *Err
	if !asErr(err, &ge) {
		t.Fatalf("err = %T (%v), want *Err", err, err)
	}
}

// asErr walks the error chain (errors.As).
func asErr(err error, target **Err) bool {
	for e := err; e != nil; {
		if ge, ok := e.(*Err); ok {
			*target = ge
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := e.(unwrapper); ok {
			e = u.Unwrap()
			continue
		}
		break
	}
	return false
}