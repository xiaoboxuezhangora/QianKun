package scan

type FileKind string

const (
	KindSource        FileKind = "source"
	KindConfig        FileKind = "config"
	KindDocumentation FileKind = "documentation"
	KindLockfile      FileKind = "lockfile"
	KindBuildArtifact FileKind = "build_artifact"
	KindIDEConfig     FileKind = "ide_config"
	KindAICache       FileKind = "ai_cache"
	KindBinary        FileKind = "binary"
	KindUnknown       FileKind = "unknown"
)

const (
	SkipBuildArtifact     = "build_artifact"
	SkipIDEConfig         = "ide_config"
	SkipAICache           = "ai_cache"
	SkipLockfile          = "lockfile"
	SkipBinary            = "binary"
	SkipUserExclude       = "user_exclude"
	SkipIncludeMismatch   = "include_mismatch"
	SkipGitignore         = "gitignore"
	SkipContextgateIgnore = "contextgateignore"
	SkipReadError         = "read_error"
	SkipWalkError         = "walk_error"
)

const (
	LargeTextFileThresholdBytes = 64 * 1024
	TailSampleBytes             = 8 * 1024
)

type Options struct {
	Root    string
	Include []string
	Exclude []string
	NowUTC  func() string
}

type Result struct {
	Root           string                    `json:"root"`
	GeneratedAt    string                    `json:"generated_at"`
	Totals         Totals                    `json:"totals"`
	SkippedSummary map[string]SkippedSummary `json:"skipped_summary"`
	Files          []FileEntry               `json:"files"`
}

type Totals struct {
	FilesSeen          int `json:"files_seen"`
	FilesIndexed       int `json:"files_indexed"`
	FilesSkipped       int `json:"files_skipped"`
	DirectoriesSkipped int `json:"directories_skipped,omitempty"`
	EstimatedTokens    int `json:"estimated_tokens"`
}

type SkippedSummary struct {
	Count               int      `json:"count"`
	RepresentativePaths []string `json:"representative_paths"`
}

type FileEntry struct {
	Path          string   `json:"path"`
	SizeBytes     int64    `json:"size_bytes"`
	Kind          FileKind `json:"kind"`
	Weight        int      `json:"weight"`
	TokenEstimate int      `json:"token_estimate"`
	Skipped       bool     `json:"skipped"`
	SkipReason    string   `json:"skip_reason,omitempty"`
	Profile       string   `json:"profile,omitempty"`
	Role          string   `json:"role,omitempty"`
	Sampled       bool     `json:"sampled,omitempty"`
	SampleBytes   int      `json:"sample_bytes,omitempty"`
	Error         string   `json:"error,omitempty"`
}
