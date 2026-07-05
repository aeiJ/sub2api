package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrUpstreamChannelNotFound = infraerrors.NotFound("UPSTREAM_CHANNEL_NOT_FOUND", "upstream channel not found")
	ErrUpstreamChannelExists   = infraerrors.Conflict("UPSTREAM_CHANNEL_EXISTS", "upstream channel name already exists")
	ErrUpstreamPlatformExists  = infraerrors.Conflict("UPSTREAM_PLATFORM_EXISTS", "upstream platform already exists in this channel")
	ErrUpstreamKeyPoolExists   = infraerrors.Conflict("UPSTREAM_KEY_POOL_EXISTS", "upstream key pool name already exists in this platform")
)

const (
	upstreamSyncExtraManaged          = "upstream_managed"
	upstreamSyncExtraChannelID        = "upstream_channel_id"
	upstreamSyncExtraChannelName      = "upstream_channel_name"
	upstreamSyncExtraPlatformID       = "upstream_platform_id"
	upstreamSyncExtraPlatformProvider = "upstream_platform_provider"
	upstreamSyncExtraPoolID           = "upstream_pool_id"
	upstreamSyncExtraKeyID            = "upstream_key_id"
)

type UpstreamChannelRepository interface {
	Create(ctx context.Context, channel *UpstreamChannel) error
	Update(ctx context.Context, channel *UpstreamChannel) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*UpstreamChannel, error)
	List(ctx context.Context, params pagination.PaginationParams, status, search string) ([]UpstreamChannel, *pagination.PaginationResult, error)
	UpdatePoolSyncedGroupID(ctx context.Context, poolID int64, groupID int64) error
	UpdateKeySyncedAccountID(ctx context.Context, keyID int64, accountID int64) error
	UpdateKeyTestResult(ctx context.Context, keyID int64, result UpstreamTestResult) error
	RecordSyncEvent(ctx context.Context, channelID int64, action string, summary map[string]int) error
}

type UpstreamSyncAdmin interface {
	CreateGroup(ctx context.Context, input *CreateGroupInput) (*Group, error)
	CreateAccount(ctx context.Context, input *CreateAccountInput) (*Account, error)
	UpdateAccount(ctx context.Context, id int64, input *UpdateAccountInput) (*Account, error)
}

type UpstreamAccountReader interface {
	GetByID(ctx context.Context, id int64) (*Account, error)
	FindByExtraField(ctx context.Context, key string, value any) ([]Account, error)
}

type UpstreamGroupWriter interface {
	GetByID(ctx context.Context, id int64) (*Group, error)
	Update(ctx context.Context, group *Group) error
}

type UpstreamAccountTester interface {
	FetchUpstreamSupportedModels(ctx context.Context, account *Account) ([]string, error)
	SelectDefaultSupportedTestModel(account *Account, models []string) string
	RunTestBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
}

type UpstreamChannel struct {
	ID          int64              `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Status      string             `json:"status"`
	Platforms   []UpstreamPlatform `json:"platforms"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

type UpstreamPlatform struct {
	ID          int64             `json:"id"`
	ChannelID   int64             `json:"channel_id"`
	Provider    string            `json:"provider"`
	DisplayName string            `json:"display_name"`
	BaseURL     string            `json:"base_url"`
	Status      string            `json:"status"`
	KeyPools    []UpstreamKeyPool `json:"key_pools"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type UpstreamKeyPool struct {
	ID                          int64         `json:"id"`
	PlatformID                  int64         `json:"platform_id"`
	Name                        string        `json:"name"`
	GroupName                   string        `json:"group_name"`
	UpstreamGroupRateMultiplier float64       `json:"upstream_group_rate_multiplier"`
	GroupRateMultiplier         float64       `json:"group_rate_multiplier"`
	AccountRateMultiplier       float64       `json:"account_rate_multiplier"`
	LoadFactor                  int           `json:"load_factor"`
	Concurrency                 int           `json:"concurrency"`
	Status                      string        `json:"status"`
	SyncedGroupID               *int64        `json:"synced_group_id,omitempty"`
	Keys                        []UpstreamKey `json:"keys"`
	CreatedAt                   time.Time     `json:"created_at"`
	UpdatedAt                   time.Time     `json:"updated_at"`
}

type UpstreamKey struct {
	ID                int64      `json:"id"`
	PoolID            int64      `json:"pool_id"`
	Name              string     `json:"name"`
	APIKey            string     `json:"api_key,omitempty"`
	APIKeyMasked      string     `json:"api_key_masked"`
	HasAPIKey         bool       `json:"has_api_key"`
	EncryptedAPIKey   string     `json:"-"`
	APIKeyFingerprint string     `json:"api_key_fingerprint,omitempty"`
	Status            string     `json:"status"`
	SyncedAccountID   *int64     `json:"synced_account_id,omitempty"`
	SupportedModels   []string   `json:"supported_models"`
	LastTestModel     string     `json:"last_test_model,omitempty"`
	LastTestLatencyMS *int       `json:"last_test_latency_ms,omitempty"`
	LastTestStatus    string     `json:"last_test_status"`
	LastTestMessage   string     `json:"last_test_message"`
	LastTestedAt      *time.Time `json:"last_tested_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type UpstreamSyncPreview struct {
	ChannelID int64                     `json:"channel_id"`
	Summary   map[string]int            `json:"summary"`
	Items     []UpstreamSyncPreviewItem `json:"items"`
	Groups    []UpstreamSyncChange      `json:"groups"`
	Accounts  []UpstreamSyncChange      `json:"accounts"`
	Warnings  []string                  `json:"warnings,omitempty"`
}

type UpstreamSyncPreviewItem struct {
	Entity     string   `json:"entity"`
	Action     string   `json:"action"`
	Name       string   `json:"name"`
	Platform   string   `json:"platform"`
	UpstreamID int64    `json:"upstream_id"`
	ExistingID *int64   `json:"existing_id,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
}

type UpstreamSyncChange struct {
	Action  string `json:"action"`
	Target  string `json:"target"`
	ID      *int64 `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

type UpstreamSyncResult struct {
	ChannelID int64                     `json:"channel_id"`
	Summary   map[string]int            `json:"summary"`
	Items     []UpstreamSyncPreviewItem `json:"items"`
	Groups    []UpstreamSyncChange      `json:"groups"`
	Accounts  []UpstreamSyncChange      `json:"accounts"`
	Warnings  []string                  `json:"warnings,omitempty"`
}

type UpstreamTestResult struct {
	KeyID       int64      `json:"key_id"`
	KeyName     string     `json:"key_name,omitempty"`
	PoolName    string     `json:"pool_name,omitempty"`
	GroupName   string     `json:"group_name,omitempty"`
	Status      string     `json:"status"`
	LatencyMS   *int       `json:"latency_ms,omitempty"`
	Message     string     `json:"message"`
	TestModel   string     `json:"test_model,omitempty"`
	Models      []string   `json:"models,omitempty"`
	TestedAt    *time.Time `json:"tested_at,omitempty"`
	AccountID   *int64     `json:"account_id,omitempty"`
	Platform    string     `json:"platform"`
	PlatformID  int64      `json:"platform_id"`
	ChannelID   int64      `json:"channel_id"`
	ChannelName string     `json:"channel_name"`
}

type UpstreamChannelService struct {
	repo                 UpstreamChannelRepository
	admin                UpstreamSyncAdmin
	accountRepo          UpstreamAccountReader
	groupRepo            UpstreamGroupWriter
	encryptor            SecretEncryptor
	authCacheInvalidator APIKeyAuthCacheInvalidator
	accountTester        UpstreamAccountTester
}

func NewUpstreamChannelService(
	repo UpstreamChannelRepository,
	admin UpstreamSyncAdmin,
	accountRepo UpstreamAccountReader,
	groupRepo UpstreamGroupWriter,
	encryptor SecretEncryptor,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	accountTester *AccountTestService,
) *UpstreamChannelService {
	return &UpstreamChannelService{
		repo:                 repo,
		admin:                admin,
		accountRepo:          accountRepo,
		groupRepo:            groupRepo,
		encryptor:            encryptor,
		authCacheInvalidator: authCacheInvalidator,
		accountTester:        accountTester,
	}
}

func (s *UpstreamChannelService) List(ctx context.Context, params pagination.PaginationParams, status, search string) ([]UpstreamChannel, *pagination.PaginationResult, error) {
	channels, result, err := s.repo.List(ctx, params, normalizeOptionalStatus(status), strings.TrimSpace(search))
	if err != nil {
		return nil, nil, err
	}
	for i := range channels {
		s.hydrateMaskedKeys(&channels[i])
	}
	return channels, result, nil
}

func (s *UpstreamChannelService) GetByID(ctx context.Context, id int64) (*UpstreamChannel, error) {
	channel, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.hydrateMaskedKeys(channel)
	return channel, nil
}

func (s *UpstreamChannelService) Create(ctx context.Context, input *UpstreamChannel) (*UpstreamChannel, error) {
	if input == nil {
		return nil, infraerrors.BadRequest("UPSTREAM_CHANNEL_NIL_INPUT", "upstream channel input cannot be nil")
	}
	channel := cloneUpstreamChannel(input)
	if err := s.normalizeChannel(&channel, nil); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, &channel); err != nil {
		return nil, err
	}
	created, err := s.repo.GetByID(ctx, channel.ID)
	if err != nil {
		return nil, err
	}
	s.hydrateMaskedKeys(created)
	return created, nil
}

func (s *UpstreamChannelService) Update(ctx context.Context, id int64, input *UpstreamChannel) (*UpstreamChannel, error) {
	if input == nil {
		return nil, infraerrors.BadRequest("UPSTREAM_CHANNEL_NIL_INPUT", "upstream channel input cannot be nil")
	}
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	channel := cloneUpstreamChannel(input)
	channel.ID = id
	if err := s.normalizeChannel(&channel, existing); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, &channel); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.hydrateMaskedKeys(updated)
	return updated, nil
}

func (s *UpstreamChannelService) UpdateStatus(ctx context.Context, id int64, status string) (*UpstreamChannel, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	channel := cloneUpstreamChannel(existing)
	channel.Status = normalizeStatus(status)
	if err := s.repo.Update(ctx, &channel); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.hydrateMaskedKeys(updated)
	return updated, nil
}

func (s *UpstreamChannelService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *UpstreamChannelService) SyncPreview(ctx context.Context, channelID int64) (*UpstreamSyncPreview, error) {
	channel, err := s.repo.GetByID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return s.buildSyncPreview(ctx, channel)
}

func (s *UpstreamChannelService) Sync(ctx context.Context, channelID int64) (*UpstreamSyncResult, error) {
	channel, err := s.repo.GetByID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	result := &UpstreamSyncResult{
		ChannelID: channel.ID,
		Summary:   map[string]int{},
		Items:     []UpstreamSyncPreviewItem{},
		Groups:    []UpstreamSyncChange{},
		Accounts:  []UpstreamSyncChange{},
	}
	for pi := range channel.Platforms {
		platform := &channel.Platforms[pi]
		provider := normalizeProvider(platform.Provider)
		for pki := range platform.KeyPools {
			pool := &platform.KeyPools[pki]
			group, action, err := s.syncPoolGroup(ctx, channel, platform, pool)
			if err != nil {
				return nil, err
			}
			result.Summary[action]++
			result.addItem(UpstreamSyncPreviewItem{
				Entity:     "group",
				Action:     action,
				Name:       group.Name,
				Platform:   provider,
				UpstreamID: pool.ID,
				ExistingID: &group.ID,
			})
			if err := s.repo.UpdatePoolSyncedGroupID(ctx, pool.ID, group.ID); err != nil {
				return nil, err
			}
			pool.SyncedGroupID = &group.ID

			for ki := range pool.Keys {
				key := &pool.Keys[ki]
				account, keyAction, warnings, err := s.syncKeyAccount(ctx, channel, platform, pool, key, group.ID)
				if err != nil {
					return nil, err
				}
				result.Summary[keyAction]++
				item := UpstreamSyncPreviewItem{
					Entity:     "account",
					Action:     keyAction,
					Name:       key.Name,
					Platform:   provider,
					UpstreamID: key.ID,
					Warnings:   warnings,
				}
				if account != nil {
					item.ExistingID = &account.ID
					if err := s.repo.UpdateKeySyncedAccountID(ctx, key.ID, account.ID); err != nil {
						return nil, err
					}
					key.SyncedAccountID = &account.ID
				}
				result.addItem(item)
			}
		}
	}
	if err := s.repo.RecordSyncEvent(ctx, channel.ID, "sync", result.Summary); err != nil {
		return nil, err
	}
	return result, nil
}

type UpstreamTestFilter struct {
	KeyID  *int64
	PoolID *int64
}

func (s *UpstreamChannelService) TestChannel(ctx context.Context, channelID int64) ([]UpstreamTestResult, error) {
	return s.TestChannelFiltered(ctx, channelID, UpstreamTestFilter{})
}

func (s *UpstreamChannelService) TestChannelFiltered(ctx context.Context, channelID int64, filter UpstreamTestFilter) ([]UpstreamTestResult, error) {
	if filter.KeyID != nil && *filter.KeyID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_UPSTREAM_KEY_ID", "invalid upstream key ID")
	}
	if filter.PoolID != nil && *filter.PoolID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_UPSTREAM_POOL_ID", "invalid upstream pool ID")
	}
	channel, err := s.repo.GetByID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	results := make([]UpstreamTestResult, 0)
	for pi := range channel.Platforms {
		platform := &channel.Platforms[pi]
		for pki := range platform.KeyPools {
			pool := &platform.KeyPools[pki]
			if filter.PoolID != nil && pool.ID != *filter.PoolID {
				continue
			}
			for ki := range pool.Keys {
				key := &pool.Keys[ki]
				if filter.KeyID != nil && key.ID != *filter.KeyID {
					continue
				}
				result := s.testKey(ctx, channel, platform, pool, key)
				if err := s.repo.UpdateKeyTestResult(ctx, key.ID, result); err != nil {
					return nil, err
				}
				results = append(results, result)
			}
		}
	}
	return results, nil
}

func (s *UpstreamChannelService) buildSyncPreview(ctx context.Context, channel *UpstreamChannel) (*UpstreamSyncPreview, error) {
	preview := &UpstreamSyncPreview{
		ChannelID: channel.ID,
		Summary:   map[string]int{},
		Items:     []UpstreamSyncPreviewItem{},
		Groups:    []UpstreamSyncChange{},
		Accounts:  []UpstreamSyncChange{},
	}
	for pi := range channel.Platforms {
		platform := &channel.Platforms[pi]
		provider := normalizeProvider(platform.Provider)
		for pki := range platform.KeyPools {
			pool := &platform.KeyPools[pki]
			groupID, groupWarnings := s.resolveSyncedGroupID(ctx, pool)
			groupAction := "create_group"
			if groupID != nil {
				groupAction = "update_group"
			}
			preview.Summary[groupAction]++
			preview.addItem(UpstreamSyncPreviewItem{
				Entity:     "group",
				Action:     groupAction,
				Name:       pool.GroupName,
				Platform:   provider,
				UpstreamID: pool.ID,
				ExistingID: groupID,
				Warnings:   groupWarnings,
			})

			for ki := range pool.Keys {
				key := &pool.Keys[ki]
				accountID, accountWarnings := s.resolveSyncedAccountID(ctx, key)
				action := "create_account"
				if accountID != nil {
					action = "update_account"
				}
				if accountID == nil && strings.TrimSpace(key.EncryptedAPIKey) == "" {
					action = "skip_account"
					accountWarnings = append(accountWarnings, "missing api key")
				}
				preview.Summary[action]++
				preview.addItem(UpstreamSyncPreviewItem{
					Entity:     "account",
					Action:     action,
					Name:       key.Name,
					Platform:   provider,
					UpstreamID: key.ID,
					ExistingID: accountID,
					Warnings:   accountWarnings,
				})
			}
		}
	}
	return preview, nil
}

func (p *UpstreamSyncPreview) addItem(item UpstreamSyncPreviewItem) {
	p.Items = append(p.Items, item)
	change := syncItemToChange(item)
	switch item.Entity {
	case "group":
		p.Groups = append(p.Groups, change)
	case "account":
		p.Accounts = append(p.Accounts, change)
	}
	if len(item.Warnings) > 0 {
		p.Warnings = append(p.Warnings, item.Warnings...)
	}
}

func (r *UpstreamSyncResult) addItem(item UpstreamSyncPreviewItem) {
	r.Items = append(r.Items, item)
	change := syncItemToChange(item)
	switch item.Entity {
	case "group":
		r.Groups = append(r.Groups, change)
	case "account":
		r.Accounts = append(r.Accounts, change)
	}
	if len(item.Warnings) > 0 {
		r.Warnings = append(r.Warnings, item.Warnings...)
	}
}

func syncItemToChange(item UpstreamSyncPreviewItem) UpstreamSyncChange {
	return UpstreamSyncChange{
		Action:  item.Action,
		Target:  item.Entity,
		ID:      item.ExistingID,
		Name:    item.Name,
		Status:  item.Platform,
		Message: strings.Join(item.Warnings, "; "),
	}
}

func (s *UpstreamChannelService) syncPoolGroup(ctx context.Context, channel *UpstreamChannel, platform *UpstreamPlatform, pool *UpstreamKeyPool) (*Group, string, error) {
	provider := normalizeProvider(platform.Provider)
	groupID, _ := s.resolveSyncedGroupID(ctx, pool)
	description := fmt.Sprintf("Synced from upstream channel %s / %s / %s", channel.Name, platform.DisplayName, pool.Name)
	if groupID != nil {
		group, err := s.groupRepo.GetByID(ctx, *groupID)
		if err != nil {
			return nil, "", err
		}
		group.Name = pool.GroupName
		group.Description = description
		group.Platform = provider
		group.RateMultiplier = pool.GroupRateMultiplier
		group.Status = normalizeStatus(pool.Status)
		if group.SubscriptionType == "" {
			group.SubscriptionType = SubscriptionTypeStandard
		}
		if group.ImageRateMultiplier < 0 {
			group.ImageRateMultiplier = 1
		}
		if err := s.groupRepo.Update(ctx, group); err != nil {
			return nil, "", err
		}
		if s.authCacheInvalidator != nil {
			s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, group.ID)
		}
		return group, "update_group", nil
	}

	group, err := s.admin.CreateGroup(ctx, &CreateGroupInput{
		Name:             pool.GroupName,
		Description:      description,
		Platform:         provider,
		RateMultiplier:   pool.GroupRateMultiplier,
		IsExclusive:      false,
		SubscriptionType: SubscriptionTypeStandard,
	})
	if err != nil {
		return nil, "", err
	}
	if normalizeStatus(pool.Status) != StatusActive {
		group.Status = normalizeStatus(pool.Status)
		if err := s.groupRepo.Update(ctx, group); err != nil {
			return nil, "", err
		}
		if s.authCacheInvalidator != nil {
			s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, group.ID)
		}
	}
	return group, "create_group", nil
}

func (s *UpstreamChannelService) syncKeyAccount(ctx context.Context, channel *UpstreamChannel, platform *UpstreamPlatform, pool *UpstreamKeyPool, key *UpstreamKey, groupID int64) (*Account, string, []string, error) {
	warnings := []string{}
	provider := normalizeProvider(platform.Provider)
	extra := upstreamAccountExtra(channel, platform, pool, key)
	accountID, _ := s.resolveSyncedAccountID(ctx, key)
	apiKey, decryptErr := s.decryptKey(key.EncryptedAPIKey)
	apiKey = strings.TrimSpace(apiKey)
	hasUpstreamKey := decryptErr == nil && apiKey != ""
	if !hasUpstreamKey && accountID == nil {
		warnings = append(warnings, "missing or undecryptable api key")
		return nil, "skip_account", warnings, nil
	}
	var credentials map[string]any
	if hasUpstreamKey {
		credentials = map[string]any{"api_key": apiKey}
		if baseURL := normalizeUpstreamBaseURL(platform.BaseURL, provider); baseURL != "" {
			credentials["base_url"] = baseURL
		}
	}
	rate := pool.AccountRateMultiplier
	if rate < 0 {
		rate = 1
	}
	concurrency := pool.Concurrency
	if concurrency <= 0 {
		concurrency = 3
	}
	loadFactor := positiveIntPtr(pool.LoadFactor)
	groupIDs := []int64{groupID}
	status := normalizeStatus(key.Status)

	if accountID != nil {
		existing, err := s.accountRepo.GetByID(ctx, *accountID)
		if err != nil {
			return nil, "", warnings, err
		}
		mergedExtra := mergeStringAny(existing.Extra, extra)
		account, err := s.admin.UpdateAccount(ctx, existing.ID, &UpdateAccountInput{
			Name:                  key.Name,
			Type:                  AccountTypeAPIKey,
			Credentials:           credentials,
			Extra:                 mergedExtra,
			Concurrency:           &concurrency,
			RateMultiplier:        &rate,
			LoadFactor:            loadFactor,
			Status:                status,
			GroupIDs:              &groupIDs,
			SkipMixedChannelCheck: true,
		})
		return account, "update_account", warnings, err
	}

	account, err := s.admin.CreateAccount(ctx, &CreateAccountInput{
		Name:                  key.Name,
		Platform:              provider,
		Type:                  AccountTypeAPIKey,
		Credentials:           credentials,
		Extra:                 extra,
		Concurrency:           concurrency,
		Priority:              50,
		RateMultiplier:        &rate,
		LoadFactor:            loadFactor,
		GroupIDs:              groupIDs,
		SkipDefaultGroupBind:  true,
		SkipMixedChannelCheck: true,
	})
	if err != nil {
		return nil, "", warnings, err
	}
	if status != StatusActive {
		account, err = s.admin.UpdateAccount(ctx, account.ID, &UpdateAccountInput{
			Status:                status,
			GroupIDs:              &groupIDs,
			SkipMixedChannelCheck: true,
		})
		if err != nil {
			return nil, "", warnings, err
		}
	}
	return account, "create_account", warnings, nil
}

func (s *UpstreamChannelService) testKey(ctx context.Context, channel *UpstreamChannel, platform *UpstreamPlatform, pool *UpstreamKeyPool, key *UpstreamKey) UpstreamTestResult {
	now := time.Now().UTC()
	result := UpstreamTestResult{
		KeyID:       key.ID,
		KeyName:     key.Name,
		PoolName:    pool.Name,
		GroupName:   pool.GroupName,
		Status:      "error",
		Message:     "",
		TestedAt:    &now,
		AccountID:   key.SyncedAccountID,
		Platform:    normalizeProvider(platform.Provider),
		PlatformID:  platform.ID,
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
	}
	if s.accountRepo == nil || s.accountTester == nil {
		result.Status = "failed"
		result.Message = "account test service is not configured"
		return result
	}
	accountID, accountWarnings := s.resolveSyncedAccountID(ctx, key)
	if accountID == nil {
		result.Status = "failed"
		result.Message = "please sync this upstream key to an account before testing"
		if len(accountWarnings) > 0 {
			result.Message = strings.Join(accountWarnings, "; ")
		}
		return result
	}
	result.AccountID = accountID
	account, err := s.accountRepo.GetByID(ctx, *accountID)
	if err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		return result
	}
	models, err := s.accountTester.FetchUpstreamSupportedModels(ctx, account)
	if err != nil {
		result.Status = "failed"
		result.Message = safeUpstreamModelSyncMessage(err)
		return result
	}
	result.Models = models
	key.SupportedModels = append([]string(nil), models...)
	testModel := s.accountTester.SelectDefaultSupportedTestModel(account, models)
	if testModel == "" {
		result.Status = "failed"
		result.Message = "upstream returned no supported models"
		return result
	}
	result.TestModel = testModel

	testResult, err := s.accountTester.RunTestBackground(ctx, account.ID, testModel)
	if err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		return result
	}
	latency := int(testResult.LatencyMs)
	result.LatencyMS = &latency
	if testResult.Status == "success" {
		result.Status = "operational"
		result.Message = fmt.Sprintf("account test succeeded using %s", testModel)
		return result
	}
	result.Status = "failed"
	if isAuthFailureMessage(testResult.ErrorMessage) {
		result.Status = "auth_error"
	}
	if strings.TrimSpace(testResult.ErrorMessage) != "" {
		result.Message = fmt.Sprintf("%s using %s", testResult.ErrorMessage, testModel)
	} else {
		result.Message = fmt.Sprintf("account test failed using %s", testModel)
	}
	return result
}

func safeUpstreamModelSyncMessage(err error) string {
	var syncErr *UpstreamModelSyncError
	if errors.As(err, &syncErr) {
		return syncErr.SafeMessage()
	}
	return err.Error()
}

func isAuthFailureMessage(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "invalid api key") ||
		strings.Contains(lower, "authentication") ||
		strings.Contains(lower, "401") ||
		strings.Contains(lower, "403")
}

func (s *UpstreamChannelService) resolveSyncedGroupID(ctx context.Context, pool *UpstreamKeyPool) (*int64, []string) {
	if pool.SyncedGroupID == nil || *pool.SyncedGroupID <= 0 {
		return nil, nil
	}
	if _, err := s.groupRepo.GetByID(ctx, *pool.SyncedGroupID); err != nil {
		return nil, []string{"previous synced group no longer exists"}
	}
	id := *pool.SyncedGroupID
	return &id, nil
}

func (s *UpstreamChannelService) resolveSyncedAccountID(ctx context.Context, key *UpstreamKey) (*int64, []string) {
	if s.accountRepo == nil {
		return nil, []string{"account repository is not configured"}
	}
	if key.SyncedAccountID != nil && *key.SyncedAccountID > 0 {
		if _, err := s.accountRepo.GetByID(ctx, *key.SyncedAccountID); err == nil {
			id := *key.SyncedAccountID
			return &id, nil
		}
	}
	if key.ID <= 0 {
		return nil, nil
	}
	accounts, err := s.accountRepo.FindByExtraField(ctx, upstreamSyncExtraKeyID, key.ID)
	if err != nil {
		return nil, []string{"failed to lookup existing account by upstream key id"}
	}
	if len(accounts) == 0 {
		return nil, nil
	}
	id := accounts[0].ID
	return &id, nil
}

func (s *UpstreamChannelService) normalizeChannel(channel *UpstreamChannel, existing *UpstreamChannel) error {
	channel.Name = strings.TrimSpace(channel.Name)
	if channel.Name == "" {
		return infraerrors.BadRequest("UPSTREAM_CHANNEL_NAME_REQUIRED", "upstream channel name is required")
	}
	channel.Description = strings.TrimSpace(channel.Description)
	channel.Status = normalizeStatus(channel.Status)

	existingPlatforms := map[int64]*UpstreamPlatform{}
	existingPools := map[int64]*UpstreamKeyPool{}
	existingKeys := map[int64]*UpstreamKey{}
	if existing != nil {
		for pi := range existing.Platforms {
			p := &existing.Platforms[pi]
			existingPlatforms[p.ID] = p
			for pki := range p.KeyPools {
				pool := &p.KeyPools[pki]
				existingPools[pool.ID] = pool
				for ki := range pool.Keys {
					key := &pool.Keys[ki]
					existingKeys[key.ID] = key
				}
			}
		}
	}

	seenProviders := map[string]struct{}{}
	for pi := range channel.Platforms {
		platform := &channel.Platforms[pi]
		platform.Provider = normalizeProvider(platform.Provider)
		if platform.Provider == "" {
			return infraerrors.BadRequest("UPSTREAM_PLATFORM_PROVIDER_REQUIRED", "platform provider is required")
		}
		if !isSupportedUpstreamProvider(platform.Provider) {
			return infraerrors.BadRequest("UPSTREAM_PLATFORM_PROVIDER_UNSUPPORTED", "unsupported platform provider")
		}
		if _, ok := seenProviders[platform.Provider]; ok {
			return infraerrors.BadRequest("UPSTREAM_PLATFORM_DUPLICATE", "a channel can only include one platform for each provider")
		}
		seenProviders[platform.Provider] = struct{}{}
		if platform.DisplayName = strings.TrimSpace(platform.DisplayName); platform.DisplayName == "" {
			platform.DisplayName = defaultProviderDisplayName(platform.Provider)
		}
		platform.BaseURL = normalizeUpstreamBaseURL(platform.BaseURL, platform.Provider)
		platform.Status = normalizeStatus(platform.Status)
		seenPoolNames := map[string]struct{}{}
		for pki := range platform.KeyPools {
			pool := &platform.KeyPools[pki]
			explicitPoolName := strings.TrimSpace(pool.Name)
			if explicitPoolName == "" {
				pool.Name = uniqueUpstreamName(fmt.Sprintf("%s pool", platform.DisplayName), seenPoolNames)
			} else {
				pool.Name = explicitPoolName
				poolNameKey := strings.ToLower(pool.Name)
				if _, ok := seenPoolNames[poolNameKey]; ok {
					return infraerrors.BadRequest("UPSTREAM_KEY_POOL_DUPLICATE", "key pool names must be unique within the same platform")
				}
				seenPoolNames[poolNameKey] = struct{}{}
			}
			pool.GroupName = strings.TrimSpace(pool.GroupName)
			if pool.GroupName == "" {
				pool.GroupName = fmt.Sprintf("%s-%s", channel.Name, platform.Provider)
			}
			if pool.UpstreamGroupRateMultiplier <= 0 {
				pool.UpstreamGroupRateMultiplier = 1
			}
			if pool.GroupRateMultiplier <= 0 {
				pool.GroupRateMultiplier = 1
			}
			if pool.AccountRateMultiplier < 0 {
				return infraerrors.BadRequest("UPSTREAM_ACCOUNT_RATE_INVALID", "account_rate_multiplier must be >= 0")
			}
			if pool.AccountRateMultiplier == 0 {
				pool.AccountRateMultiplier = 1
			}
			if pool.Concurrency <= 0 {
				pool.Concurrency = 3
			}
			pool.Status = normalizeStatus(pool.Status)
			if existingPool := existingPools[pool.ID]; existingPool != nil && pool.SyncedGroupID == nil {
				pool.SyncedGroupID = existingPool.SyncedGroupID
			}
			for ki := range pool.Keys {
				key := &pool.Keys[ki]
				key.Name = strings.TrimSpace(key.Name)
				if key.Name == "" {
					key.Name = fmt.Sprintf("%s key %d", pool.Name, ki+1)
				}
				key.Status = normalizeStatus(key.Status)
				if err := s.normalizeKeySecret(key, existingKeys[key.ID]); err != nil {
					return err
				}
			}
		}
		if existingPlatform := existingPlatforms[platform.ID]; existingPlatform != nil {
			_ = existingPlatform
		}
	}
	return nil
}

func uniqueUpstreamName(base string, seen map[string]struct{}) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "Unnamed"
	}
	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s %d", base, i)
		}
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			return candidate
		}
	}
}

func (s *UpstreamChannelService) normalizeKeySecret(key *UpstreamKey, existing *UpstreamKey) error {
	plain := strings.TrimSpace(key.APIKey)
	if plain != "" {
		encrypted, err := s.encryptor.Encrypt(plain)
		if err != nil {
			return fmt.Errorf("encrypt upstream api key: %w", err)
		}
		key.EncryptedAPIKey = encrypted
		key.APIKeyFingerprint = fingerprintSecret(plain)
		key.APIKeyMasked = maskSecret(plain)
		key.APIKey = ""
		return nil
	}
	if existing != nil {
		key.EncryptedAPIKey = existing.EncryptedAPIKey
		key.APIKeyFingerprint = existing.APIKeyFingerprint
		key.APIKeyMasked = existing.APIKeyMasked
		key.SyncedAccountID = firstNonNilInt64(key.SyncedAccountID, existing.SyncedAccountID)
		key.LastTestLatencyMS = firstNonNilInt(key.LastTestLatencyMS, existing.LastTestLatencyMS)
		if key.LastTestStatus == "" {
			key.LastTestStatus = existing.LastTestStatus
		}
		if key.LastTestMessage == "" {
			key.LastTestMessage = existing.LastTestMessage
		}
		if key.LastTestModel == "" {
			key.LastTestModel = existing.LastTestModel
		}
		if key.SupportedModels == nil {
			key.SupportedModels = append([]string(nil), existing.SupportedModels...)
		}
		if key.LastTestedAt == nil {
			key.LastTestedAt = existing.LastTestedAt
		}
		return nil
	}
	key.EncryptedAPIKey = strings.TrimSpace(key.EncryptedAPIKey)
	return nil
}

func (s *UpstreamChannelService) hydrateMaskedKeys(channel *UpstreamChannel) {
	if channel == nil {
		return
	}
	for pi := range channel.Platforms {
		for pki := range channel.Platforms[pi].KeyPools {
			for ki := range channel.Platforms[pi].KeyPools[pki].Keys {
				key := &channel.Platforms[pi].KeyPools[pki].Keys[ki]
				key.APIKey = ""
				if key.EncryptedAPIKey == "" {
					key.HasAPIKey = false
					key.APIKeyMasked = ""
					continue
				}
				key.HasAPIKey = true
				if plain, err := s.decryptKey(key.EncryptedAPIKey); err == nil {
					key.APIKeyMasked = maskSecret(plain)
					if key.APIKeyFingerprint == "" {
						key.APIKeyFingerprint = fingerprintSecret(plain)
					}
				}
			}
		}
	}
}

func (s *UpstreamChannelService) decryptKey(encrypted string) (string, error) {
	encrypted = strings.TrimSpace(encrypted)
	if encrypted == "" {
		return "", errors.New("empty encrypted api key")
	}
	return s.encryptor.Decrypt(encrypted)
}

func upstreamAccountExtra(channel *UpstreamChannel, platform *UpstreamPlatform, pool *UpstreamKeyPool, key *UpstreamKey) map[string]any {
	return map[string]any{
		upstreamSyncExtraManaged:          true,
		upstreamSyncExtraChannelID:        channel.ID,
		upstreamSyncExtraChannelName:      channel.Name,
		upstreamSyncExtraPlatformID:       platform.ID,
		upstreamSyncExtraPlatformProvider: normalizeProvider(platform.Provider),
		upstreamSyncExtraPoolID:           pool.ID,
		upstreamSyncExtraKeyID:            key.ID,
	}
}

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusDisabled:
		return StatusDisabled
	case StatusError:
		return StatusError
	default:
		return StatusActive
	}
}

func normalizeOptionalStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusActive, StatusDisabled, StatusError:
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return ""
	}
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude", "anthropic":
		return PlatformAnthropic
	case "gpt", "chatgpt", "openai":
		return PlatformOpenAI
	case "gemini", "google":
		return PlatformGemini
	case "antigravity":
		return PlatformAntigravity
	case "grok", "xai":
		return PlatformGrok
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func isSupportedUpstreamProvider(provider string) bool {
	switch provider {
	case PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok:
		return true
	default:
		return false
	}
}

func defaultProviderDisplayName(provider string) string {
	switch provider {
	case PlatformAnthropic:
		return "Anthropic"
	case PlatformOpenAI:
		return "OpenAI"
	case PlatformGemini:
		return "Gemini"
	case PlatformAntigravity:
		return "Antigravity"
	case PlatformGrok:
		return "Grok"
	default:
		return provider
	}
}

func normalizeUpstreamBaseURL(raw string, provider string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultBaseURL(provider)
	}
	return strings.TrimRight(raw, "/")
}

func defaultBaseURL(provider string) string {
	switch provider {
	case PlatformAnthropic, PlatformAntigravity:
		return "https://api.anthropic.com/v1"
	case PlatformOpenAI:
		return "https://api.openai.com/v1"
	case PlatformGemini:
		return "https://generativelanguage.googleapis.com/v1beta"
	case PlatformGrok:
		return "https://api.x.ai/v1"
	default:
		return ""
	}
}

func upstreamModelsEndpoint(baseURL, provider string) string {
	base := normalizeUpstreamBaseURL(baseURL, provider)
	if base == "" {
		base = defaultBaseURL(provider)
	}
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/models") {
		path += "/models"
	}
	u.Path = path
	return u.String()
}

func fingerprintSecret(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func maskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return "****"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}

func positiveIntPtr(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func mergeStringAny(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

func firstNonNilInt64(a, b *int64) *int64 {
	if a != nil {
		return a
	}
	return b
}

func firstNonNilInt(a, b *int) *int {
	if a != nil {
		return a
	}
	return b
}

func cloneUpstreamChannel(input *UpstreamChannel) UpstreamChannel {
	out := *input
	out.Platforms = make([]UpstreamPlatform, len(input.Platforms))
	for i := range input.Platforms {
		out.Platforms[i] = input.Platforms[i]
		out.Platforms[i].KeyPools = make([]UpstreamKeyPool, len(input.Platforms[i].KeyPools))
		for j := range input.Platforms[i].KeyPools {
			out.Platforms[i].KeyPools[j] = input.Platforms[i].KeyPools[j]
			out.Platforms[i].KeyPools[j].Keys = append([]UpstreamKey(nil), input.Platforms[i].KeyPools[j].Keys...)
		}
	}
	return out
}
