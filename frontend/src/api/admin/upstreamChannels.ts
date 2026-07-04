/**
 * Admin Upstream Channels API endpoints
 * Handles centralized upstream channel management for administrators.
 */

import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

const BASE_PATH = '/admin/upstream-channels'

export type UpstreamResourceStatus = 'active' | 'disabled' | 'inactive' | (string & {})
export type UpstreamProvider = 'anthropic' | 'openai' | (string & {})

export interface UpstreamChannel {
  id: number
  name: string
  description: string
  status: UpstreamResourceStatus
  platforms: UpstreamPlatform[]
  created_at: string
  updated_at: string
}

export interface UpstreamPlatform {
  id: number
  channel_id: number
  provider: UpstreamProvider
  display_name: string
  base_url: string
  status: UpstreamResourceStatus
  key_pools: UpstreamKeyPool[]
}

export interface UpstreamKeyPool {
  id: number
  platform_id: number
  name: string
  group_name: string
  group_rate_multiplier: number
  account_rate_multiplier: number
  load_factor: number
  concurrency: number
  status: UpstreamResourceStatus
  synced_group_id: number | null
  keys: UpstreamKey[]
}

export interface UpstreamKey {
  id: number
  pool_id: number
  name: string
  api_key?: string
  api_key_masked: string
  status: UpstreamResourceStatus
  synced_account_id: number | null
  last_test_latency_ms: number | null
  last_test_status: string | null
  last_test_message: string | null
}

export interface UpstreamKeyPayload {
  id?: number
  name: string
  api_key?: string
  status?: UpstreamResourceStatus
  synced_account_id?: number | null
}

export interface UpstreamKeyPoolPayload {
  id?: number
  name: string
  group_name: string
  group_rate_multiplier: number
  account_rate_multiplier: number
  load_factor: number
  concurrency: number
  status?: UpstreamResourceStatus
  synced_group_id?: number | null
  keys: UpstreamKeyPayload[]
}

export interface UpstreamPlatformPayload {
  id?: number
  provider: UpstreamProvider
  display_name: string
  base_url: string
  status?: UpstreamResourceStatus
  key_pools: UpstreamKeyPoolPayload[]
}

export interface CreateUpstreamChannelRequest {
  name: string
  description?: string
  status?: UpstreamResourceStatus
  platforms: UpstreamPlatformPayload[]
}

export interface UpdateUpstreamChannelRequest {
  name?: string
  description?: string
  status?: UpstreamResourceStatus
  platforms?: UpstreamPlatformPayload[]
}

export interface ListUpstreamChannelsParams {
  page?: number
  page_size?: number
  status?: string
  provider?: string
  search?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export interface UpstreamSyncChange {
  action: 'create' | 'update' | 'skip' | 'error' | string
  target?: 'group' | 'account' | string
  id?: number | null
  name?: string
  status?: string
  message?: string
}

export interface UpstreamSyncPreviewResponse {
  channel_id?: number
  summary?: Record<string, number | string>
  groups?: UpstreamSyncChange[]
  accounts?: UpstreamSyncChange[]
  warnings?: string[]
}

export interface UpstreamSyncResponse extends UpstreamSyncPreviewResponse {
  message?: string
}

export interface UpstreamTestRequest {
  platform_id?: number
  pool_id?: number
  key_id?: number
  provider?: UpstreamProvider
  base_url?: string
  api_key?: string
}

export interface UpstreamTestResult {
  key_id?: number
  key_name?: string
  pool_name?: string
  group_name?: string
  account_id?: number | null
  platform?: UpstreamProvider
  platform_id?: number
  channel_id?: number
  channel_name?: string
  status: string
  latency_ms?: number | null
  message?: string
  tested_at?: string
}

export interface UpstreamTestResponse {
  status?: string
  latency_ms?: number | null
  message?: string
  results?: UpstreamTestResult[]
}

export async function list(
  params: ListUpstreamChannelsParams = {},
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<UpstreamChannel>> {
  const { data } = await apiClient.get<PaginatedResponse<UpstreamChannel>>(BASE_PATH, {
    params,
    signal: options?.signal,
  })
  return data
}

export async function get(id: number): Promise<UpstreamChannel> {
  const { data } = await apiClient.get<UpstreamChannel>(`${BASE_PATH}/${id}`)
  return data
}

export async function create(payload: CreateUpstreamChannelRequest): Promise<UpstreamChannel> {
  const { data } = await apiClient.post<UpstreamChannel>(BASE_PATH, payload)
  return data
}

export async function update(
  id: number,
  payload: UpdateUpstreamChannelRequest
): Promise<UpstreamChannel> {
  const { data } = await apiClient.put<UpstreamChannel>(`${BASE_PATH}/${id}`, payload)
  return data
}

export async function remove(id: number): Promise<void> {
  await apiClient.delete(`${BASE_PATH}/${id}`)
}

export async function syncPreview(id: number): Promise<UpstreamSyncPreviewResponse> {
  const { data } = await apiClient.post<UpstreamSyncPreviewResponse>(`${BASE_PATH}/${id}/sync-preview`)
  return data
}

export async function sync(id: number): Promise<UpstreamSyncResponse> {
  const { data } = await apiClient.post<UpstreamSyncResponse>(`${BASE_PATH}/${id}/sync`)
  return data
}

export async function test(
  id: number,
  payload: UpstreamTestRequest = {}
): Promise<UpstreamTestResponse> {
  const { data } = await apiClient.post<UpstreamTestResponse>(`${BASE_PATH}/${id}/test`, payload)
  return data
}

const upstreamChannelsAPI = {
  list,
  get,
  create,
  update,
  remove,
  syncPreview,
  sync,
  test,
}

export default upstreamChannelsAPI
