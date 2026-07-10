package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type errorPassthroughHandlerRepo struct {
	rules []*model.ErrorPassthroughRule
}

func (r *errorPassthroughHandlerRepo) List(context.Context) ([]*model.ErrorPassthroughRule, error) {
	return r.rules, nil
}

func (r *errorPassthroughHandlerRepo) GetByID(_ context.Context, id int64) (*model.ErrorPassthroughRule, error) {
	for _, rule := range r.rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return nil, nil
}

func (r *errorPassthroughHandlerRepo) Create(_ context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	rule.ID = int64(len(r.rules) + 1)
	r.rules = append(r.rules, rule)
	return rule, nil
}

func (r *errorPassthroughHandlerRepo) Update(_ context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	for i, existing := range r.rules {
		if existing.ID == rule.ID {
			r.rules[i] = rule
			return rule, nil
		}
	}
	return rule, nil
}

func (r *errorPassthroughHandlerRepo) Delete(_ context.Context, id int64) error {
	for i, rule := range r.rules {
		if rule.ID == id {
			r.rules = append(r.rules[:i], r.rules[i+1:]...)
			return nil
		}
	}
	return nil
}

func TestErrorPassthroughHandler_CreateDefaultsToSafeCustomMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &errorPassthroughHandlerRepo{}
	handler := NewErrorPassthroughHandler(service.NewErrorPassthroughService(repo, nil))

	router := gin.New()
	router.POST("/rules", handler.Create)

	body := []byte(`{"name":"safe 422","error_codes":[422],"keywords":["invalid schema"]}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, repo.rules, 1)
	require.False(t, repo.rules[0].PassthroughBody)
	require.NotNil(t, repo.rules[0].CustomMessage)
	require.Equal(t, defaultErrorPassthroughCustomMessage, *repo.rules[0].CustomMessage)

	var payload struct {
		Data model.ErrorPassthroughRule `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.False(t, payload.Data.PassthroughBody)
	require.NotNil(t, payload.Data.CustomMessage)
	require.Equal(t, defaultErrorPassthroughCustomMessage, *payload.Data.CustomMessage)
}
