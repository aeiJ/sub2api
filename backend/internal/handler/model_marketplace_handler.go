package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type ModelMarketplaceHandler struct {
	service *service.ModelMarketplaceService
}

func NewModelMarketplaceHandler(service *service.ModelMarketplaceService) *ModelMarketplaceHandler {
	return &ModelMarketplaceHandler{service: service}
}

// List returns public model marketplace data.
// GET /api/v1/models/marketplace
func (h *ModelMarketplaceHandler) List(c *gin.Context) {
	result, err := h.service.List(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
