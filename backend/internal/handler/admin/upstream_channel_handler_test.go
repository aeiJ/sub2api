package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpstreamChannelHandlerCreateMasksAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newAdminUpstreamFakeRepo()
	svc := service.NewUpstreamChannelService(repo, nil, nil, nil, adminUpstreamFakeEncryptor{}, nil, nil)
	handler := NewUpstreamChannelHandler(svc)

	router := gin.New()
	router.POST("/upstreams", handler.Create)

	body := `{
		"name":"Prod upstream",
		"platforms":[{
			"provider":"openai",
			"display_name":"GPT",
			"base_url":"https://api.openai.com/v1",
			"key_pools":[{
				"name":"GPT pool",
				"group_name":"GPT group",
				"upstream_group_rate_multiplier":1.75,
				"group_rate_multiplier":1,
				"account_rate_multiplier":1,
				"load_factor":1,
				"concurrency":2,
				"keys":[{"name":"primary","api_key":"sk-secret-123456","status":"active"}]
			}]
		}]
	}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/upstreams", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "sk-secret-123456")
	require.Contains(t, w.Body.String(), `"api_key_masked":"sk-s...3456"`)

	stored := repo.channels[1].Platforms[0].KeyPools[0].Keys[0]
	require.Equal(t, "enc:sk-secret-123456", stored.EncryptedAPIKey)
	require.Empty(t, stored.APIKey)
	require.Equal(t, 1.75, repo.channels[1].Platforms[0].KeyPools[0].UpstreamGroupRateMultiplier)
}

func TestUpstreamChannelHandlerUpdateStatusOnlyPreservesChannelConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newAdminUpstreamFakeRepo()
	svc := service.NewUpstreamChannelService(repo, nil, nil, nil, adminUpstreamFakeEncryptor{}, nil, nil)
	handler := NewUpstreamChannelHandler(svc)

	router := gin.New()
	router.PUT("/upstreams/:id", handler.Update)

	repo.channels[1] = &service.UpstreamChannel{
		ID:     1,
		Name:   "Prod upstream",
		Status: service.StatusActive,
		Platforms: []service.UpstreamPlatform{{
			ID:          2,
			Provider:    service.PlatformOpenAI,
			DisplayName: "GPT",
			BaseURL:     "https://api.openai.com/v1",
			Status:      service.StatusActive,
			KeyPools: []service.UpstreamKeyPool{{
				ID:                  3,
				Name:                "GPT pool",
				GroupName:           "GPT group",
				GroupRateMultiplier: 1,
				Concurrency:         2,
				Status:              service.StatusActive,
				Keys: []service.UpstreamKey{{
					ID:                4,
					Name:              "primary",
					EncryptedAPIKey:   "enc:sk-secret-123456",
					APIKeyFingerprint: "fingerprint",
					Status:            service.StatusActive,
				}},
			}},
		}},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/upstreams/1", strings.NewReader(`{"status":"disabled"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"status":"disabled"`)
	stored := repo.channels[1]
	require.Equal(t, "Prod upstream", stored.Name)
	require.Equal(t, service.StatusDisabled, stored.Status)
	require.Len(t, stored.Platforms, 1)
	require.Len(t, stored.Platforms[0].KeyPools, 1)
	require.Len(t, stored.Platforms[0].KeyPools[0].Keys, 1)
}

func TestUpstreamChannelHandlerTestAcceptsFilteredBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newAdminUpstreamFakeRepo()
	svc := service.NewUpstreamChannelService(repo, nil, nil, nil, adminUpstreamFakeEncryptor{}, nil, nil)
	handler := NewUpstreamChannelHandler(svc)

	router := gin.New()
	router.POST("/upstreams/:id/test", handler.Test)

	repo.channels[1] = &service.UpstreamChannel{
		ID:     1,
		Name:   "Prod upstream",
		Status: service.StatusActive,
		Platforms: []service.UpstreamPlatform{{
			ID:       2,
			Provider: service.PlatformOpenAI,
			KeyPools: []service.UpstreamKeyPool{
				{
					ID:        3,
					Name:      "pool-a",
					GroupName: "group-a",
					Keys: []service.UpstreamKey{
						{ID: 4, Name: "a-1", Status: service.StatusActive},
						{ID: 5, Name: "a-2", Status: service.StatusActive},
					},
				},
				{
					ID:        6,
					Name:      "pool-b",
					GroupName: "group-b",
					Keys:      []service.UpstreamKey{{ID: 7, Name: "b-1", Status: service.StatusActive}},
				},
			},
		}},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/upstreams/1/test", strings.NewReader(`{"pool_id":3,"key_id":5}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"key_id":5`)
	require.NotContains(t, w.Body.String(), `"key_id":4`)
	require.NotContains(t, w.Body.String(), `"key_id":7`)
	require.Equal(t, []int64{5}, repo.testedKeyIDs)
}

func TestUpstreamChannelHandlerTestRejectsMalformedFilterBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newAdminUpstreamFakeRepo()
	svc := service.NewUpstreamChannelService(repo, nil, nil, nil, adminUpstreamFakeEncryptor{}, nil, nil)
	handler := NewUpstreamChannelHandler(svc)

	router := gin.New()
	router.POST("/upstreams/:id/test", handler.Test)

	repo.channels[1] = &service.UpstreamChannel{ID: 1, Name: "Prod upstream"}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/upstreams/1/test", strings.NewReader(`{"key_id":`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, repo.testedKeyIDs)
}

type adminUpstreamFakeEncryptor struct{}

func (adminUpstreamFakeEncryptor) Encrypt(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}

func (adminUpstreamFakeEncryptor) Decrypt(ciphertext string) (string, error) {
	if strings.HasPrefix(ciphertext, "enc:") {
		return strings.TrimPrefix(ciphertext, "enc:"), nil
	}
	return "", fmt.Errorf("invalid ciphertext")
}

type adminUpstreamFakeRepo struct {
	nextID       int64
	channels     map[int64]*service.UpstreamChannel
	testedKeyIDs []int64
}

func newAdminUpstreamFakeRepo() *adminUpstreamFakeRepo {
	return &adminUpstreamFakeRepo{nextID: 1, channels: map[int64]*service.UpstreamChannel{}}
}

func (r *adminUpstreamFakeRepo) Create(_ context.Context, channel *service.UpstreamChannel) error {
	r.assignIDs(channel)
	now := time.Now()
	channel.CreatedAt = now
	channel.UpdatedAt = now
	cp := cloneAdminUpstreamChannel(channel)
	r.channels[channel.ID] = &cp
	return nil
}

func (r *adminUpstreamFakeRepo) Update(_ context.Context, channel *service.UpstreamChannel) error {
	r.assignIDs(channel)
	cp := cloneAdminUpstreamChannel(channel)
	r.channels[channel.ID] = &cp
	return nil
}

func (r *adminUpstreamFakeRepo) Delete(_ context.Context, id int64) error {
	delete(r.channels, id)
	return nil
}

func (r *adminUpstreamFakeRepo) GetByID(_ context.Context, id int64) (*service.UpstreamChannel, error) {
	channel := r.channels[id]
	if channel == nil {
		return nil, service.ErrUpstreamChannelNotFound
	}
	cp := cloneAdminUpstreamChannel(channel)
	return &cp, nil
}

func (r *adminUpstreamFakeRepo) List(_ context.Context, _ pagination.PaginationParams, _, _ string) ([]service.UpstreamChannel, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}

func (r *adminUpstreamFakeRepo) UpdatePoolSyncedGroupID(context.Context, int64, int64) error {
	return nil
}

func (r *adminUpstreamFakeRepo) UpdateKeySyncedAccountID(context.Context, int64, int64) error {
	return nil
}

func (r *adminUpstreamFakeRepo) UpdateKeyTestResult(_ context.Context, keyID int64, result service.UpstreamTestResult) error {
	r.testedKeyIDs = append(r.testedKeyIDs, keyID)
	for _, channel := range r.channels {
		for pi := range channel.Platforms {
			for pki := range channel.Platforms[pi].KeyPools {
				for ki := range channel.Platforms[pi].KeyPools[pki].Keys {
					key := &channel.Platforms[pi].KeyPools[pki].Keys[ki]
					if key.ID == keyID {
						key.LastTestLatencyMS = result.LatencyMS
						key.LastTestStatus = result.Status
						key.LastTestMessage = result.Message
						key.LastTestedAt = result.TestedAt
						return nil
					}
				}
			}
		}
	}
	return service.ErrUpstreamChannelNotFound
}

func (r *adminUpstreamFakeRepo) RecordSyncEvent(context.Context, int64, string, map[string]int) error {
	return nil
}

func (r *adminUpstreamFakeRepo) assignIDs(channel *service.UpstreamChannel) {
	if channel.ID == 0 {
		channel.ID = r.nextID
		r.nextID++
	}
	for pi := range channel.Platforms {
		if channel.Platforms[pi].ID == 0 {
			channel.Platforms[pi].ID = r.nextID
			r.nextID++
		}
		channel.Platforms[pi].ChannelID = channel.ID
		for pki := range channel.Platforms[pi].KeyPools {
			pool := &channel.Platforms[pi].KeyPools[pki]
			if pool.ID == 0 {
				pool.ID = r.nextID
				r.nextID++
			}
			pool.PlatformID = channel.Platforms[pi].ID
			for ki := range pool.Keys {
				if pool.Keys[ki].ID == 0 {
					pool.Keys[ki].ID = r.nextID
					r.nextID++
				}
				pool.Keys[ki].PoolID = pool.ID
			}
		}
	}
}

func cloneAdminUpstreamChannel(input *service.UpstreamChannel) service.UpstreamChannel {
	out := *input
	out.Platforms = make([]service.UpstreamPlatform, len(input.Platforms))
	for i := range input.Platforms {
		out.Platforms[i] = input.Platforms[i]
		out.Platforms[i].KeyPools = make([]service.UpstreamKeyPool, len(input.Platforms[i].KeyPools))
		for j := range input.Platforms[i].KeyPools {
			out.Platforms[i].KeyPools[j] = input.Platforms[i].KeyPools[j]
			out.Platforms[i].KeyPools[j].Keys = append([]service.UpstreamKey(nil), input.Platforms[i].KeyPools[j].Keys...)
		}
	}
	return out
}
