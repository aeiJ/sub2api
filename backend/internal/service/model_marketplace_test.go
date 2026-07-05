//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type marketplaceAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (s *marketplaceAccountRepoStub) ListSchedulable(context.Context) ([]Account, error) {
	return s.accounts, nil
}

type marketplaceGroupRepoStub struct {
	GroupRepository
	groups []Group
}

func (s *marketplaceGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	return s.groups, nil
}

func TestModelMarketplaceList_PublicGroupsLowestRateAndPricing(t *testing.T) {
	svc := NewModelMarketplaceService(
		&marketplaceAccountRepoStub{accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformAnthropic,
				Status:      StatusActive,
				Schedulable: true,
				GroupIDs:    []int64{1, 2, 3, 4},
				Credentials: map[string]any{
					"model_mapping": map[string]any{
						"claude-sonnet-4":     "claude-sonnet-4",
						"not-priceable-alias": "not-priceable-alias",
					},
				},
			},
			{
				ID:          2,
				Platform:    PlatformAnthropic,
				Status:      StatusDisabled,
				Schedulable: true,
				GroupIDs:    []int64{1},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-opus-4.5": "claude-opus-4.5"},
				},
			},
		}},
		&marketplaceGroupRepoStub{groups: []Group{
			{ID: 1, Name: "Public", Platform: PlatformAnthropic, Status: StatusActive, RateMultiplier: 1.2},
			{ID: 2, Name: "Pro", Platform: PlatformAnthropic, Status: StatusActive, RateMultiplier: 0.8},
			{ID: 3, Name: "Private", Platform: PlatformAnthropic, Status: StatusActive, IsExclusive: true, RateMultiplier: 0.1},
			{ID: 4, Name: "Disabled", Platform: PlatformAnthropic, Status: StatusDisabled, RateMultiplier: 0.5},
		}},
		NewBillingService(&config.Config{}, nil),
	)

	resp, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, resp.Models, 1)

	model := resp.Models[0]
	require.Equal(t, "claude-sonnet-4", model.Name)
	require.Equal(t, "Anthropic", model.Provider)
	require.ElementsMatch(t, []string{"Public", "Pro"}, model.GroupNames)
	require.InDelta(t, 0.8, model.RateMultiplier, 1e-9)
	require.NotNil(t, model.Pricing.InputPrice)
	require.InDelta(t, 2.4, *model.Pricing.InputPrice, 1e-9)
	require.Equal(t, string(BillingModeToken), model.BillingMode)

	require.Equal(t, 1, resp.Summary.TotalModels)
	require.Equal(t, 1, resp.Summary.ProviderCounts["Anthropic"])
	require.NotNil(t, resp.Summary.MinRateMultiplier)
	require.InDelta(t, 0.8, *resp.Summary.MinRateMultiplier, 1e-9)
	require.ElementsMatch(t, []string{"Public", "Pro"}, resp.Filters.Groups)
	require.NotContains(t, resp.Filters.Groups, "Private")
}

func TestModelMarketplaceList_FiltersUnknownPricing(t *testing.T) {
	svc := NewModelMarketplaceService(
		&marketplaceAccountRepoStub{accounts: []Account{{
			ID:          1,
			Platform:    PlatformAnthropic,
			Status:      StatusActive,
			Schedulable: true,
			GroupIDs:    []int64{1},
			Credentials: map[string]any{
				"model_mapping": map[string]any{"not-priceable-alias": "not-priceable-alias"},
			},
		}}},
		&marketplaceGroupRepoStub{groups: []Group{{
			ID: 1, Name: "Public", Platform: PlatformAnthropic, Status: StatusActive, RateMultiplier: 1,
		}}},
		NewBillingService(&config.Config{}, nil),
	)

	resp, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, resp.Models)
	require.Empty(t, resp.Filters.Groups)
	require.Equal(t, 0, resp.Summary.TotalModels)
	require.Nil(t, resp.Summary.MinRateMultiplier)
}

func TestModelMarketplaceList_EmptyWhenNoPublicGroups(t *testing.T) {
	svc := NewModelMarketplaceService(
		&marketplaceAccountRepoStub{accounts: []Account{{
			ID:          1,
			Platform:    PlatformAnthropic,
			Status:      StatusActive,
			Schedulable: true,
			GroupIDs:    []int64{1},
		}}},
		&marketplaceGroupRepoStub{groups: []Group{{
			ID: 1, Name: "Private", Platform: PlatformAnthropic, Status: StatusActive, IsExclusive: true, RateMultiplier: 0.1,
		}}},
		NewBillingService(&config.Config{}, nil),
	)

	resp, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, resp.Models)
	require.Empty(t, resp.Filters.Providers)
	require.Equal(t, map[string]int{}, resp.Summary.ProviderCounts)
}

func TestModelMarketplaceList_UnknownPlatformWithoutMappingIsIgnored(t *testing.T) {
	svc := NewModelMarketplaceService(
		&marketplaceAccountRepoStub{accounts: []Account{{
			ID:          1,
			Platform:    "custom",
			Status:      StatusActive,
			Schedulable: true,
			GroupIDs:    []int64{1},
		}}},
		&marketplaceGroupRepoStub{groups: []Group{{
			ID: 1, Name: "Public", Platform: "custom", Status: StatusActive, RateMultiplier: 1,
		}}},
		NewBillingService(&config.Config{}, nil),
	)

	resp, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, resp.Models)
	require.Empty(t, resp.Filters.Platforms)
	require.Equal(t, 0, resp.Summary.TotalModels)
}
