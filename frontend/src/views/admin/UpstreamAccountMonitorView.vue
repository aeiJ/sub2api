<template>
  <AppLayout>
    <div class="monitor-card-page">
      <div class="monitor-card-section">
        <section class="py-3 md:py-4">
          <div class="flex flex-wrap items-center justify-end gap-3">
            <div
              role="tablist"
              class="inline-flex rounded-xl border border-gray-200/60 bg-gray-100 p-0.5 text-xs dark:border-dark-700/60 dark:bg-dark-800"
            >
              <button
                v-for="option in windowOptions"
                :key="option.value"
                type="button"
                role="tab"
                :aria-selected="currentWindow === option.value"
                class="rounded-lg px-3 py-1 transition-colors"
                :class="currentWindow === option.value
                  ? 'bg-white font-semibold text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
                  : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'"
                @click="handleWindowChange(option.value)"
              >
                {{ option.label }}
              </button>
            </div>

            <span
              class="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold uppercase tracking-wider"
              :class="overallChipClass"
            >
              <span class="mr-1.5 h-1.5 w-1.5 rounded-full animate-pulse" :class="overallDotClass"></span>
              {{ t(`admin.upstreamAccountMonitor.overall.${overallStatus}`) }}
            </span>

            <button
              type="button"
              class="flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-200"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="reload"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </section>

        <div class="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-64">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('admin.upstreamAccountMonitor.searchPlaceholder')"
                class="input pl-10"
                @input="handleSearch"
              />
            </div>

            <Select v-model="platformFilter" class="w-44" :options="platformOptions" @change="reloadFirstPage" />
            <Select v-model="statusFilter" class="w-40" :options="statusOptions" @change="reloadFirstPage" />
            <Select v-model="groupFilter" class="w-44" :options="groupOptions" @change="reloadFirstPage" />
            <Select v-model="availabilitySort" class="w-48" :options="availabilitySortOptions" @change="reloadFirstPage" />
          </div>

          <div class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto">
            <button
              type="button"
              class="btn btn-primary"
              :disabled="loading"
              @click="openBatchDialog"
            >
              <Icon name="cog" size="md" class="mr-2" />
              {{ t('admin.upstreamAccountMonitor.batch.open') }}
            </button>
          </div>
        </div>
      </div>

      <div class="monitor-card-scroll">
        <div v-if="loading" class="monitor-card-grid">
          <div v-for="index in 6" :key="index" class="account-monitor-card animate-pulse">
            <div class="flex items-start gap-3">
              <div class="h-9 w-9 rounded-xl bg-gray-200 dark:bg-dark-700"></div>
              <div class="min-w-0 flex-1 space-y-2">
                <div class="h-4 w-2/3 rounded bg-gray-200 dark:bg-dark-700"></div>
                <div class="h-3 w-1/2 rounded bg-gray-200 dark:bg-dark-700"></div>
              </div>
              <div class="h-6 w-16 rounded-full bg-gray-200 dark:bg-dark-700"></div>
            </div>
            <div class="mt-5 grid grid-cols-2 gap-2">
              <div class="h-16 rounded-xl bg-gray-100 dark:bg-dark-900/40"></div>
              <div class="h-16 rounded-xl bg-gray-100 dark:bg-dark-900/40"></div>
            </div>
            <div class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700/60">
              <div class="space-y-2">
                <div class="h-3 w-full rounded bg-gray-100 dark:bg-dark-900/40"></div>
                <div class="h-3 w-4/5 rounded bg-gray-100 dark:bg-dark-900/40"></div>
              </div>
            </div>
          </div>
        </div>

        <div v-else-if="!items.length" class="rounded-2xl border border-gray-200/80 bg-white/70 p-8 shadow-card dark:border-dark-700/70 dark:bg-dark-800/60">
          <EmptyState
            :title="t('admin.upstreamAccountMonitor.emptyTitle')"
            :description="t('admin.upstreamAccountMonitor.emptyDescription')"
          />
        </div>

        <div v-else class="monitor-card-grid">
          <article
            v-for="row in items"
            :key="row.account_id"
            class="account-monitor-card"
            :class="{ 'account-monitor-card--checking': isChecking(row) }"
            :aria-busy="isChecking(row)"
          >
            <div
              v-if="isChecking(row)"
              class="checking-overlay"
            >
              <div class="checking-panel">
                <span class="checking-spinner"></span>
                <span>{{ t('admin.upstreamAccountMonitor.checking') }}</span>
              </div>
            </div>

            <header class="flex items-start gap-2.5">
              <span
                class="grid h-8 w-8 flex-shrink-0 place-items-center rounded-lg ring-1 ring-black/5 dark:ring-white/10"
                :class="[providerGradient(row.platform), providerTintClass(row.platform)]"
              >
                <ProviderIcon :provider="row.platform" :size="18" />
              </span>

              <div class="min-w-0 flex-1">
                <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-gray-100">
                  {{ row.account_name }}
                </h3>
                <div class="mt-0.5 flex min-w-0 flex-wrap items-center gap-1">
                  <span
                    class="inline-flex flex-shrink-0 items-center rounded px-1.5 py-0.5 text-[10px] font-medium leading-none"
                    :class="providerBadgeClass(row.platform)"
                  >
                    {{ platformLabel(row.platform) }}
                  </span>
                  <span
                    class="inline-flex min-w-0 max-w-[9rem] items-center truncate rounded bg-gray-100 px-1.5 py-0.5 font-mono text-[10px] font-medium leading-none text-gray-600 dark:bg-dark-700 dark:text-gray-300 sm:max-w-[11rem]"
                    :title="monitorModelLabel(row)"
                  >
                    {{ monitorModelLabel(row) }}
                  </span>
                  <span class="truncate font-mono text-[11px] text-gray-500 dark:text-gray-400">
                    #{{ row.account_id }}
                  </span>
                  <template v-if="row.groups?.length">
                    <span
                      v-for="group in visibleGroups(row)"
                      :key="group.id"
                      class="inline-flex max-w-[5.5rem] flex-shrink-0 items-center truncate rounded px-1.5 py-0.5 text-[10px] font-medium leading-none"
                      :class="groupChipClass(group.id)"
                      :title="groupDisplayTitle(row)"
                    >
                      {{ group.name }}
                    </span>
                    <span
                      v-if="hiddenGroupCount(row) > 0"
                      class="inline-flex flex-shrink-0 items-center rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium leading-none text-gray-500 dark:bg-dark-700 dark:text-gray-300"
                      :title="groupDisplayTitle(row)"
                    >
                      +{{ hiddenGroupCount(row) }}
                    </span>
                  </template>
                  <span
                    v-else
                    class="inline-flex flex-shrink-0 items-center rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium leading-none text-gray-500 dark:bg-dark-700 dark:text-gray-300"
                  >
                    {{ t('admin.upstreamAccountMonitor.noGroups') }}
                  </span>
                </div>
              </div>

              <span
                class="flex-shrink-0 rounded-full px-2 py-0.5 text-[11px] font-semibold"
                :class="statusBadgeClass(monitorHealthStatus(row))"
              >
                {{ monitorHealthStatusLabel(monitorHealthStatus(row)) }}
              </span>
            </header>

            <MonitorMetricPair
              primary-icon="bolt"
              :primary-label="t('monitorCommon.dialogLatency')"
              :primary-value="formatLatency(row.latest_result?.latency_ms)"
              primary-unit="ms"
              secondary-icon="globe"
              :secondary-label="t('monitorCommon.endpointPing')"
              :secondary-value="formatLatency(row.latest_result?.ping_latency_ms)"
              secondary-unit="ms"
            />

            <div class="mt-2.5 flex items-center justify-between gap-2 rounded-lg bg-gray-50 px-2.5 py-1.5 dark:bg-dark-900/30">
              <div class="min-w-0">
                <div class="truncate text-xs font-medium text-gray-700 dark:text-gray-200">
                  {{ t('admin.upstreamAccountMonitor.autoRecover') }}
                </div>
                <div class="truncate text-[11px] text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamAccountMonitor.autoRecoverHint') }}
                </div>
              </div>
              <Toggle
                :model-value="row.auto_recover"
                :disabled="autoRecoverSavingAccountId === row.account_id || batchRunning"
                @update:modelValue="toggleAutoRecover(row, $event)"
              />
            </div>

            <p
              v-if="latestError(row) && resultStatusVariant(row) !== 'error'"
              class="mt-2 line-clamp-2 text-xs text-red-500"
              :title="latestError(row)"
            >
              {{ latestError(row) }}
            </p>

            <div class="mt-3">
              <div
                v-if="resultStatusVariant(row) !== 'error'"
                class="border-t border-gray-100 pt-3 dark:border-dark-700/60"
              >
                <MonitorAvailabilityRow
                  :window-label="`${t('monitorCommon.availabilityPrefix')} · ${selectedWindowLabel}`"
                  :value="availabilityNumber(windowAvailability(row))"
                />

                <MonitorTimeline
                  :buckets="row.timeline || []"
                  :countdown-seconds="countdownSeconds(row.next_run_at)"
                />
              </div>
              <div
                v-else
                class="border-t border-gray-100 pt-3 dark:border-dark-700/60"
              >
                <div
                  class="flex min-h-[118px] max-h-[150px] overflow-y-auto rounded-lg border border-red-200/50 bg-red-500/5 p-3 text-xs leading-5 text-red-500 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-300"
                  :title="latestError(row)"
                >
                  <p class="whitespace-pre-wrap break-words">
                    {{ latestError(row) }}
                  </p>
                </div>
              </div>

              <div class="mt-3 grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2">
                <div
                  class="inline-flex h-9 min-w-0 items-center gap-2 rounded-lg border border-gray-200 px-2.5 text-xs font-medium text-gray-600 dark:border-dark-700 dark:text-gray-300"
                  :title="monitorActionLabel(row)"
                >
                  <Toggle
                    :model-value="row.monitor_enabled"
                    :disabled="monitorToggleSavingAccountId === row.account_id || batchRunning"
                    :title="row.monitor_enabled ? t('admin.upstreamAccountMonitor.disableMonitoring') : t('admin.upstreamAccountMonitor.enableMonitoring')"
                    @update:modelValue="toggleMonitorEnabled(row, $event)"
                  />
                  <span class="hidden whitespace-nowrap sm:inline">
                    {{ monitorActionLabel(row) }}
                  </span>
                </div>
                <button
                  class="inline-flex min-w-0 items-center justify-center rounded-lg border border-gray-200 px-3 py-1.5 text-sm font-medium text-gray-600 transition-colors hover:border-primary-300 hover:text-primary-600 dark:border-dark-700 dark:text-gray-300 dark:hover:border-primary-500/50 dark:hover:text-primary-400"
                  :disabled="runningAccountId === row.account_id || batchRunning"
                  @click="runOne(row.account_id)"
                >
                  <Icon :name="isChecking(row) ? 'refresh' : 'play'" size="sm" class="mr-2" :class="{ 'animate-spin': isChecking(row) }" />
                  {{ isChecking(row) ? t('admin.upstreamAccountMonitor.checking') : t('admin.upstreamAccountMonitor.runOne') }}
                </button>
                <button
                  class="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-500 transition-colors hover:border-primary-300 hover:text-primary-600 disabled:opacity-50 dark:border-dark-700 dark:text-gray-300 dark:hover:border-primary-500/50 dark:hover:text-primary-400"
                  :disabled="savingSettings"
                  :title="t('admin.upstreamAccountMonitor.settings.title')"
                  @click="openSettings(row)"
                >
                  <Icon name="cog" size="sm" />
                </button>
              </div>
            </div>
          </article>
        </div>
      </div>

      <div class="monitor-card-section">
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </div>

      <div
        v-if="batchDialogOpen"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
        @click.self="closeBatchDialog"
      >
        <div class="w-full max-w-xl rounded-2xl border border-gray-200 bg-white p-6 shadow-card dark:border-dark-700 dark:bg-dark-800">
          <div class="mb-5 flex items-start justify-between gap-4">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
                {{ t('admin.upstreamAccountMonitor.batch.title') }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ batchScopeLabel }}
              </p>
            </div>
            <button
              type="button"
              class="rounded-lg p-1 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-200"
              @click="closeBatchDialog"
            >
              <Icon name="x" size="md" />
            </button>
          </div>

          <div class="grid gap-4">
            <section class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
              <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div class="text-sm font-semibold text-gray-800 dark:text-gray-100">
                    {{ t('admin.upstreamAccountMonitor.batch.checkTitle') }}
                  </div>
                  <div class="text-xs text-gray-500 dark:text-gray-400">
                    {{ t('admin.upstreamAccountMonitor.batch.checkDescription') }}
                  </div>
                </div>
                <div class="flex flex-wrap justify-end gap-2">
                  <button type="button" class="btn btn-primary" :disabled="batchRunning || loading" @click="runAll">
                    <Icon :name="batchAction === 'runAll' ? 'refresh' : 'play'" size="sm" class="mr-2" :class="{ 'animate-spin': batchAction === 'runAll' }" />
                    {{ batchAction === 'runAll' ? t('admin.upstreamAccountMonitor.checking') : batchRunLabel }}
                  </button>
                  <button v-if="batchAction === 'runAll'" type="button" class="btn btn-secondary" @click="cancelRunAll">
                    {{ t('common.cancel') }}
                  </button>
                </div>
              </div>
              <div v-if="batchAction === 'runAll' || batchProgress.total > 0" class="mt-3 rounded-lg bg-gray-50 px-3 py-2 text-sm text-gray-600 dark:bg-dark-900/40 dark:text-gray-300">
                {{ t('admin.upstreamAccountMonitor.batch.progress', {
                  completed: batchProgress.completed,
                  total: batchProgress.total,
                  success: batchProgress.success,
                  failed: batchProgress.failed
                }) }}
              </div>
            </section>

            <section class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
              <div class="mb-3">
                <div class="text-sm font-semibold text-gray-800 dark:text-gray-100">
                  {{ t('admin.upstreamAccountMonitor.batch.monitorTitle') }}
                </div>
                <div class="text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamAccountMonitor.batch.monitorDescription') }}
                </div>
              </div>
              <div class="flex flex-wrap gap-2">
                <button type="button" class="btn btn-secondary" :disabled="batchRunning || loading" @click="enableAll">
                  <Icon name="check" size="sm" class="mr-2" />
                  {{ batchEnableLabel }}
                </button>
                <button type="button" class="btn btn-secondary" :disabled="batchRunning || loading" @click="disableAll">
                  <Icon name="x" size="sm" class="mr-2" />
                  {{ batchDisableLabel }}
                </button>
              </div>
            </section>

            <section class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
              <div class="mb-3">
                <div class="text-sm font-semibold text-gray-800 dark:text-gray-100">
                  {{ t('admin.upstreamAccountMonitor.batch.intervalTitle') }}
                </div>
                <div class="text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamAccountMonitor.batch.intervalDescription') }}
                </div>
              </div>
              <div class="grid gap-3 sm:grid-cols-2">
                <div>
                  <label class="input-label">{{ t('admin.upstreamAccountMonitor.settings.intervalMinutes') }}</label>
                  <input v-model.number="batchSettingsForm.interval_minutes" class="input" type="number" min="1" max="1440" />
                </div>
                <div>
                  <label class="input-label">{{ t('admin.upstreamAccountMonitor.settings.jitterSeconds') }}</label>
                  <input v-model.number="batchSettingsForm.jitter_seconds" class="input" type="number" min="0" :max="batchJitterMax" />
                </div>
              </div>
              <div class="mt-4 flex justify-end">
                <button type="button" class="btn btn-primary" :disabled="batchRunning || loading" @click="applyBatchInterval">
                  <Icon name="check" size="sm" class="mr-2" />
                  {{ batchIntervalLabel }}
                </button>
              </div>
            </section>
          </div>
        </div>
      </div>

      <div
        v-if="settingsDialogOpen"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
        @click.self="closeSettings"
      >
        <form
          class="w-full max-w-lg rounded-2xl border border-gray-200 bg-white p-6 shadow-card dark:border-dark-700 dark:bg-dark-800"
          @submit.prevent="saveSettings"
        >
          <div class="mb-5 flex items-start justify-between gap-4">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
                {{ t('admin.upstreamAccountMonitor.settings.title') }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.upstreamAccountMonitor.settings.description') }}
              </p>
            </div>
            <button
              type="button"
              class="rounded-lg p-1 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-200"
              @click="closeSettings"
            >
              <Icon name="x" size="md" />
            </button>
          </div>

          <div class="grid gap-4">
            <div>
              <label class="input-label">{{ t('admin.upstreamAccountMonitor.settings.primaryModel') }}</label>
              <Select
                v-model="settingsForm.model_id"
                :options="modelOptions"
                :disabled="loadingModelOptions"
                searchable
                clearable
                :placeholder="t('admin.upstreamAccountMonitor.settings.primaryModelPlaceholder')"
              />
            </div>
            <div class="grid gap-4 sm:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.upstreamAccountMonitor.settings.intervalMinutes') }}</label>
                <input v-model.number="settingsForm.interval_minutes" class="input" type="number" min="1" max="1440" required />
              </div>
              <div>
                <label class="input-label">{{ t('admin.upstreamAccountMonitor.settings.jitterSeconds') }}</label>
                <input v-model.number="settingsForm.jitter_seconds" class="input" type="number" min="0" :max="settingsJitterMax" required />
              </div>
            </div>
            <div class="flex items-center justify-between gap-3 rounded-xl border border-gray-200 px-3 py-2 dark:border-dark-700">
              <div>
                <div class="text-sm font-medium text-gray-700 dark:text-gray-200">
                  {{ t('admin.upstreamAccountMonitor.autoRecover') }}
                </div>
                <div class="text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.upstreamAccountMonitor.autoRecoverHint') }}
                </div>
              </div>
              <Toggle v-model="settingsForm.auto_recover" />
            </div>
          </div>

          <div class="mt-6 flex justify-end gap-3">
            <button type="button" class="btn btn-secondary" @click="closeSettings">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary" :disabled="savingSettings">
              <Icon name="check" size="sm" class="mr-2" />
              {{ t('common.save') }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  UpstreamAccountMonitorBatchResponse,
  UpstreamAccountMonitorItem,
  UpstreamAccountMonitorListParams,
  UpstreamAccountMonitorRunAllStreamEvent,
  UpstreamAccountMonitorStatus,
} from '@/api/admin/upstreamAccountMonitor'
import type { MonitorStatus } from '@/api/admin/channelMonitor'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import MonitorMetricPair from '@/components/user/monitor/MonitorMetricPair.vue'
import MonitorAvailabilityRow from '@/components/user/monitor/MonitorAvailabilityRow.vue'
import MonitorTimeline from '@/components/user/monitor/MonitorTimeline.vue'
import {
  providerGradient,
  useChannelMonitorFormat,
} from '@/composables/useChannelMonitorFormat'
import {
  STATUS_DEGRADED,
  STATUS_FAILED,
  STATUS_OPERATIONAL,
} from '@/constants/channelMonitor'
import type { ClaudeModel } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()
const { providerBadgeClass, formatLatency, statusBadgeClass } = useChannelMonitorFormat()

const items = ref<UpstreamAccountMonitorItem[]>([])
const groups = ref<AdminGroup[]>([])
const loading = ref(false)
const searchQuery = ref('')
const platformFilter = ref('')
const statusFilter = ref<UpstreamAccountMonitorStatus | ''>('')
const groupFilter = ref('')
const currentWindow = ref<UpstreamMonitorWindow>('7d')
const availabilitySort = ref<AvailabilitySort>('')
const batchAction = ref<'runAll' | 'enableAll' | 'disableAll' | 'settings' | null>(null)
const runningAccountId = ref<number | null>(null)
const autoRecoverSavingAccountId = ref<number | null>(null)
const monitorToggleSavingAccountId = ref<number | null>(null)
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const batchDialogOpen = ref(false)
const settingsDialogOpen = ref(false)
const savingSettings = ref(false)
const loadingModelOptions = ref(false)
const settingsAccountId = ref<number | null>(null)
const availableModels = ref<ClaudeModel[]>([])
const settingsForm = reactive({
  model_id: '' as string | null,
  interval_minutes: 60,
  jitter_seconds: 0,
  auto_recover: false,
})
const batchSettingsForm = reactive({
  interval_minutes: 60,
  jitter_seconds: 0,
})
const batchProgress = reactive({
  total: 0,
  completed: 0,
  success: 0,
  failed: 0,
})
const checkingAccountIds = ref<Set<number>>(new Set())

let abortController: AbortController | null = null
let searchTimeout: ReturnType<typeof setTimeout> | null = null
let runAllAbortController: AbortController | null = null

const batchRunning = computed(() => batchAction.value !== null)
const settingsJitterMax = computed(() => Math.max(0, settingsForm.interval_minutes * 60 - 1))
const batchJitterMax = computed(() => Math.max(0, batchSettingsForm.interval_minutes * 60 - 1))
type UpstreamMonitorWindow = '7d' | '15d'
type AvailabilitySort = '' | 'asc' | 'desc'
const hasActiveFilters = computed(() =>
  Boolean(platformFilter.value || statusFilter.value || groupFilter.value || searchQuery.value.trim())
)
const batchScopeLabel = computed(() =>
  hasActiveFilters.value
    ? t('admin.upstreamAccountMonitor.batch.scopeFiltered')
    : t('admin.upstreamAccountMonitor.batch.scopeAll')
)
const batchRunLabel = computed(() =>
  hasActiveFilters.value
    ? t('admin.upstreamAccountMonitor.batch.runFiltered')
    : t('admin.upstreamAccountMonitor.batch.runAll')
)
const batchEnableLabel = computed(() =>
  hasActiveFilters.value
    ? t('admin.upstreamAccountMonitor.batch.enableFiltered')
    : t('admin.upstreamAccountMonitor.batch.enableAll')
)
const batchDisableLabel = computed(() =>
  hasActiveFilters.value
    ? t('admin.upstreamAccountMonitor.batch.disableFiltered')
    : t('admin.upstreamAccountMonitor.batch.disableAll')
)
const batchIntervalLabel = computed(() =>
  hasActiveFilters.value
    ? t('admin.upstreamAccountMonitor.batch.intervalFiltered')
    : t('admin.upstreamAccountMonitor.batch.intervalAll')
)

const windowOptions = computed<{ value: UpstreamMonitorWindow; label: string }[]>(() => [
  { value: '7d', label: t('admin.upstreamAccountMonitor.windowTab.7d') },
  { value: '15d', label: t('admin.upstreamAccountMonitor.windowTab.15d') },
])

const selectedWindowLabel = computed(() => t(`admin.upstreamAccountMonitor.windowTab.${currentWindow.value}`))

const overallStatus = computed<'operational' | 'degraded'>(() => {
  if (items.value.length === 0) return 'operational'
  return items.value.some((item) => item.account_status === 'error' || monitorHealthStatus(item) !== STATUS_OPERATIONAL)
    ? 'degraded'
    : 'operational'
})

const overallChipClass = computed(() =>
  overallStatus.value === 'operational'
    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    : 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
)

const overallDotClass = computed(() =>
  overallStatus.value === 'operational' ? 'bg-emerald-500' : 'bg-amber-500'
)

const MONITOR_DEGRADED_LATENCY_MS = 6000

const platformOptions = computed(() => [
  { value: '', label: t('admin.accounts.allPlatforms') },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' },
])

const statusOptions = computed(() => [
  { value: '', label: t('admin.accounts.allStatus') },
  { value: STATUS_OPERATIONAL, label: t('admin.upstreamAccountMonitor.monitorStatus.operational') },
  { value: STATUS_DEGRADED, label: t('admin.upstreamAccountMonitor.monitorStatus.degraded') },
  { value: STATUS_FAILED, label: t('admin.upstreamAccountMonitor.monitorStatus.failed') },
])

const groupOptions = computed(() => [
  { value: '', label: t('admin.accounts.allGroups') },
  ...groups.value.map((group) => ({ value: String(group.id), label: group.name })),
])

const availabilitySortOptions = computed(() => [
  { value: '', label: t('admin.upstreamAccountMonitor.sort.default') },
  { value: 'asc', label: t('admin.upstreamAccountMonitor.sort.availabilityAsc') },
  { value: 'desc', label: t('admin.upstreamAccountMonitor.sort.availabilityDesc') },
])

const modelOptions = computed(() => {
  const seen = new Set<string>()
  const options = availableModels.value
    .map((model) => ({
      value: model.id,
      label: model.display_name || model.id,
    }))
    .filter((option) => {
      if (!option.value || seen.has(option.value)) return false
      seen.add(option.value)
      return true
    })
  const current = (settingsForm.model_id || '').trim()
  if (current && !seen.has(current)) {
    options.unshift({ value: current, label: current })
  }
  return options
})

function currentFilterParams(): UpstreamAccountMonitorListParams {
  return {
    platform: platformFilter.value || undefined,
    monitor_status: statusFilter.value || undefined,
    search: searchQuery.value.trim() || undefined,
    group_id: groupFilter.value || undefined,
  }
}

function currentListParams(): UpstreamAccountMonitorListParams {
  const params: UpstreamAccountMonitorListParams = { ...currentFilterParams() }
  if (availabilitySort.value) {
    params.sort_by = 'availability'
    params.sort_order = availabilitySort.value
    params.availability_window = currentWindow.value
  }
  return params
}

async function reload() {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  loading.value = true
  try {
    const res = await adminAPI.upstreamAccountMonitor.list({
      page: pagination.page,
      page_size: pagination.page_size,
      ...currentListParams(),
    }, { signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    items.value = res.items || []
    pagination.total = res.total
  } catch (error: any) {
    if (error?.name === 'AbortError' || error?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamAccountMonitor.loadError')))
  } finally {
    if (abortController === ctrl) {
      loading.value = false
      abortController = null
    }
  }
}

async function loadGroups() {
  try {
    groups.value = await adminAPI.groups.getAllIncludingInactive()
  } catch {
    groups.value = []
  }
}

function reloadFirstPage() {
  cancelRunAll()
  pagination.page = 1
  void reload()
}

function handleWindowChange(value: UpstreamMonitorWindow) {
  if (currentWindow.value === value) return
  currentWindow.value = value
  if (availabilitySort.value) {
    reloadFirstPage()
  }
}

function handleSearch() {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(reloadFirstPage, 300)
}

function handlePageChange(page: number) {
  pagination.page = page
  void reload()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  void reload()
}

function openBatchDialog() {
  batchSettingsForm.interval_minutes = 60
  batchSettingsForm.jitter_seconds = 0
  batchDialogOpen.value = true
}

function closeBatchDialog() {
  if (batchAction.value && batchAction.value !== 'runAll') return
  batchDialogOpen.value = false
}

async function runBatch(action: 'enableAll' | 'disableAll' | 'settings', request: () => Promise<UpstreamAccountMonitorBatchResponse>) {
  if (batchAction.value) return
  batchAction.value = action
  try {
    const result = await request()
    appStore.showSuccess(batchMessage(action, result))
    await reload()
  } catch (error: any) {
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamAccountMonitor.batchError')))
  } finally {
    batchAction.value = null
  }
}

function runAll() {
  if (batchAction.value) return
  batchAction.value = 'runAll'
  resetBatchProgress()
  checkingAccountIds.value = new Set(items.value.map((item) => item.account_id))
  const ctrl = new AbortController()
  runAllAbortController = ctrl
  void runAllStream(ctrl)
}

async function runAllStream(ctrl: AbortController) {
  try {
    await adminAPI.upstreamAccountMonitor.runAllStream(
      currentFilterParams(),
      handleRunAllStreamEvent,
      { signal: ctrl.signal }
    )
    await reload()
  } catch (error: any) {
    if (error?.name !== 'AbortError') {
      appStore.showError(extractApiErrorMessage(error, t('admin.upstreamAccountMonitor.batchError')))
    }
  } finally {
    if (runAllAbortController === ctrl) {
      runAllAbortController = null
      checkingAccountIds.value = new Set()
      batchAction.value = null
    }
  }
}

function handleRunAllStreamEvent(event: UpstreamAccountMonitorRunAllStreamEvent) {
  if (event.type === 'started') {
    batchProgress.total = event.total || 0
    return
  }
  if (event.type === 'item') {
    batchProgress.completed += 1
    const failed = Boolean(event.error || !event.result || event.result.status !== 'success')
    if (failed) batchProgress.failed += 1
    else batchProgress.success += 1
    updateRowFromRunEvent(event)
    removeCheckingAccount(event.account_id)
    return
  }
  if (event.type === 'done') {
    batchProgress.total = event.total || batchProgress.total
    batchProgress.success = event.success ?? batchProgress.success
    batchProgress.failed = event.failed ?? batchProgress.failed
    appStore.showSuccess(t('admin.upstreamAccountMonitor.runAllSuccess', {
      success: batchProgress.success,
      failed: batchProgress.failed,
      total: batchProgress.total,
    }))
    return
  }
  if (event.type === 'error') {
    appStore.showError(event.message || event.error || t('admin.upstreamAccountMonitor.batchError'))
  }
}

function updateRowFromRunEvent(event: Extract<UpstreamAccountMonitorRunAllStreamEvent, { type: 'item' }>) {
  const row = items.value.find((item) => item.account_id === event.account_id)
  if (!row) return
  if (typeof event.plan_id === 'number' && event.plan_id > 0) {
    row.plan_id = event.plan_id
  }
  if (event.result) {
    row.latest_result = event.result
    row.last_run_at = event.result.finished_at || event.result.started_at || row.last_run_at
  }
}

function removeCheckingAccount(accountId: number) {
  const next = new Set(checkingAccountIds.value)
  next.delete(accountId)
  checkingAccountIds.value = next
}

function resetBatchProgress() {
  batchProgress.total = 0
  batchProgress.completed = 0
  batchProgress.success = 0
  batchProgress.failed = 0
}

function cancelRunAll() {
  runAllAbortController?.abort()
}

function enableAll() {
  void runBatch('enableAll', () => adminAPI.upstreamAccountMonitor.batchUpdateSettings({
    monitor_enabled: true,
  }, currentFilterParams()))
}

function disableAll() {
  void runBatch('disableAll', () => adminAPI.upstreamAccountMonitor.batchUpdateSettings({
    monitor_enabled: false,
  }, currentFilterParams()))
}

function applyBatchInterval() {
  if (batchSettingsForm.interval_minutes < 1 || batchSettingsForm.interval_minutes > 1440) {
    appStore.showError(t('admin.upstreamAccountMonitor.settings.intervalInvalid'))
    return
  }
  if (batchSettingsForm.jitter_seconds < 0 || batchSettingsForm.jitter_seconds >= batchSettingsForm.interval_minutes * 60) {
    appStore.showError(t('admin.upstreamAccountMonitor.settings.jitterInvalid'))
    return
  }
  void runBatch('settings', () => adminAPI.upstreamAccountMonitor.batchUpdateSettings({
    interval_minutes: batchSettingsForm.interval_minutes,
    jitter_seconds: batchSettingsForm.jitter_seconds,
  }, currentFilterParams()))
}

function isChecking(row: UpstreamAccountMonitorItem): boolean {
  return checkingAccountIds.value.has(row.account_id) || runningAccountId.value === row.account_id
}

async function runOne(accountId: number) {
  if (runningAccountId.value !== null) return
  runningAccountId.value = accountId
  try {
    const result = await adminAPI.upstreamAccountMonitor.runOne(accountId)
    if (result.error || result.result?.status === 'failed') {
      appStore.showError(result.error || result.result?.error_message || t('admin.upstreamAccountMonitor.runFailed'))
    } else {
      appStore.showSuccess(t('admin.upstreamAccountMonitor.runSuccess'))
    }
    await reload()
  } catch (error: any) {
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamAccountMonitor.runFailed')))
  } finally {
    runningAccountId.value = null
  }
}

async function openSettings(row: UpstreamAccountMonitorItem) {
  settingsAccountId.value = row.account_id
  settingsForm.model_id = row.model_id || ''
  settingsForm.interval_minutes = row.interval_minutes || 60
  settingsForm.jitter_seconds = row.jitter_seconds || 0
  settingsForm.auto_recover = row.auto_recover
  availableModels.value = []
  settingsDialogOpen.value = true
  await loadModelOptions(row)
}

function closeSettings() {
  if (savingSettings.value) return
  settingsDialogOpen.value = false
  settingsAccountId.value = null
}

async function saveSettings() {
  if (!settingsAccountId.value || savingSettings.value) return
  if (settingsForm.interval_minutes < 1 || settingsForm.interval_minutes > 1440) {
    appStore.showError(t('admin.upstreamAccountMonitor.settings.intervalInvalid'))
    return
  }
  if (settingsForm.jitter_seconds < 0 || settingsForm.jitter_seconds >= settingsForm.interval_minutes * 60) {
    appStore.showError(t('admin.upstreamAccountMonitor.settings.jitterInvalid'))
    return
  }
  savingSettings.value = true
  try {
    await adminAPI.upstreamAccountMonitor.updateSettings(settingsAccountId.value, {
      model_id: (settingsForm.model_id || '').trim(),
      interval_minutes: settingsForm.interval_minutes,
      jitter_seconds: settingsForm.jitter_seconds,
      auto_recover: settingsForm.auto_recover,
    })
    appStore.showSuccess(t('admin.upstreamAccountMonitor.settings.saveSuccess'))
    settingsDialogOpen.value = false
    settingsAccountId.value = null
    await reload()
  } catch (error: any) {
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamAccountMonitor.settings.saveFailed')))
  } finally {
    savingSettings.value = false
  }
}

async function loadModelOptions(row: UpstreamAccountMonitorItem) {
  loadingModelOptions.value = true
  try {
    availableModels.value = await adminAPI.accounts.getAvailableModels(row.account_id)
  } catch (error: any) {
    availableModels.value = []
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamAccountMonitor.settings.modelLoadFailed')))
  } finally {
    loadingModelOptions.value = false
  }
}

async function toggleAutoRecover(row: UpstreamAccountMonitorItem, value: boolean) {
  if (autoRecoverSavingAccountId.value !== null) return
  const previous = row.auto_recover
  row.auto_recover = value
  autoRecoverSavingAccountId.value = row.account_id
  try {
    await adminAPI.upstreamAccountMonitor.updateSettings(row.account_id, {
      model_id: row.model_id || '',
      interval_minutes: row.interval_minutes || 60,
      jitter_seconds: row.jitter_seconds || 0,
      auto_recover: value,
      monitor_enabled: row.monitor_enabled,
    })
    appStore.showSuccess(t('admin.upstreamAccountMonitor.settings.saveSuccess'))
    await reload()
  } catch (error: any) {
    row.auto_recover = previous
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamAccountMonitor.settings.saveFailed')))
  } finally {
    autoRecoverSavingAccountId.value = null
  }
}

async function toggleMonitorEnabled(row: UpstreamAccountMonitorItem, value: boolean) {
  if (monitorToggleSavingAccountId.value !== null) return
  if (!value && row.monitor_required) {
    row.monitor_enabled = true
    appStore.showWarning(t('admin.upstreamAccountMonitor.cannotDisableSchedulable'))
    return
  }
  const previous = row.monitor_enabled
  row.monitor_enabled = value
  monitorToggleSavingAccountId.value = row.account_id
  try {
    await adminAPI.upstreamAccountMonitor.updateSettings(row.account_id, {
      model_id: row.model_id || '',
      interval_minutes: row.interval_minutes || 60,
      jitter_seconds: row.jitter_seconds || 0,
      auto_recover: row.auto_recover,
      monitor_enabled: value,
    })
    appStore.showSuccess(t('admin.upstreamAccountMonitor.settings.saveSuccess'))
    await reload()
  } catch (error: any) {
    row.monitor_enabled = previous
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamAccountMonitor.settings.saveFailed')))
  } finally {
    monitorToggleSavingAccountId.value = null
  }
}

function batchMessage(action: 'enableAll' | 'disableAll' | 'settings', result: UpstreamAccountMonitorBatchResponse): string {
  if (action === 'enableAll') {
    return t('admin.upstreamAccountMonitor.enableAllSuccess', {
      enabled: result.enabled ?? result.updated ?? 0,
      created: result.created ?? 0,
      skipped: result.skipped ?? 0,
    })
  }
  if (action === 'settings') {
    return t('admin.upstreamAccountMonitor.batch.intervalSuccess', {
      updated: result.updated ?? 0,
      created: result.created ?? 0,
      skipped: result.skipped ?? 0,
    })
  }
  return t('admin.upstreamAccountMonitor.disableAllSuccess', {
    disabled: result.disabled ?? result.updated ?? 0,
    skipped: result.skipped ?? 0,
  })
}

function platformLabel(platform: string): string {
  const labels: Record<string, string> = {
    anthropic: 'Anthropic',
    openai: 'OpenAI',
    gemini: 'Gemini',
    antigravity: 'Antigravity',
    grok: 'Grok',
  }
  return labels[platform] || platform
}

function monitorModelLabel(row: UpstreamAccountMonitorItem): string {
  const explicit = row.model_id?.trim()
  if (explicit) return explicit
  const defaults: Record<string, string> = {
    anthropic: 'claude-sonnet-4-5-20250929',
    openai: 'gpt-5.4',
    gemini: 'gemini-2.0-flash',
    antigravity: 'claude-sonnet-4-5',
    grok: 'grok-4.3',
  }
  return defaults[row.platform] || '-'
}

function monitorActionLabel(row: UpstreamAccountMonitorItem): string {
  return row.monitor_enabled
    ? t('admin.upstreamAccountMonitor.disableMonitoring')
    : t('admin.upstreamAccountMonitor.enableMonitoring')
}

function resultStatusVariant(row: UpstreamAccountMonitorItem): 'success' | 'error' | 'warning' | 'inactive' {
  if (!row.plan_id) return 'inactive'
  if (!row.latest_result) return 'warning'
  return row.latest_result.status === 'success' ? 'success' : 'error'
}

function monitorHealthStatus(row: UpstreamAccountMonitorItem): MonitorStatus | '' {
  if (!row.plan_id || !row.latest_result) return ''
  if (row.latest_result.status !== 'success') return STATUS_FAILED
  return row.latest_result.latency_ms >= MONITOR_DEGRADED_LATENCY_MS ? STATUS_DEGRADED : STATUS_OPERATIONAL
}

function monitorHealthStatusLabel(status: MonitorStatus | ''): string {
  if (!status) return t('admin.upstreamAccountMonitor.notChecked')
  if (status === STATUS_OPERATIONAL || status === STATUS_DEGRADED || status === STATUS_FAILED) {
    return t(`admin.upstreamAccountMonitor.monitorStatus.${status}`)
  }
  return t('admin.upstreamAccountMonitor.notChecked')
}

function latestError(row: UpstreamAccountMonitorItem): string {
  if (row.latest_result) {
    return row.latest_result.status === 'success' ? '' : row.latest_result.error_message || ''
  }
  return row.account_status === 'error' ? row.error_message || '' : ''
}

function providerTintClass(platform: string): string {
  const classes: Record<string, string> = {
    openai: 'text-emerald-600 dark:text-emerald-300',
    anthropic: 'text-orange-600 dark:text-orange-300',
    gemini: 'text-sky-600 dark:text-sky-300',
    antigravity: 'text-indigo-600 dark:text-indigo-300',
    grok: 'text-gray-700 dark:text-gray-200',
  }
  return classes[platform] ?? 'text-gray-500 dark:text-gray-300'
}

function groupChipClass(groupId: number): string {
  const classes = [
    'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
    'bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300',
    'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
    'bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300',
    'bg-indigo-100 text-indigo-700 dark:bg-indigo-500/15 dark:text-indigo-300',
    'bg-teal-100 text-teal-700 dark:bg-teal-500/15 dark:text-teal-300',
  ]
  return classes[Math.abs(groupId) % classes.length]
}

function visibleGroups(row: UpstreamAccountMonitorItem) {
  return (row.groups || []).slice(0, 2)
}

function hiddenGroupCount(row: UpstreamAccountMonitorItem): number {
  return Math.max(0, (row.groups?.length || 0) - visibleGroups(row).length)
}

function groupDisplayTitle(row: UpstreamAccountMonitorItem): string {
  return (row.groups || []).map((group) => group.name).join(', ')
}

function availabilityNumber(value: number | null | undefined): number | null {
  if (typeof value !== 'number' || Number.isNaN(value)) return null
  return value
}

function windowAvailability(row: UpstreamAccountMonitorItem): number | null | undefined {
  return currentWindow.value === '15d' ? row.availability_15d : row.availability_7d
}

function countdownSeconds(value: string | null | undefined): number {
  if (!value) return 0
  const ts = Date.parse(value)
  if (Number.isNaN(ts)) return 0
  return Math.max(0, Math.round((ts - Date.now()) / 1000))
}

onMounted(() => {
  void loadGroups()
  void reload()
})

onUnmounted(() => {
  cancelRunAll()
  abortController?.abort()
  if (searchTimeout) clearTimeout(searchTimeout)
})
</script>

<style scoped>
.monitor-card-page {
  @apply flex flex-col gap-6;
  height: calc(100vh - 64px - 4rem);
}

.monitor-card-section {
  @apply flex-shrink-0;
}

.monitor-card-scroll {
  @apply min-h-0 flex-1 overflow-y-auto pr-1;
}

.monitor-card-grid {
  @apply grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4;
}

.account-monitor-card {
  @apply relative flex min-h-[236px] w-full flex-col overflow-hidden rounded-xl border border-gray-200/80 bg-white/70 p-4 shadow-card backdrop-blur-xl transition-all duration-300 ease-out dark:border-dark-700/70 dark:bg-dark-800/60;
}

.account-monitor-card:hover {
  @apply -translate-y-1 border-gray-300 shadow-card-hover dark:border-primary-500/30;
}

.account-monitor-card--checking {
  @apply border-primary-300 shadow-card-hover dark:border-primary-500/40;
}

.account-monitor-card--checking::before {
  content: '';
  @apply pointer-events-none absolute left-0 right-0 top-0 z-20 h-1 bg-primary-500/70;
  animation: checking-sweep 1.2s ease-in-out infinite;
}

.checking-overlay {
  @apply pointer-events-none absolute inset-0 z-10 flex items-center justify-center bg-white/70 backdrop-blur-[1px] dark:bg-dark-900/60;
}

.checking-panel {
  @apply inline-flex items-center gap-2 rounded-full border border-primary-200 bg-white px-3 py-1.5 text-xs font-semibold text-primary-600 shadow-lg dark:border-primary-500/30 dark:bg-dark-800 dark:text-primary-300;
}

.checking-spinner {
  @apply h-3.5 w-3.5 rounded-full border-2 border-primary-200 border-t-primary-600 dark:border-primary-500/25 dark:border-t-primary-300;
  animation: checking-spin 0.8s linear infinite;
}

@keyframes checking-spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes checking-sweep {
  0% {
    transform: translateX(-100%);
  }
  50% {
    transform: translateX(0);
  }
  100% {
    transform: translateX(100%);
  }
}

@media (max-width: 767px) {
  .monitor-card-page {
    height: auto;
  }

  .monitor-card-scroll {
    @apply overflow-visible pr-0;
  }

  .monitor-card-grid {
    @apply grid-cols-1;
  }
}
</style>
