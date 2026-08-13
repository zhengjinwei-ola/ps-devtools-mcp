package testredis

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	maxCommandLength = 4096
	maxRangeItems    = 500
	maxScanCount     = 500
)

var fixedArity = map[string]struct{ min, max int }{
	"GET": {2, 2}, "MGET": {2, 51}, "STRLEN": {2, 2}, "EXISTS": {2, 51},
	"TYPE": {2, 2}, "TTL": {2, 2}, "PTTL": {2, 2},
	"HGET": {3, 3}, "HMGET": {3, 52}, "HLEN": {2, 2}, "HEXISTS": {3, 3},
	"LLEN": {2, 2}, "LINDEX": {3, 3},
	"SCARD": {2, 2}, "SISMEMBER": {3, 3},
	"ZCARD": {2, 2}, "ZSCORE": {3, 3}, "ZRANK": {3, 3}, "ZREVRANK": {3, 3},
}

// ValidateReadOnlyCommand deliberately exposes a smaller command set than the
// upstream test endpoint. Commands that can return an unbounded collection are
// rejected locally even though the server also enforces read-only semantics.
func ValidateReadOnlyCommand(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("redis command is empty")
	}
	if len(command) > maxCommandLength {
		return fmt.Errorf("redis command too long (max %d bytes)", maxCommandLength)
	}
	if strings.ContainsAny(command, ";\r\n\x00") {
		return fmt.Errorf("redis command contains a separator or invalid character")
	}
	parts := strings.Fields(command)
	name := strings.ToUpper(parts[0])
	if arity, ok := fixedArity[name]; ok {
		if len(parts) < arity.min || len(parts) > arity.max {
			return fmt.Errorf("%s expects %d to %d tokens", name, arity.min, arity.max)
		}
		return nil
	}
	switch name {
	case "LRANGE", "ZRANGE", "ZREVRANGE":
		return validateRange(name, parts)
	case "SCAN":
		return validateScan(name, parts, 2)
	case "HSCAN", "SSCAN", "ZSCAN":
		return validateScan(name, parts, 3)
	default:
		return fmt.Errorf("redis command %q is not allowed", name)
	}
}

func validateRange(name string, parts []string) error {
	if len(parts) != 4 {
		return fmt.Errorf("%s expects key, start, and stop", name)
	}
	start, err := strconv.Atoi(parts[2])
	if err != nil || start < 0 {
		return fmt.Errorf("%s start must be a non-negative integer", name)
	}
	stop, err := strconv.Atoi(parts[3])
	if err != nil || stop < start || stop-start+1 > maxRangeItems {
		return fmt.Errorf("%s range must contain at most %d items", name, maxRangeItems)
	}
	return nil
}

func validateScan(name string, parts []string, baseTokens int) error {
	if len(parts) < baseTokens {
		return fmt.Errorf("%s requires a cursor", name)
	}
	if _, err := strconv.ParseUint(parts[baseTokens-1], 10, 64); err != nil {
		return fmt.Errorf("%s cursor must be a non-negative integer", name)
	}
	seenCount := false
	for i := baseTokens; i < len(parts); {
		switch strings.ToUpper(parts[i]) {
		case "MATCH":
			if i+1 >= len(parts) || parts[i+1] == "" {
				return fmt.Errorf("%s MATCH requires a pattern", name)
			}
			i += 2
		case "COUNT":
			if i+1 >= len(parts) || seenCount {
				return fmt.Errorf("%s COUNT requires one value", name)
			}
			count, err := strconv.Atoi(parts[i+1])
			if err != nil || count < 1 || count > maxScanCount {
				return fmt.Errorf("%s COUNT must be between 1 and %d", name, maxScanCount)
			}
			seenCount = true
			i += 2
		default:
			return fmt.Errorf("%s only supports MATCH and COUNT options", name)
		}
	}
	if !seenCount {
		return fmt.Errorf("%s requires COUNT with a maximum of %d", name, maxScanCount)
	}
	return nil
}
