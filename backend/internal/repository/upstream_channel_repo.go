package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type upstreamChannelRepository struct {
	db *sql.DB
}

func NewUpstreamChannelRepository(db *sql.DB) service.UpstreamChannelRepository {
	return &upstreamChannelRepository{db: db}
}

func (r *upstreamChannelRepository) Create(ctx context.Context, channel *service.UpstreamChannel) error {
	return r.runInTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO upstream_channels (name, description, status)
			 VALUES ($1, $2, $3)
			 RETURNING id, created_at, updated_at`,
			channel.Name, channel.Description, channel.Status,
		).Scan(&channel.ID, &channel.CreatedAt, &channel.UpdatedAt); err != nil {
			if mapped := upstreamUniqueViolationError(err); mapped != nil {
				return mapped
			}
			return fmt.Errorf("insert upstream channel: %w", err)
		}
		for i := range channel.Platforms {
			channel.Platforms[i].ChannelID = channel.ID
			if err := r.savePlatformTx(ctx, tx, &channel.Platforms[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *upstreamChannelRepository) Update(ctx context.Context, channel *service.UpstreamChannel) error {
	return r.runInTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx,
			`UPDATE upstream_channels
			 SET name = $1, description = $2, status = $3, updated_at = NOW()
			 WHERE id = $4`,
			channel.Name, channel.Description, channel.Status, channel.ID,
		)
		if err != nil {
			if mapped := upstreamUniqueViolationError(err); mapped != nil {
				return mapped
			}
			return fmt.Errorf("update upstream channel: %w", err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return service.ErrUpstreamChannelNotFound
		}

		keepPlatformIDs := make([]int64, 0, len(channel.Platforms))
		for i := range channel.Platforms {
			channel.Platforms[i].ChannelID = channel.ID
			if err := r.savePlatformTx(ctx, tx, &channel.Platforms[i]); err != nil {
				return err
			}
			if channel.Platforms[i].ID > 0 {
				keepPlatformIDs = append(keepPlatformIDs, channel.Platforms[i].ID)
			}
		}
		return deleteMissingByParentTx(ctx, tx, "upstream_platforms", "channel_id", channel.ID, keepPlatformIDs)
	})
}

func (r *upstreamChannelRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM upstream_channels WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete upstream channel: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return service.ErrUpstreamChannelNotFound
	}
	return nil
}

func (r *upstreamChannelRepository) GetByID(ctx context.Context, id int64) (*service.UpstreamChannel, error) {
	channel := service.UpstreamChannel{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, status, created_at, updated_at
		 FROM upstream_channels WHERE id = $1`,
		id,
	).Scan(&channel.ID, &channel.Name, &channel.Description, &channel.Status, &channel.CreatedAt, &channel.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, service.ErrUpstreamChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get upstream channel: %w", err)
	}
	channels := []service.UpstreamChannel{channel}
	if err := r.loadNested(ctx, channels); err != nil {
		return nil, err
	}
	return &channels[0], nil
}

func (r *upstreamChannelRepository) List(ctx context.Context, params pagination.PaginationParams, status, search string) ([]service.UpstreamChannel, *pagination.PaginationResult, error) {
	where := []string{"1=1"}
	args := []any{}
	argIdx := 1
	if status != "" {
		where = append(where, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, status)
		argIdx++
	}
	if search != "" {
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR description ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+escapeLike(search)+"%")
		argIdx++
	}
	whereClause := strings.Join(where, " AND ")

	var total int64
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM upstream_channels WHERE %s`, whereClause)
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count upstream channels: %w", err)
	}

	pageSize := params.Limit()
	page := params.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize
	orderBy := upstreamChannelOrderBy(params)
	query := fmt.Sprintf(
		`SELECT id, name, description, status, created_at, updated_at
		 FROM upstream_channels WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		whereClause, orderBy, argIdx, argIdx+1,
	)
	args = append(args, pageSize, offset)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query upstream channels: %w", err)
	}
	defer func() { _ = rows.Close() }()

	channels := make([]service.UpstreamChannel, 0)
	for rows.Next() {
		var ch service.UpstreamChannel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Description, &ch.Status, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan upstream channel: %w", err)
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate upstream channels: %w", err)
	}
	if err := r.loadNested(ctx, channels); err != nil {
		return nil, nil, err
	}
	result := &pagination.PaginationResult{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Pages:    int((total + int64(pageSize) - 1) / int64(pageSize)),
	}
	if result.Pages < 1 {
		result.Pages = 1
	}
	return channels, result, nil
}

func (r *upstreamChannelRepository) UpdatePoolSyncedGroupID(ctx context.Context, poolID int64, groupID int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE upstream_key_pools SET synced_group_id = $1, updated_at = NOW() WHERE id = $2`,
		groupID, poolID,
	)
	if err != nil {
		return fmt.Errorf("update upstream pool synced group: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return service.ErrUpstreamChannelNotFound
	}
	return nil
}

func (r *upstreamChannelRepository) UpdateKeySyncedAccountID(ctx context.Context, keyID int64, accountID int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE upstream_keys SET synced_account_id = $1, updated_at = NOW() WHERE id = $2`,
		accountID, keyID,
	)
	if err != nil {
		return fmt.Errorf("update upstream key synced account: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return service.ErrUpstreamChannelNotFound
	}
	return nil
}

func (r *upstreamChannelRepository) UpdateKeyTestResult(ctx context.Context, keyID int64, result service.UpstreamTestResult) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE upstream_keys
		 SET last_test_latency_ms = $1,
		     last_test_status = $2,
		     last_test_message = $3,
		     last_tested_at = $4,
		     updated_at = NOW()
		 WHERE id = $5`,
		nullableInt(result.LatencyMS), result.Status, result.Message, result.TestedAt, keyID,
	)
	if err != nil {
		return fmt.Errorf("update upstream key test result: %w", err)
	}
	return nil
}

func (r *upstreamChannelRepository) RecordSyncEvent(ctx context.Context, channelID int64, action string, summary map[string]int) error {
	payload, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal upstream sync summary: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO upstream_sync_events (channel_id, action, summary) VALUES ($1, $2, $3)`,
		channelID, action, payload,
	)
	if err != nil {
		return fmt.Errorf("record upstream sync event: %w", err)
	}
	return nil
}

func (r *upstreamChannelRepository) runInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upstream tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *upstreamChannelRepository) savePlatformTx(ctx context.Context, tx *sql.Tx, platform *service.UpstreamPlatform) error {
	if platform.ID > 0 {
		result, err := tx.ExecContext(ctx,
			`UPDATE upstream_platforms
			 SET provider = $1, display_name = $2, base_url = $3, status = $4, updated_at = NOW()
			 WHERE id = $5 AND channel_id = $6`,
			platform.Provider, platform.DisplayName, platform.BaseURL, platform.Status, platform.ID, platform.ChannelID,
		)
		if err != nil {
			if mapped := upstreamUniqueViolationError(err); mapped != nil {
				return mapped
			}
			return fmt.Errorf("update upstream platform: %w", err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return service.ErrUpstreamChannelNotFound
		}
	} else {
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO upstream_platforms (channel_id, provider, display_name, base_url, status)
			 VALUES ($1, $2, $3, $4, $5)
			 RETURNING id, created_at, updated_at`,
			platform.ChannelID, platform.Provider, platform.DisplayName, platform.BaseURL, platform.Status,
		).Scan(&platform.ID, &platform.CreatedAt, &platform.UpdatedAt); err != nil {
			if mapped := upstreamUniqueViolationError(err); mapped != nil {
				return mapped
			}
			return fmt.Errorf("insert upstream platform: %w", err)
		}
	}

	keepPoolIDs := make([]int64, 0, len(platform.KeyPools))
	for i := range platform.KeyPools {
		platform.KeyPools[i].PlatformID = platform.ID
		if err := r.savePoolTx(ctx, tx, &platform.KeyPools[i]); err != nil {
			return err
		}
		if platform.KeyPools[i].ID > 0 {
			keepPoolIDs = append(keepPoolIDs, platform.KeyPools[i].ID)
		}
	}
	return deleteMissingByParentTx(ctx, tx, "upstream_key_pools", "platform_id", platform.ID, keepPoolIDs)
}

func (r *upstreamChannelRepository) savePoolTx(ctx context.Context, tx *sql.Tx, pool *service.UpstreamKeyPool) error {
	if pool.ID > 0 {
		result, err := tx.ExecContext(ctx,
			`UPDATE upstream_key_pools
			 SET name = $1,
			     group_name = $2,
			     group_rate_multiplier = $3,
			     account_rate_multiplier = $4,
			     load_factor = $5,
			     concurrency = $6,
			     status = $7,
			     synced_group_id = $8,
			     updated_at = NOW()
			 WHERE id = $9 AND platform_id = $10`,
			pool.Name, pool.GroupName, pool.GroupRateMultiplier, pool.AccountRateMultiplier,
			pool.LoadFactor, pool.Concurrency, pool.Status, pool.SyncedGroupID, pool.ID, pool.PlatformID,
		)
		if err != nil {
			if mapped := upstreamUniqueViolationError(err); mapped != nil {
				return mapped
			}
			return fmt.Errorf("update upstream key pool: %w", err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return service.ErrUpstreamChannelNotFound
		}
	} else {
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO upstream_key_pools
			 (platform_id, name, group_name, group_rate_multiplier, account_rate_multiplier, load_factor, concurrency, status, synced_group_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 RETURNING id, created_at, updated_at`,
			pool.PlatformID, pool.Name, pool.GroupName, pool.GroupRateMultiplier, pool.AccountRateMultiplier,
			pool.LoadFactor, pool.Concurrency, pool.Status, pool.SyncedGroupID,
		).Scan(&pool.ID, &pool.CreatedAt, &pool.UpdatedAt); err != nil {
			if mapped := upstreamUniqueViolationError(err); mapped != nil {
				return mapped
			}
			return fmt.Errorf("insert upstream key pool: %w", err)
		}
	}

	keepKeyIDs := make([]int64, 0, len(pool.Keys))
	for i := range pool.Keys {
		pool.Keys[i].PoolID = pool.ID
		if err := r.saveKeyTx(ctx, tx, &pool.Keys[i]); err != nil {
			return err
		}
		if pool.Keys[i].ID > 0 {
			keepKeyIDs = append(keepKeyIDs, pool.Keys[i].ID)
		}
	}
	return deleteMissingByParentTx(ctx, tx, "upstream_keys", "pool_id", pool.ID, keepKeyIDs)
}

func (r *upstreamChannelRepository) saveKeyTx(ctx context.Context, tx *sql.Tx, key *service.UpstreamKey) error {
	if key.ID > 0 {
		result, err := tx.ExecContext(ctx,
			`UPDATE upstream_keys
			 SET name = $1,
			     encrypted_api_key = $2,
			     api_key_fingerprint = $3,
			     status = $4,
			     synced_account_id = $5,
			     last_test_latency_ms = $6,
			     last_test_status = $7,
			     last_test_message = $8,
			     last_tested_at = $9,
			     updated_at = NOW()
			 WHERE id = $10 AND pool_id = $11`,
			key.Name, key.EncryptedAPIKey, key.APIKeyFingerprint, key.Status, key.SyncedAccountID,
			nullableInt(key.LastTestLatencyMS), key.LastTestStatus, key.LastTestMessage, key.LastTestedAt,
			key.ID, key.PoolID,
		)
		if err != nil {
			return fmt.Errorf("update upstream key: %w", err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return service.ErrUpstreamChannelNotFound
		}
		return nil
	}
	return tx.QueryRowContext(ctx,
		`INSERT INTO upstream_keys
		 (pool_id, name, encrypted_api_key, api_key_fingerprint, status, synced_account_id,
		  last_test_latency_ms, last_test_status, last_test_message, last_tested_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id, created_at, updated_at`,
		key.PoolID, key.Name, key.EncryptedAPIKey, key.APIKeyFingerprint, key.Status, key.SyncedAccountID,
		nullableInt(key.LastTestLatencyMS), key.LastTestStatus, key.LastTestMessage, key.LastTestedAt,
	).Scan(&key.ID, &key.CreatedAt, &key.UpdatedAt)
}

func (r *upstreamChannelRepository) loadNested(ctx context.Context, channels []service.UpstreamChannel) error {
	if len(channels) == 0 {
		return nil
	}
	channelIDs := make([]int64, 0, len(channels))
	channelIndex := make(map[int64]int, len(channels))
	for i := range channels {
		channelIDs = append(channelIDs, channels[i].ID)
		channelIndex[channels[i].ID] = i
		channels[i].Platforms = []service.UpstreamPlatform{}
	}

	platforms, platformIDs, err := r.loadPlatforms(ctx, channelIDs)
	if err != nil {
		return err
	}
	platformIndexByID := map[int64]upstreamPlatformLocation{}
	for _, platform := range platforms {
		if idx, ok := channelIndex[platform.ChannelID]; ok {
			platformIndex := len(channels[idx].Platforms)
			channels[idx].Platforms = append(channels[idx].Platforms, platform)
			platformIndexByID[platform.ID] = upstreamPlatformLocation{
				channelID:     platform.ChannelID,
				platformIndex: platformIndex,
			}
		}
	}
	if len(platformIDs) == 0 {
		return nil
	}

	pools, poolIDs, err := r.loadPools(ctx, platformIDs)
	if err != nil {
		return err
	}
	poolIndexByID := map[int64]upstreamPoolLocation{}
	for _, pool := range pools {
		loc, ok := platformIndexByID[pool.PlatformID]
		if !ok {
			continue
		}
		channel := &channels[channelIndex[loc.channelID]]
		poolIndex := len(channel.Platforms[loc.platformIndex].KeyPools)
		channel.Platforms[loc.platformIndex].KeyPools = append(channel.Platforms[loc.platformIndex].KeyPools, pool)
		poolIndexByID[pool.ID] = upstreamPoolLocation{
			channelID:     loc.channelID,
			platformIndex: loc.platformIndex,
			poolIndex:     poolIndex,
		}
	}
	if len(poolIDs) == 0 {
		return nil
	}

	keys, err := r.loadKeys(ctx, poolIDs)
	if err != nil {
		return err
	}
	for _, key := range keys {
		loc, ok := poolIndexByID[key.PoolID]
		if !ok {
			continue
		}
		channel := &channels[channelIndex[loc.channelID]]
		pool := &channel.Platforms[loc.platformIndex].KeyPools[loc.poolIndex]
		pool.Keys = append(pool.Keys, key)
	}
	return nil
}

type upstreamPlatformLocation struct {
	channelID     int64
	platformIndex int
}

type upstreamPoolLocation struct {
	channelID     int64
	platformIndex int
	poolIndex     int
}

func (r *upstreamChannelRepository) loadPlatforms(ctx context.Context, channelIDs []int64) ([]service.UpstreamPlatform, []int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, channel_id, provider, display_name, base_url, status, created_at, updated_at
		 FROM upstream_platforms WHERE channel_id = ANY($1) ORDER BY id`,
		pq.Array(channelIDs),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query upstream platforms: %w", err)
	}
	defer func() { _ = rows.Close() }()

	platforms := []service.UpstreamPlatform{}
	platformIDs := []int64{}
	for rows.Next() {
		var p service.UpstreamPlatform
		if err := rows.Scan(&p.ID, &p.ChannelID, &p.Provider, &p.DisplayName, &p.BaseURL, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan upstream platform: %w", err)
		}
		p.KeyPools = []service.UpstreamKeyPool{}
		platforms = append(platforms, p)
		platformIDs = append(platformIDs, p.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate upstream platforms: %w", err)
	}
	return platforms, platformIDs, nil
}

func (r *upstreamChannelRepository) loadPools(ctx context.Context, platformIDs []int64) ([]service.UpstreamKeyPool, []int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, platform_id, name, group_name, group_rate_multiplier, account_rate_multiplier,
		        load_factor, concurrency, status, synced_group_id, created_at, updated_at
		 FROM upstream_key_pools WHERE platform_id = ANY($1) ORDER BY id`,
		pq.Array(platformIDs),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query upstream key pools: %w", err)
	}
	defer func() { _ = rows.Close() }()

	pools := []service.UpstreamKeyPool{}
	poolIDs := []int64{}
	for rows.Next() {
		var pool service.UpstreamKeyPool
		var syncedGroupID sql.NullInt64
		if err := rows.Scan(
			&pool.ID, &pool.PlatformID, &pool.Name, &pool.GroupName, &pool.GroupRateMultiplier,
			&pool.AccountRateMultiplier, &pool.LoadFactor, &pool.Concurrency, &pool.Status,
			&syncedGroupID, &pool.CreatedAt, &pool.UpdatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan upstream key pool: %w", err)
		}
		if syncedGroupID.Valid {
			pool.SyncedGroupID = &syncedGroupID.Int64
		}
		pool.Keys = []service.UpstreamKey{}
		pools = append(pools, pool)
		poolIDs = append(poolIDs, pool.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate upstream key pools: %w", err)
	}
	return pools, poolIDs, nil
}

func (r *upstreamChannelRepository) loadKeys(ctx context.Context, poolIDs []int64) ([]service.UpstreamKey, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, pool_id, name, encrypted_api_key, api_key_fingerprint, status, synced_account_id,
		        last_test_latency_ms, last_test_status, last_test_message, last_tested_at, created_at, updated_at
		 FROM upstream_keys WHERE pool_id = ANY($1) ORDER BY id`,
		pq.Array(poolIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("query upstream keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	keys := []service.UpstreamKey{}
	for rows.Next() {
		var key service.UpstreamKey
		var accountID sql.NullInt64
		var latency sql.NullInt64
		var testedAt sql.NullTime
		if err := rows.Scan(
			&key.ID, &key.PoolID, &key.Name, &key.EncryptedAPIKey, &key.APIKeyFingerprint,
			&key.Status, &accountID, &latency, &key.LastTestStatus, &key.LastTestMessage,
			&testedAt, &key.CreatedAt, &key.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan upstream key: %w", err)
		}
		if accountID.Valid {
			key.SyncedAccountID = &accountID.Int64
		}
		if latency.Valid {
			v := int(latency.Int64)
			key.LastTestLatencyMS = &v
		}
		if testedAt.Valid {
			key.LastTestedAt = &testedAt.Time
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream keys: %w", err)
	}
	return keys, nil
}

func deleteMissingByParentTx(ctx context.Context, tx *sql.Tx, table string, parentColumn string, parentID int64, keepIDs []int64) error {
	if !isAllowedUpstreamTable(table) || !isAllowedUpstreamParentColumn(parentColumn) {
		return fmt.Errorf("invalid upstream delete target")
	}
	if len(keepIDs) == 0 {
		_, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = $1`, table, parentColumn), parentID)
		return err
	}
	_, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE %s = $1 AND NOT (id = ANY($2))`, table, parentColumn),
		parentID, pq.Array(keepIDs),
	)
	return err
}

func isAllowedUpstreamTable(table string) bool {
	switch table {
	case "upstream_platforms", "upstream_key_pools", "upstream_keys":
		return true
	default:
		return false
	}
}

func isAllowedUpstreamParentColumn(column string) bool {
	switch column {
	case "channel_id", "platform_id", "pool_id":
		return true
	default:
		return false
	}
}

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func upstreamUniqueViolationError(err error) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr == nil || pqErr.Code != "23505" {
		return nil
	}
	switch pqErr.Constraint {
	case "idx_upstream_channels_name":
		return service.ErrUpstreamChannelExists
	case "idx_upstream_platforms_channel_provider":
		return service.ErrUpstreamPlatformExists
	case "idx_upstream_key_pools_platform_name":
		return service.ErrUpstreamKeyPoolExists
	default:
		return service.ErrUpstreamChannelExists
	}
}

func upstreamChannelOrderBy(params pagination.PaginationParams) string {
	order := params.NormalizedSortOrder(pagination.SortOrderDesc)
	switch strings.ToLower(strings.TrimSpace(params.SortBy)) {
	case "name":
		return "name " + order + ", id DESC"
	case "status":
		return "status " + order + ", id DESC"
	case "updated_at":
		return "updated_at " + order + ", id DESC"
	default:
		return "created_at " + order + ", id DESC"
	}
}
