package testdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	defaultMaxRows       = 500
	defaultMaxResultBody = 2 << 20
)

var safeDatabaseName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

var qualifiedIdentifier = regexp.MustCompile("(?i)(?:`[^`]+`|[a-z_][a-z0-9_$]*)\\s*\\.\\s*(?:`[^`]+`|[a-z_][a-z0-9_$]*)")

var allowedDirectShow = []*regexp.Regexp{
	regexp.MustCompile("(?i)^SHOW\\s+(?:FULL\\s+)?TABLES(?:\\s+(?:LIKE|WHERE)\\b.*)?$"),
	regexp.MustCompile("(?i)^SHOW\\s+(?:FULL\\s+)?(?:COLUMNS|FIELDS)\\s+FROM\\s+`?[a-z0-9_$]+`?(?:\\s+(?:LIKE|WHERE)\\b.*)?$"),
	regexp.MustCompile("(?i)^SHOW\\s+(?:INDEX|INDEXES|KEYS)\\s+FROM\\s+`?[a-z0-9_$]+`?(?:\\s+WHERE\\b.*)?$"),
	regexp.MustCompile("(?i)^SHOW\\s+CREATE\\s+TABLE\\s+`?[a-z0-9_$]+`?$"),
	regexp.MustCompile("(?i)^SHOW\\s+TABLE\\s+STATUS(?:\\s+(?:LIKE|WHERE)\\b.*)?$"),
}

type MySQLConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	XianshiDatabase string
	ConfigDatabase  string
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
	MaxRows         int
	MaxResultBody   int
}

type MySQLClient struct {
	xianshi      *sql.DB
	config       *sql.DB
	queryTimeout time.Duration
	maxRows      int
	maxBody      int
}

type directResult struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

func OpenMySQLClient(ctx context.Context, config MySQLConfig) (*MySQLClient, error) {
	if err := validateMySQLConfig(config); err != nil {
		return nil, err
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 5 * time.Second
	}
	if config.QueryTimeout == 0 {
		config.QueryTimeout = 10 * time.Second
	}
	if config.MaxRows == 0 {
		config.MaxRows = defaultMaxRows
	}
	if config.MaxResultBody == 0 {
		config.MaxResultBody = defaultMaxResultBody
	}

	open := func(database string) (*sql.DB, error) {
		driverConfig := mysql.NewConfig()
		driverConfig.User = config.User
		driverConfig.Passwd = config.Password
		driverConfig.Net = "tcp"
		driverConfig.Addr = net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port))
		driverConfig.DBName = database
		driverConfig.ParseTime = true
		driverConfig.Timeout = config.ConnectTimeout
		driverConfig.ReadTimeout = config.QueryTimeout
		driverConfig.WriteTimeout = config.ConnectTimeout
		driverConfig.Params = map[string]string{"charset": "utf8mb4"}

		db, err := sql.Open("mysql", driverConfig.FormatDSN())
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(2)
		db.SetConnMaxLifetime(5 * time.Minute)
		pingCtx, cancel := context.WithTimeout(ctx, config.ConnectTimeout)
		defer cancel()
		if err := db.PingContext(pingCtx); err != nil {
			db.Close()
			return nil, err
		}
		return db, nil
	}

	xianshiDB, err := open(config.XianshiDatabase)
	if err != nil {
		return nil, fmt.Errorf("connect xianshi test database: %w", err)
	}
	configDB, err := open(config.ConfigDatabase)
	if err != nil {
		xianshiDB.Close()
		return nil, fmt.Errorf("connect config test database: %w", err)
	}
	return newMySQLClient(xianshiDB, configDB, config.QueryTimeout, config.MaxRows, config.MaxResultBody), nil
}

func newMySQLClient(xianshiDB, configDB *sql.DB, queryTimeout time.Duration, maxRows, maxBody int) *MySQLClient {
	return &MySQLClient{
		xianshi: xianshiDB, config: configDB, queryTimeout: queryTimeout,
		maxRows: maxRows, maxBody: maxBody,
	}
}

func validateMySQLConfig(config MySQLConfig) error {
	if strings.TrimSpace(config.Host) == "" || strings.ContainsAny(config.Host, "\x00\r\n/\\") {
		return fmt.Errorf("test database host is invalid")
	}
	if config.Port < 1 || config.Port > 65535 {
		return fmt.Errorf("test database port must be between 1 and 65535")
	}
	if config.User == "" || config.Password == "" {
		return fmt.Errorf("test database user and password are required")
	}
	for label, database := range map[string]string{
		"xianshi": config.XianshiDatabase,
		"config":  config.ConfigDatabase,
	} {
		if !safeDatabaseName.MatchString(database) {
			return fmt.Errorf("%s database name is invalid", label)
		}
	}
	if config.ConnectTimeout < 0 || config.QueryTimeout < 0 || config.MaxRows < 0 || config.MaxResultBody < 0 {
		return fmt.Errorf("test database limits cannot be negative")
	}
	return nil
}

func (c *MySQLClient) Query(ctx context.Context, input QueryInput) (QueryOutput, error) {
	if err := ValidateReadOnlySQL(input.Statement); err != nil {
		return QueryOutput{}, fmt.Errorf("validate direct sql: %w", err)
	}
	if err := validateDirectDatabaseScope(input.Statement); err != nil {
		return QueryOutput{}, err
	}
	db, err := c.database(input.Engine)
	if err != nil {
		return QueryOutput{}, err
	}
	queryCtx := ctx
	cancel := func() {}
	if c.queryTimeout > 0 {
		queryCtx, cancel = context.WithTimeout(ctx, c.queryTimeout)
	}
	defer cancel()

	started := time.Now()
	tx, err := db.BeginTx(queryCtx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return QueryOutput{}, fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(queryCtx, input.Statement)
	if err != nil {
		return QueryOutput{}, fmt.Errorf("execute read-only query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return QueryOutput{}, fmt.Errorf("read result columns: %w", err)
	}
	result := directResult{Columns: columns, Rows: make([][]any, 0)}
	for rows.Next() {
		if len(result.Rows) >= c.maxRows {
			return QueryOutput{}, fmt.Errorf("query result exceeds %d rows; narrow the query", c.maxRows)
		}
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return QueryOutput{}, fmt.Errorf("scan query result: %w", err)
		}
		for index, value := range values {
			if raw, ok := value.([]byte); ok {
				values[index] = string(raw)
			}
		}
		result.Rows = append(result.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return QueryOutput{}, fmt.Errorf("iterate query result: %w", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return QueryOutput{}, fmt.Errorf("encode query result: %w", err)
	}
	if len(encoded) > c.maxBody {
		return QueryOutput{}, fmt.Errorf("query result exceeds %d bytes; narrow the query", c.maxBody)
	}
	return QueryOutput{
		Engine: input.Engine, ResultJSON: string(encoded), RowCount: int64(len(result.Rows)),
		ElapsedMS: time.Since(started).Milliseconds(),
	}, nil
}

// validateDirectDatabaseScope prevents a privileged local credential from
// escaping the database selected by the engine. The conservative rule also
// rejects alias-qualified columns; callers can use unqualified identifiers or
// the external query endpoint when a more complex diagnostic query is needed.
func validateDirectDatabaseScope(statement string) error {
	if strings.Contains(statement, `"`) {
		return fmt.Errorf("direct database mode does not allow double-quoted SQL; use single quotes for values and backticks for identifiers")
	}
	withoutLiterals, err := stripStringLiterals(statement)
	if err != nil {
		return err
	}
	if qualifiedIdentifier.MatchString(withoutLiterals) {
		return fmt.Errorf("direct database mode does not allow schema- or alias-qualified identifiers; select the engine and use unqualified names")
	}
	normalized := strings.Join(strings.Fields(strings.TrimSuffix(withoutLiterals, ";")), " ")
	if strings.HasPrefix(strings.ToUpper(normalized), "SHOW ") {
		for _, allowed := range allowedDirectShow {
			if allowed.MatchString(normalized) {
				return nil
			}
		}
		return fmt.Errorf("direct database mode only allows table-scoped SHOW commands in the selected engine database")
	}
	return nil
}

func stripStringLiterals(statement string) (string, error) {
	var result strings.Builder
	for index := 0; index < len(statement); {
		switch statement[index] {
		case '\'', '"':
			quote := statement[index]
			result.WriteByte(' ')
			index++
			closed := false
			for index < len(statement) {
				if statement[index] == '\\' {
					index += 2
					continue
				}
				if statement[index] == quote {
					if index+1 < len(statement) && statement[index+1] == quote {
						index += 2
						continue
					}
					index++
					closed = true
					break
				}
				index++
			}
			if !closed {
				return "", fmt.Errorf("sql contains an unclosed quoted value")
			}
			result.WriteByte(' ')
		default:
			result.WriteByte(statement[index])
			index++
		}
	}
	return result.String(), nil
}

func (c *MySQLClient) database(engine int) (*sql.DB, error) {
	switch engine {
	case EngineXianshiSQL:
		return c.xianshi, nil
	case EngineConfigSQL:
		return c.config, nil
	default:
		return nil, fmt.Errorf("engine must be %d (xianshi) or %d (config)", EngineXianshiSQL, EngineConfigSQL)
	}
}

func (c *MySQLClient) Close() error {
	return errors.Join(c.xianshi.Close(), c.config.Close())
}
