package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- getConfigDir tests ----

func TestGetConfigDir_WithXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test-config")
	t.Setenv("HOME", "/home/someone")

	got := getConfigDir()
	want := "/tmp/xdg-test-config/shimsumm"
	if got != want {
		t.Errorf("getConfigDir() = %q, want %q", got, want)
	}
}

func TestGetConfigDir_WithOnlyHOME(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/testuser")

	got := getConfigDir()
	want := "/home/testuser/.config/shimsumm"
	if got != want {
		t.Errorf("getConfigDir() = %q, want %q", got, want)
	}
}

func TestGetConfigDir_XDGTakesPrecedenceOverHOME(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	t.Setenv("HOME", "/home/testuser")

	got := getConfigDir()
	want := "/custom/xdg/shimsumm"
	if got != want {
		t.Errorf("getConfigDir() = %q, want %q", got, want)
	}
}

// Note: the case where neither XDG_CONFIG_HOME nor HOME is set cannot be
// unit-tested because getConfigDir() calls os.Exit(1), which would
// terminate the test process. Testing that path would require restructuring
// the function to return an error instead.

// ---- parseSkipChecks tests ----

func TestParseSkipChecks_ValidSingleCheck(t *testing.T) {
	f := filepath.Join(t.TempDir(), "myfilter")
	os.WriteFile(f, []byte("#!/bin/sh\n# shimsumm-doctor: skip executable\n"), 0644)

	skips, warnings := parseSkipChecks(f)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !skips["executable"] {
		t.Error("expected 'executable' to be skipped")
	}
	if len(skips) != 1 {
		t.Errorf("expected 1 skip, got %d", len(skips))
	}
}

func TestParseSkipChecks_MultipleChecksOnOneLine(t *testing.T) {
	f := filepath.Join(t.TempDir(), "myfilter")
	os.WriteFile(f, []byte("#!/bin/sh\n# shimsumm-doctor: skip executable, shebang, syntax\n"), 0644)

	skips, warnings := parseSkipChecks(f)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	for _, name := range []string{"executable", "shebang", "syntax"} {
		if !skips[name] {
			t.Errorf("expected %q to be skipped", name)
		}
	}
	if len(skips) != 3 {
		t.Errorf("expected 3 skips, got %d", len(skips))
	}
}

func TestParseSkipChecks_UnknownCheckName(t *testing.T) {
	f := filepath.Join(t.TempDir(), "myfilter")
	os.WriteFile(f, []byte("#!/bin/sh\n# shimsumm-doctor: skip bogus-check\n"), 0644)

	skips, warnings := parseSkipChecks(f)

	if len(skips) != 0 {
		t.Errorf("expected 0 skips for unknown check, got %v", skips)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if !strings.Contains(warnings[0], "bogus-check") {
		t.Errorf("warning should mention 'bogus-check', got %q", warnings[0])
	}
	if !strings.Contains(warnings[0], "unknown check name") {
		t.Errorf("warning should say 'unknown check name', got %q", warnings[0])
	}
}

func TestParseSkipChecks_MixedKnownAndUnknown(t *testing.T) {
	f := filepath.Join(t.TempDir(), "myfilter")
	os.WriteFile(f, []byte("#!/bin/sh\n# shimsumm-doctor: skip executable, nonsense\n"), 0644)

	skips, warnings := parseSkipChecks(f)

	if !skips["executable"] {
		t.Error("expected 'executable' to be skipped")
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning for 'nonsense', got %d", len(warnings))
	}
	if !strings.Contains(warnings[0], "nonsense") {
		t.Errorf("warning should mention 'nonsense', got %q", warnings[0])
	}
}

func TestParseSkipChecks_NoSkipComments(t *testing.T) {
	f := filepath.Join(t.TempDir(), "myfilter")
	os.WriteFile(f, []byte("#!/bin/sh\n# just a regular comment\necho hello\n"), 0644)

	skips, warnings := parseSkipChecks(f)

	if len(skips) != 0 {
		t.Errorf("expected 0 skips, got %d", len(skips))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %v", warnings)
	}
}

func TestParseSkipChecks_NonexistentFile(t *testing.T) {
	skips, warnings := parseSkipChecks("/nonexistent/path/to/file")

	if len(skips) != 0 {
		t.Errorf("expected 0 skips, got %d", len(skips))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %v", warnings)
	}
}

func TestParseSkipChecks_MultipleSkipLines(t *testing.T) {
	f := filepath.Join(t.TempDir(), "myfilter")
	content := "#!/bin/sh\n# shimsumm-doctor: skip executable\n# shimsumm-doctor: skip shebang\n"
	os.WriteFile(f, []byte(content), 0644)

	skips, warnings := parseSkipChecks(f)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !skips["executable"] || !skips["shebang"] {
		t.Errorf("expected both 'executable' and 'shebang' to be skipped, got %v", skips)
	}
}

func TestParseSkipChecks_AllValidChecks(t *testing.T) {
	f := filepath.Join(t.TempDir(), "myfilter")
	content := "#!/bin/sh\n# shimsumm-doctor: skip executable, shebang, sources-wrap, calls-wrap, syntax, sources-cleanly\n"
	os.WriteFile(f, []byte(content), 0644)

	skips, warnings := parseSkipChecks(f)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if len(skips) != 6 {
		t.Errorf("expected 6 skips, got %d: %v", len(skips), skips)
	}
}

// ---- generateUnifiedDiff tests ----

func TestGenerateUnifiedDiff_IdenticalInputs(t *testing.T) {
	lines := []string{"line1", "line2", "line3"}
	diff := generateUnifiedDiff("a.txt", "b.txt", lines, lines)

	if !strings.Contains(diff, "--- a.txt") {
		t.Error("expected '--- a.txt' header")
	}
	if !strings.Contains(diff, "+++ b.txt") {
		t.Error("expected '+++ b.txt' header")
	}
	// No added or removed lines expected; all lines should be context (space-prefixed)
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--- ") {
			t.Errorf("unexpected removed line in identical diff: %q", line)
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ ") {
			t.Errorf("unexpected added line in identical diff: %q", line)
		}
	}
}

func TestGenerateUnifiedDiff_SingleLineChange(t *testing.T) {
	from := []string{"hello"}
	to := []string{"world"}
	diff := generateUnifiedDiff("from.txt", "to.txt", from, to)

	if !strings.Contains(diff, "-hello") {
		t.Error("expected '-hello' in diff")
	}
	if !strings.Contains(diff, "+world") {
		t.Error("expected '+world' in diff")
	}
}

func TestGenerateUnifiedDiff_AddedLines(t *testing.T) {
	from := []string{"line1"}
	to := []string{"line1", "line2", "line3"}
	diff := generateUnifiedDiff("from.txt", "to.txt", from, to)

	if !strings.Contains(diff, "+line2") {
		t.Error("expected '+line2' in diff")
	}
	if !strings.Contains(diff, "+line3") {
		t.Error("expected '+line3' in diff")
	}
	// line1 is unchanged so should not be marked as removed
	if strings.Contains(diff, "-line1") {
		t.Error("line1 is unchanged and should not be marked as removed")
	}
}

func TestGenerateUnifiedDiff_RemovedLines(t *testing.T) {
	from := []string{"line1", "line2", "line3"}
	to := []string{"line1"}
	diff := generateUnifiedDiff("from.txt", "to.txt", from, to)

	if !strings.Contains(diff, "-line2") {
		t.Error("expected '-line2' in diff")
	}
	if !strings.Contains(diff, "-line3") {
		t.Error("expected '-line3' in diff")
	}
	// line1 is unchanged
	if strings.Contains(diff, "-line1") {
		t.Error("line1 is unchanged and should not be marked as removed")
	}
}

func TestGenerateUnifiedDiff_HasHeaders(t *testing.T) {
	diff := generateUnifiedDiff("expected.txt", "actual", []string{"a"}, []string{"b"})

	if !strings.Contains(diff, "--- expected.txt") {
		t.Error("expected '--- expected.txt' header")
	}
	if !strings.Contains(diff, "+++ actual") {
		t.Error("expected '+++ actual' header")
	}
	if !strings.Contains(diff, "@@") {
		t.Error("expected @@ hunk header")
	}
}
