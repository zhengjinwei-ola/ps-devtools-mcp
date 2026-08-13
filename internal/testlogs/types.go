package testlogs

type ListSourcesInput struct{}
type SourceInfo struct {
	Name string `json:"name"`
}
type ListSourcesOutput struct {
	Sources []SourceInfo `json:"sources"`
}

type SearchInput struct {
	Source        string `json:"source"`
	Query         string `json:"query,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
}
type LogEntry struct {
	File string `json:"file"`
	Line string `json:"line"`
}
type SearchOutput struct {
	Source       string     `json:"source"`
	Matches      []LogEntry `json:"matches"`
	ScannedFiles int        `json:"scanned_files"`
	ScannedBytes int64      `json:"scanned_bytes"`
	Truncated    bool       `json:"truncated"`
}

type TraceInput struct {
	TraceID string   `json:"trace_id"`
	Sources []string `json:"sources"`
	Limit   int      `json:"limit,omitempty"`
}
type TraceEntry struct {
	Source string `json:"source"`
	File   string `json:"file"`
	Line   string `json:"line"`
}
type TraceOutput struct {
	Entries      []TraceEntry `json:"entries"`
	ScannedFiles int          `json:"scanned_files"`
	ScannedBytes int64        `json:"scanned_bytes"`
	Truncated    bool         `json:"truncated"`
}

type ListRuntimeSourcesInput struct{}
type ListRuntimeSourcesOutput struct {
	Sources []SourceInfo `json:"sources"`
}
type GetRuntimeInput struct {
	Source string `json:"source"`
}
type BinaryInfo struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	ModifiedAt  string `json:"modified_at,omitempty"`
	GoVersion   string `json:"go_version,omitempty"`
	Module      string `json:"module,omitempty"`
	Version     string `json:"version,omitempty"`
	VCSRevision string `json:"vcs_revision,omitempty"`
	VCSTime     string `json:"vcs_time,omitempty"`
	VCSModified string `json:"vcs_modified,omitempty"`
	Error       string `json:"error,omitempty"`
}
type ConfigInfo struct {
	Name       string            `json:"name"`
	SizeBytes  int64             `json:"size_bytes,omitempty"`
	ModifiedAt string            `json:"modified_at,omitempty"`
	SHA256     string            `json:"sha256,omitempty"`
	Values     map[string]string `json:"values,omitempty"`
	Error      string            `json:"error,omitempty"`
}
type GetRuntimeOutput struct {
	Source      string       `json:"source"`
	Binaries    []BinaryInfo `json:"binaries"`
	ConfigFiles []ConfigInfo `json:"config_files"`
}
