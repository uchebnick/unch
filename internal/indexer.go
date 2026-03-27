package internal

// @filectx: File walker and marker extractor for semantic indexing comments inside repository files.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

type IndexedComment struct {
	Line          int
	Text          string
	FollowingText string
}

type Indexer struct {
	embedder *Embedder
	repo     *Repository
}

// @search: IndexDirectory and collectJobs use the same skip rules and comment extraction behavior.
func NewIndexer(embedder *Embedder, repo *Repository) *Indexer {
	return &Indexer{
		embedder: embedder,
		repo:     repo,
	}
}

func (i *Indexer) IndexDirectory(
	ctx context.Context,
	root string,
	contextPrefix string,
	commentPrefix string,
	excludePatterns []string,
	gitignorePath ...string,
) error {
	root = filepath.Clean(root)

	resolvedGitignorePath, err := resolveGitignorePath(root, gitignorePath...)
	if err != nil {
		return fmt.Errorf("resolve gitignore path: %w", err)
	}

	matcher, err := buildIgnoreMatcher(resolvedGitignorePath, excludePatterns)
	if err != nil {
		return fmt.Errorf("build ignore matcher: %w", err)
	}

	workingVersion, err := i.repo.WorkingVersion(ctx)
	if err != nil {
		return fmt.Errorf("get working version: %w", err)
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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

		if err := i.IndexFile(ctx, path, contextPrefix, commentPrefix, workingVersion); err != nil {
			return fmt.Errorf("index file %s: %w", path, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk dir: %w", err)
	}

	if err := i.repo.ActivateVersion(ctx, workingVersion); err != nil {
		return fmt.Errorf("activate version: %w", err)
	}

	if err := i.repo.CleanupOldVersions(ctx, workingVersion); err != nil {
		return fmt.Errorf("cleanup old versions: %w", err)
	}

	return nil
}

func resolveGitignorePath(root string, gitignorePath ...string) (string, error) {
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

// @search: .semsearch, .git, and README files are skipped during walking to avoid indexing runtime files, databases, logs, downloaded libraries, and documentation examples.
func shouldSkipIndexedPath(rel string) bool {
	rel = strings.Trim(strings.TrimSpace(filepath.ToSlash(rel)), "/")
	if rel == "" || rel == "." {
		return false
	}

	base := strings.ToLower(strings.TrimSpace(filepath.Base(rel)))
	if strings.HasPrefix(base, "readme") {
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

// @search: ExtractPrefixedBlocks reads lines like // @search: and // @filectx: so searchable notes can live in normal source comments next to functions and types.
// @search: binary files and files with extremely long lines are skipped so repository indexing does not fail on compiled artifacts or generated bundles.
func ExtractPrefixedBlocks(path string, searchPrefix string, ctxPrefix string) ([]IndexedComment, string, error) {
	const indexedTrailingLines = 10

	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	isBinary, err := looksLikeBinaryFile(file)
	if err != nil {
		return nil, "", err
	}
	if isBinary {
		return nil, "", nil
	}

	var comments []IndexedComment
	var commentsContext strings.Builder
	var lines []string

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineN := 0

	for scanner.Scan() {
		lineN++

		line := scanner.Text()
		lines = append(lines, line)

		if payload, ok := extractDirectivePayload(line, searchPrefix); ok {
			if payload != "" {
				comments = append(comments, IndexedComment{Line: lineN, Text: payload})
			}
			continue
		}

		if payload, ok := extractDirectivePayload(line, ctxPrefix); ok {
			if payload != "" {
				comments = append(comments, IndexedComment{
					Line: lineN,
					Text: payload,
				})
				if commentsContext.Len() > 0 {
					commentsContext.WriteByte('\n')
				}
				commentsContext.WriteString(payload)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if err == bufio.ErrTooLong {
			return nil, "", nil
		}
		return nil, "", err
	}

	for idx := range comments {
		comments[idx].FollowingText = collectFollowingLines(lines, comments[idx].Line, indexedTrailingLines)
	}

	return comments, commentsContext.String(), nil
}

func collectFollowingLines(lines []string, line int, limit int) string {
	if limit <= 0 || line <= 0 || line >= len(lines) {
		return ""
	}

	start := line
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}
	if start >= end {
		return ""
	}

	return strings.Join(lines[start:end], "\n")
}

func extractDirectivePayload(line string, prefix string) (string, bool) {
	candidate := normalizeDirectiveLine(line)
	if candidate == "" {
		return "", false
	}

	if payload, ok := matchDirectivePrefix(candidate, prefix); ok {
		return payload, true
	}

	trimmedPrefix := strings.TrimSuffix(strings.TrimSpace(prefix), ":")
	if trimmedPrefix != "" && trimmedPrefix != prefix {
		return matchDirectivePrefix(candidate, trimmedPrefix)
	}

	return "", false
}

func matchDirectivePrefix(candidate string, prefix string) (string, bool) {
	if !strings.HasPrefix(candidate, prefix) {
		return "", false
	}

	payload := strings.TrimSpace(strings.TrimPrefix(candidate, prefix))
	if strings.HasPrefix(payload, ":") {
		payload = strings.TrimSpace(strings.TrimPrefix(payload, ":"))
	}
	payload = strings.TrimSpace(strings.TrimSuffix(payload, "*/"))
	return payload, true
}

func normalizeDirectiveLine(line string) string {
	candidate := strings.TrimSpace(line)
	for {
		updated := strings.TrimSpace(strings.TrimSuffix(candidate, "*/"))

		switch {
		case strings.HasPrefix(updated, "//"):
			candidate = strings.TrimSpace(strings.TrimPrefix(updated, "//"))
		case strings.HasPrefix(updated, "/*"):
			candidate = strings.TrimSpace(strings.TrimPrefix(updated, "/*"))
		case strings.HasPrefix(updated, "*"):
			candidate = strings.TrimSpace(strings.TrimPrefix(updated, "*"))
		case strings.HasPrefix(updated, "#"):
			candidate = strings.TrimSpace(strings.TrimPrefix(updated, "#"))
		case strings.HasPrefix(updated, "--"):
			candidate = strings.TrimSpace(strings.TrimPrefix(updated, "--"))
		case strings.HasPrefix(updated, ";"):
			candidate = strings.TrimSpace(strings.TrimPrefix(updated, ";"))
		default:
			return updated
		}
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

func (i *Indexer) IndexFile(
	ctx context.Context,
	path string,
	contextPrefix string,
	commentPrefix string,
	workingVersion int64,
) error {
	comments, commentsContext, err := ExtractPrefixedBlocks(path, commentPrefix, contextPrefix)
	if err != nil {
		return fmt.Errorf("extract blocks: %w", err)
	}

	for _, comment := range comments {
		if err := i.IndexComment(ctx, path, comment.Line, comment.Text, comment.FollowingText, workingVersion, commentsContext); err != nil {
			return fmt.Errorf("index comment at %s:%d: %w", path, comment.Line, err)
		}
	}

	return nil
}

func (i *Indexer) IndexComment(
	ctx context.Context,
	path string,
	line int,
	comment string,
	followingText string,
	workingVersion int64,
	commentContext string,
) error {
	documentInput := formatIndexedCommentDocument(path, comment, commentContext, followingText)
	hash := HashComment("embedding_doc_format:" + embeddingDocFormatVersion + "\n" + documentInput)

	exists, err := i.repo.EmbeddingExists(ctx, hash)
	if err != nil {
		return fmt.Errorf("check embedding exists: %w", err)
	}

	if !exists {
		vec, err := i.embedder.Embed(documentInput)
		if err != nil {
			return fmt.Errorf("embed comment: %w", err)
		}

		if err := i.repo.AddEmbedding(ctx, hash, vec); err != nil {
			return fmt.Errorf("store embedding: %w", err)
		}
	}

	if err := i.repo.UpsertComment(ctx, path, line, hash, workingVersion); err != nil {
		return fmt.Errorf("upsert comment: %w", err)
	}

	return nil
}
