import { apiClient } from '../client'
import type { MonitorStatus } from './channelMonitor'

export interface UpstreamMonitorGroupView {
  id: number
  name: string
}

export interface UpstreamMonitorTimelinePoint {
  status: MonitorStatus
  latency_ms: number | null
  ping_latency_ms: number | null
  checked_at: string
}

export interface ScheduledMonitorResult {
  id: number
  plan_id: number
  status: string
  response_text: string
  error_message: string
  latency_ms: number
  ping_latency_ms: number | null
  started_at: string
  finished_at: string
  created_at: string
}

export interface UpstreamAccountMonitorItem {
  account_id: number
  account_name: string
  platform: string
  account_status: string
  error_message: string
  monitor_required: boolean
  groups: UpstreamMonitorGroupView[]
  monitor_enabled: boolean
  plan_id: number | null
  model_id: string
  cron_expression: string
  interval_minutes: number
  jitter_seconds: number
  auto_recover: boolean
  last_run_at: string | null
  next_run_at: string | null
  latest_result: ScheduledMonitorResult | null
  availability_7d: number | null
  availability_15d: number | null
  timeline: UpstreamMonitorTimelinePoint[]
}

export interface UpstreamAccountMonitorListParams {
  page?: number
  page_size?: number
  platform?: string
  status?: string
  monitor_status?: MonitorStatus | ''
  search?: string
  group_id?: number | string
}

export interface UpstreamAccountMonitorListResponse {
  items: UpstreamAccountMonitorItem[]
  total: number
  page: number
  page_size: number
  pages: number
  monitor_enabled_total: number
  monitor_disabled_total: number
}

export interface UpstreamAccountMonitorBatchResponse {
  total: number
  created?: number
  updated?: number
  enabled?: number
  disabled?: number
  success?: number
  failed?: number
  skipped?: number
}

export interface UpstreamAccountMonitorRunResponse {
  account_id: number
  plan_id: number
  result: ScheduledMonitorResult | null
  error?: string
}

export interface UpstreamAccountMonitorSettingsRequest {
  model_id: string
  interval_minutes: number
  jitter_seconds: number
  auto_recover: boolean
  monitor_enabled?: boolean
}

const BASE_PATH = '/admin/upstream-account-monitors'

export async function list(
  params: UpstreamAccountMonitorListParams = {},
  options?: { signal?: AbortSignal }
): Promise<UpstreamAccountMonitorListResponse> {
  const { data } = await apiClient.get<UpstreamAccountMonitorListResponse>(BASE_PATH, {
    params,
    signal: options?.signal,
  })
  return data
}

export async function runAll(params: UpstreamAccountMonitorListParams = {}): Promise<UpstreamAccountMonitorBatchResponse> {
  const { data } = await apiClient.post<UpstreamAccountMonitorBatchResponse>(`${BASE_PATH}/run-all`, null, {
    params,
    timeout: 180000,
  })
  return data
}

export async function enableAll(params: UpstreamAccountMonitorListParams = {}): Promise<UpstreamAccountMonitorBatchResponse> {
  const { data } = await apiClient.post<UpstreamAccountMonitorBatchResponse>(`${BASE_PATH}/enable-all`, null, { params })
  return data
}

export async function disableAll(params: UpstreamAccountMonitorListParams = {}): Promise<UpstreamAccountMonitorBatchResponse> {
  const { data } = await apiClient.post<UpstreamAccountMonitorBatchResponse>(`${BASE_PATH}/disable-all`, null, { params })
  return data
}

export async function runOne(accountId: number): Promise<UpstreamAccountMonitorRunResponse> {
  const { data } = await apiClient.post<UpstreamAccountMonitorRunResponse>(`${BASE_PATH}/${accountId}/run`)
  return data
}

export async function updateSettings(
  accountId: number,
  params: UpstreamAccountMonitorSettingsRequest
): Promise<UpstreamAccountMonitorItem> {
  const { data } = await apiClient.put<UpstreamAccountMonitorItem>(`${BASE_PATH}/${accountId}/settings`, params)
  return data
}

const upstreamAccountMonitorAPI = {
  list,
  runAll,
  enableAll,
  disableAll,
  runOne,
  updateSettings,
}

export default upstreamAccountMonitorAPI
