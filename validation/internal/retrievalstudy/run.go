package retrievalstudy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/agentic-mcps/go/internal/intelligence/retrieval"
)

const retrievalGranularity = "go_declaration_name_anchor"
const candidatePoolAuditLimit = 10_000

type Options struct {
	Manifest       string
	RepositoryPath string
	SourceRepositoryPath string
	Output         string
	GoplsBinary    string
	TextAblation   bool
	Repetitions    int
	Timeout        time.Duration
	MaxSourceBytes int64
	MaxFileBytes   int64
	RGOutputBytes  int64
	RGLineLimit    int
}

func Execute(parent context.Context, options Options) error {
	if err := validateOptions(options); err != nil {
		return err
	}
	manifest, manifestSHA256, err := LoadManifest(options.Manifest)
	if err != nil {
		return err
	}
	if options.SourceRepositoryPath == "" {
		options.SourceRepositoryPath = "."
	}
	reproducibility, err := sourceIdentity(parent, options.SourceRepositoryPath, options.Timeout, manifestSHA256)
	if err != nil {
		return err
	}
	temporaryRoot, err := os.MkdirTemp("", "agentic-go-retrievalbench-*")
	if err != nil {
		return fmt.Errorf("creating temporary benchmark workspace: %w", err)
	}
	defer os.RemoveAll(temporaryRoot)
	workspace := filepath.Join(temporaryRoot, "snapshot")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		return fmt.Errorf("creating snapshot workspace: %w", err)
	}
	archiveStarted := time.Now()
	repository, source, err := ExportCommit(parent, options.RepositoryPath, workspace, manifest, archiveLimits{
		maxSourceBytes: options.MaxSourceBytes,
		maxFileBytes:   options.MaxFileBytes,
		timeout:        options.Timeout,
		indexTextCandidates: options.TextAblation,
	})
	archiveMS := float64(time.Since(archiveStarted)) / float64(time.Millisecond)
	if err != nil {
		return err
	}
	if err := validateGold(source, manifest); err != nil {
		return err
	}
	goPackages, err := inventoryGoPackages(parent, workspace, options.Timeout)
	if err != nil {
		return err
	}
	rgVersion, err := ProbeRG(parent, options.Timeout)
	if err != nil {
		return err
	}
	var initial runtime.MemStats
	runtime.ReadMemStats(&initial)
	peakHeap := initial.HeapAlloc

	goplsMeasurement := GoplsMeasurement{
		Status: "unavailable", UnavailableReason: "gopls baseline not run; provide --gopls with the pinned v0.21.0 binary",
		QueryLatency: latency(nil), Granularity: "workspace_symbol_anchor",
	}
	var session *goplsSession
	if options.GoplsBinary != "" {
		var version string
		var initializeMS float64
		session, version, initializeMS, err = startGopls(parent, options.GoplsBinary, workspace, options.Timeout)
		if version != "" {
			goplsMeasurement.Version = version
			goplsMeasurement.InitializeMS = &initializeMS
		}
		if err != nil {
			goplsMeasurement.UnavailableReason = "configured gopls binary could not initialize the pinned workspace/symbol provider"
		} else {
			goplsMeasurement.Status = "unknown_completeness"
			goplsMeasurement.UnavailableReason = ""
			goplsMeasurement.CompletenessNote = "workspace/symbol result caps and exhaustive workspace coverage are not established; per-query scores cover returned candidates"
		}
	}
	defer func() { session.close(options.Timeout) }()

	queryResults := make([]QueryResult, 0, len(manifest.Queries))
	var firstSearchResult *retrieval.Result
	var allGoplsLatencies []float64
	var allNativeLatencies []float64
	var allNativeGatherLatencies []float64
	var allNativeRankingLatencies []float64
	var goplsCompletedQueries int
	for _, query := range manifest.Queries {
		if err := parent.Err(); err != nil {
			return err
		}
		retrievalRanking, retrievalTimings, retrievalProfiles, searchResult, err := measureRetrieval(parent, query, source, repository, options)
		if err != nil {
			return err
		}
		if searchResult != nil && firstSearchResult == nil {
			copyResult := *searchResult
			firstSearchResult = &copyResult
		}
		heapSample(&peakHeap)
		var retrievalCandidatePool *CandidatePoolAudit
		var textCandidateAblation *TextCandidateResult
		if options.TextAblation {
			retrievalCandidatePool, textCandidateAblation, err = measureTextAblation(parent, query, source, repository, options)
			if err != nil {
				return fmt.Errorf("text candidate ablation for query %q: %w", query.ID, err)
			}
			heapSample(&peakHeap)
		}

		native, err := RunNativeRG(parent, workspace, query, source, query.Gold, options.Repetitions, nativeLimits{
			timeout: options.Timeout, outputBytes: options.RGOutputBytes, lineCount: options.RGLineLimit,
		})
		if err != nil {
			return fmt.Errorf("native rg query %q: %w", query.ID, err)
		}
		allNativeLatencies = append(allNativeLatencies, native.totalLatency.Samples...)
		allNativeGatherLatencies = append(allNativeGatherLatencies, native.commandLatency.Samples...)
		allNativeRankingLatencies = append(allNativeRankingLatencies, native.rankingLatency.Samples...)
		heapSample(&peakHeap)

		queryResult := QueryResult{
			ID: query.ID, Text: query.Text, Gold: append([]GoldSpan(nil), query.Gold...),
			Retrieval: retrievalRanking, RetrievalTimings: retrievalTimings,
			RetrievalProfiles: retrievalProfiles,
			RetrievalCandidatePool: retrievalCandidatePool,
			TextCandidateAblation: textCandidateAblation,
			NativeRG: native.ranking, NativeRGLatency: native.totalLatency,
			NativeRGCommandLatency: native.commandLatency,
			NativeRGRankingLatency: native.rankingLatency,
		}
		if session != nil {
			goplsRanking, goplsLatency, searchErr := session.search(parent, options.Timeout, query, source, query.Gold, options.Repetitions)
			if searchErr != nil {
				return fmt.Errorf("gopls query %q: %w", query.ID, searchErr)
			}
			queryResult.GoplsQuery = query.GoplsQuery
			if queryResult.GoplsQuery == "" {
				queryResult.GoplsQuery = query.Text
			}
			queryResult.Gopls = goplsRanking
			queryResult.GoplsQueryLatency = goplsLatency
			allGoplsLatencies = append(allGoplsLatencies, goplsLatency.Samples...)
			if goplsRanking.Status == "unknown_completeness" {
				goplsCompletedQueries++
			} else {
				goplsMeasurement.Status = "partial"
				if goplsMeasurement.CompletenessNote == "" {
					goplsMeasurement.CompletenessNote = "one or more workspace/symbol queries failed or timed out"
				}
			}
			heapSample(&peakHeap)
		} else {
			queryResult.Gopls = unavailableRanking("gopls provider did not initialize")
		}
		queryResults = append(queryResults, queryResult)
	}
	if firstSearchResult != nil {
		source.coverage.GoFilesWithDeclarations = firstSearchResult.IndexedFiles
		source.coverage.GoParseIncompleteFiles = firstSearchResult.SkippedFiles
	}
	if session != nil {
		goplsMeasurement.QueryLatency = latency(allGoplsLatencies)
		if goplsCompletedQueries == 0 && len(queryResults) > 0 {
			goplsMeasurement.Status = "partial"
		}
	}
	var final runtime.MemStats
	runtime.ReadMemStats(&final)
	heapSampleValue := final.HeapAlloc
	if heapSampleValue > peakHeap {
		peakHeap = heapSampleValue
	}
	var summary Summary
	summary, err = summarize(queryResults)
	if err != nil {
		return err
	}
	summary.GoplsStatus = goplsMeasurement.Status
	created := time.Now().UTC()
	reportVersion := "agentic-go.retrieval-report/v1"
	var textCandidateIndex *TextCandidateIndexCoverage
	if options.TextAblation {
		reportVersion = "agentic-go.retrieval-report/v2"
		coverage := source.textCandidateIndex
		textCandidateIndex = &coverage
	}
	report := Report{
		SchemaVersion: reportVersion, CreatedUTC: created,
		Repository: repository, Stratum: manifest.Stratum,
		Reproducibility: reproducibility,
		GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		RGVersion: rgVersion, SnapshotExportMS: archiveMS,
		RetrievalTimingNote: "Cache.SearchProfiled reports parse, aggregation and rank stages; its total covers the full Go declaration candidate retrieval call. Top-5 is scored from the prefix of the same top-10 result, so top-5 and top-10 timing samples are identical and the large corpus is not parsed twice for two cutoffs. The optional text candidate ablation uses the same scorer over a bounded mixed candidate pool and is reported separately. This screen measures the candidate retrieval kernel, not full go_context output or semantic resolution.",
		NativeRGWorkflow: NativeWorkflow{
			CommandTemplate: "rg --no-ignore --hidden --glob-case-insensitive --no-heading --with-filename --line-number --color never --fixed-strings --ignore-case --text --null [supported-source globs] -e <sorted unique query token>... -- .",
			Tokenizer: "retrieval Unicode letter/digit/underscore tokenizer with lower-to-upper camel-case splits; duplicate tokens removed and sorted",
			Ranking: []string{"distinct query tokens present on line descending", "query-token occurrences on line descending", "repository-relative path ascending", "line ascending"},
			Globs: append([]string(nil), nativeGlobs...),
			PathScope: "Git-archive files with supported source suffixes only; archive ignores checkout working-tree changes",
		},
		GoPackages: goPackages, Coverage: source.coverage, TextCandidateIndex: textCandidateIndex,
		Configuration: Configuration{
			Repetitions: options.Repetitions, CommandTimeoutMS: options.Timeout.Milliseconds(),
			SearchTimeoutMS: options.Timeout.Milliseconds(), MaxSourceBytes: options.MaxSourceBytes,
			MaxFileBytes: options.MaxFileBytes, NativeOutputLimit: options.RGOutputBytes,
			NativeLineLimit: options.RGLineLimit,
		},
		Gopls: goplsMeasurement, Queries: queryResults, Summary: summary,
		Heap: HeapStats{
			StartHeapAllocBytes: initial.HeapAlloc, EndHeapAllocBytes: final.HeapAlloc,
			PeakSampledHeapAllocBytes: peakHeap, EndHeapSysBytes: final.HeapSys,
			TotalAllocBytes: final.TotalAlloc - initial.TotalAlloc, NumGC: final.NumGC - initial.NumGC,
		},
	}
	return writeReport(options.Output, report)
}

func validateOptions(options Options) error {
	if options.Manifest == "" || options.RepositoryPath == "" || options.Output == "" {
		return fmt.Errorf("--manifest, --repo, and --out are required")
	}
	if options.Repetitions < 1 || options.Repetitions > 20 {
		return fmt.Errorf("--repetitions must be between 1 and 20")
	}
	if options.Timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	if options.MaxSourceBytes <= 0 || options.MaxFileBytes <= 0 || options.MaxFileBytes > options.MaxSourceBytes {
		return fmt.Errorf("source byte limits must be positive and per-file limit cannot exceed total source limit")
	}
	if options.RGOutputBytes <= 0 || options.RGLineLimit <= 0 {
		return fmt.Errorf("native rg output limits must be positive")
	}
	return nil
}

func validateGold(source archivedSource, manifest Manifest) error {
	for _, query := range manifest.Queries {
		for _, span := range query.Gold {
			meta, ok := source.meta[span.Path]
			if !ok {
				if _, expected := source.expected[span.Path]; expected {
					continue
				}
				return fmt.Errorf("query %q gold span path %q is not a supported source file in the pinned tree", query.ID, span.Path)
			}
			if span.EndLine > meta.lines {
				return fmt.Errorf("query %q gold span %q ends past the archived file's %d lines", query.ID, span.Path, meta.lines)
			}
		}
	}
	return nil
}

type searchObservation struct {
	result   retrieval.Result
	profile  retrieval.SearchProfile
	wallTime float64
	err      error
}

func measureRetrieval(parent context.Context, query Query, source archivedSource, repository Repository, options Options) (Ranking, RetrievalTimings, RetrievalProfiles, *retrieval.Result, error) {
	timings := RetrievalTimings{Cold: make(map[string]Latency), Warm: make(map[string]Latency)}
	profiles := RetrievalProfiles{Cold: make(map[string]RetrievalProfile), Warm: make(map[string]RetrievalProfile)}
	complete := true
	reason := ""
	var latestCold searchObservation
	var cache *retrieval.Cache
	coldSamples := make([]float64, 0, options.Repetitions)
	coldProfiles := make([]retrieval.SearchProfile, 0, options.Repetitions)
	for repetition := 0; repetition < options.Repetitions; repetition++ {
		runtime.GC()
		cache = retrieval.NewCache()
		observation, err := profiledSearch(parent, cache, source, repository, query.Text, 10, options.Timeout)
		if err != nil && parent.Err() != nil {
			return Ranking{}, timings, profiles, nil, parent.Err()
		}
		latestCold = observation
		coldSamples = append(coldSamples, observation.wallTime)
		coldProfiles = append(coldProfiles, observation.profile)
		if err != nil {
			complete = false
			if reason == "" {
				reason = "Cache.SearchProfiled cold sample failed or timed out"
			}
		}
	}
	coldLatency := latency(coldSamples)
	coldProfile := profileSeries(coldProfiles)
	warmSamples := make([]float64, 0, options.Repetitions)
	warmProfiles := make([]retrieval.SearchProfile, 0, options.Repetitions)
	var latestWarm searchObservation
	for repetition := 0; repetition < options.Repetitions; repetition++ {
		observation, err := profiledSearch(parent, cache, source, repository, query.Text, 10, options.Timeout)
		if err != nil && parent.Err() != nil {
			return Ranking{}, timings, profiles, nil, parent.Err()
		}
		latestWarm = observation
		warmSamples = append(warmSamples, observation.wallTime)
		warmProfiles = append(warmProfiles, observation.profile)
		if err != nil {
			complete = false
			if reason == "" {
				reason = "Cache.SearchProfiled warm sample failed or timed out"
			}
		}
	}
	warmLatency := latency(warmSamples)
	warmProfile := profileSeries(warmProfiles)
	for _, label := range []string{"5", "10"} {
		timings.Cold[label] = coldLatency
		timings.Warm[label] = warmLatency
		profiles.Cold[label] = coldProfile
		profiles.Warm[label] = warmProfile
	}
	selected := latestWarm
	if selected.err != nil {
		selected = latestCold
	}
	var topTenResult *retrieval.Result
	if selected.err == nil {
		copyResult := selected.result
		topTenResult = &copyResult
		if !copyResult.Complete {
			complete = false
			if reason == "" {
				reason = "one or more Go files could not be parsed completely"
			}
		}
	}
	if topTenResult == nil {
		return unavailableRanking("retrieval timed out before producing top-10 results"), timings, profiles, nil, nil
	}
	resultCandidates := make([]Candidate, 0, len(topTenResult.Candidates))
	for _, candidate := range topTenResult.Candidates {
		resultCandidates = append(resultCandidates, Candidate{
			Path: candidate.Path, Line: candidate.Line, Name: candidate.Name,
			Kind: candidate.Kind, Container: candidate.Package,
		})
	}
	ranking := scoreRanking(resultCandidates, len(resultCandidates), query.Gold, complete, reason, retrievalGranularity, nil)
	if !source.coverage.SourceArchiveComplete {
		ranking.Complete = false
		ranking.MetricsUsable = false
		ranking.Status = "partial"
		if ranking.IncompleteReason == "" {
			ranking.IncompleteReason = source.coverage.SourceArchiveIncompleteReason
		}
	}
	ranking.CandidateCountComplete = !topTenResult.Truncated && topTenResult.Complete && source.coverage.SourceArchiveComplete
	return ranking, timings, profiles, topTenResult, nil
}

func profiledSearch(parent context.Context, cache *retrieval.Cache, source archivedSource, repository Repository, query string, limit int, timeout time.Duration) (searchObservation, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	key := retrieval.Key{
		Workspace: repository.ID, Scope: repository.Commit,
		Build: "retrievalbench", Provider: "lexical-declaration-index",
	}
	start := time.Now()
	result, profile, err := cache.SearchProfiled(ctx, key, source.goFiles, query, limit)
	wall := float64(time.Since(start)) / float64(time.Millisecond)
	observation := searchObservation{result: result, profile: profile, wallTime: wall, err: err}
	if err != nil {
		return observation, fmt.Errorf("search timed out or failed: %w", err)
	}
	return observation, nil
}

func measureTextAblation(parent context.Context, query Query, source archivedSource, repository Repository, options Options) (*CandidatePoolAudit, *TextCandidateResult, error) {
	key := retrieval.Key{
		Workspace: repository.ID, Scope: repository.Commit,
		Build: "retrievalbench", Provider: "lexical-declaration-index",
	}
	search := func(includeText bool) (retrieval.Result, RetrievalProfile, float64, error) {
		ctx, cancel := context.WithTimeout(parent, options.Timeout)
		defer cancel()
		cache := retrieval.NewCache()
		started := time.Now()
		var result retrieval.Result
		var profile retrieval.SearchProfile
		var err error
		if includeText {
			result, profile, err = cache.SearchWithTextProfiled(ctx, key, source.goFiles, source.textFiles, query.Text, candidatePoolAuditLimit)
		} else {
			result, profile, err = cache.SearchProfiled(ctx, key, source.goFiles, query.Text, candidatePoolAuditLimit)
		}
		wallMS := float64(time.Since(started)) / float64(time.Millisecond)
		return result, profileSeries([]retrieval.SearchProfile{profile}), wallMS, err
	}
	goResult, _, _, err := search(false)
	if err != nil {
		return nil, nil, fmt.Errorf("auditing declaration candidate pool: %w", err)
	}
	basePool := candidatePoolAudit(goResult, query.Gold, source, false)
	textResult, textProfile, wallMS, err := search(true)
	if err != nil {
		return nil, nil, fmt.Errorf("searching bounded text candidate pool: %w", err)
	}
	textPool := candidatePoolAudit(textResult, query.Gold, source, true)
	textCandidates := studyCandidates(textResult.Candidates)
	usableCount := len(textCandidates)
	if usableCount > 10 {
		usableCount = 10
	}
	rankingComplete := textResult.Complete && source.coverage.SourceArchiveComplete && source.textCandidateIndex.Status == "complete"
	rankingReason := ""
	if !textResult.Complete {
		rankingReason = "one or more Go or text candidate files could not be indexed completely"
	} else if !source.coverage.SourceArchiveComplete {
		rankingReason = source.coverage.SourceArchiveIncompleteReason
	} else if source.textCandidateIndex.Status != "complete" {
		rankingReason = source.textCandidateIndex.Reason
	}
	ranking := scoreRanking(textCandidates[:usableCount], textResult.CandidateCount, query.Gold, rankingComplete, rankingReason, "go_declaration_plus_text_line", nil)
	ranking.CandidateCountComplete = textPool.Complete
	return &basePool, &TextCandidateResult{
		Ranking: ranking, CandidatePool: textPool,
		Latency: latency([]float64{wallMS}), Profile: textProfile,
		IndexedTextFragments: textResult.TextIndexedFragments,
		SkippedTextFiles: textResult.TextSkippedFiles,
		MaximumTextFragments: retrieval.MaximumTextLineFragments,
	}, nil
}

func candidatePoolAudit(result retrieval.Result, gold []GoldSpan, source archivedSource, includesText bool) CandidatePoolAudit {
	candidates := studyCandidates(result.Candidates)
	hits := make([]bool, len(gold))
	for _, candidate := range candidates {
		for index, span := range gold {
			if candidate.Path == span.Path && candidate.Line >= span.StartLine && candidate.Line <= span.EndLine {
				hits[index] = true
			}
		}
	}
	hitCount := 0
	for _, hit := range hits {
		if hit {
			hitCount++
		}
	}
	complete := result.Complete && source.coverage.SourceArchiveComplete && !result.Truncated
	reason := ""
	if !result.Complete {
		reason = "one or more candidate source files could not be indexed completely"
	}
	if result.Truncated {
		reason = "candidate audit reached the 10000-candidate cap"
	}
	if !source.coverage.SourceArchiveComplete {
		complete = false
		if reason == "" {
			reason = source.coverage.SourceArchiveIncompleteReason
		}
	}
	if includesText && source.textCandidateIndex.Status != "complete" {
		complete = false
		if reason == "" {
			reason = source.textCandidateIndex.Reason
		}
	}
	status := "complete"
	if !complete {
		status = "partial"
	}
	return CandidatePoolAudit{
		Status: status, Complete: complete,
		CandidateCount: result.CandidateCount, CandidatesObserved: len(candidates),
		CandidateLimit: candidatePoolAuditLimit, GoldSpans: len(gold), HitGoldSpans: hitCount,
		Recall: ratio(hitCount, len(gold)), IncompleteReason: reason,
	}
}

func studyCandidates(candidates []retrieval.Candidate) []Candidate {
	result := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, Candidate{
			Path: candidate.Path, Line: candidate.Line, Name: candidate.Name,
			Kind: candidate.Kind, Container: candidate.Package,
		})
	}
	return result
}

func profileSeries(profiles []retrieval.SearchProfile) RetrievalProfile {
	series := RetrievalProfile{
		Samples: make([]RetrievalProfileSample, 0, len(profiles)),
	}
	searchMS, parseMS, aggregateMS, rankMS := make([]float64, 0, len(profiles)), make([]float64, 0, len(profiles)), make([]float64, 0, len(profiles)), make([]float64, 0, len(profiles))
	for _, profile := range profiles {
		search := float64(profile.SearchDuration) / float64(time.Millisecond)
		parse := float64(profile.ParseDuration) / float64(time.Millisecond)
		aggregate := float64(profile.AggregateDuration) / float64(time.Millisecond)
		rank := float64(profile.RankDuration) / float64(time.Millisecond)
		series.Samples = append(series.Samples, RetrievalProfileSample{
			SearchMS: search, ParseMS: parse, AggregateMS: aggregate, RankMS: rank,
			FileVisits: profile.FileVisits, CacheHits: profile.CacheHits, FilesParsed: profile.FilesParsed,
		})
		searchMS, parseMS, aggregateMS, rankMS = append(searchMS, search), append(parseMS, parse), append(aggregateMS, aggregate), append(rankMS, rank)
	}
	series.Search, series.Parse = latency(searchMS), latency(parseMS)
	series.Aggregate, series.Rank = latency(aggregateMS), latency(rankMS)
	return series
}

func heapSample(peak *uint64) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	if stats.HeapAlloc > *peak {
		*peak = stats.HeapAlloc
	}
}

func unavailableRanking(reason string) Ranking {
	return Ranking{
		Status: "unavailable", Complete: false, IncompleteReason: reason,
		Candidates: []Candidate{}, CandidateCountComplete: false,
		Granularity: retrievalGranularity,
		Metrics: Metrics{}, Misses: EvidenceMisses{ByTypeAt5: map[string]int{}, ByTypeAt10: map[string]int{}},
	}
}

func summarize(queries []QueryResult) (Summary, error) {
	retrievalRankings := make([]Ranking, 0, len(queries))
	nativeRankings := make([]Ranking, 0, len(queries))
	goplsRankings := make([]Ranking, 0, len(queries))
	coldSamples := map[string][]float64{"5": {}, "10": {}}
	warmSamples := map[string][]float64{"5": {}, "10": {}}
	coldStageSamples := map[string][]RetrievalProfileSample{"5": {}, "10": {}}
	warmStageSamples := map[string][]RetrievalProfileSample{"5": {}, "10": {}}
	var nativeTotal, nativeGather, nativeRanking, goplsQuery []float64
	for _, query := range queries {
		retrievalRankings = append(retrievalRankings, query.Retrieval)
		nativeRankings = append(nativeRankings, query.NativeRG)
		if query.Gopls.Status != "unavailable" {
			goplsRankings = append(goplsRankings, query.Gopls)
		}
		for _, limit := range []string{"5", "10"} {
			coldSamples[limit] = append(coldSamples[limit], query.RetrievalTimings.Cold[limit].Samples...)
			warmSamples[limit] = append(warmSamples[limit], query.RetrievalTimings.Warm[limit].Samples...)
			coldStageSamples[limit] = append(coldStageSamples[limit], query.RetrievalProfiles.Cold[limit].Samples...)
			warmStageSamples[limit] = append(warmStageSamples[limit], query.RetrievalProfiles.Warm[limit].Samples...)
		}
		nativeTotal = append(nativeTotal, query.NativeRGLatency.Samples...)
		nativeGather = append(nativeGather, query.NativeRGCommandLatency.Samples...)
		nativeRanking = append(nativeRanking, query.NativeRGRankingLatency.Samples...)
		goplsQuery = append(goplsQuery, query.GoplsQueryLatency.Samples...)
	}
	summary := Summary{
		Retrieval: ArmSummary{At5: aggregateRankings(retrievalRankings, 5), At10: aggregateRankings(retrievalRankings, 10)},
		NativeRG: ArmSummary{At5: aggregateRankings(nativeRankings, 5), At10: aggregateRankings(nativeRankings, 10)},
		Gopls: ArmSummary{At5: aggregateRankings(goplsRankings, 5), At10: aggregateRankings(goplsRankings, 10)},
		RetrievalColdP50MS: make(map[string]float64), RetrievalColdP95MS: make(map[string]float64),
		RetrievalWarmP50MS: make(map[string]float64), RetrievalWarmP95MS: make(map[string]float64),
		RetrievalStages: RetrievalStageSummary{Cold: make(map[string]RetrievalStageLatencySummary), Warm: make(map[string]RetrievalStageLatencySummary)},
	}
	for _, limit := range []string{"5", "10"} {
		cold, warm := latency(coldSamples[limit]), latency(warmSamples[limit])
		summary.RetrievalColdP50MS[limit], summary.RetrievalColdP95MS[limit] = cold.P50MS, cold.P95MS
		summary.RetrievalWarmP50MS[limit], summary.RetrievalWarmP95MS[limit] = warm.P50MS, warm.P95MS
		summary.RetrievalStages.Cold[limit] = summarizeRetrievalStages(coldStageSamples[limit])
		summary.RetrievalStages.Warm[limit] = summarizeRetrievalStages(warmStageSamples[limit])
	}
	nativeLatency := latency(nativeTotal)
	nativeGatherLatency := latency(nativeGather)
	nativeRankingLatency := latency(nativeRanking)
	summary.NativeRGP50MS, summary.NativeRGP95MS = nativeLatency.P50MS, nativeLatency.P95MS
	summary.NativeRGCommandP50MS, summary.NativeRGCommandP95MS = nativeGatherLatency.P50MS, nativeGatherLatency.P95MS
	summary.NativeRGRankingP50MS, summary.NativeRGRankingP95MS = nativeRankingLatency.P50MS, nativeRankingLatency.P95MS
	if len(goplsQuery) > 0 {
		goplsLatency := latency(goplsQuery)
		summary.GoplsQueryP50MS, summary.GoplsQueryP95MS = &goplsLatency.P50MS, &goplsLatency.P95MS
	}
	if math.IsNaN(summary.Retrieval.At5.MacroRecall) {
		return Summary{}, fmt.Errorf("summary contains a non-finite metric")
	}
	return summary, nil
}

func summarizeRetrievalStages(samples []RetrievalProfileSample) RetrievalStageLatencySummary {
	search, parse, aggregate, rank := make([]float64, 0, len(samples)), make([]float64, 0, len(samples)), make([]float64, 0, len(samples)), make([]float64, 0, len(samples))
	for _, sample := range samples {
		search = append(search, sample.SearchMS)
		parse = append(parse, sample.ParseMS)
		aggregate = append(aggregate, sample.AggregateMS)
		rank = append(rank, sample.RankMS)
	}
	searchStats, parseStats := latency(search), latency(parse)
	aggregateStats, rankStats := latency(aggregate), latency(rank)
	return RetrievalStageLatencySummary{
		SearchP50MS: searchStats.P50MS, SearchP95MS: searchStats.P95MS,
		ParseP50MS: parseStats.P50MS, ParseP95MS: parseStats.P95MS,
		AggregateP50MS: aggregateStats.P50MS, AggregateP95MS: aggregateStats.P95MS,
		RankP50MS: rankStats.P50MS, RankP95MS: rankStats.P95MS,
	}
}

func writeReport(filename string, report Report) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return fmt.Errorf("creating result directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(filename), ".retrieval-report-*.tmp")
	if err != nil {
		return fmt.Errorf("creating result file: %w", err)
	}
	temporaryName := file.Name()
	defer os.Remove(temporaryName)
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		return fmt.Errorf("setting result file permissions: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_ = file.Close()
		return fmt.Errorf("encoding result report: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("syncing result report: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing result report: %w", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("publishing result report: %w", err)
	}
	return nil
}
