import { apiClient, buildApiUrl } from '../client'
import type { MonitorStatus } from './channelMonitor'

export type UpstreamAccountMonitorStatus = Extract<MonitorStatus, 'operational' | 'degraded' | 'failed'>
export type UpstreamAccountMonitorAvailabilityWindow = '7d' | '15d'

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
  monitor_status?: UpstreamAccountMonitorStatus | ''
  search?: string
  group_id?: number | string
  sort_by?: 'availability'
  sort_order?: 'asc' | 'desc'
  availability_window?: UpstreamAccountMonitorAvailabilityWindow
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

export interface UpstreamAccountMonitorBatchSettingsRequest {
  monitor_enabled?: boolean
  interval_minutes?: number
  jitter_seconds?: number
}

export interface UpstreamAccountMonitorRunResponse {
  account_id: number
  plan_id: number
  result: ScheduledMonitorResult | null
  error?: string
}

export type UpstreamAccountMonitorRunAllStreamEvent =
  | { type: 'started'; total: number }
  | { type: 'item'; account_id: number; plan_id?: number; result?: ScheduledMonitorResult | null; error?: string }
  | { type: 'done'; total: number; success?: number; failed?: number; skipped?: number }
  | { type: 'error'; error?: string; message?: string }

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

export async function runAllStream(
  params: UpstreamAccountMonitorListParams = {},
  onEvent: (event: UpstreamAccountMonitorRunAllStreamEvent) => void,
  options: { signal?: AbortSignal } = {}
): Promise<void> {
  const url = new URL(buildApiUrl(`${BASE_PATH}/run-all/stream`), window.location.origin)
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      url.searchParams.set(key, String(value))
    }
  })

  const headers: Record<string, string> = { Accept: 'text/event-stream' }
  const token = localStorage.getItem('auth_token')
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }

  const response = await fetch(url.toString(), {
    method: 'GET',
    headers,
    credentials: 'include',
    signal: options.signal,
  })
  if (!response.ok || !response.body) {
    const text = await response.text().catch(() => '')
    throw new Error(text || `Stream request failed with HTTP ${response.status}`)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      buffer = parseSSEBuffer(buffer, onEvent)
    }
    buffer += decoder.decode()
    parseSSEBuffer(`${buffer}\n\n`, onEvent)
  } finally {
    reader.releaseLock()
  }
}

function parseSSEBuffer(
  buffer: string,
  onEvent: (event: UpstreamAccountMonitorRunAllStreamEvent) => void
): string {
  const normalized = buffer.replace(/\r\n/g, '\n')
  const frames = normalized.split('\n\n')
  const rest = frames.pop() || ''
  for (const frame of frames) {
    const dataLines: string[] = []
    for (const line of frame.split('\n')) {
      if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trimStart())
      }
    }
    if (!dataLines.length) continue
    const data = dataLines.join('\n')
    if (!data.trim()) continue
    onEvent(JSON.parse(data) as UpstreamAccountMonitorRunAllStreamEvent)
  }
  return rest
}

export async function enableAll(params: UpstreamAccountMonitorListParams = {}): Promise<UpstreamAccountMonitorBatchResponse> {
  const { data } = await apiClient.post<UpstreamAccountMonitorBatchResponse>(`${BASE_PATH}/enable-all`, null, { params })
  return data
}

export async function disableAll(params: UpstreamAccountMonitorListParams = {}): Promise<UpstreamAccountMonitorBatchResponse> {
  const { data } = await apiClient.post<UpstreamAccountMonitorBatchResponse>(`${BASE_PATH}/disable-all`, null, { params })
  return data
}

export async function batchUpdateSettings(
  params: UpstreamAccountMonitorBatchSettingsRequest,
  filters: UpstreamAccountMonitorListParams = {}
): Promise<UpstreamAccountMonitorBatchResponse> {
  const { data } = await apiClient.post<UpstreamAccountMonitorBatchResponse>(`${BASE_PATH}/batch-settings`, params, {
    params: filters,
  })
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
  runAllStream,
  enableAll,
  disableAll,
  batchUpdateSettings,
  runOne,
  updateSettings,
}

export default upstreamAccountMonitorAPI
