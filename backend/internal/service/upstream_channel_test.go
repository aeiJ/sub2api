package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestUpstreamChannelServiceCreateEncryptsAndMasksAPIKeys(t *testing.T) {
	repo := newFakeUpstreamRepo()
	svc := NewUpstreamChannelService(repo, newFakeUpstreamAdmin(newFakeUpstreamGroups()), nil, nil, fakeUpstreamEncryptor{}, nil)

	created, err := svc.Create(context.Background(), &UpstreamChannel{
		Name:   "Prod upstream",
		Status: StatusActive,
		Platforms: []UpstreamPlatform{{
			Provider:    PlatformOpenAI,
			DisplayName: "GPT",
			BaseURL:     "https://api.openai.com/v1",
			KeyPools: []UpstreamKeyPool{{
				Name:        "GPT pool",
				GroupName:   "GPT group",
				Concurrency: 2,
				Keys: []UpstreamKey{{
					Name:   "primary",
					APIKey: "sk-test-123456",
				}},
			}},
		}},
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)

	stored := repo.channels[created.ID]
	storedKey := stored.Platforms[0].KeyPools[0].Keys[0]
	require.Empty(t, storedKey.APIKey)
	require.Equal(t, "enc:sk-test-123456", storedKey.EncryptedAPIKey)
	require.NotEmpty(t, storedKey.APIKeyFingerprint)
	require.Equal(t, "sk-t...3456", created.Platforms[0].KeyPools[0].Keys[0].APIKeyMasked)
}

func TestUpstreamChannelServiceCreateGeneratesUniqueDefaultPoolNames(t *testing.T) {
	repo := newFakeUpstreamRepo()
	svc := NewUpstreamChannelService(repo, nil, nil, nil, fakeUpstreamEncryptor{}, nil)

	created, err := svc.Create(context.Background(), &UpstreamChannel{
		Name: "Default pools",
		Platforms: []UpstreamPlatform{{
			Provider:    PlatformAnthropic,
			DisplayName: "Claude",
			KeyPools: []UpstreamKeyPool{
				{},
				{},
			},
		}},
	})

	require.NoError(t, err)
	require.Len(t, created.Platforms[0].KeyPools, 2)
	require.Equal(t, "Claude pool", created.Platforms[0].KeyPools[0].Name)
	require.Equal(t, "Claude pool 2", created.Platforms[0].KeyPools[1].Name)
}

func TestUpstreamChannelServiceCreateRejectsDuplicatePlatformsAndPools(t *testing.T) {
	svc := NewUpstreamChannelService(newFakeUpstreamRepo(), nil, nil, nil, fakeUpstreamEncryptor{}, nil)

	_, err := svc.Create(context.Background(), &UpstreamChannel{
		Name: "Duplicate providers",
		Platforms: []UpstreamPlatform{
			{Provider: "claude"},
			{Provider: "anthropic"},
		},
	})
	require.Error(t, err)
	require.Equal(t, "UPSTREAM_PLATFORM_DUPLICATE", infraerrors.Reason(err))

	_, err = svc.Create(context.Background(), &UpstreamChannel{
		Name: "Duplicate pools",
		Platforms: []UpstreamPlatform{{
			Provider: PlatformOpenAI,
			KeyPools: []UpstreamKeyPool{
				{Name: "shared", GroupName: "group-a"},
				{Name: " shared ", GroupName: "group-b"},
			},
		}},
	})
	require.Error(t, err)
	require.Equal(t, "UPSTREAM_KEY_POOL_DUPLICATE", infraerrors.Reason(err))
}

func TestUpstreamChannelServiceSyncIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeUpstreamRepo()
	groups := newFakeUpstreamGroups()
	admin := newFakeUpstreamAdmin(groups)
	svc := NewUpstreamChannelService(repo, admin, admin, groups, fakeUpstreamEncryptor{}, nil)

	channel, err := svc.Create(ctx, &UpstreamChannel{
		Name:   "Mixed upstream",
		Status: StatusActive,
		Platforms: []UpstreamPlatform{{
			Provider:    PlatformAnthropic,
			DisplayName: "Claude",
			BaseURL:     "https://api.anthropic.com/v1",
			KeyPools: []UpstreamKeyPool{{
				Name:                  "Claude pool",
				GroupName:             "Claude group",
				GroupRateMultiplier:   1.25,
				AccountRateMultiplier: 0.8,
				LoadFactor:            4,
				Concurrency:           2,
				Status:                StatusActive,
				Keys: []UpstreamKey{{
					Name:   "claude-key",
					APIKey: "sk-ant-123456",
					Status: StatusActive,
				}},
			}},
		}},
	})
	require.NoError(t, err)

	first, err := svc.Sync(ctx, channel.ID)
	require.NoError(t, err)
	require.Equal(t, 1, first.Summary["create_group"])
	require.Equal(t, 1, first.Summary["create_account"])
	require.Len(t, first.Groups, 1)
	require.Len(t, first.Accounts, 1)
	require.Equal(t, 1, admin.createGroupCalls)
	require.Equal(t, 1, admin.createAccountCalls)

	reloaded, err := repo.GetByID(ctx, channel.ID)
	require.NoError(t, err)
	pool := reloaded.Platforms[0].KeyPools[0]
	key := pool.Keys[0]
	require.NotNil(t, pool.SyncedGroupID)
	require.NotNil(t, key.SyncedAccountID)

	second, err := svc.Sync(ctx, channel.ID)
	require.NoError(t, err)
	require.Equal(t, 1, second.Summary["update_group"])
	require.Equal(t, 1, second.Summary["update_account"])
	require.Equal(t, 1, admin.createGroupCalls)
	require.Equal(t, 1, admin.createAccountCalls)
	require.Equal(t, 1, admin.updateAccountCalls)

	account := admin.accounts[*key.SyncedAccountID]
	require.Equal(t, PlatformAnthropic, account.Platform)
	require.Equal(t, AccountTypeAPIKey, account.Type)
	require.Equal(t, "sk-ant-123456", account.Credentials["api_key"])
	require.Equal(t, int64(key.ID), account.Extra[upstreamSyncExtraKeyID])
	require.Equal(t, []int64{*pool.SyncedGroupID}, account.GroupIDs)
	require.Equal(t, 0.8, *account.RateMultiplier)
	require.Equal(t, 4, *account.LoadFactor)
}

func TestUpstreamChannelServiceSyncUsesExistingAccountCredentialsWhenUpstreamKeyBlank(t *testing.T) {
	ctx := context.Background()
	repo := newFakeUpstreamRepo()
	groups := newFakeUpstreamGroups()
	admin := newFakeUpstreamAdmin(groups)
	svc := NewUpstreamChannelService(repo, admin, admin, groups, fakeUpstreamEncryptor{}, nil)

	channel, err := svc.Create(ctx, &UpstreamChannel{
		Name:   "Existing account upstream",
		Status: StatusActive,
		Platforms: []UpstreamPlatform{{
			Provider:    PlatformOpenAI,
			DisplayName: "GPT",
			BaseURL:     "https://api.openai.com/v1",
			KeyPools: []UpstreamKeyPool{{
				Name:                  "GPT pool",
				GroupName:             "GPT group",
				GroupRateMultiplier:   1,
				AccountRateMultiplier: 1.5,
				LoadFactor:            3,
				Concurrency:           2,
				Status:                StatusActive,
				Keys: []UpstreamKey{{
					Name:   "gpt-key",
					APIKey: "sk-existing-123456",
					Status: StatusActive,
				}},
			}},
		}},
	})
	require.NoError(t, err)

	first, err := svc.Sync(ctx, channel.ID)
	require.NoError(t, err)
	require.Equal(t, 1, first.Summary["create_account"])

	stored := repo.channels[channel.ID]
	pool := &stored.Platforms[0].KeyPools[0]
	key := &pool.Keys[0]
	require.NotNil(t, key.SyncedAccountID)
	accountID := *key.SyncedAccountID
	require.Equal(t, "sk-existing-123456", admin.accounts[accountID].Credentials["api_key"])

	key.EncryptedAPIKey = ""
	key.APIKeyFingerprint = ""
	key.APIKeyMasked = ""
	pool.AccountRateMultiplier = 2.25

	preview, err := svc.SyncPreview(ctx, channel.ID)
	require.NoError(t, err)
	require.Equal(t, 1, preview.Summary["update_account"])
	require.Zero(t, preview.Summary["skip_account"])

	second, err := svc.Sync(ctx, channel.ID)
	require.NoError(t, err)
	require.Equal(t, 1, second.Summary["update_account"])
	require.Zero(t, second.Summary["skip_account"])
	require.Equal(t, "sk-existing-123456", admin.accounts[accountID].Credentials["api_key"])
	require.Equal(t, 2.25, *admin.accounts[accountID].RateMultiplier)
}

func TestUpstreamChannelServiceUpdateStatusPreservesNestedConfig(t *testing.T) {
	ctx := context.Background()
	repo := newFakeUpstreamRepo()
	svc := NewUpstreamChannelService(repo, nil, nil, nil, fakeUpstreamEncryptor{}, nil)

	channel, err := svc.Create(ctx, &UpstreamChannel{
		Name:   "Managed upstream",
		Status: StatusActive,
		Platforms: []UpstreamPlatform{{
			Provider:    PlatformOpenAI,
			DisplayName: "GPT",
			BaseURL:     "https://api.openai.com/v1",
			KeyPools: []UpstreamKeyPool{{
				Name:                "GPT pool",
				GroupName:           "GPT group",
				GroupRateMultiplier: 1.2,
				Concurrency:         2,
				Keys: []UpstreamKey{{
					Name:   "group key",
					APIKey: "sk-test-123456",
				}},
			}},
		}},
	})
	require.NoError(t, err)

	updated, err := svc.UpdateStatus(ctx, channel.ID, StatusDisabled)
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, updated.Status)
	require.Equal(t, "Managed upstream", updated.Name)
	require.Len(t, updated.Platforms, 1)
	require.Equal(t, PlatformOpenAI, updated.Platforms[0].Provider)
	require.Len(t, updated.Platforms[0].KeyPools, 1)
	require.Equal(t, "GPT group", updated.Platforms[0].KeyPools[0].GroupName)
	require.Len(t, updated.Platforms[0].KeyPools[0].Keys, 1)
	require.Equal(t, "group key", updated.Platforms[0].KeyPools[0].Keys[0].Name)
	require.Equal(t, "sk-t...3456", updated.Platforms[0].KeyPools[0].Keys[0].APIKeyMasked)
}

type fakeUpstreamEncryptor struct{}

func (fakeUpstreamEncryptor) Encrypt(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}
func (fakeUpstreamEncryptor) Decrypt(ciphertext string) (string, error) {
	if len(ciphertext) >= 4 && ciphertext[:4] == "enc:" {
		return ciphertext[4:], nil
	}
	return "", fmt.Errorf("invalid ciphertext")
}

type fakeUpstreamRepo struct {
	nextChannelID  int64
	nextPlatformID int64
	nextPoolID     int64
	nextKeyID      int64
	channels       map[int64]*UpstreamChannel
}

func newFakeUpstreamRepo() *fakeUpstreamRepo {
	return &fakeUpstreamRepo{
		nextChannelID:  10,
		nextPlatformID: 100,
		nextPoolID:     200,
		nextKeyID:      300,
		channels:       map[int64]*UpstreamChannel{},
	}
}

func (r *fakeUpstreamRepo) Create(_ context.Context, channel *UpstreamChannel) error {
	r.assignIDs(channel)
	now := time.Now()
	channel.CreatedAt = now
	channel.UpdatedAt = now
	cp := cloneUpstreamChannel(channel)
	r.channels[channel.ID] = &cp
	return nil
}

func (r *fakeUpstreamRepo) Update(_ context.Context, channel *UpstreamChannel) error {
	r.assignIDs(channel)
	cp := cloneUpstreamChannel(channel)
	r.channels[channel.ID] = &cp
	return nil
}

func (r *fakeUpstreamRepo) Delete(_ context.Context, id int64) error {
	delete(r.channels, id)
	return nil
}

func (r *fakeUpstreamRepo) GetByID(_ context.Context, id int64) (*UpstreamChannel, error) {
	channel := r.channels[id]
	if channel == nil {
		return nil, ErrUpstreamChannelNotFound
	}
	cp := cloneUpstreamChannel(channel)
	return &cp, nil
}

func (r *fakeUpstreamRepo) List(_ context.Context, _ pagination.PaginationParams, _, _ string) ([]UpstreamChannel, *pagination.PaginationResult, error) {
	out := make([]UpstreamChannel, 0, len(r.channels))
	for _, channel := range r.channels {
		out = append(out, cloneUpstreamChannel(channel))
	}
	return out, &pagination.PaginationResult{Total: int64(len(out)), Page: 1, PageSize: 20, Pages: 1}, nil
}

func (r *fakeUpstreamRepo) UpdatePoolSyncedGroupID(_ context.Context, poolID int64, groupID int64) error {
	for _, channel := range r.channels {
		for pi := range channel.Platforms {
			for pki := range channel.Platforms[pi].KeyPools {
				pool := &channel.Platforms[pi].KeyPools[pki]
				if pool.ID == poolID {
					pool.SyncedGroupID = &groupID
					return nil
				}
			}
		}
	}
	return ErrUpstreamChannelNotFound
}

func (r *fakeUpstreamRepo) UpdateKeySyncedAccountID(_ context.Context, keyID int64, accountID int64) error {
	for _, channel := range r.channels {
		for pi := range channel.Platforms {
			for pki := range channel.Platforms[pi].KeyPools {
				for ki := range channel.Platforms[pi].KeyPools[pki].Keys {
					key := &channel.Platforms[pi].KeyPools[pki].Keys[ki]
					if key.ID == keyID {
						key.SyncedAccountID = &accountID
						return nil
					}
				}
			}
		}
	}
	return ErrUpstreamChannelNotFound
}

func (r *fakeUpstreamRepo) UpdateKeyTestResult(_ context.Context, _ int64, _ UpstreamTestResult) error {
	return nil
}

func (r *fakeUpstreamRepo) RecordSyncEvent(_ context.Context, _ int64, _ string, _ map[string]int) error {
	return nil
}

func (r *fakeUpstreamRepo) assignIDs(channel *UpstreamChannel) {
	if channel.ID == 0 {
		channel.ID = r.nextChannelID
		r.nextChannelID++
	}
	for pi := range channel.Platforms {
		if channel.Platforms[pi].ID == 0 {
			channel.Platforms[pi].ID = r.nextPlatformID
			r.nextPlatformID++
		}
		channel.Platforms[pi].ChannelID = channel.ID
		for pki := range channel.Platforms[pi].KeyPools {
			pool := &channel.Platforms[pi].KeyPools[pki]
			if pool.ID == 0 {
				pool.ID = r.nextPoolID
				r.nextPoolID++
			}
			pool.PlatformID = channel.Platforms[pi].ID
			for ki := range pool.Keys {
				if pool.Keys[ki].ID == 0 {
					pool.Keys[ki].ID = r.nextKeyID
					r.nextKeyID++
				}
				pool.Keys[ki].PoolID = pool.ID
			}
		}
	}
}

type fakeUpstreamGroups struct {
	nextID int64
	items  map[int64]*Group
}

func newFakeUpstreamGroups() *fakeUpstreamGroups {
	return &fakeUpstreamGroups{nextID: 1, items: map[int64]*Group{}}
}

func (g *fakeUpstreamGroups) GetByID(_ context.Context, id int64) (*Group, error) {
	group := g.items[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	cp := *group
	return &cp, nil
}

func (g *fakeUpstreamGroups) Update(_ context.Context, group *Group) error {
	cp := *group
	g.items[group.ID] = &cp
	return nil
}

type fakeUpstreamAdmin struct {
	groups             *fakeUpstreamGroups
	nextAccountID      int64
	accounts           map[int64]*Account
	createGroupCalls   int
	createAccountCalls int
	updateAccountCalls int
}

func newFakeUpstreamAdmin(groups *fakeUpstreamGroups) *fakeUpstreamAdmin {
	return &fakeUpstreamAdmin{
		groups:        groups,
		nextAccountID: 1000,
		accounts:      map[int64]*Account{},
	}
}

func (a *fakeUpstreamAdmin) CreateGroup(_ context.Context, input *CreateGroupInput) (*Group, error) {
	a.createGroupCalls++
	id := a.groups.nextID
	a.groups.nextID++
	group := &Group{
		ID:               id,
		Name:             input.Name,
		Description:      input.Description,
		Platform:         input.Platform,
		RateMultiplier:   input.RateMultiplier,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeStandard,
	}
	a.groups.items[id] = group
	cp := *group
	return &cp, nil
}

func (a *fakeUpstreamAdmin) CreateAccount(_ context.Context, input *CreateAccountInput) (*Account, error) {
	a.createAccountCalls++
	id := a.nextAccountID
	a.nextAccountID++
	account := &Account{
		ID:             id,
		Name:           input.Name,
		Platform:       input.Platform,
		Type:           input.Type,
		Credentials:    input.Credentials,
		Extra:          input.Extra,
		Concurrency:    input.Concurrency,
		Priority:       input.Priority,
		RateMultiplier: input.RateMultiplier,
		LoadFactor:     input.LoadFactor,
		Status:         StatusActive,
		GroupIDs:       append([]int64(nil), input.GroupIDs...),
	}
	a.accounts[id] = account
	cp := *account
	return &cp, nil
}

func (a *fakeUpstreamAdmin) UpdateAccount(_ context.Context, id int64, input *UpdateAccountInput) (*Account, error) {
	a.updateAccountCalls++
	account := a.accounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if input.Name != "" {
		account.Name = input.Name
	}
	if input.Type != "" {
		account.Type = input.Type
	}
	if input.Credentials != nil {
		account.Credentials = input.Credentials
	}
	if input.Extra != nil {
		account.Extra = input.Extra
	}
	if input.Concurrency != nil {
		account.Concurrency = *input.Concurrency
	}
	if input.RateMultiplier != nil {
		account.RateMultiplier = input.RateMultiplier
	}
	if input.LoadFactor != nil {
		account.LoadFactor = input.LoadFactor
	}
	if input.Status != "" {
		account.Status = input.Status
	}
	if input.GroupIDs != nil {
		account.GroupIDs = append([]int64(nil), (*input.GroupIDs)...)
	}
	cp := *account
	return &cp, nil
}

func (a *fakeUpstreamAdmin) GetByID(_ context.Context, id int64) (*Account, error) {
	account := a.accounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	cp := *account
	return &cp, nil
}

func (a *fakeUpstreamAdmin) FindByExtraField(_ context.Context, key string, value any) ([]Account, error) {
	out := []Account{}
	for _, account := range a.accounts {
		if account.Extra[key] == value {
			cp := *account
			out = append(out, cp)
		}
	}
	return out, nil
}
