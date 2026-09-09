package testapi

type CallInput struct {
	URL     string            `json:"url" jsonschema:"absolute HTTP or HTTPS URL to call"`
	Method  string            `json:"method,omitempty" jsonschema:"HTTP method; defaults to GET"`
	Query   map[string]string `json:"query,omitempty" jsonschema:"URL query parameters"`
	Headers map[string]string `json:"headers,omitempty" jsonschema:"HTTP request headers"`
	Body    any               `json:"body,omitempty" jsonschema:"request body; strings are sent verbatim and other values are JSON encoded"`
}

type CallOutput struct {
	URL         string `json:"url"`
	Method      string `json:"method"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type,omitempty"`
	Body        any    `json:"body,omitempty"`
	ElapsedMS   int64  `json:"elapsed_ms"`
	Truncated   bool   `json:"truncated,omitempty"`
}
