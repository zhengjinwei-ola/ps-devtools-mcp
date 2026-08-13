package testdb

const (
	EngineXianshiSQL = 1
	EngineRedis      = 2
	EngineConfigSQL  = 3
)

type QueryInput struct {
	Statement string `json:"statement" jsonschema:"one read-only SQL statement; SELECT and WITH require LIMIT"`
	Engine    int    `json:"engine" jsonschema:"1 for xianshi SQL or 3 for config SQL"`
}

type QueryOutput struct {
	Engine     int    `json:"engine"`
	ResultJSON string `json:"result_json"`
	RowCount   int64  `json:"row_count"`
	ElapsedMS  int64  `json:"elapsed_ms"`
}
