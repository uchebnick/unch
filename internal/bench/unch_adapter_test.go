package bench

import "testing"

func TestParseSearchHits(t *testing.T) {
	t.Parallel()

	stdout := " 1. mux.go:32  0.7747\n 2. route.go:177  0.8123\n"
	hits, err := parseSearchHits(stdout, stdout)
	if err != nil {
		t.Fatalf("parseSearchHits() error: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("parseSearchHits() hits = %d", len(hits))
	}
	if hits[0].Rank != 1 || hits[0].Path != "mux.go" || hits[0].Line != 32 {
		t.Fatalf("unexpected first hit: %+v", hits[0])
	}
}

func TestParseSearchHitsNoMatches(t *testing.T) {
	t.Parallel()

	hits, err := parseSearchHits("", "Loaded model       dim=768\nNo matches found\n")
	if err != nil {
		t.Fatalf("parseSearchHits() error: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("parseSearchHits() hits = %+v, want empty", hits)
	}
}

func TestParseIndexSummary(t *testing.T) {
	t.Parallel()

	summary, indexedSymbols, indexedFiles, err := parseIndexSummary("Loaded model dim=768\nIndexed 278 symbols in 16 files\n")
	if err != nil {
		t.Fatalf("parseIndexSummary() error: %v", err)
	}
	if summary != "Indexed 278 symbols in 16 files" {
		t.Fatalf("parseIndexSummary() summary = %q", summary)
	}
	if indexedSymbols != 278 || indexedFiles != 16 {
		t.Fatalf("parseIndexSummary() = (%d, %d)", indexedSymbols, indexedFiles)
	}
}

func TestParseIndexSummaryUpToDate(t *testing.T) {
	t.Parallel()

	summary, indexedSymbols, indexedFiles, err := parseIndexSummary("Loaded model dim=768\nIndex already up to date\n")
	if err != nil {
		t.Fatalf("parseIndexSummary() error: %v", err)
	}
	if summary != indexUpToDateSummary {
		t.Fatalf("parseIndexSummary() summary = %q", summary)
	}
	if indexedSymbols != 0 || indexedFiles != 0 {
		t.Fatalf("parseIndexSummary() = (%d, %d)", indexedSymbols, indexedFiles)
	}
}
