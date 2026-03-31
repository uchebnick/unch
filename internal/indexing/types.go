package indexing

import (
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
)

type IndexedComment struct {
	Line          int
	Text          string
	FollowingText string
}

// IndexedSymbol is the normalized search unit produced by Tree-sitter extraction
// or by the legacy prefix fallback.
type IndexedSymbol struct {
	Line          int
	Kind          string
	Name          string
	Container     string
	QualifiedName string
	Signature     string
	Documentation string
	Body          string
	FileContext   string
}

// StableID returns a deterministic identifier for a symbol within a file so the
// index can update moved or rewritten declarations without depending on line numbers.
func (s IndexedSymbol) StableID() string {
	payload := strings.Join([]string{
		strings.TrimSpace(strings.ToLower(s.Kind)),
		strings.TrimSpace(s.QualifiedName),
		strings.TrimSpace(s.Signature),
	}, "\n")
	if payload == "\n\n" {
		payload = strings.TrimSpace(s.Name) + "\n" + strconv.Itoa(s.Line)
	}

	sum := xxhash.Sum64String(payload)
	var buf [8]byte
	buf[0] = byte(sum >> 56)
	buf[1] = byte(sum >> 48)
	buf[2] = byte(sum >> 40)
	buf[3] = byte(sum >> 32)
	buf[4] = byte(sum >> 24)
	buf[5] = byte(sum >> 16)
	buf[6] = byte(sum >> 8)
	buf[7] = byte(sum)
	return hex.EncodeToString(buf[:])
}

// SearchText flattens the symbol into a plain-text document suitable for lexical ranking.
func (s IndexedSymbol) SearchText() string {
	var parts []string
	for _, value := range []string{
		s.Kind,
		s.Name,
		s.Container,
		s.QualifiedName,
		s.Signature,
		s.Documentation,
		s.FileContext,
		s.Body,
	} {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n")
}

// FileJob describes one source file and the symbols extracted from it during a scan.
type FileJob struct {
	Path       string
	SourcePath string
	Symbols    []IndexedSymbol
}
