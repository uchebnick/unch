package indexing

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func extractLegacySymbols(path string, searchPrefix string, ctxPrefix string) ([]IndexedSymbol, error) {
	comments, context, err := ExtractPrefixedBlocks(path, searchPrefix, ctxPrefix)
	if err != nil {
		return nil, err
	}

	symbols := make([]IndexedSymbol, 0, len(comments))
	for _, comment := range comments {
		if strings.TrimSpace(comment.Text) == "" {
			continue
		}

		name := fmt.Sprintf("annotation:%d", comment.Line)
		symbols = append(symbols, IndexedSymbol{
			Line:          comment.Line,
			Kind:          "annotation",
			Name:          name,
			QualifiedName: name,
			Documentation: strings.TrimSpace(comment.Text),
			Body:          strings.TrimSpace(comment.FollowingText),
			FileContext:   strings.TrimSpace(context),
		})
	}

	return symbols, nil
}

func ExtractPrefixedBlocks(path string, searchPrefix string, ctxPrefix string) ([]IndexedComment, string, error) {
	const indexedTrailingLines = 10

	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		_ = file.Close()
	}()

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
				comments = append(comments, IndexedComment{Line: lineN, Text: payload})
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
