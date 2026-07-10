package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

const marketplacePriceScale = 1_000_000

type ModelMarketplaceService struct {
	accountRepo AccountRepository
	groupRepo   GroupRepository
	billingSvc  *BillingService
}

type MarketplacePricing struct {
	InputPrice       *float64 `json:"input_price"`
	OutputPrice      *float64 `json:"output_price"`
	CacheWritePrice  *float64 `json:"cache_write_price"`
	CacheReadPrice   *float64 `json:"cache_read_price"`
	ImageOutputPrice *float64 `json:"image_output_price"`
	PerRequestPrice  *float64 `json:"per_request_price"`
}

type MarketplaceModel struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Platform       string             `json:"platform"`
	Provider       string             `json:"provider"`
	GroupNames     []string           `json:"group_names"`
	RateMultiplier float64            `json:"rate_multiplier"`
	BillingMode    string             `json:"billing_mode"`
	Pricing        MarketplacePricing `json:"pricing"`
	Tags           []string           `json:"tags"`
	ContextWindow  string             `json:"context_window,omitempty"`
	Description    string             `json:"description,omitempty"`
}

type MarketplaceFilters struct {
	Providers    []string `json:"providers"`
	Groups       []string `json:"groups"`
	BillingModes []string `json:"billing_modes"`
	Tags         []string `json:"tags"`
	Platforms    []string `json:"platforms"`
}

type MarketplaceSummary struct {
	TotalModels       int            `json:"total_models"`
	ProviderCounts    map[string]int `json:"provider_counts"`
	MinRateMultiplier *float64       `json:"min_rate_multiplier"`
}

type MarketplaceResponse struct {
	Models  []MarketplaceModel `json:"models"`
	Filters MarketplaceFilters `json:"filters"`
	Summary MarketplaceSummary `json:"summary"`
}

type marketplaceModelAccumulator struct {
	name          string
	platform      string
	provider      string
	groupNames    map[string]struct{}
	minMultiplier float64
	hasMultiplier bool
}

func NewModelMarketplaceService(
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	billingSvc *BillingService,
) *ModelMarketplaceService {
	return &ModelMarketplaceService{
		accountRepo: accountRepo,
		groupRepo:   groupRepo,
		billingSvc:  billingSvc,
	}
}

func (s *ModelMarketplaceService) List(ctx context.Context) (*MarketplaceResponse, error) {
	if s == nil || s.accountRepo == nil || s.groupRepo == nil || s.billingSvc == nil {
		return nil, fmt.Errorf("model marketplace service is not configured")
	}

	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active groups: %w", err)
	}
	publicGroups := make(map[int64]Group, len(groups))
	for _, group := range groups {
		if group.ID <= 0 || group.IsExclusive || !group.IsActive() {
			continue
		}
		publicGroups[group.ID] = group
	}
	if len(publicGroups) == 0 {
		return emptyMarketplaceResponse(), nil
	}

	accounts, err := s.accountRepo.ListSchedulable(ctx)
	if err != nil {
		return nil, fmt.Errorf("list schedulable accounts: %w", err)
	}

	accumulators := make(map[string]*marketplaceModelAccumulator)
	for _, account := range accounts {
		if !account.IsSchedulable() {
			continue
		}
		groupIDs := marketplaceAccountGroupIDs(account)
		if len(groupIDs) == 0 {
			continue
		}
		visibleGroups := marketplaceVisibleGroups(groupIDs, publicGroups, account.Platform)
		if len(visibleGroups) == 0 {
			continue
		}
		models := marketplaceAccountModelIDs(account)
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model == "" || strings.Contains(model, "*") {
				continue
			}
			key := account.Platform + "\x00" + model
			acc, ok := accumulators[key]
			if !ok {
				acc = &marketplaceModelAccumulator{
					name:       model,
					platform:   account.Platform,
					provider:   marketplaceProviderLabel(account.Platform),
					groupNames: make(map[string]struct{}),
				}
				accumulators[key] = acc
			}
			for _, group := range visibleGroups {
				acc.groupNames[group.Name] = struct{}{}
				if !acc.hasMultiplier || group.RateMultiplier < acc.minMultiplier {
					acc.minMultiplier = group.RateMultiplier
					acc.hasMultiplier = true
				}
			}
		}
	}

	models := make([]MarketplaceModel, 0, len(accumulators))
	for _, acc := range accumulators {
		if !acc.hasMultiplier {
			continue
		}
		pricing, err := s.billingSvc.GetModelPricing(acc.name)
		if err != nil || pricing == nil {
			continue
		}
		model := marketplaceModelFromAccumulator(acc, pricing)
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].Name < models[j].Name
	})

	return buildMarketplaceResponse(models), nil
}

func emptyMarketplaceResponse() *MarketplaceResponse {
	return &MarketplaceResponse{
		Models:  []MarketplaceModel{},
		Filters: MarketplaceFilters{},
		Summary: MarketplaceSummary{ProviderCounts: map[string]int{}},
	}
}

func marketplaceVisibleGroups(groupIDs []int64, groups map[int64]Group, platform string) []Group {
	out := make([]Group, 0, len(groupIDs))
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, id := range groupIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		group, ok := groups[id]
		if !ok || group.Platform != platform {
			continue
		}
		out = append(out, group)
	}
	return out
}

func marketplaceAccountGroupIDs(account Account) []int64 {
	if len(account.GroupIDs) > 0 {
		return account.GroupIDs
	}
	if len(account.AccountGroups) > 0 {
		out := make([]int64, 0, len(account.AccountGroups))
		for _, rel := range account.AccountGroups {
			if rel.GroupID > 0 {
				out = append(out, rel.GroupID)
			}
		}
		return out
	}
	if len(account.Groups) > 0 {
		out := make([]int64, 0, len(account.Groups))
		for _, group := range account.Groups {
			if group != nil && group.ID > 0 {
				out = append(out, group.ID)
			}
		}
		return out
	}
	return nil
}

func marketplaceAccountModelIDs(account Account) []string {
	mapping := account.GetModelMapping()
	if len(mapping) > 0 {
		models := make([]string, 0, len(mapping))
		for model := range mapping {
			models = append(models, model)
		}
		sort.Strings(models)
		return models
	}
	return marketplaceDefaultModelIDs(account.Platform)
}

func marketplaceDefaultModelIDs(platform string) []string {
	switch platform {
	case PlatformAnthropic:
		ids := make([]string, 0, len(claude.DefaultModels))
		for _, model := range claude.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformOpenAI:
		return openai.DefaultModelIDs()
	case PlatformGemini:
		ids := make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformAntigravity:
		models := antigravity.DefaultModels()
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformGrok:
		return xai.DefaultModelIDs()
	default:
		return nil
	}
}

func marketplaceModelFromAccumulator(acc *marketplaceModelAccumulator, pricing *ModelPricing) MarketplaceModel {
	multiplier := acc.minMultiplier
	groupNames := make([]string, 0, len(acc.groupNames))
	for group := range acc.groupNames {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)

	model := MarketplaceModel{
		ID:             acc.name,
		Name:           acc.name,
		Platform:       acc.platform,
		Provider:       acc.provider,
		GroupNames:     groupNames,
		RateMultiplier: multiplier,
		BillingMode:    marketplaceBillingMode(acc.name, pricing),
		Pricing: MarketplacePricing{
			InputPrice:       marketplacePricePtr(pricing.InputPricePerToken, multiplier),
			OutputPrice:      marketplacePricePtr(pricing.OutputPricePerToken, multiplier),
			CacheWritePrice:  marketplacePricePtr(pricing.CacheCreationPricePerToken, multiplier),
			CacheReadPrice:   marketplacePricePtr(pricing.CacheReadPricePerToken, multiplier),
			ImageOutputPrice: marketplacePricePtr(pricing.ImageOutputPricePerToken, multiplier),
		},
		Tags:          marketplaceTags(acc.name, pricing),
		ContextWindow: marketplaceContextWindow(acc.name, pricing),
		Description:   fmt.Sprintf("%s is an AI model provided by %s.", acc.name, strings.ToLower(acc.provider)),
	}
	return model
}

func marketplacePricePtr(pricePerToken float64, multiplier float64) *float64 {
	if pricePerToken <= 0 {
		return nil
	}
	v := pricePerToken * marketplacePriceScale * multiplier
	return &v
}

func marketplaceBillingMode(model string, pricing *ModelPricing) string {
	lower := strings.ToLower(model)
	if strings.Contains(lower, "image") || (pricing != nil && pricing.ImageOutputPricePerToken > 0 && pricing.InputPricePerToken == 0 && pricing.OutputPricePerToken == 0) {
		return string(BillingModeImage)
	}
	return string(BillingModeToken)
}

func marketplaceTags(model string, pricing *ModelPricing) []string {
	lower := strings.ToLower(model)
	tags := make([]string, 0, 6)
	add := func(tag string) {
		for _, existing := range tags {
			if existing == tag {
				return
			}
		}
		tags = append(tags, tag)
	}
	if strings.Contains(lower, "reason") || strings.Contains(lower, "thinking") || strings.Contains(lower, "o1") || strings.Contains(lower, "o3") {
		add("reasoning")
	}
	if strings.Contains(lower, "tool") || strings.Contains(lower, "codex") || strings.Contains(lower, "code") {
		add("tools")
	}
	if strings.Contains(lower, "vision") || strings.Contains(lower, "image") {
		add("vision")
	}
	if strings.Contains(lower, "file") || strings.Contains(lower, "pdf") {
		add("files")
	}
	if strings.Contains(lower, "image") {
		add("image")
	}
	if pricing != nil && pricing.LongContextInputThreshold > 0 {
		add("context")
	}
	return tags
}

func marketplaceContextWindow(model string, pricing *ModelPricing) string {
	if pricing != nil && pricing.LongContextInputThreshold > 0 {
		return fmt.Sprintf("%dk", pricing.LongContextInputThreshold/1000)
	}
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "1m"):
		return "1m"
	case strings.Contains(lower, "400k"):
		return "400k"
	case strings.Contains(lower, "128k"):
		return "128k"
	default:
		return ""
	}
}

func marketplaceProviderLabel(platform string) string {
	switch platform {
	case PlatformAnthropic:
		return "Anthropic"
	case PlatformOpenAI:
		return "OpenAI"
	case PlatformGemini:
		return "Google"
	case PlatformAntigravity:
		return "Antigravity"
	case PlatformGrok:
		return "xAI"
	default:
		if platform == "" {
			return "Unknown"
		}
		return strings.ToUpper(platform[:1]) + platform[1:]
	}
}

func buildMarketplaceResponse(models []MarketplaceModel) *MarketplaceResponse {
	providers := map[string]struct{}{}
	groups := map[string]struct{}{}
	billingModes := map[string]struct{}{}
	tags := map[string]struct{}{}
	platforms := map[string]struct{}{}
	providerCounts := map[string]int{}
	var minRate *float64

	for _, model := range models {
		providers[model.Provider] = struct{}{}
		platforms[model.Platform] = struct{}{}
		billingModes[model.BillingMode] = struct{}{}
		providerCounts[model.Provider]++
		if minRate == nil || model.RateMultiplier < *minRate {
			rate := model.RateMultiplier
			minRate = &rate
		}
		for _, group := range model.GroupNames {
			groups[group] = struct{}{}
		}
		for _, tag := range model.Tags {
			tags[tag] = struct{}{}
		}
	}

	return &MarketplaceResponse{
		Models: models,
		Filters: MarketplaceFilters{
			Providers:    marketplaceSortedKeys(providers),
			Groups:       marketplaceSortedKeys(groups),
			BillingModes: marketplaceSortedKeys(billingModes),
			Tags:         marketplaceSortedKeys(tags),
			Platforms:    marketplaceSortedKeys(platforms),
		},
		Summary: MarketplaceSummary{
			TotalModels:       len(models),
			ProviderCounts:    providerCounts,
			MinRateMultiplier: minRate,
		},
	}
}

func marketplaceSortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		if key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
