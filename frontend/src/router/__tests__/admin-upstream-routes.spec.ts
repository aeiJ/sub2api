import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const routerPath = resolve(dirname(fileURLToPath(import.meta.url)), '../index.ts')
const routerSource = readFileSync(routerPath, 'utf8')

describe('admin upstream routes', () => {
  it('keeps custom upstream monitor separate from native channel monitor', () => {
    expect(routerSource).toContain("path: '/admin/upstreams/monitor'")
    expect(routerSource).toContain("name: 'AdminUpstreamAccountMonitor'")
    expect(routerSource).toContain("component: () => import('@/views/admin/UpstreamAccountMonitorView.vue')")
    expect(routerSource).toContain("titleKey: 'admin.upstreamAccountMonitor.title'")

    expect(routerSource).toContain("path: '/admin/channels/monitor'")
    expect(routerSource).toContain("name: 'AdminChannelMonitor'")
    expect(routerSource).toContain("component: () => import('@/views/admin/ChannelMonitorView.vue')")
    expect(routerSource).toContain("titleKey: 'admin.channelMonitor.title'")
  })
})
