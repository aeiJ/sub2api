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
