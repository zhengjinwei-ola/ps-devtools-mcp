package testapi

type CallInput struct {
	Endpoint string            `json:"endpoint" jsonschema:"configured read-only endpoint name"`
	Query    map[string]string `json:"query,omitempty" jsonschema:"allowlisted URL query parameters"`
	Body     map[string]any    `json:"body,omitempty" jsonschema:"allowlisted JSON body fields"`
}

type CallOutput struct {
	Endpoint    string `json:"endpoint"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type,omitempty"`
	Body        any    `json:"body,omitempty"`
	ElapsedMS   int64  `json:"elapsed_ms"`
	Truncated   bool   `json:"truncated,omitempty"`
}
