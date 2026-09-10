package testdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// 受限 DDL 白名单：仅放行测试环境建表/加列两类操作。
// 设计约束（与 query_test_db 的只读防线同一思路）：
//   - 只允许 engine=1（xianshi 库）
//   - 仅 CREATE TABLE ... (IF NOT EXISTS) 与 ALTER TABLE ... ADD [COLUMN] 两种前缀
//   - 禁止危险关键字（DROP/DELETE/UPDATE/INSERT/RENAME/MODIFY/CHANGE 等），防拼夹带
//   - 单条语句、去注释、去字符串字面量后校验，防注释/字面量内夹带
var (
	ddlForbidden = regexp.MustCompile(`(?i)\b(DROP|DELETE|UPDATE|INSERT|TRUNCATE|RENAME|MODIFY|CHANGE|GRANT|REVOKE|CREATE\s+(DATABASE|INDEX|VIEW|TRIGGER|PROCEDURE|FUNCTION|USER)|SET|USE|LOCK|UNLOCK|CALL|ALTER\s+TABLE\s+\S+\s+(DROP|RENAME|MODIFY|CHANGE|CONVERT|ENGINE|AUTO_INCREMENT|DEFAULT|COMMENT\b\s*(=|\s)))`)

	// 注意：ddlNormalize 会把反引号标识符/字符串字面量替换为空格，因此匹配的是"去标识符后"的骨架：
	//   CREATE TABLE (IF NOT EXISTS)? <列定义>    /    ALTER TABLE <表名> ADD (COLUMN)? <列定义>
	// 表名已被剥掉，所以 ALTER 分支用"TABLE 后跳过一段非保留字表名再遇到 ADD"的结构匹配。
	ddlCreateTable = regexp.MustCompile(`(?i)^CREATE\s+TABLE\s+(IF\s+NOT\s+EXISTS\s+)?[^(]*\(`)
	ddlAddColumn   = regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+[^(]*?\bADD\s+(COLUMN\s+)?[^;]+$`)
)

// ExecDDLInput 受限 DDL 输入：仅 engine=1（xianshi）
type ExecDDLInput struct {
	Statement string `json:"statement" jsonschema:"one DDL statement; only CREATE TABLE [IF NOT EXISTS] or ALTER TABLE ... ADD [COLUMN] allowed, engine must be 1"`
	Engine    int    `json:"engine" jsonschema:"must be 1 (xianshi SQL)"`
}

// ExecDDLOutput 执行结果
type ExecDDLOutput struct {
	Engine    int    `json:"engine"`
	Affected  int64  `json:"affected"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Message   string `json:"message"`
}

// ValidateDDLSQL 校验一条受限 DDL：白名单前缀 + 危险关键字拒绝 + 单条语句
func ValidateDDLSQL(statement string) error {
	statement = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(statement), ";"))
	if statement == "" {
		return fmt.Errorf("ddl statement is empty")
	}
	if len(statement) > maxSQLLength {
		return fmt.Errorf("ddl statement too long (max %d bytes)", maxSQLLength)
	}
	if strings.Contains(statement, "\x00") {
		return fmt.Errorf("ddl statement contains a null byte")
	}
	if strings.Contains(statement, `"`) {
		return fmt.Errorf("ddl mode does not allow double-quoted SQL; use single quotes for values and backticks for identifiers")
	}
	// 去注释、去字面量后校验（复用只读校验的规范化思路，自行实现以区分错误语义）
	normalized, err := ddlNormalize(statement)
	if err != nil {
		return err
	}
	if !ddlCreateTable.MatchString(normalized) && !ddlAddColumn.MatchString(normalized) {
		return fmt.Errorf("only CREATE TABLE [IF NOT EXISTS] or ALTER TABLE ... ADD [COLUMN] statements are allowed")
	}
	if ddlForbidden.MatchString(normalized) {
		return fmt.Errorf("ddl statement contains a forbidden keyword or operation")
	}
	return nil
}

// ddlNormalize 去掉字符串字面量与注释，压平空白，供白名单正则匹配
func ddlNormalize(statement string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(statement); i++ {
		ch := statement[i]
		switch {
		case ch == '\'' || ch == '"' || ch == '`':
			quote := ch
			b.WriteByte(' ')
			i++
			closed := false
			for i < len(statement) {
				if quote != '`' && statement[i] == '\\' {
					if i+1 >= len(statement) {
						return "", fmt.Errorf("ddl statement has an invalid escape sequence")
					}
					i += 2
					continue
				}
				if statement[i] == quote {
					if quote == '\'' && i+1 < len(statement) && statement[i+1] == '\'' {
						i += 2
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "", fmt.Errorf("ddl statement contains an unclosed quoted value")
			}
			b.WriteByte(' ')
		case ch == '-' && i+1 < len(statement) && statement[i+1] == '-':
			return "", fmt.Errorf("ddl statements do not allow comments; strip them before executing")
		case ch == '#':
			return "", fmt.Errorf("ddl statements do not allow comments; strip them before executing")
		case ch == '/' && i+1 < len(statement) && (statement[i+1] == '*' || statement[i+1] == '!'):
			return "", fmt.Errorf("ddl statements do not allow comments; strip them before executing")
		default:
			b.WriteByte(ch)
		}
	}
	return strings.Join(strings.Fields(b.String()), " "), nil
}

// ExecDDL 执行一条受限 DDL（engine=1 xianshi 库）
func (s *Service) ExecDDL(ctx context.Context, input ExecDDLInput) (ExecDDLOutput, error) {
	input.Statement = strings.TrimSpace(input.Statement)
	if input.Engine != EngineXianshiSQL {
		return ExecDDLOutput{}, fmt.Errorf("ddl execution is restricted to engine 1 (xianshi)")
	}
	if err := ValidateDDLSQL(input.Statement); err != nil {
		return ExecDDLOutput{}, fmt.Errorf("validate ddl: %w", err)
	}
	mysqlClient, ok := s.client.(interface {
		ExecDDL(ctx context.Context, statement string) (int64, error)
	})
	if !ok {
		return ExecDDLOutput{}, fmt.Errorf("ddl execution requires the direct mysql client")
	}
	started := time.Now()
	affected, err := mysqlClient.ExecDDL(ctx, input.Statement)
	duration := time.Since(started)
	if s.logger != nil {
		sum := sha256.Sum256([]byte(input.Statement))
		s.logger.Printf("tool=exec_test_db_ddl engine=1 ddl_hash=%s duration_ms=%d affected=%d error=%t",
			hex.EncodeToString(sum[:8]), duration.Milliseconds(), affected, err != nil)
	}
	if err != nil {
		return ExecDDLOutput{Engine: input.Engine, ElapsedMS: duration.Milliseconds()}, err
	}
	return ExecDDLOutput{
		Engine: input.Engine, Affected: affected, ElapsedMS: duration.Milliseconds(),
		Message: "ddl executed",
	}, nil
}
