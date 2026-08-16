// Package git reads the git state of a working tree via the `git` subprocess.
// No go-git; one `git -C <path> status --porcelain=v2 --branch` invocation per
// repo yields branch, ahead/behind, and classified counts.
package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// callTimeout is the bounded wall-clock budget for a single git invocation.
const callTimeout = 15 * time.Second

// shortOIDLen is the length of the abbreviated commit OID reported for a
// detached HEAD; porcelain v2 emits the full 40-char oid on branch.oid.
const shortOIDLen = 7

// Err is a typed git failure so the scanner can set Project.Err and still
// render the row rather than aborting the whole scan.
type Err struct {
	Op  string
	Msg string
}

func (e *Err) Error() string { return "git: " + e.Op + ": " + e.Msg }

// State is the parsed result of one porcelain v2 status invocation.
type State struct {
	Branch     string // short OID when detached
	Detached   bool
	Modified   int // tracked-modified + unmerged
	Untracked  int
	Ahead      int      // -1 if no upstream
	Behind     int      // -1 if no upstream
	DirtyPaths []string // porcelain paths whose mtime feeds activity
}

// Scan runs one porcelain v2 invocation and parses it.
func Scan(dir string) (*State, error) {
	out, err := runGit(dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

// LastCommitTime runs `git log -1 --format=%ct`; returns zero time (ok=false)
// when the repo has no commits yet.
func LastCommitTime(dir string) (time.Time, bool, error) {
	out, err := runGit(dir, "log", "-1", "--format=%ct")
	if err != nil {
		// A repo with no commits exits 128 with the message
		// "your current branch '...' does not have any commits yet".
		var ge *Err
		if errors.As(err, &ge) && strings.Contains(ge.Msg, "does not have any commits yet") {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	sec, perr := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if perr != nil {
		return time.Time{}, false, &Err{Op: "log", Msg: "bad %ct: " + perr.Error()}
	}
	return time.Unix(sec, 0).UTC(), true, nil
}

// HasGitEntry reports a `.git` entry (dir or file for worktrees) in dir.
func HasGitEntry(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// fetchTimeout is the per-repo wall-clock budget for `git fetch` (--fetch / f).
// Longer than the read path's callTimeout because a network round-trip is slow.
const fetchTimeout = 30 * time.Second

// Fetch runs `git -C dir fetch --all --tags` with hang-proofing (locked in the
// spec): GIT_TERMINAL_PROMPT=0 and an SSH command forced to BatchMode=yes so
// HTTPS credential prompts and SSH passphrase prompts fail fast instead of
// blocking a worker forever. A bounded fetchTimeout also caps the wait.
//
// Network failures are returned as a typed *Err; callers degrade gracefully
// (show last-known ahead/behind, "no remote" indicator) rather than aborting.
func Fetch(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--all", "--tags")
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(string(out))
		}
		if msg == "" {
			msg = err.Error()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "timeout"
		}
		return &Err{Op: "fetch", Msg: msg}
	}
	return nil
}

// runGit spawns `git -C dir args...` with a bounded timeout, no terminal
// prompt, and returns the stdout. Failures are wrapped in a typed *Err.
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr strings.Builder
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "timeout"
		}
		return "", &Err{Op: args[0], Msg: msg}
	}
	return string(out), nil
}

// parseStatus decodes the porcelain v2 output into a State.
// Header fields per the v2 spec:
//
//	# branch.oid <OID>
//	# branch.head <name | (detached)>
//	# branch.upstream <upstream>     (absent when detached / no upstream)
//	# branch.ab +<ahead> -<behind>    (absent without upstream)
//
// Entry lines:
//   - "1 "/"2 " = tracked change (renamed 2 counted once each)
//   - "u "      = unmerged
//   - "? "      = untracked
func parseStatus(out string) *State {
	s := &State{Ahead: -1, Behind: -1}
	var oid string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimSpace(strings.TrimPrefix(line, "# branch.oid "))
		case strings.HasPrefix(line, "# branch.head "):
			s.Branch = strings.TrimSpace(strings.TrimPrefix(line, "# branch.head "))
		case strings.HasPrefix(line, "# branch.upstream "):
			// presence recorded; ahead/behind come from branch.ab
		case strings.HasPrefix(line, "# branch.ab "):
			parseBranchAB(s, line)
		case strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 "):
			s.Modified++
			if p := porcelainPath(line); p != "" {
				s.DirtyPaths = append(s.DirtyPaths, p)
			}
		case strings.HasPrefix(line, "u "):
			s.Modified++
			if p := porcelainPath(line); p != "" {
				s.DirtyPaths = append(s.DirtyPaths, p)
			}
		case strings.HasPrefix(line, "? "):
			s.Untracked++
			if p := porcelainPath(line); p != "" {
				s.DirtyPaths = append(s.DirtyPaths, p)
			}
		}
	}
	if s.Branch == "(detached)" {
		s.Detached = true
		s.Branch = shortOID(oid)
	}
	return s
}

// branch.ab line example: `# branch.ab +2 -0`
func parseBranchAB(s *State, line string) {
	for _, f := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
		if len(f) < 2 {
			continue
		}
		n, err := strconv.Atoi(f[1:])
		if err != nil {
			continue
		}
		switch f[0] {
		case '+':
			s.Ahead = n
		case '-':
			s.Behind = n
		}
	}
}

// porcelainPath extracts the path field from a v2 entry line:
//   - "1": fields[8]  (9th field) is the path
//   - "2": fields[9]  (10th field) is the path; fields[8] is renamed-from
//   - "u": fields[10] (11th field) is the path
//   - "?": fields[1]  (2nd field) is the path
func porcelainPath(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	switch line[0] {
	case '1':
		if len(fields) >= 9 {
			return unquote(fields[8])
		}
	case '2':
		if len(fields) >= 10 {
			return unquote(fields[9])
		}
	case 'u':
		if len(fields) >= 11 {
			return unquote(fields[10])
		}
	case '?':
		if len(fields) >= 2 {
			return unquote(fields[1])
		}
	}
	return ""
}

// shortOID abbreviates a full OID to the typical short form.
func shortOID(oid string) string {
	if len(oid) > shortOIDLen {
		return oid[:shortOIDLen]
	}
	return oid
}

// unquote reverses porcelain c-quote escaping; git quotes paths containing
// special bytes with a C-style prefix and escapes.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if uq, err := strconv.Unquote(s); err == nil {
			return uq
		}
	}
	return s
}
