import { apiClient } from './client'

export interface MarketplacePricing {
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  image_output_price: number | null
  per_request_price: number | null
}

export interface MarketplaceModel {
  id: string
  name: string
  platform: string
  provider: string
  group_names: string[]
  rate_multiplier: number
  billing_mode: string
  pricing: MarketplacePricing
  tags: string[]
  context_window?: string
  description?: string
}

export interface MarketplaceFilters {
  providers: string[]
  groups: string[]
  billing_modes: string[]
  tags: string[]
  platforms: string[]
}

export interface MarketplaceSummary {
  total_models: number
  provider_counts: Record<string, number>
  min_rate_multiplier: number | null
}

export interface MarketplaceResponse {
  models: MarketplaceModel[]
  filters: MarketplaceFilters
  summary: MarketplaceSummary
}

export async function getModelMarketplace(options?: { signal?: AbortSignal }): Promise<MarketplaceResponse> {
  const { data } = await apiClient.get<MarketplaceResponse>('/models/marketplace', {
    signal: options?.signal,
  })
  return data
}

export const modelMarketplaceAPI = { getModelMarketplace }

export default modelMarketplaceAPI
