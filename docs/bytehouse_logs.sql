CREATE TABLE
  gravitex_api_test.logs UUID '219b072d-f9af-41ff-84d5-9dc1676d7945' (
    `id` Int64 DEFAULT generateSnowflakeID(),
    `user_id` Nullable(Int64) DEFAULT 0,
    `created_at` Nullable(Int64) DEFAULT 0,
    `type` Nullable(Int32) DEFAULT 0,
    `content` Nullable(String) DEFAULT '',
    `username` Nullable(String) DEFAULT '',
    `token_name` Nullable(String) DEFAULT '',
    `model_name` LowCardinality(Nullable(String)) DEFAULT '',
    `quota` Nullable(Int64) DEFAULT 0,
    `prompt_tokens` Nullable(Int64) DEFAULT 0,
    `completion_tokens` Nullable(Int64) DEFAULT 0,
    `use_time` Nullable(Int64) DEFAULT 0,
    `is_stream` Nullable(UInt8) DEFAULT 0,
    `channel_id` Nullable(Int64) DEFAULT 0,
    `channel_name` Nullable(String) DEFAULT '',
    `token_id` Nullable(Int64) DEFAULT 0,
    `group` LowCardinality(Nullable(String)) DEFAULT '',
    `ip` Nullable(String) DEFAULT '',
    `request_id` Nullable(String) DEFAULT '',
    `upstream_request_id` Nullable(String) DEFAULT '',
    `other` Nullable(String) DEFAULT '',
    INDEX idx_request_id request_id TYPE bloom_filter GRANULARITY 4,
    INDEX idx_username username TYPE bloom_filter GRANULARITY 4,
    INDEX idx_token_id token_id TYPE minmax GRANULARITY 4,
    INDEX idx_ip ip TYPE bloom_filter GRANULARITY 4
  ) ENGINE = CnchMergeTree
PARTITION BY
  toYYYYMM(toDateTime(created_at))
ORDER BY
  (created_at, user_id, id)
SETTINGS
  index_granularity = 8192,
  cnch_vw_write = 'vw-3000990276-gravitex-bytehouse',
  storage_policy = 'cnch_default_hdfs',
  allow_nullable_key = 1,
  storage_dialect_type = 'MYSQL'