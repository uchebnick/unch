package indexing

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cespare/xxhash/v2"
	ignore "github.com/sabhiram/go-gitignore"
)

// FileScanner walks repository files and extracts symbols into index jobs.
type FileScanner struct {
	Root string
}

func (FileScanner) CollectJob(path string, rel string, source []byte, commentPrefix string, contextPrefix string) (FileJob, bool, error) {
	symbols, err := extractSymbolsForSource(path, source, commentPrefix, contextPrefix)
	if err != nil {
		return FileJob{}, false, fmt.Errorf("extract symbols from %s: %w", path, err)
	}
	job := FileJob{
		Path:       rel,
		SourcePath: path,
		Symbols:    symbols,
	}
	return job, len(symbols) > 0, nil
}

func extractSymbolsForSource(path string, source []byte, commentPrefix string, contextPrefix string) ([]IndexedSymbol, error) {
	if symbols, ok := extractTreeSitterSymbols(path, source); ok {
		return symbols, nil
	}

	return extractLegacySymbols(path, commentPrefix, contextPrefix)
}

func extractSymbolsForPath(path string, commentPrefix string, contextPrefix string) ([]IndexedSymbol, error) {
	source, binary, err := readSourceFile(path)
	if err != nil {
		return nil, err
	}
	if binary {
		return nil, nil
	}

	return extractSymbolsForSource(path, source, commentPrefix, contextPrefix)
}

func walkIndexedPaths(root string, gitignorePath string, extraPatterns []string, handleFile func(path string, rel string) error) error {
	matcher, err := buildIgnoreMatcher(gitignorePath, extraPatterns)
	if err != nil {
		return fmt.Errorf("build ignore matcher: %w", err)
	}

	return filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make relative path: %w", err)
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}

		if shouldSkipIndexedPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if matcher != nil && matcher.MatchesPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		return handleFile(path, rel)
	})
}

func walkSourceFiles(root string, gitignorePath string, extraPatterns []string, visit func(path string, rel string, source []byte) error) error {
	return walkIndexedPaths(root, gitignorePath, extraPatterns, func(path string, rel string) error {
		source, binary, err := readSourceFile(path)
		if err != nil {
			return err
		}
		if binary {
			return nil
		}

		return visit(path, rel, source)
	})
}

func readSourceFile(path string) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		_ = file.Close()
	}()

	binary, err := looksLikeBinaryFile(file)
	if err != nil {
		return nil, false, err
	}
	if binary {
		return nil, true, nil
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}

	return data, false, nil
}

func hashSourceFile(path string) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer func() {
		_ = file.Close()
	}()

	binary, err := looksLikeBinaryFile(file)
	if err != nil {
		return "", false, err
	}
	if binary {
		return "", true, nil
	}

	hasher := xxhash.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", false, fmt.Errorf("hash %s: %w", path, err)
	}

	return fmt.Sprintf("%016x", hasher.Sum64()), false, nil
}

// ResolveGitignorePath expands an optional gitignore path relative to the
// repository root and falls back to <root>/.gitignore when omitted.
func ResolveGitignorePath(root string, gitignorePath ...string) (string, error) {
	switch len(gitignorePath) {
	case 0:
		return filepath.Join(root, ".gitignore"), nil
	case 1:
		p := strings.TrimSpace(gitignorePath[0])
		if p == "" {
			return filepath.Join(root, ".gitignore"), nil
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		return filepath.Clean(p), nil
	default:
		return "", fmt.Errorf("expected at most one gitignore path, got %d", len(gitignorePath))
	}
}

func buildIgnoreMatcher(gitignorePath string, extraPatterns []string) (*ignore.GitIgnore, error) {
	_, err := os.Stat(gitignorePath)
	switch {
	case err == nil:
		if len(extraPatterns) > 0 {
			return ignore.CompileIgnoreFileAndLines(gitignorePath, extraPatterns...)
		}
		return ignore.CompileIgnoreFile(gitignorePath)
	case os.IsNotExist(err):
		if len(extraPatterns) == 0 {
			return nil, nil
		}
		return ignore.CompileIgnoreLines(extraPatterns...), nil
	default:
		return nil, err
	}
}

func shouldSkipIndexedPath(rel string) bool {
	rel = strings.Trim(strings.TrimSpace(filepath.ToSlash(rel)), "/")
	if rel == "" || rel == "." {
		return false
	}

	base := strings.ToLower(strings.TrimSpace(filepath.Base(rel)))
	if base == "readme" || strings.HasPrefix(base, "readme.") {
		return true
	}

	top := rel
	if idx := strings.IndexByte(top, '/'); idx >= 0 {
		top = top[:idx]
	}

	switch top {
	case ".git", ".semsearch":
		return true
	default:
		return false
	}
}

func looksLikeBinaryFile(file *os.File) (bool, error) {
	buf := make([]byte, 8192)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read probe: %w", err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("reset probe offset: %w", err)
	}

	return looksLikeBinary(buf[:n]), nil
}

func looksLikeBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}

	suspicious := 0
	for _, b := range data {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' && b != '\f' {
			suspicious++
		}
	}

	return suspicious*100/len(data) > 10
}

func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}
