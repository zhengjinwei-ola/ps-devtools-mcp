package testredis

type QueryInput struct {
	Command string `json:"command" jsonschema:"one bounded read-only Redis command; unbounded collection commands are rejected"`
}

type QueryOutput struct {
	ResultJSON string `json:"result_json"`
	ElapsedMS  int64  `json:"elapsed_ms"`
}
