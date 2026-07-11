import { beforeEach, describe, expect, it, vi } from 'vitest'

const getMock = vi.hoisted(() => vi.fn())
const postMock = vi.hoisted(() => vi.fn())

vi.mock('@/api/client', () => ({
  apiClient: {
    get: getMock,
    post: postMock,
  },
  buildApiUrl: (path: string) => `/api/v1${path}`,
}))

import upstreamAccountMonitorAPI from '@/api/admin/upstreamAccountMonitor'

function streamResponse(...frames: string[]) {
  const encoder = new TextEncoder()
  let index = 0
  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn(async () => {
          if (index >= frames.length) return { done: true, value: undefined }
          const value = encoder.encode(frames[index])
          index += 1
          return { done: false, value }
        }),
        releaseLock: vi.fn(),
      }),
    },
  }
}

describe('admin upstreamAccountMonitor API', () => {
  beforeEach(() => {
    getMock.mockReset()
    postMock.mockReset()
    vi.restoreAllMocks()
    localStorage.clear()
  })

  it('lists upstream account monitors with health status and availability sort params', async () => {
    const signal = new AbortController().signal
    getMock.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20, pages: 0 } })

    await upstreamAccountMonitorAPI.list({
      page: 2,
      page_size: 50,
      platform: 'openai',
      monitor_status: 'degraded',
      search: 'fast',
      group_id: 12,
      sort_by: 'availability',
      sort_order: 'asc',
      availability_window: '15d',
    }, { signal })

    expect(getMock).toHaveBeenCalledWith('/admin/upstream-account-monitors', {
      params: {
        page: 2,
        page_size: 50,
        platform: 'openai',
        monitor_status: 'degraded',
        search: 'fast',
        group_id: 12,
        sort_by: 'availability',
        sort_order: 'asc',
        availability_window: '15d',
      },
      signal,
    })
  })

  it('sends batch settings in the body and filters in query params', async () => {
    postMock.mockResolvedValue({ data: { total: 3, updated: 2, skipped: 1 } })

    await upstreamAccountMonitorAPI.batchUpdateSettings(
      { monitor_enabled: false, interval_minutes: 15, jitter_seconds: 30 },
      { platform: 'openai', monitor_status: 'failed', search: 'prod' }
    )

    expect(postMock).toHaveBeenCalledWith(
      '/admin/upstream-account-monitors/batch-settings',
      { monitor_enabled: false, interval_minutes: 15, jitter_seconds: 30 },
      { params: { platform: 'openai', monitor_status: 'failed', search: 'prod' } }
    )
  })

  it('parses streaming run-all SSE events and passes auth header', async () => {
    const fetchMock = vi.fn().mockResolvedValue(streamResponse(
      'event: started\ndata: {"type":"started","total":2}\n\n',
      'event: item\ndata: {"type":"item","account_id":7,"result":{"status":"success"}}\n\n'
    ))
    vi.stubGlobal('fetch', fetchMock)
    localStorage.setItem('auth_token', 'token-123')
    const events: unknown[] = []

    await upstreamAccountMonitorAPI.runAllStream(
      { platform: 'openai', monitor_status: 'operational' },
      (event) => events.push(event)
    )

    expect(events).toEqual([
      { type: 'started', total: 2 },
      { type: 'item', account_id: 7, result: { status: 'success' } },
    ])
    const [url, init] = fetchMock.mock.calls[0]
    const parsed = new URL(url)
    expect(parsed.pathname).toBe('/api/v1/admin/upstream-account-monitors/run-all/stream')
    expect(parsed.searchParams.get('platform')).toBe('openai')
    expect(parsed.searchParams.get('monitor_status')).toBe('operational')
    expect(init).toMatchObject({
      method: 'GET',
      credentials: 'include',
      headers: {
        Accept: 'text/event-stream',
        Authorization: 'Bearer token-123',
      },
    })
  })

  it('parses streaming run-all events split across chunks with CRLF frames', async () => {
    const fetchMock = vi.fn().mockResolvedValue(streamResponse(
      'event: started\r\ndata: {"type":"started","total":1}\r\n\r\nevent: item\r\ndata: {"type":"item","account_id":',
      '9,"result":{"status":"failed"}}\r\n\r\n',
      'event: done\r\ndata: {"type":"done","total":1,"success":0,"failed":1}\r\n\r\n'
    ))
    vi.stubGlobal('fetch', fetchMock)
    const events: unknown[] = []

    await upstreamAccountMonitorAPI.runAllStream({}, (event) => events.push(event))

    expect(events).toEqual([
      { type: 'started', total: 1 },
      { type: 'item', account_id: 9, result: { status: 'failed' } },
      { type: 'done', total: 1, success: 0, failed: 1 },
    ])
  })
})
