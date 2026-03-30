package termui

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	getter "github.com/hashicorp/go-getter"
)

type Session struct {
	log     *log.Logger
	logFile *os.File
	logPath string
	started time.Time
	ui      *terminalUI
}

func NewSession(localDir string) (*Session, error) {
	logsDir := filepath.Join(localDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	logPath := filepath.Join(logsDir, "run.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	s := &Session{
		log:     log.New(logFile, "", log.LstdFlags|log.Lmicroseconds),
		logFile: logFile,
		logPath: logPath,
		started: time.Now(),
		ui:      newTerminalUI(os.Stderr),
	}
	s.Logf("session started")
	return s, nil
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}

	s.ui.Clear()
	s.Logf("session finished in %s", time.Since(s.started).Round(time.Millisecond))

	if s.logFile != nil {
		return s.logFile.Close()
	}
	return nil
}

func (s *Session) Logf(format string, args ...any) {
	if s != nil && s.log != nil {
		s.log.Printf(format, args...)
	}
}

func (s *Session) ProgressTracker(label string) getter.ProgressTracker {
	return &uiProgressTracker{
		label: label,
		ui:    s.ui,
		logf:  s.Logf,
	}
}

func (s *Session) CountProgress(label string, current, total int) {
	s.ui.Status(formatCountProgress(label, int64(current), int64(total)))
}

func (s *Session) Finish(message string) {
	s.ui.Finish(message)
}

func (s *Session) Status(message string) {
	s.ui.Status(message)
}

func (s *Session) Clear() {
	s.ui.Clear()
}

func (s *Session) Interactive() bool {
	return s != nil && s.ui != nil && s.ui.interactive
}

type terminalUI struct {
	mu          sync.Mutex
	out         io.Writer
	lastLen     int
	interactive bool
}

func newTerminalUI(out io.Writer) *terminalUI {
	interactive := false
	if file, ok := out.(*os.File); ok {
		if info, err := file.Stat(); err == nil {
			interactive = info.Mode()&os.ModeCharDevice != 0
		}
	}
	return &terminalUI{out: out, interactive: interactive}
}

func (u *terminalUI) Status(message string) {
	u.render(message, false)
}

func (u *terminalUI) Finish(message string) {
	u.render(message, true)
}

func (u *terminalUI) Clear() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.lastLen == 0 {
		return
	}

	fmt.Fprintf(u.out, "\r%s\r", strings.Repeat(" ", u.lastLen))
	u.lastLen = 0
}

func (u *terminalUI) render(message string, newline bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	padding := ""
	if diff := u.lastLen - len(message); diff > 0 {
		padding = strings.Repeat(" ", diff)
	}

	fmt.Fprintf(u.out, "\r%s%s", message, padding)
	if newline {
		fmt.Fprintln(u.out)
		u.lastLen = 0
		return
	}

	u.lastLen = len(message)
}

type uiProgressTracker struct {
	label string
	ui    *terminalUI
	logf  func(string, ...any)
}

func (t *uiProgressTracker) TrackProgress(src string, currentSize, totalSize int64, stream io.ReadCloser) io.ReadCloser {
	if t.logf != nil {
		t.logf("%s started: src=%s total=%d", t.label, src, totalSize)
	}

	return &uiProgressReader{
		label:      t.label,
		ui:         t.ui,
		logf:       t.logf,
		src:        src,
		current:    currentSize,
		total:      totalSize,
		started:    time.Now(),
		lastDrawn:  time.Time{},
		ReadCloser: stream,
	}
}

type uiProgressReader struct {
	label string
	ui    *terminalUI
	logf  func(string, ...any)
	src   string

	current   int64
	total     int64
	started   time.Time
	lastDrawn time.Time

	io.ReadCloser
}

func (r *uiProgressReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.current += int64(n)

	now := time.Now()
	if now.Sub(r.lastDrawn) >= 250*time.Millisecond || err == io.EOF {
		r.ui.Status(formatBytesProgress(r.label, r.current, r.total))
		r.lastDrawn = now
	}

	return n, err
}

func (r *uiProgressReader) Close() error {
	r.ui.Finish(formatBytesProgress(r.label, r.current, r.total))
	if r.logf != nil {
		r.logf("%s finished in %s: src=%s bytes=%d", r.label, time.Since(r.started).Round(time.Millisecond), r.src, r.current)
	}
	return r.ReadCloser.Close()
}

func formatCountProgress(label string, current, total int64) string {
	return fmt.Sprintf("%-18s %s %d/%d", label, renderBar(current, total, 28), current, total)
}

func formatBytesProgress(label string, current, total int64) string {
	if total <= 0 {
		return fmt.Sprintf("%-18s %s", label, humanBytes(current))
	}

	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}

	percent := current * 100 / total
	return fmt.Sprintf("%-18s %s %3d%% %s/%s", label, renderBar(current, total, 28), percent, humanBytes(current), humanBytes(total))
}

func renderBar(current, total int64, width int) string {
	if width <= 0 {
		width = 20
	}
	if total <= 0 {
		return "[" + strings.Repeat("-", width) + "]"
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}

	filled := int(current * int64(width) / total)
	return "[" + strings.Repeat("=", filled) + strings.Repeat("-", width-filled) + "]"
}

func humanBytes(n int64) string {
	const unit = 1024 * 1024
	if n < unit {
		return fmt.Sprintf("%d KiB", n/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/unit)
}
