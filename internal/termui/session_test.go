package termui

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatHelpers(t *testing.T) {
	t.Parallel()

	if got := renderBar(5, 10, 10); got != "[=====-----]" {
		t.Fatalf("renderBar() = %q", got)
	}
	if got := humanBytes(2 * 1024 * 1024); got != "2.0 MiB" {
		t.Fatalf("humanBytes() = %q", got)
	}
	if got := humanBytes(2 * 1024); got != "2 KiB" {
		t.Fatalf("humanBytes() = %q", got)
	}

	count := formatCountProgress("Indexing", 3, 10)
	if !strings.Contains(count, "3/10") {
		t.Fatalf("formatCountProgress() = %q", count)
	}

	bytesProgress := formatBytesProgress("Downloading", 512*1024, 1024*1024)
	if !strings.Contains(bytesProgress, "50%") || !strings.Contains(bytesProgress, "512 KiB/1.0 MiB") {
		t.Fatalf("formatBytesProgress() = %q", bytesProgress)
	}
}

func TestTerminalUIRenderAndClear(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ui := newTerminalUI(&out)

	ui.Status("Working")
	ui.Finish("Done")
	ui.Status("Again")
	ui.Clear()

	got := out.String()
	for _, want := range []string{"Working", "Done", "Again"} {
		if !strings.Contains(got, want) {
			t.Fatalf("terminal output is missing %q in %q", want, got)
		}
	}
}

func TestSessionLifecycleAndProgressTracker(t *testing.T) {
	localDir := t.TempDir()

	s, err := NewSession(localDir)
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	s.Logf("custom log line %d", 7)
	s.CountProgress("Indexing", 1, 2)
	s.Status("Preparing")
	s.Finish("Finished")

	reader := s.ProgressTracker("Downloading").TrackProgress(
		"source",
		0,
		3,
		io.NopCloser(strings.NewReader("abc")),
	)
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("ReadAll(progress reader) error: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("progress reader Close() error: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Session.Close() error: %v", err)
	}

	logPath := filepath.Join(localDir, "logs", "run.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	logText := string(data)
	for _, want := range []string{
		"session started",
		"custom log line 7",
		"Downloading started",
		"Downloading finished",
		"session finished",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log output is missing %q in %q", want, logText)
		}
	}
}
