package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseUpstreamAccountMonitorListParamsRejectsInvalidGroupID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req, err := http.NewRequest(http.MethodGet, "/admin/upstream-account-monitors?group_id=bad", nil)
	require.NoError(t, err)
	c.Request = req

	_, err = parseUpstreamAccountMonitorListParams(c)

	require.Error(t, err)
}

func TestParseUpstreamAccountMonitorListParamsSupportsUngrouped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req, err := http.NewRequest(http.MethodGet, "/admin/upstream-account-monitors?group_id=ungrouped", nil)
	require.NoError(t, err)
	c.Request = req

	params, err := parseUpstreamAccountMonitorListParams(c)

	require.NoError(t, err)
	require.Equal(t, service.AccountListGroupUngrouped, params.GroupID)
}

func TestParseUpstreamAccountMonitorListParamsKeepsFiltersAndSorting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req, err := http.NewRequest(http.MethodGet, "/admin/upstream-account-monitors?platform=openai&monitor_status=degraded&search=fast&group_id=12&sort_by=availability&sort_order=asc&availability_window=15d", nil)
	require.NoError(t, err)
	c.Request = req

	params, err := parseUpstreamAccountMonitorListParams(c)

	require.NoError(t, err)
	require.Equal(t, "openai", params.Platform)
	require.Empty(t, params.Status)
	require.Equal(t, "degraded", params.MonitorStatus)
	require.Equal(t, "fast", params.Search)
	require.Equal(t, int64(12), params.GroupID)
	require.Equal(t, "availability", params.SortBy)
	require.Equal(t, "asc", params.SortOrder)
	require.Equal(t, "15d", params.AvailabilityWindow)
}

func TestParseUpstreamAccountMonitorListParamsRejectsLegacyDisabledMonitorStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req, err := http.NewRequest(http.MethodGet, "/admin/upstream-account-monitors?monitor_status=disabled", nil)
	require.NoError(t, err)
	c.Request = req

	_, err = parseUpstreamAccountMonitorListParams(c)

	require.Error(t, err)
	require.Contains(t, err.Error(), "monitor_status")
}

func TestParseUpstreamAccountMonitorBatchParamsKeepsAccountStatusFilterSeparate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req, err := http.NewRequest(http.MethodPost, "/admin/upstream-account-monitors/enable-all?platform=openai&status=active&monitor_status=failed&search=fast&group_id=12", nil)
	require.NoError(t, err)
	c.Request = req

	params, err := parseUpstreamAccountMonitorBatchParams(c)

	require.NoError(t, err)
	require.Equal(t, "openai", params.Platform)
	require.Equal(t, "active", params.Status)
	require.Equal(t, "failed", params.MonitorStatus)
	require.Equal(t, "fast", params.Search)
	require.Equal(t, int64(12), params.GroupID)
}
