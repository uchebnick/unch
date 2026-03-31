package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hybridgroup/yzma/pkg/llama"
	llamaembed "github.com/uchebnick/unch-searcher/internal/embed/llama"
	"github.com/uchebnick/unch-searcher/internal/termui"
)

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func formatSearchResultPath(root string, target string) string {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return target
	}
	if rel == "." {
		return rel
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return target
	}
	return filepath.ToSlash(rel)
}

func printSessionLine(s *termui.Session, format string, args ...any) {
	if s != nil {
		s.Clear()
		s.Logf(format, args...)
	}
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func confirmRemoteReindex(s *termui.Session, in io.Reader, out io.Writer, interactive bool) (bool, error) {
	if !interactive {
		return true, nil
	}

	if s != nil {
		s.Clear()
	}

	reader := bufio.NewReader(in)
	for {
		if _, err := fmt.Fprint(out, "Continue with local reindex and detach remote CI? [yes/no]: "); err != nil {
			return false, fmt.Errorf("write confirmation prompt: %w", err)
		}

		answer, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, fmt.Errorf("read confirmation: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "yes", "y":
			return true, nil
		case "", "no", "n":
			return false, nil
		default:
			if _, writeErr := fmt.Fprintln(out, "Please answer yes or no."); writeErr != nil {
				return false, fmt.Errorf("write confirmation hint: %w", writeErr)
			}
			if errors.Is(err, io.EOF) {
				return false, nil
			}
		}
	}
}

func isCharDevice(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func loadEmbedderWithSpinner(ctx context.Context, s *termui.Session, cfg llamaembed.Config) (*llamaembed.Embedder, error) {
	if s == nil || !s.Interactive() {
		embedder, err := llamaembed.New(cfg)
		if err != nil {
			if s != nil {
				s.Clear()
			}
			return nil, fmt.Errorf("load embedder: %w", err)
		}
		if s != nil {
			s.Finish(fmt.Sprintf("Loaded model       dim=%d", embedder.Dim()))
		}
		return embedder, nil
	}

	done := make(chan struct{})

	go func() {
		frames := []rune{'|', '/', '-', '\\'}
		idx := 0
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			default:
				s.Status(fmt.Sprintf("Loading model      %c", frames[idx%len(frames)]))
				time.Sleep(120 * time.Millisecond)
				idx++
			}
		}
	}()

	embedder, err := llamaembed.New(cfg)
	close(done)

	if err != nil {
		s.Clear()
		return nil, fmt.Errorf("load embedder: %w", err)
	}

	s.Finish(fmt.Sprintf("Loaded model       dim=%d", embedder.Dim()))
	return embedder, nil
}

func defaultPooling() llama.PoolingType {
	return llama.PoolingTypeMean
}
