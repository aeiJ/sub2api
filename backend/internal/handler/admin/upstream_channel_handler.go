package admin

import (
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type UpstreamChannelHandler struct {
	service *service.UpstreamChannelService
}

func NewUpstreamChannelHandler(service *service.UpstreamChannelService) *UpstreamChannelHandler {
	return &UpstreamChannelHandler{service: service}
}

type upstreamChannelRequest struct {
	Name        string                    `json:"name" binding:"max=100"`
	Description string                    `json:"description"`
	Status      string                    `json:"status" binding:"omitempty,oneof=active disabled error"`
	Platforms   []upstreamPlatformRequest `json:"platforms"`
}

type upstreamPlatformRequest struct {
	ID          int64                    `json:"id"`
	Provider    string                   `json:"provider" binding:"required,max=50"`
	DisplayName string                   `json:"display_name"`
	BaseURL     string                   `json:"base_url"`
	Status      string                   `json:"status" binding:"omitempty,oneof=active disabled error"`
	KeyPools    []upstreamKeyPoolRequest `json:"key_pools"`
}

type upstreamKeyPoolRequest struct {
	ID                    int64                `json:"id"`
	Name                  string               `json:"name"`
	GroupName             string               `json:"group_name"`
	GroupRateMultiplier   float64              `json:"group_rate_multiplier"`
	AccountRateMultiplier float64              `json:"account_rate_multiplier"`
	LoadFactor            int                  `json:"load_factor"`
	Concurrency           int                  `json:"concurrency"`
	Status                string               `json:"status" binding:"omitempty,oneof=active disabled error"`
	SyncedGroupID         *int64               `json:"synced_group_id"`
	Keys                  []upstreamKeyRequest `json:"keys"`
}

type upstreamKeyRequest struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	APIKey          string `json:"api_key"`
	Status          string `json:"status" binding:"omitempty,oneof=active disabled error"`
	SyncedAccountID *int64 `json:"synced_account_id"`
}

func (h *UpstreamChannelHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	status := c.Query("status")
	search := strings.TrimSpace(c.Query("search"))
	if len(search) > 100 {
		search = search[:100]
	}
	channels, pag, err := h.service.List(c.Request.Context(), pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}, status, search)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, channels, pag.Total, page, pageSize)
}

func (h *UpstreamChannelHandler) GetByID(c *gin.Context) {
	id, err := parseUpstreamChannelID(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	channel, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, channel)
}

func (h *UpstreamChannelHandler) Create(c *gin.Context) {
	var req upstreamChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	channel, err := h.service.Create(c.Request.Context(), upstreamChannelRequestToService(req))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, channel)
}

func (h *UpstreamChannelHandler) Update(c *gin.Context) {
	id, err := parseUpstreamChannelID(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var req upstreamChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	if isUpstreamStatusOnlyUpdate(req) {
		channel, err := h.service.UpdateStatus(c.Request.Context(), id, req.Status)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, channel)
		return
	}
	channel, err := h.service.Update(c.Request.Context(), id, upstreamChannelRequestToService(req))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, channel)
}

func (h *UpstreamChannelHandler) Delete(c *gin.Context) {
	id, err := parseUpstreamChannelID(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *UpstreamChannelHandler) SyncPreview(c *gin.Context) {
	id, err := parseUpstreamChannelID(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	preview, err := h.service.SyncPreview(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *UpstreamChannelHandler) Sync(c *gin.Context) {
	id, err := parseUpstreamChannelID(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := h.service.Sync(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *UpstreamChannelHandler) Test(c *gin.Context) {
	id, err := parseUpstreamChannelID(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	results, err := h.service.TestChannel(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"results": results})
}

func parseUpstreamChannelID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, infraerrors.BadRequest("INVALID_UPSTREAM_CHANNEL_ID", "invalid upstream channel ID")
	}
	return id, nil
}

func isUpstreamStatusOnlyUpdate(req upstreamChannelRequest) bool {
	return req.Status != "" &&
		req.Name == "" &&
		req.Description == "" &&
		req.Platforms == nil
}

func upstreamChannelRequestToService(req upstreamChannelRequest) *service.UpstreamChannel {
	channel := &service.UpstreamChannel{
		Name:        req.Name,
		Description: req.Description,
		Status:      req.Status,
		Platforms:   make([]service.UpstreamPlatform, 0, len(req.Platforms)),
	}
	for _, p := range req.Platforms {
		platform := service.UpstreamPlatform{
			ID:          p.ID,
			Provider:    p.Provider,
			DisplayName: p.DisplayName,
			BaseURL:     p.BaseURL,
			Status:      p.Status,
			KeyPools:    make([]service.UpstreamKeyPool, 0, len(p.KeyPools)),
		}
		for _, kp := range p.KeyPools {
			pool := service.UpstreamKeyPool{
				ID:                    kp.ID,
				Name:                  kp.Name,
				GroupName:             kp.GroupName,
				GroupRateMultiplier:   kp.GroupRateMultiplier,
				AccountRateMultiplier: kp.AccountRateMultiplier,
				LoadFactor:            kp.LoadFactor,
				Concurrency:           kp.Concurrency,
				Status:                kp.Status,
				SyncedGroupID:         kp.SyncedGroupID,
				Keys:                  make([]service.UpstreamKey, 0, len(kp.Keys)),
			}
			for _, k := range kp.Keys {
				pool.Keys = append(pool.Keys, service.UpstreamKey{
					ID:              k.ID,
					Name:            k.Name,
					APIKey:          k.APIKey,
					Status:          k.Status,
					SyncedAccountID: k.SyncedAccountID,
				})
			}
			platform.KeyPools = append(platform.KeyPools, pool)
		}
		channel.Platforms = append(channel.Platforms, platform)
	}
	return channel
}
