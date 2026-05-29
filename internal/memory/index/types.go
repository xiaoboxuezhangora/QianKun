package index

type Options struct {
	DBPath string
}

type SyncStats struct {
	Root            string `json:"root"`
	DBPath          string `json:"db_path"`
	FTS5Enabled     bool   `json:"fts5_enabled"`
	FilesIndexed    int    `json:"files_indexed"`
	FilesUpdated    int    `json:"files_updated"`
	FilesUnchanged  int    `json:"files_unchanged"`
	FilesDeleted    int    `json:"files_deleted"`
	ReadErrors      int    `json:"read_errors"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

func (s SyncStats) Changed() bool {
	return s.FilesUpdated > 0 || s.FilesDeleted > 0
}

type RootStats struct {
	Root            string `json:"root"`
	DBPath          string `json:"db_path"`
	FTS5Enabled     bool   `json:"fts5_enabled"`
	FilesIndexed    int    `json:"files_indexed"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

type QueryResult struct {
	Path          string  `json:"path"`
	Kind          string  `json:"kind"`
	Weight        int     `json:"weight"`
	Score         float64 `json:"score"`
	TokenEstimate int     `json:"token_estimate"`
	Snippet       string  `json:"snippet"`
}

type QueryResponse struct {
	Root    string        `json:"root"`
	Query   string        `json:"query"`
	TopK    int           `json:"top_k"`
	Results []QueryResult `json:"results"`
}
