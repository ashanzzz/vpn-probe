package probe

import (
	"strings"
	"testing"
)

func TestTailBuffer_RecentLines_StripsANSI(t *testing.T) {
	buf := NewTailBuffer(1024)
	ansiInput := "\x1b[36mINFO\x1b[0m[0000] [\x1b[38;5;194m1879068850\x1b[0m 0ms] inbound connection to www.gstatic.com:443\n" +
		"\x1b[31mERROR\x1b[0m[0005] connection reset by peer\n"

	_, err := buf.Write([]byte(ansiInput))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	recent := buf.RecentLines(2)
	if strings.Contains(recent, "\x1b") {
		t.Errorf("expected recent lines to NOT contain ANSI escape characters, got: %q", recent)
	}
	expected := "INFO[0000] [1879068850 0ms] inbound connection to www.gstatic.com:443 | ERROR[0005] connection reset by peer"
	if recent != expected {
		t.Errorf("RecentLines = %q, want %q", recent, expected)
	}
}

func TestTailBuffer_RecentLines_Truncation(t *testing.T) {
	buf := NewTailBuffer(1024)
	input := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	_, _ = buf.Write([]byte(input))

	recent := buf.RecentLines(3)
	expected := "line 3 | line 4 | line 5"
	if recent != expected {
		t.Errorf("RecentLines(3) = %q, want %q", recent, expected)
	}

	all := buf.RecentLines(10)
	expectedAll := "line 1 | line 2 | line 3 | line 4 | line 5"
	if all != expectedAll {
		t.Errorf("RecentLines(10) = %q, want %q", all, expectedAll)
	}
}

func TestTailBuffer_WriteAndCap(t *testing.T) {
	buf := NewTailBuffer(20)
	_, _ = buf.Write([]byte("12345678901234567890EXTRA"))
	str := buf.String()
	if len(str) > 20 {
		t.Errorf("expected buffer length <= 20, got %d", len(str))
	}
	if !strings.HasSuffix(str, "EXTRA") {
		t.Errorf("expected buffer to end with EXTRA, got %q", str)
	}
}
