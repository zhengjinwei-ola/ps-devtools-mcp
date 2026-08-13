package testdb

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const maxSQLLength = 8192

var (
	allowedPrefixes = []string{"SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "WITH"}
	forbiddenSQL    = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|TRUNCATE|ALTER|CREATE|REPLACE|RENAME|GRANT|REVOKE|CALL|LOAD|LOAD_FILE|LOCK|UNLOCK|SET|EXEC|EXECUTE|PREPARE|DEALLOCATE|HANDLER|PROCEDURE|FUNCTION|TRIGGER|EVENT|OUTFILE|DUMPFILE|INFILE|INTO|FOR\s+UPDATE|LOCK\s+IN\s+SHARE\s+MODE|SLEEP\s*\(|BENCHMARK\s*\(|GET_LOCK\s*\(|RELEASE_LOCK\s*\()\b`)
	suspiciousSQL   = regexp.MustCompile(`(?i)(0x[0-9a-f]{8,}|unhex\s*\(|extractvalue\s*\(|updatexml\s*\(|sleep\s*\(|benchmark\s*\()`)
	limitClause     = regexp.MustCompile(`(?i)\bLIMIT\s+\d+`)
)

// ValidateReadOnlySQL applies a deliberately conservative client-side policy.
// The external service validates again; this layer prevents unsafe requests
// from leaving the developer machine and is not the database security boundary.
func ValidateReadOnlySQL(statement string) error {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return fmt.Errorf("sql statement is empty")
	}
	if len(statement) > maxSQLLength {
		return fmt.Errorf("sql statement too long (max %d bytes)", maxSQLLength)
	}
	if strings.Contains(statement, "\x00") {
		return fmt.Errorf("sql contains a null byte")
	}
	trimmed := strings.TrimSuffix(statement, ";")
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("multiple sql statements are not allowed")
	}

	normalized, err := normalizeForValidation(trimmed)
	if err != nil {
		return err
	}
	if err := rejectDisallowedCharacters(normalized); err != nil {
		return err
	}
	upper := strings.ToUpper(strings.TrimSpace(normalized))
	if !hasAllowedPrefix(upper) {
		return fmt.Errorf("only SELECT, SHOW, DESCRIBE, DESC, EXPLAIN, and WITH are allowed")
	}
	if forbiddenSQL.MatchString(normalized) {
		return fmt.Errorf("sql contains a forbidden keyword or operation")
	}
	if suspiciousSQL.MatchString(normalized) {
		return fmt.Errorf("sql contains a suspicious function or expression")
	}
	if (strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH")) && !limitClause.MatchString(normalized) {
		return fmt.Errorf("SELECT and WITH queries must include a numeric LIMIT")
	}
	return nil
}

func hasAllowedPrefix(statement string) bool {
	for _, prefix := range allowedPrefixes {
		if statement == prefix || strings.HasPrefix(statement, prefix+" ") {
			return true
		}
	}
	return false
}

func normalizeForValidation(statement string) (string, error) {
	var result strings.Builder
	for i := 0; i < len(statement); {
		ch := statement[i]
		switch ch {
		case '\'', '"', '`':
			quote := ch
			result.WriteByte(' ')
			i++
			closed := false
			for i < len(statement) {
				if quote != '`' && statement[i] == '\\' {
					if i+1 >= len(statement) {
						return "", fmt.Errorf("sql has an invalid escape sequence")
					}
					i += 2
					continue
				}
				if statement[i] == quote {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "", fmt.Errorf("sql contains an unclosed quoted value")
			}
			result.WriteByte(' ')
		case '-':
			if i+1 < len(statement) && statement[i+1] == '-' {
				return "", fmt.Errorf("sql comments are not allowed")
			}
			result.WriteByte(ch)
			i++
		case '#':
			return "", fmt.Errorf("sql comments are not allowed")
		case '/':
			if i+1 < len(statement) && (statement[i+1] == '*' || statement[i+1] == '!') {
				return "", fmt.Errorf("sql comments are not allowed")
			}
			result.WriteByte(ch)
			i++
		case ';':
			return "", fmt.Errorf("multiple sql statements are not allowed")
		case '\\':
			return "", fmt.Errorf("sql escape sequences outside strings are not allowed")
		default:
			result.WriteByte(ch)
			i++
		}
	}
	return strings.Join(strings.Fields(result.String()), " "), nil
}

func rejectDisallowedCharacters(statement string) error {
	for _, char := range statement {
		switch {
		case unicode.IsLetter(char), unicode.IsDigit(char):
		case char == ' ', char == '\t', char == '\n', char == '\r':
		case strings.ContainsRune(`_.,'"`+"`"+`()-=<>+*/%|&^~[]?:@`, char):
		default:
			return fmt.Errorf("sql contains disallowed character %q", char)
		}
	}
	return nil
}
