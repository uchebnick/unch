package bench

import "testing"

func TestLatestIndexedSnapshotFallsBackToLastCountedRun(t *testing.T) {
	t.Parallel()

	report := RepositoryReport{
		ColdIndexRuns: []IndexRunReport{
			{Summary: "Indexed 278 symbols in 16 files", IndexedSymbols: 278, IndexedFiles: 16},
		},
		WarmIndexRuns: []IndexRunReport{
			{Summary: indexUpToDateSummary},
		},
	}

	latest, ok := LatestIndexedSnapshot(report)
	if !ok {
		t.Fatal("LatestIndexedSnapshot() ok = false")
	}
	if latest.IndexedSymbols != 278 || latest.IndexedFiles != 16 {
		t.Fatalf("LatestIndexedSnapshot() = %+v", latest)
	}
}
