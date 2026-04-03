package bench

import (
	"math"
	"time"
)

func ScoreQuery(expectedHits []string, hits []SearchHit) QueryMetrics {
	expected := make(map[string]struct{}, len(expectedHits))
	for _, hit := range expectedHits {
		expected[hit] = struct{}{}
	}

	metrics := QueryMetrics{}
	for _, hit := range hits {
		if hit.Rank > 10 {
			break
		}
		if _, ok := expected[hit.ExactRef()]; !ok {
			continue
		}

		metrics.Top1Success = hit.Rank == 1
		metrics.Top3Success = hit.Rank <= 3
		metrics.RR = 1.0 / float64(hit.Rank)
		metrics.ObservedRank = hit.Rank
		return metrics
	}

	return metrics
}

func AggregateQueryMetrics(metrics []QueryMetrics) AggregateMetrics {
	if len(metrics) == 0 {
		return AggregateMetrics{}
	}

	var top1Sum float64
	var top3Sum float64
	var rrSum float64
	for _, metric := range metrics {
		if metric.Top1Success {
			top1Sum++
		}
		if metric.Top3Success {
			top3Sum++
		}
		rrSum += metric.RR
	}

	top1 := top1Sum / float64(len(metrics))
	top3 := top3Sum / float64(len(metrics))
	mrr := rrSum / float64(len(metrics))

	return AggregateMetrics{
		Top1:         top1,
		Top3:         top3,
		MRR:          mrr,
		QualityScore: int(math.Round(100 * (0.5*top1 + 0.2*top3 + 0.3*mrr))),
	}
}

func meanDuration(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}

	var total time.Duration
	for _, value := range values {
		total += value
	}
	return total / time.Duration(len(values))
}

func durationMS(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}
