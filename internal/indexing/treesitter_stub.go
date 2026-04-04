//go:build !cgo

package indexing

// Tree-sitter grammars are cgo-only, so the caller should fall back when cgo
// is unavailable in the current build configuration.
func extractTreeSitterSymbols(path string, source []byte) ([]IndexedSymbol, bool) {
	return nil, false
}
