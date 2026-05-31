package instructions

type Options struct {
	Root string
}

type Report struct {
	Root        string    `json:"root"`
	FilesRead   int       `json:"files_read"`
	Findings    []Finding `json:"findings"`
	GeneratedAt string    `json:"generated_at"`
}

type Finding struct {
	Severity   string `json:"severity"`
	Rule       string `json:"rule"`
	File       string `json:"file"`
	Line       int    `json:"line,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
	Excerpt    string `json:"excerpt,omitempty"`
}
