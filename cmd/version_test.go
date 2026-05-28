package cmd

import (
	"runtime"
	"strings"
	"testing"
)

func TestVersionStringContainsKeyFields(t *testing.T) {
	got := versionString()
	for _, sub := range []string{
		"tc-logview ",
		version,
		"commit:",
		"built with:",
		runtime.Version(),
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("versionString() = %q; missing %q", got, sub)
		}
	}
}

func TestVcsRevisionShape(t *testing.T) {
	got := vcsRevision()
	if got == "" {
		t.Fatal("vcsRevision() returned empty string")
	}
	if got != "(unknown)" && len(got) > 7 {
		t.Errorf("vcsRevision() = %q; expected at most 7 chars or \"(unknown)\"", got)
	}
}
