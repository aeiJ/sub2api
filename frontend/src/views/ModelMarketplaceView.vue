<template>
  <div class="min-h-screen overflow-x-hidden bg-slate-50 text-slate-900 dark:bg-[#05070d] dark:text-slate-100">
    <header class="sticky top-0 z-30 border-b border-slate-200/80 bg-white/90 backdrop-blur-xl dark:border-white/10 dark:bg-[#080c16]/90">
      <nav class="mx-auto flex max-w-7xl items-center justify-between gap-3 px-4 py-3 sm:px-6">
        <router-link to="/" class="flex min-w-0 items-center gap-3">
          <span class="h-9 w-9 flex-shrink-0 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm dark:border-white/10 dark:bg-white/5">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
          </span>
          <span class="min-w-0 truncate text-sm font-semibold text-slate-900 sm:text-base dark:text-slate-100">{{ siteName }}</span>
        </router-link>

        <div class="flex min-w-0 items-center gap-1.5 sm:gap-2">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="rounded-lg p-2 text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-white/[0.08] dark:hover:text-slate-100"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="sm" />
          </a>
          <button
            type="button"
            class="cursor-pointer rounded-lg p-2 text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-cyan-500 dark:text-slate-400 dark:hover:bg-white/[0.08] dark:hover:text-slate-100 dark:focus-visible:ring-cyan-400"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="sm" />
            <Icon v-else name="moon" size="sm" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex items-center whitespace-nowrap rounded-full border border-cyan-200 bg-cyan-50 px-3 py-1.5 text-xs font-medium text-cyan-700 transition-colors hover:bg-cyan-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-cyan-500 dark:border-cyan-300/20 dark:bg-cyan-400/10 dark:text-cyan-100 dark:hover:bg-cyan-400/20 dark:focus-visible:ring-cyan-400"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="mx-auto max-w-7xl px-4 py-5 sm:px-6 lg:py-8">
      <section class="mb-5 overflow-hidden rounded-lg border border-slate-200 bg-gradient-to-br from-sky-50 via-white to-emerald-50 px-5 py-6 shadow-lg shadow-slate-200/70 sm:px-7 sm:py-8 dark:border-white/10 dark:from-[#0b1220] dark:via-[#0f172a] dark:to-[#052e2b] dark:shadow-2xl dark:shadow-cyan-950/20">
        <div class="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
          <div class="min-w-0">
            <p class="mb-2 text-xs font-semibold uppercase tracking-wide text-cyan-700 dark:text-cyan-200">
              {{ t('modelMarketplace.publicCatalog') }}
            </p>
            <h1 class="text-2xl font-bold text-slate-950 sm:text-3xl dark:text-white">{{ t('modelMarketplace.title') }}</h1>
            <p class="mt-2 max-w-2xl text-sm leading-6 text-slate-600 sm:text-base dark:text-slate-300">
              {{ t('modelMarketplace.subtitle') }}
            </p>
          </div>
          <div class="grid grid-cols-3 gap-2 sm:min-w-[360px]">
            <div class="rounded-lg border border-slate-200 bg-white/80 p-3 shadow-sm dark:border-white/10 dark:bg-white/[0.06] dark:shadow-none">
              <p class="text-[11px] text-slate-500 dark:text-slate-400">{{ t('modelMarketplace.summary.models') }}</p>
              <p class="mt-1 text-xl font-semibold text-slate-950 dark:text-white">{{ summary.total_models }}</p>
            </div>
            <div class="rounded-lg border border-slate-200 bg-white/80 p-3 shadow-sm dark:border-white/10 dark:bg-white/[0.06] dark:shadow-none">
              <p class="text-[11px] text-slate-500 dark:text-slate-400">{{ t('modelMarketplace.summary.providers') }}</p>
              <p class="mt-1 text-xl font-semibold text-slate-950 dark:text-white">{{ providerCount }}</p>
            </div>
            <div class="rounded-lg border border-emerald-200 bg-emerald-50/90 p-3 shadow-sm dark:border-emerald-300/20 dark:bg-emerald-400/10 dark:shadow-none">
              <p class="text-[11px] text-emerald-700 dark:text-emerald-200/80">{{ t('modelMarketplace.summary.minRate') }}</p>
              <p class="mt-1 text-xl font-semibold text-emerald-800 dark:text-emerald-100">{{ formatMultiplier(summary.min_rate_multiplier) }}</p>
            </div>
          </div>
        </div>
      </section>

      <section class="mb-5 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div class="relative min-w-0 flex-1">
          <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
          <input
            v-model="searchQuery"
            data-testid="model-search"
            type="search"
            class="h-11 w-full rounded-lg border border-slate-200 bg-white py-2.5 pl-9 pr-3 text-sm text-slate-900 shadow-sm outline-none transition placeholder:text-slate-400 focus:border-cyan-500 focus:ring-2 focus:ring-cyan-500/15 dark:border-white/10 dark:bg-[#0b1020] dark:text-slate-100 dark:shadow-none dark:placeholder:text-slate-500 dark:focus:border-cyan-400 dark:focus:ring-cyan-400/15"
            :placeholder="t('modelMarketplace.searchPlaceholder')"
          />
        </div>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="inline-flex h-11 flex-1 cursor-pointer items-center justify-center rounded-lg border border-slate-200 bg-white px-3 text-sm font-medium text-slate-700 shadow-sm transition hover:bg-slate-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-cyan-500 lg:hidden dark:border-white/10 dark:bg-[#0b1020] dark:text-slate-200 dark:shadow-none dark:hover:bg-white/[0.07] dark:focus-visible:ring-cyan-400"
            @click="mobileFiltersOpen = true"
          >
            <Icon name="filter" size="sm" class="mr-1.5" />
            {{ t('modelMarketplace.filters.title') }}
          </button>
          <button
            type="button"
            class="inline-flex h-11 cursor-pointer items-center justify-center rounded-lg border border-slate-200 bg-white px-3 text-sm font-medium text-slate-600 shadow-sm transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-45 focus:outline-none focus-visible:ring-2 focus-visible:ring-cyan-500 dark:border-white/10 dark:bg-[#0b1020] dark:text-slate-300 dark:shadow-none dark:hover:bg-white/[0.07] dark:focus-visible:ring-cyan-400"
            :disabled="!hasActiveFilters"
            @click="resetFilters"
          >
            {{ t('modelMarketplace.filters.reset') }}
          </button>
        </div>
      </section>

      <div class="grid gap-5 lg:grid-cols-[280px_minmax(0,1fr)]">
        <aside class="hidden lg:block">
          <div class="sticky top-20 rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-white/10 dark:bg-[#0b1020]/90 dark:shadow-xl dark:shadow-black/20">
            <FilterPanel />
          </div>
        </aside>

        <section class="min-w-0">
          <div class="mb-4 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <p class="text-sm text-slate-500 dark:text-slate-400">
              {{ t('modelMarketplace.resultCount', { count: visibleModels.length }) }}
            </p>
            <div class="inline-flex w-fit items-center rounded-lg border border-slate-200 bg-white p-1 shadow-sm dark:border-white/10 dark:bg-[#0b1020] dark:shadow-none">
              <button type="button" class="cursor-pointer rounded-md bg-cyan-50 px-2.5 py-1.5 text-xs font-medium text-cyan-700 dark:bg-cyan-400/15 dark:text-cyan-100">
                <Icon name="grid" size="xs" class="mr-1 inline-block" />
                {{ t('modelMarketplace.gridView') }}
              </button>
            </div>
          </div>

          <div v-if="loading" class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            <div v-for="n in 6" :key="n" class="h-64 animate-pulse rounded-lg border border-slate-200 bg-white dark:border-white/10 dark:bg-[#0b1020]" />
          </div>

          <div
            v-else-if="errorMessage"
            class="rounded-lg border border-red-200 bg-red-50 p-6 text-sm text-red-700 dark:border-red-400/30 dark:bg-red-950/30 dark:text-red-200"
          >
            <p class="font-medium">{{ t('modelMarketplace.errorTitle') }}</p>
            <p class="mt-1">{{ errorMessage }}</p>
            <button type="button" class="mt-4 cursor-pointer rounded-lg bg-red-500 px-3 py-2 text-sm font-medium text-white hover:bg-red-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-300" @click="loadMarketplace">
              {{ t('modelMarketplace.retry') }}
            </button>
          </div>

          <div
            v-else-if="visibleModels.length === 0"
            class="rounded-lg border border-slate-200 bg-white p-8 text-center shadow-sm dark:border-white/10 dark:bg-[#0b1020] dark:shadow-none"
          >
            <Icon name="inbox" size="xl" class="mx-auto text-slate-500" />
            <h2 class="mt-3 text-base font-semibold text-slate-900 dark:text-slate-100">{{ t('modelMarketplace.emptyTitle') }}</h2>
            <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{{ t('modelMarketplace.emptyDescription') }}</p>
          </div>

          <div v-else class="grid min-w-0 gap-4 md:grid-cols-2 xl:grid-cols-3">
            <article
              v-for="model in visibleModels"
              :key="`${model.platform}:${model.id}`"
              data-testid="model-card"
              class="group flex min-w-0 flex-col rounded-lg border border-slate-200 bg-white p-4 shadow-sm transition duration-200 hover:-translate-y-0.5 hover:border-cyan-300 hover:shadow-md dark:border-white/10 dark:bg-[#0b1020] dark:shadow-xl dark:shadow-black/15 dark:hover:border-cyan-300/[0.35] dark:hover:bg-[#0d1426]"
            >
              <div class="mb-4 flex min-w-0 items-start justify-between gap-3">
                <div class="flex min-w-0 items-start gap-3">
                  <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg border border-cyan-200 bg-cyan-50 text-sm font-semibold text-cyan-700 dark:border-cyan-300/20 dark:bg-cyan-400/10 dark:text-cyan-100">
                    {{ providerInitial(model.provider) }}
                  </div>
                  <div class="min-w-0">
                    <h2 class="truncate text-sm font-semibold text-slate-900 sm:text-base dark:text-slate-100" :title="model.name">{{ model.name }}</h2>
                    <p class="mt-1 truncate text-xs text-slate-500 dark:text-slate-400">
                      {{ model.provider }} · {{ model.platform }}
                    </p>
                  </div>
                </div>
                <button
                  type="button"
                  class="flex h-9 w-9 flex-shrink-0 cursor-pointer items-center justify-center rounded-lg border border-slate-200 text-slate-500 transition hover:bg-slate-50 hover:text-cyan-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-cyan-500 dark:border-white/10 dark:text-slate-400 dark:hover:bg-white/[0.07] dark:hover:text-cyan-100 dark:focus-visible:ring-cyan-400"
                  :title="t('modelMarketplace.copyModel')"
                  :aria-label="t('modelMarketplace.copyModel')"
                  @click="copyModelName(model)"
                >
                  <Icon :name="copiedKey === modelKey(model) ? 'check' : 'copy'" size="sm" />
                </button>
              </div>

              <p class="mb-4 line-clamp-2 min-h-[2.5rem] text-sm leading-5 text-slate-600 dark:text-slate-400">
                {{ model.description || t('modelMarketplace.defaultDescription') }}
              </p>

              <div class="mb-3 grid gap-2 text-xs">
                <div
                  v-for="row in pricingRows(model)"
                  :key="row.labelKey"
                  class="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 dark:border-white/[0.06] dark:bg-white/[0.045]"
                >
                  <div class="flex items-start justify-between gap-3">
                    <span class="min-w-0 text-slate-500 dark:text-slate-400">{{ t(row.labelKey) }}</span>
                    <span class="flex-shrink-0 font-semibold text-slate-900 dark:text-slate-100">{{ formatPrice(row.value) }}</span>
                  </div>
                  <p class="mt-0.5 text-[10px] leading-4 text-slate-400 dark:text-slate-500">
                    {{ t(row.unitKey) }}
                  </p>
                </div>
              </div>
              <p class="mb-4 rounded-md border border-cyan-200 bg-cyan-50 px-3 py-2 text-[11px] leading-4 text-cyan-700 dark:border-cyan-300/15 dark:bg-cyan-400/5 dark:text-cyan-100/80">
                {{ t('modelMarketplace.pricing.formula', { rate: formatMultiplier(model.rate_multiplier) }) }}
              </p>

              <div class="mb-3 flex flex-wrap gap-1.5">
                <span class="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-1 text-[11px] font-medium text-emerald-700 dark:border-emerald-300/20 dark:bg-emerald-400/10 dark:text-emerald-200">
                  {{ t('modelMarketplace.rate', { rate: formatMultiplier(model.rate_multiplier) }) }}
                </span>
                <span
                  v-if="model.context_window"
                  class="rounded-full border border-indigo-200 bg-indigo-50 px-2 py-1 text-[11px] font-medium text-indigo-700 dark:border-indigo-300/20 dark:bg-indigo-400/10 dark:text-indigo-200"
                >
                  {{ model.context_window }}
                </span>
                <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] font-medium text-slate-600 dark:border-white/10 dark:bg-white/[0.06] dark:text-slate-300">
                  {{ t(`modelMarketplace.billingModes.${model.billing_mode}`) }}
                </span>
              </div>

              <div class="mt-auto space-y-3">
                <div class="flex flex-wrap gap-1.5">
                  <span
                    v-for="tag in model.tags"
                    :key="tag"
                    class="rounded-md border border-cyan-200 bg-cyan-50 px-2 py-1 text-[11px] text-cyan-700 dark:border-cyan-300/15 dark:bg-cyan-400/5 dark:text-cyan-100/90"
                  >
                    {{ t(`modelMarketplace.tags.${tag}`) }}
                  </span>
                </div>
                <div class="flex flex-wrap gap-1.5 border-t border-slate-200 pt-3 dark:border-white/[0.08]">
                  <span
                    v-for="group in model.group_names"
                    :key="group"
                    class="max-w-full truncate rounded-md bg-slate-100 px-2 py-1 text-[11px] text-slate-600 dark:bg-white/[0.06] dark:text-slate-300"
                    :title="group"
                  >
                    {{ group }}
                  </span>
                </div>
              </div>
            </article>
          </div>
        </section>
      </div>
    </main>

    <div v-if="mobileFiltersOpen" class="fixed inset-0 z-40 lg:hidden">
      <button class="absolute inset-0 bg-black/45" type="button" :aria-label="t('modelMarketplace.filters.close')" @click="mobileFiltersOpen = false" />
      <div class="absolute inset-x-0 bottom-0 max-h-[86vh] overflow-y-auto rounded-t-lg border-t border-slate-200 bg-white p-4 shadow-2xl shadow-slate-950/20 dark:border-white/10 dark:bg-[#0b1020] dark:shadow-black">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-base font-semibold text-slate-900 dark:text-slate-100">{{ t('modelMarketplace.filters.title') }}</h2>
          <button
            type="button"
            class="cursor-pointer rounded-lg p-2 text-slate-500 hover:bg-slate-100 hover:text-slate-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-cyan-500 dark:text-slate-400 dark:hover:bg-white/[0.07] dark:hover:text-slate-100 dark:focus-visible:ring-cyan-400"
            :aria-label="t('modelMarketplace.filters.close')"
            @click="mobileFiltersOpen = false"
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
        <FilterPanel />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { getModelMarketplace, type MarketplaceFilters, type MarketplaceModel, type MarketplaceResponse } from '@/api/modelMarketplace'

type FilterKey = keyof MarketplaceFilters

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const response = ref<MarketplaceResponse>({
  models: [],
  filters: { providers: [], groups: [], billing_modes: [], tags: [], platforms: [] },
  summary: { total_models: 0, provider_counts: {}, min_rate_multiplier: null },
})
const loading = ref(false)
const errorMessage = ref('')
const searchQuery = ref('')
const mobileFiltersOpen = ref(false)
const copiedKey = ref('')
const selectedFilters = ref<Record<FilterKey, string[]>>({
  providers: [],
  groups: [],
  billing_modes: [],
  tags: [],
  platforms: [],
})
const isDark = ref(document.documentElement.classList.contains('dark'))
let abortController: AbortController | null = null
let copiedTimer: number | undefined

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || '网络屠夫')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const docUrl = computed(() => appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')
const isAuthenticated = computed(() => authStore.isAuthenticated)
const dashboardPath = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))
const summary = computed(() => response.value.summary)
const providerCount = computed(() => Object.keys(summary.value.provider_counts || {}).length)

const filterSections = computed<Array<{ key: FilterKey; label: string; options: string[] }>>(() => [
  { key: 'providers', label: t('modelMarketplace.filters.providers'), options: response.value.filters.providers },
  { key: 'groups', label: t('modelMarketplace.filters.groups'), options: response.value.filters.groups },
  { key: 'billing_modes', label: t('modelMarketplace.filters.billingModes'), options: response.value.filters.billing_modes },
  { key: 'tags', label: t('modelMarketplace.filters.tags'), options: response.value.filters.tags },
  { key: 'platforms', label: t('modelMarketplace.filters.platforms'), options: response.value.filters.platforms },
])

const hasActiveFilters = computed(() => {
  return Boolean(searchQuery.value.trim()) || Object.values(selectedFilters.value).some((items) => items.length > 0)
})

const visibleModels = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return response.value.models.filter((model) => {
    if (query) {
      const haystack = [model.name, model.provider, model.platform, ...model.group_names].join(' ').toLowerCase()
      if (!haystack.includes(query)) return false
    }
    if (!matchesFilter('providers', model.provider)) return false
    if (!matchesFilter('platforms', model.platform)) return false
    if (!matchesFilter('billing_modes', model.billing_mode)) return false
    if (!matchesAny('groups', model.group_names)) return false
    if (!matchesAny('tags', model.tags)) return false
    return true
  })
})

const FilterPanel = defineComponent({
  name: 'ModelMarketplaceFilterPanel',
  setup() {
    return () => h('div', { class: 'space-y-5' }, [
      h('div', { class: 'flex items-center justify-between' }, [
        h('h2', { class: 'text-sm font-semibold text-slate-900 dark:text-slate-100' }, t('modelMarketplace.filters.title')),
        h('span', { class: 'text-xs text-slate-500 dark:text-slate-500' }, t('modelMarketplace.filters.active', { count: activeFilterCount.value })),
      ]),
      ...filterSections.value.map((section) =>
        h('section', { key: section.key }, [
          h('h3', { class: 'mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-500' }, section.label),
          section.options.length === 0
            ? h('p', { class: 'text-xs text-slate-400 dark:text-slate-600' }, t('modelMarketplace.filters.none'))
            : h('div', { class: 'flex flex-wrap gap-2' }, section.options.map((option) =>
                h('button', {
                  key: option,
                  type: 'button',
                  class: filterChipClass(section.key, option),
                  onClick: () => toggleFilter(section.key, option),
                }, filterOptionLabel(section.key, option))
              )),
        ])
      ),
    ])
  },
})

const activeFilterCount = computed(() => Object.values(selectedFilters.value).reduce((sum, items) => sum + items.length, 0))

function matchesFilter(key: FilterKey, value: string): boolean {
  const selected = selectedFilters.value[key]
  return selected.length === 0 || selected.includes(value)
}

function matchesAny(key: FilterKey, values: string[]): boolean {
  const selected = selectedFilters.value[key]
  return selected.length === 0 || values.some((value) => selected.includes(value))
}

function toggleFilter(key: FilterKey, value: string) {
  const current = selectedFilters.value[key]
  selectedFilters.value[key] = current.includes(value)
    ? current.filter((item) => item !== value)
    : [...current, value]
}

function resetFilters() {
  searchQuery.value = ''
  selectedFilters.value = {
    providers: [],
    groups: [],
    billing_modes: [],
    tags: [],
    platforms: [],
  }
}

function filterChipClass(key: FilterKey, option: string): string {
  const active = selectedFilters.value[key].includes(option)
  return [
    'max-w-full cursor-pointer truncate rounded-full border px-2.5 py-1 text-xs font-medium transition focus:outline-none focus-visible:ring-2 focus-visible:ring-cyan-500 dark:focus-visible:ring-cyan-400',
    active
      ? 'border-cyan-300 bg-cyan-50 text-cyan-700 shadow-sm shadow-cyan-100/60 dark:border-cyan-300/40 dark:bg-cyan-400/10 dark:text-cyan-100 dark:shadow-cyan-950/30'
      : 'border-slate-200 bg-white text-slate-600 shadow-sm hover:border-slate-300 hover:bg-slate-50 hover:text-slate-900 dark:border-white/10 dark:bg-white/[0.04] dark:text-slate-300 dark:shadow-none dark:hover:border-white/20 dark:hover:bg-white/[0.07] dark:hover:text-slate-100',
  ].join(' ')
}

function filterOptionLabel(key: FilterKey, option: string): string {
  if (key === 'billing_modes') return t(`modelMarketplace.billingModes.${option}`)
  if (key === 'tags') return t(`modelMarketplace.tags.${option}`)
  return option
}

async function loadMarketplace() {
  abortController?.abort()
  abortController = new AbortController()
  loading.value = true
  errorMessage.value = ''
  try {
    response.value = await getModelMarketplace({ signal: abortController.signal })
  } catch (error: any) {
    if (error?.code === 'ERR_CANCELED') return
    errorMessage.value = error?.message || t('modelMarketplace.errorMessage')
  } finally {
    loading.value = false
  }
}

type PricingRow = {
  labelKey: string
  unitKey: string
  value: number | null
}

function pricingRows(model: MarketplaceModel): PricingRow[] {
  const tokenUnit = 'modelMarketplace.pricing.units.perMillionTokens'
  const imageTokenUnit = 'modelMarketplace.pricing.units.perMillionImageTokens'
  const perRequestUnit = 'modelMarketplace.pricing.units.perRequest'
  const rows = [
    { labelKey: 'modelMarketplace.pricing.input', unitKey: tokenUnit, value: model.pricing.input_price },
    { labelKey: 'modelMarketplace.pricing.output', unitKey: tokenUnit, value: model.pricing.output_price },
    { labelKey: 'modelMarketplace.pricing.cacheWrite', unitKey: tokenUnit, value: model.pricing.cache_write_price },
    { labelKey: 'modelMarketplace.pricing.cacheRead', unitKey: tokenUnit, value: model.pricing.cache_read_price },
    { labelKey: 'modelMarketplace.pricing.imageOutput', unitKey: imageTokenUnit, value: model.pricing.image_output_price },
    { labelKey: 'modelMarketplace.pricing.perRequest', unitKey: perRequestUnit, value: model.pricing.per_request_price },
  ].filter((row) => row.value !== null && row.value !== undefined)
  if (model.billing_mode === 'image') {
    return rows.sort((a, b) => imagePricingPriority(a.labelKey) - imagePricingPriority(b.labelKey)).slice(0, 4)
  }
  return rows.slice(0, 4)
}

function imagePricingPriority(labelKey: string): number {
  switch (labelKey) {
    case 'modelMarketplace.pricing.imageOutput':
      return 0
    case 'modelMarketplace.pricing.input':
      return 1
    case 'modelMarketplace.pricing.output':
      return 2
    case 'modelMarketplace.pricing.cacheRead':
      return 3
    case 'modelMarketplace.pricing.cacheWrite':
      return 4
    default:
      return 5
  }
}

function formatPrice(value: number | null): string {
  if (value === null || value === undefined) return '-'
  if (value === 0) return '$0'
  if (value < 0.01) return `$${value.toFixed(4)}`
  return `$${value.toFixed(2)}`
}

function formatMultiplier(value: number | null): string {
  if (value === null || value === undefined) return '-'
  return `${Number(value.toFixed(4))}x`
}

function providerInitial(provider: string): string {
  return (provider || '?').slice(0, 1).toUpperCase()
}

function modelKey(model: MarketplaceModel): string {
  return `${model.platform}:${model.id}`
}

async function copyModelName(model: MarketplaceModel) {
  try {
    await navigator.clipboard.writeText(model.name)
    copiedKey.value = modelKey(model)
    window.clearTimeout(copiedTimer)
    copiedTimer = window.setTimeout(() => {
      copiedKey.value = ''
    }, 1600)
  } catch {
    errorMessage.value = t('modelMarketplace.copyFailed')
  }
}

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

onMounted(() => {
  loadMarketplace()
})

onBeforeUnmount(() => {
  abortController?.abort()
  window.clearTimeout(copiedTimer)
})
</script>
