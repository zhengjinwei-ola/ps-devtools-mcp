package redisinspect

type InspectInput struct {
	Key      string `json:"key" jsonschema:"exact Redis key without whitespace"`
	MaxItems int    `json:"max_items,omitempty" jsonschema:"maximum collection items; 1 to 500"`
}

type InspectOutput struct {
	Key        string `json:"key"`
	Type       string `json:"type"`
	TTLSeconds int64  `json:"ttl_seconds"`
	Value      any    `json:"value,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	ValueError string `json:"value_error,omitempty"`
}
