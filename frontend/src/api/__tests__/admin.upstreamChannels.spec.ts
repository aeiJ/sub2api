import { beforeEach, describe, expect, it, vi } from 'vitest'

const getMock = vi.hoisted(() => vi.fn())
const postMock = vi.hoisted(() => vi.fn())
const putMock = vi.hoisted(() => vi.fn())
const deleteMock = vi.hoisted(() => vi.fn())

vi.mock('@/api/client', () => ({
  apiClient: {
    get: getMock,
    post: postMock,
    put: putMock,
    delete: deleteMock,
  },
}))

import upstreamChannelsAPI from '@/api/admin/upstreamChannels'

describe('admin upstreamChannels API', () => {
  beforeEach(() => {
    getMock.mockReset()
    postMock.mockReset()
    putMock.mockReset()
    deleteMock.mockReset()
  })

  it('lists upstream channels with filters', async () => {
    getMock.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20, pages: 0 } })
    const signal = new AbortController().signal

    await upstreamChannelsAPI.list(
      { page: 2, page_size: 50, provider: 'anthropic', status: 'active', search: 'prod' },
      { signal }
    )

    expect(getMock).toHaveBeenCalledWith('/admin/upstream-channels', {
      params: { page: 2, page_size: 50, provider: 'anthropic', status: 'active', search: 'prod' },
      signal,
    })
  })

  it('creates a nested upstream channel payload', async () => {
    const payload = {
      name: 'Main upstream',
      status: 'active',
      platforms: [
        {
          provider: 'openai',
          display_name: 'GPT',
          base_url: 'https://api.openai.com/v1',
          status: 'active',
          key_pools: [
            {
              name: 'GPT pool',
              group_name: 'GPT group',
              group_rate_multiplier: 1,
              account_rate_multiplier: 1,
              load_factor: 1,
              concurrency: 2,
              status: 'active',
              keys: [
                { name: 'key 1', api_key: 'sk-test', status: 'active' },
              ],
            },
          ],
        },
      ],
    }
    postMock.mockResolvedValue({ data: { id: 1, ...payload } })

    await upstreamChannelsAPI.create(payload)

    expect(postMock).toHaveBeenCalledWith('/admin/upstream-channels', payload)
  })

  it('calls sync preview, sync and test action endpoints', async () => {
    postMock.mockResolvedValue({ data: {} })

    await upstreamChannelsAPI.syncPreview(7)
    await upstreamChannelsAPI.sync(7)
    await upstreamChannelsAPI.test(7, { key_id: 9 })

    expect(postMock).toHaveBeenNthCalledWith(1, '/admin/upstream-channels/7/sync-preview')
    expect(postMock).toHaveBeenNthCalledWith(2, '/admin/upstream-channels/7/sync')
    expect(postMock).toHaveBeenNthCalledWith(3, '/admin/upstream-channels/7/test', { key_id: 9 })
  })

  it('updates and deletes upstream channels', async () => {
    putMock.mockResolvedValue({ data: { id: 3 } })
    deleteMock.mockResolvedValue({ data: undefined })

    await upstreamChannelsAPI.update(3, { status: 'disabled' })
    await upstreamChannelsAPI.remove(3)

    expect(putMock).toHaveBeenCalledWith('/admin/upstream-channels/3', { status: 'disabled' })
    expect(deleteMock).toHaveBeenCalledWith('/admin/upstream-channels/3')
  })
})
