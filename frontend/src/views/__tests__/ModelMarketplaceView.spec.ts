import { describe, expect, it, beforeEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelMarketplaceView from '../ModelMarketplaceView.vue'
import { getModelMarketplace } from '@/api/modelMarketplace'

vi.mock('@/api/modelMarketplace', () => ({
  getModelMarketplace: vi.fn(),
}))

vi.mock('@/components/common/LocaleSwitcher.vue', () => ({
  default: { name: 'LocaleSwitcher', template: '<div />' },
}))

const messages: Record<string, string> = {
  'home.viewDocs': 'Docs',
  'home.switchToLight': 'Light',
  'home.switchToDark': 'Dark',
  'home.dashboard': 'Dashboard',
  'home.login': 'Login',
  'modelMarketplace.publicCatalog': 'Public model catalog',
  'modelMarketplace.title': 'Model Marketplace',
  'modelMarketplace.subtitle': 'Browse models',
  'modelMarketplace.summary.models': 'Models',
  'modelMarketplace.summary.providers': 'Providers',
  'modelMarketplace.summary.minRate': 'Min rate',
  'modelMarketplace.searchPlaceholder': 'Search',
  'modelMarketplace.filters.title': 'Filters',
  'modelMarketplace.filters.reset': 'Reset',
  'modelMarketplace.filters.active': '{count} selected',
  'modelMarketplace.filters.providers': 'Providers',
  'modelMarketplace.filters.groups': 'Groups',
  'modelMarketplace.filters.billingModes': 'Billing',
  'modelMarketplace.filters.tags': 'Tags',
  'modelMarketplace.filters.platforms': 'Platforms',
  'modelMarketplace.filters.none': 'No options',
  'modelMarketplace.resultCount': '{count} models',
  'modelMarketplace.gridView': 'Cards',
  'modelMarketplace.copyModel': 'Copy model name',
  'modelMarketplace.defaultDescription': 'Available model',
  'modelMarketplace.emptyTitle': 'No models',
  'modelMarketplace.emptyDescription': 'No models',
  'modelMarketplace.errorTitle': 'Error',
  'modelMarketplace.retry': 'Retry',
  'modelMarketplace.rate': 'Lowest rate {rate}',
  'modelMarketplace.pricing.input': 'Input',
  'modelMarketplace.pricing.output': 'Output',
  'modelMarketplace.pricing.cacheWrite': 'Cache write',
  'modelMarketplace.pricing.cacheRead': 'Cached input',
  'modelMarketplace.pricing.imageOutput': 'Image output',
  'modelMarketplace.pricing.perRequest': 'Per request',
  'modelMarketplace.pricing.formula': 'Calculated with {rate}',
  'modelMarketplace.pricing.units.perMillionTokens': 'Discounted price / 1M tokens',
  'modelMarketplace.pricing.units.perMillionImageTokens': 'Discounted price / 1M image tokens',
  'modelMarketplace.pricing.units.perRequest': 'Discounted price / request',
  'modelMarketplace.billingModes.token': 'Token',
  'modelMarketplace.tags.reasoning': 'Reasoning',
  'modelMarketplace.tags.tools': 'Tools',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, any>) => {
        let value = messages[key] ?? key
        if (params) {
          for (const [name, paramValue] of Object.entries(params)) {
            value = value.replace(`{${name}}`, String(paramValue))
          }
        }
        return value
      },
    }),
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    siteName: 'Sub2API',
    siteLogo: '',
    docUrl: '',
  }),
  useAuthStore: () => ({
    isAuthenticated: false,
    isAdmin: false,
  }),
}))

const marketplaceResponse = {
  models: [
    {
      id: 'claude-sonnet-4',
      name: 'claude-sonnet-4',
      platform: 'anthropic',
      provider: 'Anthropic',
      group_names: ['Public'],
      rate_multiplier: 0.8,
      billing_mode: 'token',
      pricing: {
        input_price: 2.4,
        output_price: 12,
        cache_write_price: null,
        cache_read_price: null,
        image_output_price: null,
        per_request_price: null,
      },
      tags: ['reasoning'],
      context_window: '200k',
      description: 'Claude model',
    },
    {
      id: 'gpt-5.4',
      name: 'gpt-5.4',
      platform: 'openai',
      provider: 'OpenAI',
      group_names: ['Public'],
      rate_multiplier: 1,
      billing_mode: 'token',
      pricing: {
        input_price: 1.25,
        output_price: 10,
        cache_write_price: null,
        cache_read_price: null,
        image_output_price: null,
        per_request_price: null,
      },
      tags: ['tools'],
      context_window: '400k',
      description: 'GPT model',
    },
  ],
  filters: {
    providers: ['Anthropic', 'OpenAI'],
    groups: ['Public'],
    billing_modes: ['token'],
    tags: ['reasoning', 'tools'],
    platforms: ['anthropic', 'openai'],
  },
  summary: {
    total_models: 2,
    provider_counts: { Anthropic: 1, OpenAI: 1 },
    min_rate_multiplier: 0.8,
  },
}

describe('ModelMarketplaceView', () => {
  beforeEach(() => {
    vi.mocked(getModelMarketplace).mockResolvedValue(marketplaceResponse)
    Object.assign(navigator, {
      clipboard: {
        writeText: vi.fn().mockResolvedValue(undefined),
      },
    })
  })

  it('renders marketplace cards and filters by search', async () => {
    const wrapper = mount(ModelMarketplaceView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
        },
      },
    })
    await flushPromises()

    expect(wrapper.findAll('[data-testid="model-card"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('claude-sonnet-4')
    expect(wrapper.text()).toContain('gpt-5.4')
    expect(wrapper.text()).toContain('Discounted price / 1M tokens')
    expect(wrapper.text()).toContain('Calculated with 0.8x')

    await wrapper.find('[data-testid="model-search"]').setValue('gpt')

    expect(wrapper.findAll('[data-testid="model-card"]')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('claude-sonnet-4')
    expect(wrapper.text()).toContain('gpt-5.4')
  })

  it('filters cards by provider', async () => {
    const wrapper = mount(ModelMarketplaceView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
        },
      },
    })
    await flushPromises()

    const openAIButton = wrapper.findAll('button').find((button) => button.text() === 'OpenAI')
    expect(openAIButton).toBeTruthy()
    await openAIButton!.trigger('click')

    expect(wrapper.findAll('[data-testid="model-card"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('gpt-5.4')
    expect(wrapper.text()).not.toContain('claude-sonnet-4')
  })
})
