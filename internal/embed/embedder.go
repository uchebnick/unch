package embed

import "github.com/uchebnick/unch/internal/indexing"

type Embedder interface {
	Close()
	Dim() int
	EmbedQuery(text string) ([]float32, error)
	IndexedSymbolHash(path string, symbol indexing.IndexedSymbol) string
	EmbedIndexedSymbol(path string, symbol indexing.IndexedSymbol) ([]float32, error)
}
