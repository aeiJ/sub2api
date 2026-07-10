package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type UpstreamAccountMonitorHandler struct {
	service *service.UpstreamAccountMonitorService
}

func NewUpstreamAccountMonitorHandler(service *service.UpstreamAccountMonitorService) *UpstreamAccountMonitorHandler {
	return &UpstreamAccountMonitorHandler{service: service}
}

func (h *UpstreamAccountMonitorHandler) List(c *gin.Context) {
	params, err := parseUpstreamAccountMonitorListParams(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.service.List(c.Request.Context(), params)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, result)
}

func (h *UpstreamAccountMonitorHandler) EnableAll(c *gin.Context) {
	params, err := parseUpstreamAccountMonitorBatchParams(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.service.EnableAll(c.Request.Context(), params)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, result)
}

func (h *UpstreamAccountMonitorHandler) DisableAll(c *gin.Context) {
	params, err := parseUpstreamAccountMonitorBatchParams(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.service.DisableAll(c.Request.Context(), params)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, result)
}

func (h *UpstreamAccountMonitorHandler) RunAll(c *gin.Context) {
	params, err := parseUpstreamAccountMonitorBatchParams(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.service.RunAll(c.Request.Context(), params)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, result)
}

func (h *UpstreamAccountMonitorHandler) StreamRunAll(c *gin.Context) {
	params, err := parseUpstreamAccountMonitorBatchParams(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	events, err := h.service.StreamRunAll(c.Request.Context(), params)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		response.InternalError(c, "streaming unsupported")
		return
	}
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	for event := range events {
		if err := writeUpstreamAccountMonitorSSE(c, event); err != nil {
			return
		}
		flusher.Flush()
	}
}

func (h *UpstreamAccountMonitorHandler) RunOne(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("accountId"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid account id")
		return
	}
	result, err := h.service.RunOne(c.Request.Context(), accountID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, result)
}

func parseUpstreamAccountMonitorListParams(c *gin.Context) (service.UpstreamAccountMonitorListParams, error) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	batch, err := parseUpstreamAccountMonitorBatchParams(c)
	if err != nil {
		return service.UpstreamAccountMonitorListParams{}, err
	}
	return service.UpstreamAccountMonitorListParams{
		Page:                              page,
		PageSize:                          pageSize,
		UpstreamAccountMonitorBatchParams: batch,
	}, nil
}

func parseUpstreamAccountMonitorBatchParams(c *gin.Context) (service.UpstreamAccountMonitorBatchParams, error) {
	groupID, err := parseUpstreamAccountMonitorGroupID(c.Query("group_id"))
	if err != nil {
		return service.UpstreamAccountMonitorBatchParams{}, err
	}
	return service.UpstreamAccountMonitorBatchParams{
		Platform:      c.Query("platform"),
		Status:        c.Query("status"),
		MonitorStatus: strings.TrimSpace(c.Query("monitor_status")),
		Search:        strings.TrimSpace(c.Query("search")),
		GroupID:       groupID,
	}, nil
}

func parseUpstreamAccountMonitorGroupID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if raw == "ungrouped" {
		return service.AccountListGroupUngrouped, nil
	}
	groupID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || groupID < 0 {
		return 0, fmt.Errorf("invalid group_id")
	}
	return groupID, nil
}

func (h *UpstreamAccountMonitorHandler) BatchUpdateSettings(c *gin.Context) {
	params, err := parseUpstreamAccountMonitorBatchParams(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var req service.UpstreamAccountMonitorBatchSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.service.BatchUpdateSettings(c.Request.Context(), params, req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, result)
}

func writeUpstreamAccountMonitorSSE(c *gin.Context, event service.UpstreamAccountMonitorRunAllStreamEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\n", event.Type); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

func (h *UpstreamAccountMonitorHandler) UpdateSettings(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("accountId"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid account id")
		return
	}
	var req service.UpstreamAccountMonitorSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.service.UpdateSettings(c.Request.Context(), accountID, req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, result)
}
