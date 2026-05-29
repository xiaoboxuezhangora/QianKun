package usage

type EventType string

const (
	EventCall          EventType = "call"
	EventCacheHit      EventType = "cache_hit"
	EventCacheMiss     EventType = "cache_miss"
	EventLatency       EventType = "latency"
	EventTokenEstimate EventType = "token_estimate"
)

type Event struct {
	Type                      EventType
	Tool                      string
	Root                      string
	TokenEstimate             int
	SavedTokens               int
	LatencyMS                 int64
	CacheKey                  string
	CacheAvoidedTokens        int
	SentContextTokensEstimate int
	AdjustedSavedTokens       int
	IgnoredTokensEstimate     int
}

type Report struct {
	DBPath                    string `json:"db_path"`
	GeneratedAt               string `json:"generated_at"`
	TotalCalls                int    `json:"total_calls"`
	CacheHits                 int    `json:"cache_hits"`
	CacheMisses               int    `json:"cache_misses"`
	EstimatedTokens           int    `json:"estimated_tokens"`
	EstimatedSavedTokens      int    `json:"estimated_saved_tokens"`
	CacheAvoidedTokens        int    `json:"cache_avoided_tokens"`
	SentContextTokensEstimate int    `json:"sent_context_tokens_estimate"`
	AdjustedSavedTokens       int    `json:"adjusted_saved_tokens"`
	IgnoredTokensEstimate     int    `json:"ignored_tokens_estimate"`
	P95LatencyMS              int64  `json:"p95_latency_ms"`
}
