package cli

import (
	"context"
	"fmt"
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
