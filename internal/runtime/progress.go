package runtime

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type ProgressTracker interface {
	TrackProgress(src string, currentSize, totalSize int64, stream io.ReadCloser) io.ReadCloser
}

var defaultProgressTracker ProgressTracker = &stdoutProgressTracker{}

type stdoutProgressTracker struct{}

func (t *stdoutProgressTracker) TrackProgress(src string, currentSize, totalSize int64, stream io.ReadCloser) io.ReadCloser {
	return &stdoutProgressReader{
		src:        src,
		current:    currentSize,
		total:      totalSize,
		started:    time.Now(),
		lastDrawn:  time.Time{},
		ReadCloser: stream,
	}
}

type stdoutProgressReader struct {
	src string

	current   int64
	total     int64
	started   time.Time
	lastDrawn time.Time

	io.ReadCloser
}

func (r *stdoutProgressReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.current += int64(n)

	now := time.Now()
	if now.Sub(r.lastDrawn) >= 250*time.Millisecond || err == io.EOF {
		_, _ = fmt.Fprintf(os.Stdout, "\r\x1b[K%s", formatDownloadProgress(r.src, r.current, r.total))
		r.lastDrawn = now
	}

	return n, err
}

func (r *stdoutProgressReader) Close() error {
	_, _ = fmt.Fprintf(os.Stdout, "\r\x1b[K%s\n", formatDownloadProgress(r.src, r.current, r.total))
	return r.ReadCloser.Close()
}

func formatDownloadProgress(src string, current, total int64) string {
	label := filepathLabel(src)
	if total <= 0 {
		return fmt.Sprintf("downloading %s... %s", label, humanDownloadBytes(current))
	}
	if total < current {
		total = current
	}
	percent := 0
	if total > 0 {
		percent = int((current * 100) / total)
	}
	return fmt.Sprintf("downloading %s... %d%% (%s/%s)", label, percent, humanDownloadBytes(current), humanDownloadBytes(total))
}

func filepathLabel(src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return "file"
	}
	for len(src) > 1 && strings.HasSuffix(src, "/") {
		src = strings.TrimSuffix(src, "/")
	}
	if idx := strings.LastIndex(src, "/"); idx >= 0 && idx+1 < len(src) {
		return src[idx+1:]
	}
	return src
}

func humanDownloadBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%d KiB", size/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(size)/(1024*1024))
}
