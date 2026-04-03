package bench

import (
	"testing"
	"time"
)

func TestScoreQueryExactHitAtRank1(t *testing.T) {
	t.Parallel()

	metrics := ScoreQuery([]string{"mux.go:32"}, []SearchHit{
		{Rank: 1, Path: "mux.go", Line: 32},
	})

	if !metrics.Top1Success || !metrics.Top3Success || metrics.RR != 1 || metrics.ObservedRank != 1 {
		t.Fatalf("ScoreQuery(rank1) = %+v", metrics)
	}
}

func TestScoreQueryExactHitAtRank3(t *testing.T) {
	t.Parallel()

	metrics := ScoreQuery([]string{"mux.go:32"}, []SearchHit{
		{Rank: 1, Path: "other.go", Line: 1},
		{Rank: 2, Path: "other.go", Line: 2},
		{Rank: 3, Path: "mux.go", Line: 32},
	})

	if metrics.Top1Success {
		t.Fatalf("expected top1=false, got %+v", metrics)
	}
	if !metrics.Top3Success {
		t.Fatalf("expected top3=true, got %+v", metrics)
	}
	if metrics.RR != 1.0/3.0 {
		t.Fatalf("expected rr=1/3, got %+v", metrics)
	}
	if metrics.ObservedRank != 3 {
		t.Fatalf("expected observed rank 3, got %+v", metrics)
	}
}

func TestScoreQueryHitOutsideTop10(t *testing.T) {
	t.Parallel()

	metrics := ScoreQuery([]string{"mux.go:32"}, []SearchHit{
		{Rank: 11, Path: "mux.go", Line: 32},
	})

	if metrics.Top1Success || metrics.Top3Success || metrics.RR != 0 {
		t.Fatalf("ScoreQuery(outside top10) = %+v", metrics)
	}
	if metrics.ObservedRank != 0 {
		t.Fatalf("expected observed rank 0, got %+v", metrics)
	}
}

func TestScoreQuerySupportsMultipleExpectedHits(t *testing.T) {
	t.Parallel()

	metrics := ScoreQuery([]string{"a.go:10", "b.go:20"}, []SearchHit{
		{Rank: 2, Path: "b.go", Line: 20},
	})

	if metrics.Top1Success {
		t.Fatalf("expected top1=false, got %+v", metrics)
	}
	if !metrics.Top3Success {
		t.Fatalf("expected top3=true, got %+v", metrics)
	}
	if metrics.RR != 0.5 {
		t.Fatalf("expected rr=0.5, got %+v", metrics)
	}
	if metrics.ObservedRank != 2 {
		t.Fatalf("expected observed rank 2, got %+v", metrics)
	}
}

func TestAggregateQueryMetrics(t *testing.T) {
	t.Parallel()

	metrics := AggregateQueryMetrics([]QueryMetrics{
		{Top1Success: true, Top3Success: true, RR: 1},
		{Top1Success: false, Top3Success: true, RR: 0.5},
		{Top1Success: false, Top3Success: false, RR: 0},
	})

	if metrics.Top1 != 1.0/3.0 {
		t.Fatalf("AggregateQueryMetrics().Top1 = %v", metrics.Top1)
	}
	if metrics.Top3 != 2.0/3.0 {
		t.Fatalf("AggregateQueryMetrics().Top3 = %v", metrics.Top3)
	}
	if metrics.MRR != 0.5 {
		t.Fatalf("AggregateQueryMetrics().MRR = %v", metrics.MRR)
	}
	if metrics.QualityScore != 45 {
		t.Fatalf("AggregateQueryMetrics().QualityScore = %d", metrics.QualityScore)
	}
}

func TestMeanDuration(t *testing.T) {
	t.Parallel()

	got := meanDuration([]time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond})
	if got != 200*time.Millisecond {
		t.Fatalf("meanDuration() = %v", got)
	}
}
