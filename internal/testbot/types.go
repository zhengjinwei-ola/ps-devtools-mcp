package testbot

type CallInput struct {
	Endpoint          string            `json:"endpoint" jsonschema:"configured testbot endpoint name"`
	Query             map[string]string `json:"query,omitempty" jsonschema:"allowlisted URL query parameters"`
	Body              map[string]string `json:"body,omitempty" jsonschema:"allowlisted request body fields"`
	ConfirmSideEffect bool              `json:"confirm_side_effect,omitempty" jsonschema:"must be true for an endpoint configured with side effects"`
}

type CallOutput struct {
	Endpoint    string `json:"endpoint"`
	RequestID   string `json:"request_id"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type,omitempty"`
	Body        any    `json:"body,omitempty"`
	ElapsedMS   int64  `json:"elapsed_ms"`
	Truncated   bool   `json:"truncated,omitempty"`
}

type ListInput struct{}

type EndpointInfo struct {
	Name       string `json:"name"`
	Method     string `json:"method"`
	SideEffect bool   `json:"side_effect"`
}

type ListOutput struct {
	Endpoints []EndpointInfo `json:"endpoints"`
}
