package embed

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/uchebnick/unch/internal/indexing"
	"github.com/uchebnick/unch/internal/modelcatalog"
)

const (
	embeddingGemmaRetrievalQueryPrefix = "task: code retrieval | query: "
	embeddingGemmaDocumentPrefix       = "title: %s | text: %s"

	qwen3CodeRetrievalInstruction = "Given a code search query, retrieve relevant code symbols and documentation that answer the query."
)

type Formatter interface {
	ProfileRevision() string
	FormatQuery(text string) string
	FormatIndexedSymbolDocument(path string, symbol indexing.IndexedSymbol) string
}

type genericFormatter struct{}

type embeddingGemmaFormatter struct{}

type qwen3Formatter struct{}

func FormatterForModel(model string) Formatter {
	if target, ok := modelcatalog.ResolveInstallTarget(model); ok {
		return formatterForTargetID(target.ID)
	}
	if target, ok := modelcatalog.RecognizeInstallTargetForPath(model); ok {
		return formatterForTargetID(target.ID)
	}

	token := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(token, "qwen3") && strings.Contains(token, "embed"):
		return qwen3Formatter{}
	case strings.Contains(token, "embeddinggemma"), strings.Contains(token, "gemma"):
		return embeddingGemmaFormatter{}
	default:
		return genericFormatter{}
	}
}

func IndexedSymbolHash(formatter Formatter, path string, symbol indexing.IndexedSymbol) string {
	document := formatter.FormatIndexedSymbolDocument(path, symbol)
	return hashNormalizedText("embedding_doc_format:" + formatter.ProfileRevision() + "\n" + document)
}

func normalizeText(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func (genericFormatter) ProfileRevision() string {
	return "generic-v1"
}

func (genericFormatter) FormatQuery(text string) string {
	return normalizeText(text)
}

func (genericFormatter) FormatIndexedSymbolDocument(path string, symbol indexing.IndexedSymbol) string {
	title, body := indexedSymbolDocumentParts(path, symbol)
	return formatGenericDocument(title, body)
}

func (embeddingGemmaFormatter) ProfileRevision() string {
	return "embeddinggemma-v4"
}

func (embeddingGemmaFormatter) FormatQuery(text string) string {
	return embeddingGemmaRetrievalQueryPrefix + normalizeText(text)
}

func (embeddingGemmaFormatter) FormatIndexedSymbolDocument(path string, symbol indexing.IndexedSymbol) string {
	title, body := indexedSymbolDocumentParts(path, symbol)
	return fmt.Sprintf(embeddingGemmaDocumentPrefix, normalizeDocumentTitle(title), normalizeText(body))
}

func (qwen3Formatter) ProfileRevision() string {
	return "qwen3-embedding-v1"
}

func (qwen3Formatter) FormatQuery(text string) string {
	return formatQwen3Query(qwen3CodeRetrievalInstruction, text)
}

func (qwen3Formatter) FormatIndexedSymbolDocument(path string, symbol indexing.IndexedSymbol) string {
	title, body := indexedSymbolDocumentParts(path, symbol)
	return formatGenericDocument(title, body)
}

func formatterForTargetID(targetID string) Formatter {
	switch targetID {
	case "qwen3":
		return qwen3Formatter{}
	case "embeddinggemma":
		return embeddingGemmaFormatter{}
	default:
		return genericFormatter{}
	}
}

func indexedSymbolDocumentParts(path string, symbol indexing.IndexedSymbol) (string, string) {
	kind := normalizeText(symbol.Kind)
	name := normalizeText(symbol.Name)
	qualifiedName := normalizeText(symbol.QualifiedName)
	signature := normalizeText(symbol.Signature)
	documentation := normalizeText(symbol.Documentation)
	fileContext := normalizeText(symbol.FileContext)
	bodyText := normalizeText(symbol.Body)

	var body strings.Builder
	body.WriteString("Path: ")
	body.WriteString(path)
	if kind != "" {
		body.WriteString("\nKind: ")
		body.WriteString(kind)
	}
	if name != "" {
		body.WriteString("\nName: ")
		body.WriteString(name)
	}
	if qualifiedName != "" && qualifiedName != name {
		body.WriteString("\nQualified name: ")
		body.WriteString(qualifiedName)
	}
	if signature != "" {
		body.WriteString("\nSignature:\n")
		body.WriteString(signature)
	}
	if documentation != "" {
		body.WriteString("\nDocumentation:\n")
		body.WriteString(documentation)
	}
	if fileContext != "" {
		body.WriteString("\nFile context:\n")
		body.WriteString(fileContext)
	}
	if bodyText != "" {
		body.WriteString("\nBody snippet:\n")
		body.WriteString(bodyText)
	}

	title := normalizeDocumentTitle(strings.TrimSpace(filepath.Base(path)))
	if qualifiedName != "" {
		title = normalizeDocumentTitle(strings.TrimSpace(title + " " + qualifiedName))
	}
	if title == "" || title == "." || title == string(filepath.Separator) {
		title = "symbol"
	}

	return title, body.String()
}

func formatQwen3Query(instruction string, query string) string {
	return fmt.Sprintf("Instruct: %s\nQuery: %s", normalizeText(instruction), normalizeText(query))
}

func formatGenericDocument(title string, text string) string {
	title = normalizeDocumentTitle(title)
	text = normalizeText(text)

	switch {
	case title == "" && text == "":
		return ""
	case title == "":
		return text
	case text == "":
		return "Title: " + title
	default:
		return "Title: " + title + "\n" + text
	}
}

func normalizeDocumentTitle(title string) string {
	title = normalizeText(title)
	title = strings.ReplaceAll(title, "|", "/")
	if title == "" {
		return "none"
	}
	return title
}

func hashNormalizedText(text string) string {
	sum := xxhash.Sum64String(normalizeText(text))
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
