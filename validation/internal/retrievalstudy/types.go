// Package retrievalstudy implements a deterministic, model-free retrieval
// screen over immutable Git archives.
package retrievalstudy

import "time"

// ManifestVersion identifies the accepted retrieval-study manifest schema.
const ManifestVersion = "agentic-go.retrieval-study/v1"

// Manifest describes one repository snapshot and its reviewed retrieval questions.
type Manifest struct {
	Version      string  `json:"version"`
	RepositoryID string  `json:"repository_id"`
	Commit       string  `json:"commit"`
	Tree         string  `json:"tree"`
	Stratum      string  `json:"stratum"`
	Queries      []Query `json:"queries"`
}

// Query is one held-out question and its independently reviewed gold spans.
type Query struct {
	ID         string     `json:"id"`
	Text       string     `json:"query"`
	GoplsQuery string     `json:"gopls_query,omitempty"`
	Gold       []GoldSpan `json:"gold"`
}

// GoldSpan identifies a source range that is relevant evidence for a query.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type GoldSpan struct {
	Type      string `json:"type"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Label     string `json:"label,omitempty"`
}

// Repository records the exact repository objects used by a study.
type Repository struct {
	ID     string `json:"id"`
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
}

// Reproducibility records the source, manifest, and retrieval-code hashes.
type Reproducibility struct {
	StudySourceCommit     string `json:"study_source_commit"`
	DirtyDiffSHA256       string `json:"dirty_diff_sha256"`
	ManifestSHA256        string `json:"manifest_sha256"`
	RetrievalSourceSHA256 string `json:"retrieval_source_sha256"`
}

// Coverage records which committed source files were extracted and indexed.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type Coverage struct {
	TrackedRegularFiles           int    `json:"tracked_regular_files"`
	TrackedRegularBytes           int64  `json:"tracked_regular_bytes"`
	ExpectedSupportedSourceFiles  int    `json:"expected_supported_source_files"`
	ExpectedSupportedSourceBytes  int64  `json:"expected_supported_source_bytes"`
	SupportedSourceFiles          int    `json:"supported_source_files"`
	SupportedSourceBytes          int64  `json:"supported_source_bytes"`
	GoFiles                       int    `json:"go_files"`
	GoBytes                       int64  `json:"go_bytes"`
	GoFilesWithDeclarations       int    `json:"go_files_with_declarations"`
	GoParseIncompleteFiles        int    `json:"go_parse_incomplete_files"`
	SupportedTextFiles            int    `json:"supported_text_files"`
	SupportedTextBytes            int64  `json:"supported_text_bytes"`
	RejectedTextFiles             int    `json:"rejected_text_files"`
	RejectedTextBytes             int64  `json:"rejected_text_bytes"`
	SymlinksNotIndexed            int    `json:"symlinks_not_indexed"`
	OtherArchiveEntriesIgnored    int    `json:"other_archive_entries_ignored"`
	ArchiveOmittedSourceFiles     int    `json:"archive_omitted_source_files"`
	ArchiveOmittedSourceBytes     int64  `json:"archive_omitted_source_bytes"`
	ArchiveTransformedSourceFiles int    `json:"archive_transformed_source_files"`
	ArchiveTransformedSourceBytes int64  `json:"archive_transformed_source_bytes"`
	SourceArchiveComplete         bool   `json:"source_archive_complete"`
	SourceArchiveIncompleteReason string `json:"source_archive_incomplete_reason,omitempty"`
	TextIndexedByRetrieval        int    `json:"text_indexed_by_retrieval"`
}

// TextCandidateIndexCoverage describes bounded text capture for the private ablation.
type TextCandidateIndexCoverage struct {
	Status            string `json:"status"`
	Reason            string `json:"reason,omitempty"`
	SupportedFiles    int    `json:"supported_files"`
	SupportedBytes    int64  `json:"supported_bytes"`
	IndexedFiles      int    `json:"indexed_files"`
	IndexedBytes      int64  `json:"indexed_bytes"`
	OmittedFiles      int    `json:"omitted_files"`
	OmittedBytes      int64  `json:"omitted_bytes"`
	MaximumFiles      int    `json:"maximum_files"`
	MaximumFileBytes  int64  `json:"maximum_file_bytes"`
	MaximumTotalBytes int64  `json:"maximum_total_bytes"`
	MaximumFragments  int    `json:"maximum_fragments_per_query"`
}

// GoPackageInventory reports the bounded `go list` inventory result.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type GoPackageInventory struct {
	Status             string  `json:"status"`
	Command            string  `json:"command"`
	PackageCount       int     `json:"package_count"`
	PackagesWithErrors int     `json:"packages_with_errors"`
	LatencyMS          float64 `json:"latency_ms"`
	TimeoutMS          int64   `json:"timeout_ms"`
	OutputBytes        int     `json:"captured_output_bytes"`
	OutputLimitBytes   int     `json:"captured_output_limit_bytes"`
	OutputTruncated    bool    `json:"output_truncated"`
	Error              string  `json:"error,omitempty"`
	Coverage           string  `json:"coverage"`
}

// Candidate is one source anchor returned by a retrieval workflow.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type Candidate struct {
	Path               string `json:"path"`
	Line               int    `json:"line"`
	Name               string `json:"name,omitempty"`
	Kind               string `json:"kind,omitempty"`
	Container          string `json:"container,omitempty"`
	ProviderKind       int    `json:"provider_kind,omitempty"`
	MatchedUniqueTerms int    `json:"matched_unique_terms,omitempty"`
	MatchedOccurrences int    `json:"matched_occurrences,omitempty"`
}

// CutoffMetrics scores one ranked candidate list at a fixed result limit.
type CutoffMetrics struct {
	RetrievedCandidates int     `json:"retrieved_candidates"`
	RelevantCandidates  int     `json:"relevant_candidates"`
	GoldSpans           int     `json:"gold_spans"`
	HitGoldSpans        int     `json:"hit_gold_spans"`
	Recall              float64 `json:"recall"`
	Precision           float64 `json:"precision"`
	MeanReciprocalRank  float64 `json:"mean_reciprocal_rank"`
}

// Metrics contains scores at the two benchmark cutoffs.
type Metrics struct {
	At5  CutoffMetrics `json:"at_5"`
	At10 CutoffMetrics `json:"at_10"`
}

// EvidenceMisses counts reviewed spans omitted at each candidate cutoff.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type EvidenceMisses struct {
	GoldSpans  int            `json:"gold_spans"`
	At5        int            `json:"missed_at_5"`
	At10       int            `json:"missed_at_10"`
	ByTypeAt5  map[string]int `json:"missed_at_5_by_type"`
	ByTypeAt10 map[string]int `json:"missed_at_10_by_type"`
}

// Ranking is the status, scores, and candidates for one workflow and query.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type Ranking struct {
	Status                 string         `json:"status"`
	Complete               bool           `json:"complete"`
	MetricsUsable          bool           `json:"metrics_usable"`
	IncompleteReason       string         `json:"incomplete_reason,omitempty"`
	Metrics                Metrics        `json:"metrics"`
	Misses                 EvidenceMisses `json:"misses"`
	Candidates             []Candidate    `json:"candidates"`
	CandidateCount         int            `json:"candidate_count"`
	CandidateCountComplete bool           `json:"candidate_count_complete"`
	Granularity            string         `json:"candidate_granularity"`
	Tokens                 []string       `json:"tokens,omitempty"`
}

// CandidatePoolAudit measures whether reviewed spans occur before top-k ranking.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type CandidatePoolAudit struct {
	Status             string  `json:"status"`
	Complete           bool    `json:"complete"`
	CandidateCount     int     `json:"candidate_count"`
	CandidatesObserved int     `json:"candidates_observed"`
	CandidateLimit     int     `json:"candidate_limit"`
	GoldSpans          int     `json:"gold_spans"`
	HitGoldSpans       int     `json:"hit_gold_spans"`
	Recall             float64 `json:"recall"`
	IncompleteReason   string  `json:"incomplete_reason,omitempty"`
}

// TextCandidateResult records the evaluation-only mixed text and Go ranking.
type TextCandidateResult struct {
	Ranking              Ranking            `json:"ranking"`
	CandidatePool        CandidatePoolAudit `json:"candidate_pool"`
	Latency              Latency            `json:"latency"`
	Profile              RetrievalProfile   `json:"profile"`
	IndexedTextFragments int                `json:"indexed_text_fragments"`
	SkippedTextFiles     int                `json:"skipped_text_files"`
	MaximumTextFragments int                `json:"maximum_text_fragments"`
}

// Latency stores measured samples and their summary statistics in milliseconds.
type Latency struct {
	Samples []float64 `json:"samples_ms"`
	P50MS   float64   `json:"p50_ms"`
	P95MS   float64   `json:"p95_ms"`
	MinMS   float64   `json:"min_ms"`
	MaxMS   float64   `json:"max_ms"`
}

// RetrievalTimings separates cold and warm query durations by result limit.
type RetrievalTimings struct {
	Cold map[string]Latency `json:"cold_by_limit"`
	Warm map[string]Latency `json:"warm_by_limit"`
}

// RetrievalProfileSample stores one retrieval call's stage timings and file counts.
type RetrievalProfileSample struct {
	SearchMS    float64 `json:"search_ms"`
	ParseMS     float64 `json:"parse_ms"`
	AggregateMS float64 `json:"aggregate_ms"`
	RankMS      float64 `json:"rank_ms"`
	FileVisits  int     `json:"file_visits"`
	CacheHits   int     `json:"cache_hits"`
	FilesParsed int     `json:"files_parsed"`
}

// RetrievalProfile aggregates stage timings for retrieval calls.
type RetrievalProfile struct {
	Samples   []RetrievalProfileSample `json:"samples"`
	Search    Latency                  `json:"search"`
	Parse     Latency                  `json:"parse"`
	Aggregate Latency                  `json:"aggregate"`
	Rank      Latency                  `json:"rank"`
}

// RetrievalProfiles groups cold and warm stage profiles by result limit.
type RetrievalProfiles struct {
	Cold map[string]RetrievalProfile `json:"cold_by_limit"`
	Warm map[string]RetrievalProfile `json:"warm_by_limit"`
}

// GoplsMeasurement records the optional workspace-symbol workflow and its limits.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type GoplsMeasurement struct {
	Status            string   `json:"status"`
	Version           string   `json:"version,omitempty"`
	UnavailableReason string   `json:"unavailable_reason,omitempty"`
	CompletenessNote  string   `json:"completeness_note,omitempty"`
	InitializeMS      *float64 `json:"initialize_ms,omitempty"`
	QueryLatency      Latency  `json:"query_latency"`
	Granularity       string   `json:"candidate_granularity"`
}

// QueryResult combines gold spans, rankings, coverage, and timings for one query.
type QueryResult struct {
	ID                     string               `json:"id"`
	Text                   string               `json:"query"`
	Gold                   []GoldSpan           `json:"gold"`
	Retrieval              Ranking              `json:"retrieval"`
	RetrievalTimings       RetrievalTimings     `json:"retrieval_timings"`
	RetrievalProfiles      RetrievalProfiles    `json:"retrieval_profiles"`
	RetrievalCandidatePool *CandidatePoolAudit  `json:"retrieval_candidate_pool,omitempty"`
	TextCandidateAblation  *TextCandidateResult `json:"text_candidate_ablation,omitempty"`
	NativeRG               Ranking              `json:"native_rg"`
	NativeRGLatency        Latency              `json:"native_rg_latency"`
	NativeRGCommandLatency Latency              `json:"native_rg_candidate_gather_latency"`
	NativeRGRankingLatency Latency              `json:"native_rg_ranking_latency"`
	GoplsQuery             string               `json:"gopls_query,omitempty"`
	Gopls                  Ranking              `json:"gopls,omitempty"`
	GoplsQueryLatency      Latency              `json:"gopls_query_latency,omitempty"`
}

// AggregateMetrics contains macro and micro scores across complete queries.
type AggregateMetrics struct {
	Queries                 int     `json:"queries"`
	MetricsQueries          int     `json:"metrics_queries"`
	CompleteQueries         int     `json:"complete_queries"`
	MacroRecall             float64 `json:"macro_recall"`
	MacroPrecision          float64 `json:"macro_precision"`
	MacroMeanReciprocalRank float64 `json:"macro_mean_reciprocal_rank"`
	MicroRecall             float64 `json:"micro_recall"`
	MicroPrecision          float64 `json:"micro_precision"`
}

// ArmSummary contains aggregate scores for both result cutoffs.
type ArmSummary struct {
	At5  AggregateMetrics `json:"at_5"`
	At10 AggregateMetrics `json:"at_10"`
}

// RetrievalStageLatencySummary reports search, parse, aggregation, and rank timings.
type RetrievalStageLatencySummary struct {
	SearchP50MS    float64 `json:"search_p50_ms"`
	SearchP95MS    float64 `json:"search_p95_ms"`
	ParseP50MS     float64 `json:"parse_p50_ms"`
	ParseP95MS     float64 `json:"parse_p95_ms"`
	AggregateP50MS float64 `json:"aggregate_p50_ms"`
	AggregateP95MS float64 `json:"aggregate_p95_ms"`
	RankP50MS      float64 `json:"rank_p50_ms"`
	RankP95MS      float64 `json:"rank_p95_ms"`
}

// RetrievalStageSummary groups cold and warm stage timings by result limit.
type RetrievalStageSummary struct {
	Cold map[string]RetrievalStageLatencySummary `json:"cold_by_limit"`
	Warm map[string]RetrievalStageLatencySummary `json:"warm_by_limit"`
}

// Summary contains aggregate retrieval, native-tool, gopls, and latency results.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type Summary struct {
	Retrieval            ArmSummary            `json:"retrieval"`
	NativeRG             ArmSummary            `json:"native_rg"`
	Gopls                ArmSummary            `json:"gopls"`
	GoplsStatus          string                `json:"gopls_status"`
	RetrievalStages      RetrievalStageSummary `json:"retrieval_stages"`
	RetrievalColdP50MS   map[string]float64    `json:"retrieval_cold_p50_ms_by_limit"`
	RetrievalColdP95MS   map[string]float64    `json:"retrieval_cold_p95_ms_by_limit"`
	RetrievalWarmP50MS   map[string]float64    `json:"retrieval_warm_p50_ms_by_limit"`
	RetrievalWarmP95MS   map[string]float64    `json:"retrieval_warm_p95_ms_by_limit"`
	NativeRGP50MS        float64               `json:"native_rg_p50_ms"`
	NativeRGP95MS        float64               `json:"native_rg_p95_ms"`
	NativeRGCommandP50MS float64               `json:"native_rg_candidate_gather_p50_ms"`
	NativeRGCommandP95MS float64               `json:"native_rg_candidate_gather_p95_ms"`
	NativeRGRankingP50MS float64               `json:"native_rg_ranking_p50_ms"`
	NativeRGRankingP95MS float64               `json:"native_rg_ranking_p95_ms"`
	GoplsQueryP50MS      *float64              `json:"gopls_query_p50_ms,omitempty"`
	GoplsQueryP95MS      *float64              `json:"gopls_query_p95_ms,omitempty"`
}

// HeapStats stores sampled Go runtime heap values for the benchmark process.
type HeapStats struct {
	StartHeapAllocBytes       uint64 `json:"start_heap_alloc_bytes"`
	EndHeapAllocBytes         uint64 `json:"end_heap_alloc_bytes"`
	PeakSampledHeapAllocBytes uint64 `json:"peak_sampled_heap_alloc_bytes"`
	EndHeapSysBytes           uint64 `json:"end_heap_sys_bytes"`
	TotalAllocBytes           uint64 `json:"total_alloc_bytes"`
	NumGC                     uint32 `json:"num_gc"`
}

// Report is the complete provenance-bound result of one retrieval screen.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type Report struct {
	SchemaVersion       string                      `json:"schema_version"`
	CreatedUTC          time.Time                   `json:"created_utc"`
	Repository          Repository                  `json:"repository"`
	Reproducibility     Reproducibility             `json:"reproducibility"`
	Stratum             string                      `json:"stratum"`
	GoVersion           string                      `json:"go_version"`
	GOOS                string                      `json:"goos"`
	GOARCH              string                      `json:"goarch"`
	RGVersion           string                      `json:"rg_version"`
	SnapshotExportMS    float64                     `json:"snapshot_export_ms"`
	RetrievalTimingNote string                      `json:"retrieval_timing_note"`
	NativeRGWorkflow    NativeWorkflow              `json:"native_rg_workflow"`
	GoPackages          GoPackageInventory          `json:"go_package_inventory"`
	Coverage            Coverage                    `json:"coverage"`
	TextCandidateIndex  *TextCandidateIndexCoverage `json:"text_candidate_index,omitempty"`
	Configuration       Configuration               `json:"configuration"`
	Gopls               GoplsMeasurement            `json:"gopls"`
	Queries             []QueryResult               `json:"queries"`
	Summary             Summary                     `json:"summary"`
	Heap                HeapStats                   `json:"heap"`
}

// NativeWorkflow describes the fixed ripgrep and local ranking procedure.
//
//nolint:govet // Field order is part of the stable JSON report shape.
type NativeWorkflow struct {
	CommandTemplate string   `json:"command_template"`
	Tokenizer       string   `json:"tokenizer"`
	Ranking         []string `json:"ranking_order"`
	Globs           []string `json:"globs"`
	PathScope       string   `json:"path_scope"`
}

// Configuration records the command bounds and repetitions used in a report.
type Configuration struct {
	Repetitions       int   `json:"repetitions"`
	CommandTimeoutMS  int64 `json:"command_timeout_ms"`
	SearchTimeoutMS   int64 `json:"search_timeout_ms"`
	MaxSourceBytes    int64 `json:"max_source_bytes"`
	MaxFileBytes      int64 `json:"max_file_bytes"`
	NativeOutputLimit int64 `json:"native_rg_output_limit_bytes"`
	NativeLineLimit   int   `json:"native_rg_line_limit"`
}
