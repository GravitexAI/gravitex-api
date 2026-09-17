package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// LogTpmSupplement adds tokens that are deliberately stored outside the two
// legacy token columns. It must return only its own delta: prompt_tokens and
// completion_tokens are always owned by the base query.
type LogTpmSupplement interface {
	Name() string
	DeltaExpression(databaseType common.DatabaseType) (string, error)
}

// ClaudeCacheTpmSupplement counts Claude cache read and cache creation tokens.
// cache_write_tokens, cache_creation_tokens, and the 5m/1h split are alternate
// representations of the same creation amount: each row selects exactly one.
type ClaudeCacheTpmSupplement struct{}

func (ClaudeCacheTpmSupplement) Name() string { return "claude-cache-tpm" }

func (ClaudeCacheTpmSupplement) DeltaExpression(databaseType common.DatabaseType) (string, error) {
	switch databaseType {
	case common.DatabaseTypeClickHouse:
		return "COALESCE(SUM(if(JSONExtractBool(other, 'claude') OR JSONExtractString(other, 'usage_semantic') = 'anthropic', JSONExtractInt(other, 'cache_tokens') + if(JSONExtractInt(other, 'cache_write_tokens') > 0, JSONExtractInt(other, 'cache_write_tokens'), if(JSONExtractInt(other, 'cache_creation_tokens') > 0, JSONExtractInt(other, 'cache_creation_tokens'), JSONExtractInt(other, 'cache_creation_tokens_5m') + JSONExtractInt(other, 'cache_creation_tokens_1h'))), 0)), 0)", nil
	case common.DatabaseTypeSQLite:
		return "COALESCE(SUM(CASE WHEN COALESCE(json_extract(other, '$.claude'), 0) = 1 OR COALESCE(json_extract(other, '$.usage_semantic'), '') = 'anthropic' THEN COALESCE(json_extract(other, '$.cache_tokens'), 0) + CASE WHEN COALESCE(json_extract(other, '$.cache_write_tokens'), 0) > 0 THEN json_extract(other, '$.cache_write_tokens') WHEN COALESCE(json_extract(other, '$.cache_creation_tokens'), 0) > 0 THEN json_extract(other, '$.cache_creation_tokens') ELSE COALESCE(json_extract(other, '$.cache_creation_tokens_5m'), 0) + COALESCE(json_extract(other, '$.cache_creation_tokens_1h'), 0) END ELSE 0 END), 0)", nil
	case common.DatabaseTypeMySQL:
		return "COALESCE(SUM(CASE WHEN JSON_UNQUOTE(JSON_EXTRACT(other, '$.claude')) = 'true' OR JSON_UNQUOTE(JSON_EXTRACT(other, '$.usage_semantic')) = 'anthropic' THEN COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_tokens')) AS SIGNED), 0) + CASE WHEN COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_write_tokens')) AS SIGNED), 0) > 0 THEN CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_write_tokens')) AS SIGNED) WHEN COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens')) AS SIGNED), 0) > 0 THEN CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens')) AS SIGNED) ELSE COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens_5m')) AS SIGNED), 0) + COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(other, '$.cache_creation_tokens_1h')) AS SIGNED), 0) END ELSE 0 END), 0)", nil
	case common.DatabaseTypePostgreSQL:
		return "COALESCE(SUM(CASE WHEN COALESCE(other::jsonb ->> 'claude', '') = 'true' OR COALESCE(other::jsonb ->> 'usage_semantic', '') = 'anthropic' THEN COALESCE((other::jsonb ->> 'cache_tokens')::bigint, 0) + CASE WHEN COALESCE((other::jsonb ->> 'cache_write_tokens')::bigint, 0) > 0 THEN (other::jsonb ->> 'cache_write_tokens')::bigint WHEN COALESCE((other::jsonb ->> 'cache_creation_tokens')::bigint, 0) > 0 THEN (other::jsonb ->> 'cache_creation_tokens')::bigint ELSE COALESCE((other::jsonb ->> 'cache_creation_tokens_5m')::bigint, 0) + COALESCE((other::jsonb ->> 'cache_creation_tokens_1h')::bigint, 0) END ELSE 0 END), 0)", nil
	default:
		return "", fmt.Errorf("unsupported log database type %q", databaseType)
	}
}

var defaultLogTpmSupplements = []LogTpmSupplement{ClaudeCacheTpmSupplement{}}

func sumLogTpmSupplements(baseQuery *gorm.DB, supplements []LogTpmSupplement) (int64, error) {
	var total int64
	for _, supplement := range supplements {
		expression, err := supplement.DeltaExpression(common.LogDatabaseType())
		if err != nil {
			return 0, fmt.Errorf("build %s query: %w", supplement.Name(), err)
		}
		var delta int64
		if err := baseQuery.Session(&gorm.Session{}).Select(expression).Row().Scan(&delta); err != nil {
			return 0, fmt.Errorf("query %s: %w", supplement.Name(), err)
		}
		total += delta
	}
	return total, nil
}
