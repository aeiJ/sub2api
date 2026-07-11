import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UpstreamAccountMonitorView from '../UpstreamAccountMonitorView.vue'

const {
  listMonitors,
  getGroups,
  batchUpdateSettings,
  runAllStream,
  runOne,
  updateSettings,
  getAvailableModels,
  showError,
  showSuccess,
  showWarning,
} = vi.hoisted(() => ({
  listMonitors: vi.fn(),
  getGroups: vi.fn(),
  batchUpdateSettings: vi.fn(),
  runAllStream: vi.fn(),
  runOne: vi.fn(),
  updateSettings: vi.fn(),
  getAvailableModels: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    upstreamAccountMonitor: {
      list: listMonitors,
      batchUpdateSettings,
      runAllStream,
      runOne,
      updateSettings,
    },
    groups: {
      getAllIncludingInactive: getGroups,
    },
    accounts: {
      getAvailableModels,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showWarning,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (!params) return key
        return `${key}:${JSON.stringify(params)}`
      },
    }),
  }
})

const SelectStub = {
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  template: `
    <select
      data-test="select"
      :value="modelValue"
      @change="$emit('update:modelValue', $event.target.value); $emit('change')"
    >
      <option
        v-for="option in options"
        :key="option.value"
        data-test="select-option"
        :value="option.value"
      >
        {{ option.label }}
      </option>
    </select>
  `,
}

function mountView() {
  return mount(UpstreamAccountMonitorView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Select: SelectStub,
        Pagination: true,
        EmptyState: { template: '<div data-test="empty-state"></div>' },
        Icon: true,
        Toggle: true,
        ProviderIcon: true,
        MonitorMetricPair: true,
        MonitorAvailabilityRow: true,
        MonitorTimeline: true,
      },
    },
  })
}

describe('UpstreamAccountMonitorView', () => {
  beforeEach(() => {
    listMonitors.mockReset()
    getGroups.mockReset()
    batchUpdateSettings.mockReset()
    runAllStream.mockReset()
    runOne.mockReset()
    updateSettings.mockReset()
    getAvailableModels.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()

    listMonitors.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0,
      monitor_enabled_total: 0,
      monitor_disabled_total: 0,
    })
    getGroups.mockResolvedValue([])
  })

  it('uses health status filters and keeps batch actions behind one top-level button', async () => {
    const wrapper = mountView()
    await flushPromises()

    const optionValues = wrapper.findAll('[data-test="select-option"]').map((node) => node.attributes('value'))
    expect(optionValues).toContain('operational')
    expect(optionValues).toContain('degraded')
    expect(optionValues).toContain('failed')
    expect(optionValues).not.toContain('disabled')

    expect(wrapper.text()).toContain('admin.upstreamAccountMonitor.batch.open')
    expect(wrapper.text()).not.toContain('admin.upstreamAccountMonitor.batch.runAll')
    expect(wrapper.text()).not.toContain('admin.upstreamAccountMonitor.batch.enableAll')
    expect(wrapper.text()).not.toContain('admin.upstreamAccountMonitor.batch.disableAll')
  })

  it('does not restore removed result and monitor-state chips on account cards', async () => {
    listMonitors.mockResolvedValue({
      items: [{
        account_id: 78,
        account_name: '白带射手',
        platform: 'openai',
        account_status: 'active',
        error_message: '',
        monitor_required: false,
        groups: [{ id: 1, name: '自动分组' }],
        monitor_enabled: false,
        plan_id: 12,
        model_id: 'gpt-5.4',
        cron_expression: '',
        interval_minutes: 60,
        jitter_seconds: 0,
        auto_recover: false,
        last_run_at: '2026-07-11T09:00:00Z',
        next_run_at: '2026-07-11T10:00:00Z',
        latest_result: {
          id: 33,
          plan_id: 12,
          status: 'success',
          response_text: '',
          error_message: '',
          latency_ms: 2036,
          ping_latency_ms: null,
          started_at: '2026-07-11T09:00:00Z',
          finished_at: '2026-07-11T09:00:02Z',
          created_at: '2026-07-11T09:00:02Z',
        },
        availability_7d: 94.12,
        availability_15d: 96.33,
        timeline: [],
      }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
      monitor_enabled_total: 0,
      monitor_disabled_total: 1,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('白带射手')
    expect(wrapper.text()).toContain('gpt-5.4')
    expect(wrapper.find('.monitor-chip').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.upstreamAccountMonitor.success')
    expect(wrapper.text()).not.toContain('admin.upstreamAccountMonitor.disabled')
  })
})
