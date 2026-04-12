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
	appembed "github.com/uchebnick/unch/internal/embed"
	llamaembed "github.com/uchebnick/unch/internal/embed/llama"
	appsearch "github.com/uchebnick/unch/internal/search"
	"github.com/uchebnick/unch/internal/termui"
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
	if !filepath.IsAbs(target) {
		return filepath.ToSlash(filepath.Clean(target))
	}

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

func compactSearchField(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 0 {
		return text
	}

	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func renderSearchResultCompact(w io.Writer, rank int, root string, result appsearch.Result) error {
	_, err := fmt.Fprintf(w, "%2d. %s:%d  %-7s\n", rank, formatSearchResultPath(root, result.Path), result.Line, result.DisplayMetric)
	return err
}

func renderSearchResultDetailed(w io.Writer, rank int, root string, result appsearch.Result) error {
	if _, err := fmt.Fprintf(w, "%2d. %s:%d  %s\n", rank, formatSearchResultPath(root, result.Path), result.Line, result.DisplayMetric); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "   kind: %s\n", compactSearchField(result.Kind, 80)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "   name: %s\n", compactSearchField(result.Name, 120)); err != nil {
		return err
	}
	if value := compactSearchField(result.QualifiedName, 160); value != "" {
		if _, err := fmt.Fprintf(w, "   qualified: %s\n", value); err != nil {
			return err
		}
	}
	if value := compactSearchField(result.Signature, 200); value != "" {
		if _, err := fmt.Fprintf(w, "   signature: %s\n", value); err != nil {
			return err
		}
	}
	if value := compactSearchField(result.Documentation, 220); value != "" {
		if _, err := fmt.Fprintf(w, "   docs: %s\n", value); err != nil {
			return err
		}
	}
	if value := compactSearchField(result.Body, 220); value != "" {
		if _, err := fmt.Fprintf(w, "   body: %s\n", value); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
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

func loadEmbedderWithSpinner(ctx context.Context, s *termui.Session, load func() (appembed.Embedder, error)) (appembed.Embedder, error) {
	if s == nil || !s.Interactive() {
		embedder, err := load()
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

	embedder, err := load()
	close(done)

	if err != nil {
		s.Clear()
		return nil, fmt.Errorf("load embedder: %w", err)
	}

	s.Finish(fmt.Sprintf("Loaded model       dim=%d", embedder.Dim()))
	return embedder, nil
}

func defaultPooling(modelPath string) llama.PoolingType {
	return llamaembed.DefaultPoolingForModelPath(modelPath)
}

func defaultContextSize(modelPath string) int {
	return llamaembed.DefaultContextSizeForModelPath(modelPath)
}
