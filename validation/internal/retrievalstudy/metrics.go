package retrievalstudy

import (
	"math"
	"sort"
)

func scoreRanking(candidates []Candidate, candidateCount int, gold []GoldSpan, complete bool, reason, granularity string, tokens []string) Ranking {
	if candidates == nil {
		candidates = []Candidate{}
	}
	ranking := Ranking{
		Status: "complete", Complete: complete, MetricsUsable: complete, IncompleteReason: reason,
		Candidates: candidates, CandidateCount: candidateCount,
		CandidateCountComplete: true, Granularity: granularity, Tokens: tokens,
	}
	if !complete {
		ranking.Status = "partial"
	}
	ranking.Metrics.At5, ranking.Misses = metricsAt(candidates, gold, 5)
	at10, missesAt10 := metricsAt(candidates, gold, 10)
	ranking.Metrics.At10 = at10
	ranking.Misses.At10 = missesAt10.At10
	ranking.Misses.ByTypeAt10 = missesAt10.ByTypeAt10
	return ranking
}

func metricsAt(candidates []Candidate, gold []GoldSpan, cutoff int) (CutoffMetrics, EvidenceMisses) {
	limit := cutoff
	if len(candidates) < limit {
		limit = len(candidates)
	}
	goldHit := make([]bool, len(gold))
	relevantCandidates := 0
	firstRelevantRank := 0
	for index := 0; index < limit; index++ {
		candidate := candidates[index]
		relevant := false
		for spanIndex, span := range gold {
			if candidate.Path == span.Path && candidate.Line >= span.StartLine && candidate.Line <= span.EndLine {
				goldHit[spanIndex] = true
				relevant = true
			}
		}
		if relevant {
			relevantCandidates++
			if firstRelevantRank == 0 {
				firstRelevantRank = index + 1
			}
		}
	}
	hitCount := 0
	misses := EvidenceMisses{GoldSpans: len(gold), ByTypeAt5: make(map[string]int), ByTypeAt10: make(map[string]int)}
	for index, span := range gold {
		if goldHit[index] {
			hitCount++
		} else {
			if cutoff == 5 {
				misses.ByTypeAt5[span.Type]++
			} else {
				misses.ByTypeAt10[span.Type]++
			}
		}
	}
	metric := CutoffMetrics{
		RetrievedCandidates: limit,
		RelevantCandidates:  relevantCandidates,
		GoldSpans:           len(gold),
		HitGoldSpans:        hitCount,
		Recall:              ratio(hitCount, len(gold)),
		Precision:           ratio(relevantCandidates, cutoff),
	}
	if firstRelevantRank != 0 {
		metric.MeanReciprocalRank = 1 / float64(firstRelevantRank)
	}
	missed := len(gold) - hitCount
	if cutoff == 5 {
		misses.At5 = missed
	} else {
		misses.At10 = missed
	}
	return metric, misses
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func latency(samples []float64) Latency {
	values := append([]float64(nil), samples...)
	if len(values) == 0 {
		return Latency{Samples: []float64{}}
	}
	sort.Float64s(values)
	return Latency{
		Samples: append([]float64(nil), samples...),
		P50MS:   percentile(values, 0.50),
		P95MS:   percentile(values, 0.95),
		MinMS:   values[0],
		MaxMS:   values[len(values)-1],
	}
}

func percentile(sorted []float64, quantile float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(quantile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func aggregateRankings(rankings []Ranking, cutoff int) AggregateMetrics {
	metric := AggregateMetrics{Queries: len(rankings)}
	macroRecall, macroPrecision, macroMRR := 0.0, 0.0, 0.0
	totalGold, totalHit, totalRelevant, totalReturned := 0, 0, 0, 0
	for _, ranking := range rankings {
		if !ranking.MetricsUsable {
			continue
		}
		metric.MetricsQueries++
		if ranking.Complete {
			metric.CompleteQueries++
		}
		cutoffMetric := ranking.Metrics.At10
		if cutoff == 5 {
			cutoffMetric = ranking.Metrics.At5
		}
		macroRecall += cutoffMetric.Recall
		macroPrecision += cutoffMetric.Precision
		macroMRR += cutoffMetric.MeanReciprocalRank
		totalGold += cutoffMetric.GoldSpans
		totalHit += cutoffMetric.HitGoldSpans
		totalRelevant += cutoffMetric.RelevantCandidates
		totalReturned += cutoff
	}
	if metric.MetricsQueries > 0 {
		denominator := float64(metric.MetricsQueries)
		metric.MacroRecall = macroRecall / denominator
		metric.MacroPrecision = macroPrecision / denominator
		metric.MacroMeanReciprocalRank = macroMRR / denominator
	}
	metric.MicroRecall = ratio(totalHit, totalGold)
	metric.MicroPrecision = ratio(totalRelevant, totalReturned)
	return metric
}
