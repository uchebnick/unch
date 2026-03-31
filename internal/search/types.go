package search

// SearchResult is the stored symbol metadata returned from lexical or semantic search.
type SearchResult struct {
	Path          string
	Line          int
	SymbolID      string
	Kind          string
	Name          string
	Container     string
	QualifiedName string
	Signature     string
	Documentation string
	Body          string
	Distance      float64
}
