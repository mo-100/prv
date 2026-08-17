package main

import (
	"bytes"
	"testing"
)

// TestVersionPlainOnNonTTY is the contract for --version: output is exactly
// "prv <version>" with no ANSI, whether piped or not.
func TestVersionPlainOnNonTTY(t *testing.T) {
	var out, err bytes.Buffer
	if e := run([]string{"--version"}, &out, &err); e != nil {
		t.Fatalf("run(--version) err: %v", e)
	}
	if want := "prv " + version + "\n"; out.String() != want {
		t.Errorf("version output = %q, want %q", out.String(), want)
	}
}
