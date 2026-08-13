package usersnapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/olaola-chat/ps-devtools-mcp/internal/testdb"
)

const maxUID = uint64(^uint32(0))

type queryClient interface {
	Query(context.Context, testdb.QueryInput) (testdb.QueryOutput, error)
}

type Service struct {
	client queryClient
	logger *log.Logger
}

type sqlResult struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

func NewService(client queryClient, logger *log.Logger) *Service {
	return &Service{client: client, logger: logger}
}

func (s *Service) Get(ctx context.Context, input GetInput) (GetOutput, error) {
	if input.UID == 0 || input.UID > maxUID {
		return GetOutput{}, fmt.Errorf("uid must be between 1 and %d", maxUID)
	}
	limit := input.BackpackLimit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return GetOutput{}, fmt.Errorf("backpack_limit must be between 1 and 100")
	}
	uid := strconv.FormatUint(input.UID, 10)
	started := time.Now()
	output := GetOutput{UID: input.UID}
	output.User = s.querySection(ctx, "SELECT uid, app_id, sex, role, online_status, online_dateline, deleted, bigarea_id, language, display_language FROM xs_user_profile WHERE uid = "+uid+" LIMIT 1", 1)
	output.VIP = s.querySection(ctx, "SELECT id, uid, level, vip_expire_time, rebate_expire_time, create_time, update_time FROM xs_user_vip WHERE uid = "+uid+" ORDER BY level DESC, id DESC LIMIT 20", 20)
	queryLimit := strconv.Itoa(limit + 1)
	output.Backpack.Commodities = s.querySection(ctx, "SELECT id, cid, type, uid, can_give, source, give_count, status, seconds, period_end, dateline, num FROM xs_user_commodity WHERE uid = "+uid+" ORDER BY id DESC LIMIT "+queryLimit, limit)
	output.Backpack.Equipped = s.querySection(ctx, "SELECT id, cid, type, uid, period_end, dress_status, dateline, updateline FROM xs_user_commodity_spu WHERE uid = "+uid+" ORDER BY id DESC LIMIT "+queryLimit, limit)
	output.Backpack.PropCards = s.querySection(ctx, "SELECT id, uid, prop_card_id, num, send_num, expired_time, dateline, prop_card_type, send_times FROM xs_user_prop_card WHERE uid = "+uid+" ORDER BY id DESC LIMIT "+queryLimit, limit)
	if s.logger != nil {
		hash := sha256.Sum256([]byte(uid))
		s.logger.Printf("tool=get_test_user_snapshot uid_hash=%s backpack_limit=%d partial_errors=%d duration_ms=%d",
			hex.EncodeToString(hash[:8]), limit, countErrors(output), time.Since(started).Milliseconds())
	}
	return output, nil
}

func (s *Service) querySection(ctx context.Context, statement string, limit int) Section {
	section := Section{Rows: make([]map[string]any, 0)}
	result, err := s.client.Query(ctx, testdb.QueryInput{Statement: statement, Engine: testdb.EngineXianshiSQL})
	if err != nil {
		section.Error = err.Error()
		return section
	}
	var decoded sqlResult
	if err := json.Unmarshal([]byte(result.ResultJSON), &decoded); err != nil {
		section.Error = "query result could not be decoded"
		return section
	}
	if len(decoded.Rows) > limit {
		decoded.Rows = decoded.Rows[:limit]
		section.Truncated = true
	}
	for _, values := range decoded.Rows {
		if len(values) != len(decoded.Columns) {
			section.Error = "query result column count mismatch"
			section.Rows = nil
			return section
		}
		row := make(map[string]any, len(values))
		for index, value := range values {
			row[decoded.Columns[index]] = value
		}
		section.Rows = append(section.Rows, row)
	}
	return section
}

func countErrors(output GetOutput) int {
	count := 0
	for _, section := range []Section{output.User, output.VIP, output.Backpack.Commodities, output.Backpack.Equipped, output.Backpack.PropCards} {
		if section.Error != "" {
			count++
		}
	}
	return count
}
