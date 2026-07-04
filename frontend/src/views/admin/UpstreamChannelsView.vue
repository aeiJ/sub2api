<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
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
                class="input pl-10"
                :placeholder="t('admin.upstreamChannels.searchPlaceholder')"
                @input="handleSearch"
              />
            </div>

            <Select
              v-model="filters.provider"
              :options="providerFilterOptions"
              :placeholder="t('admin.upstreamChannels.filters.allProviders')"
              class="w-44"
              @change="reload"
            />
          </div>

          <div class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto">
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="reload"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('admin.upstreamChannels.createButton') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="channels"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="updated_at"
          default-sort-order="desc"
          @sort="handleSort"
        >
          <template #cell-name="{ row }">
            <div class="min-w-64">
              <div class="font-medium text-gray-900 dark:text-white">{{ row.name }}</div>
              <div class="mt-0.5 max-w-md truncate text-xs text-gray-500 dark:text-gray-400">
                {{ row.description || '-' }}
              </div>
            </div>
          </template>

          <template #cell-platforms="{ row }">
            <div class="flex min-w-48 flex-wrap gap-1.5">
              <span
                v-for="platform in row.platforms || []"
                :key="platform.id"
                class="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium"
                :class="providerBadgeClass(platform.provider)"
              >
                {{ providerLabel(platform.provider) }}
                <span class="text-[10px] opacity-80">{{ platform.key_pools?.length || 0 }}</span>
              </span>
              <span v-if="!row.platforms?.length" class="text-sm text-gray-400">-</span>
            </div>
          </template>

          <template #cell-key_count="{ row }">
            <span class="text-sm text-gray-700 dark:text-gray-300">
              {{ countKeys(row) }}
            </span>
          </template>

          <template #cell-updated_at="{ value }">
            <span class="text-sm text-gray-600 dark:text-gray-400">
              {{ formatDateTime(value) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button
                type="button"
                class="table-action"
                :title="t('admin.upstreamChannels.actions.preview')"
                :disabled="previewingId === row.id"
                @click="handlePreview(row)"
              >
                <Icon name="eye" size="sm" />
                <span class="text-xs">{{ t('admin.upstreamChannels.actions.preview') }}</span>
              </button>
              <button
                type="button"
                class="table-action"
                :title="t('admin.upstreamChannels.actions.sync')"
                :disabled="syncingId === row.id"
                @click="handleSync(row)"
              >
                <Icon name="sync" size="sm" :class="syncingId === row.id ? 'animate-spin' : ''" />
                <span class="text-xs">{{ t('admin.upstreamChannels.actions.sync') }}</span>
              </button>
              <button
                type="button"
                class="table-action"
                :title="t('admin.upstreamChannels.actions.test')"
                :disabled="testingId === row.id"
                @click="handleTest(row)"
              >
                <Icon name="bolt" size="sm" />
                <span class="text-xs">{{ t('admin.upstreamChannels.actions.test') }}</span>
              </button>
              <button
                type="button"
                class="table-action"
                :title="t('common.edit')"
                :disabled="editingLoadingId === row.id"
                @click="openEditDialog(row)"
              >
                <Icon name="edit" size="sm" />
                <span class="text-xs">{{ t('common.edit') }}</span>
              </button>
              <button
                type="button"
                class="table-action table-action-danger"
                :title="t('common.delete')"
                @click="handleDelete(row)"
              >
                <Icon name="trash" size="sm" />
                <span class="text-xs">{{ t('common.delete') }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.upstreamChannels.emptyTitle')"
              :description="t('admin.upstreamChannels.emptyDescription')"
              :action-text="t('admin.upstreamChannels.createButton')"
              @action="openCreateDialog"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="showDialog"
      :title="editingChannel ? t('admin.upstreamChannels.editTitle') : t('admin.upstreamChannels.createTitle')"
      width="full"
      @close="closeDialog"
    >
      <form id="upstream-channel-form" class="upstream-dialog-body" @submit.prevent="handleSubmit">
        <section class="space-y-4">
          <div>
            <div>
              <label class="input-label">
                {{ t('admin.upstreamChannels.form.name') }} <span class="text-red-500">*</span>
              </label>
              <input
                v-model="form.name"
                type="text"
                required
                class="input"
                :placeholder="t('admin.upstreamChannels.form.namePlaceholder')"
              />
            </div>
          </div>

          <div>
            <label class="input-label">{{ t('admin.upstreamChannels.form.description') }}</label>
            <textarea
              v-model="form.description"
              rows="2"
              class="input"
              :placeholder="t('admin.upstreamChannels.form.descriptionPlaceholder')"
            ></textarea>
          </div>
        </section>

        <section class="space-y-4 border-t border-gray-200 pt-5 dark:border-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t('admin.upstreamChannels.form.platforms') }}
              </h4>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.upstreamChannels.form.platformsHint') }}
              </p>
            </div>
            <div class="flex flex-wrap gap-2">
              <button
                v-for="provider in supportedProviders"
                :key="provider"
                type="button"
                class="btn btn-secondary px-3 py-1.5"
                :disabled="hasProvider(provider)"
                @click="addPlatform(provider)"
              >
                <Icon name="plus" size="sm" class="mr-1 inline-block" />
                {{ providerLabel(provider) }}
              </button>
            </div>
          </div>

          <div v-if="form.platforms.length === 0" class="rounded-lg border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
            {{ t('admin.upstreamChannels.form.noPlatforms') }}
          </div>

          <div v-else class="space-y-4">
            <section
              v-for="(platform, platformIndex) in form.platforms"
              :key="platform.client_uid"
              class="rounded-lg border border-gray-200 dark:border-dark-700"
            >
              <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-800/60">
                <div class="flex items-center gap-2">
                  <span
                    class="inline-flex items-center rounded-md px-2 py-1 text-xs font-medium"
                    :class="providerBadgeClass(platform.provider)"
                  >
                    {{ providerLabel(platform.provider) }}
                  </span>
                  <span class="text-xs text-gray-500 dark:text-gray-400">
                    {{ t('admin.upstreamChannels.form.poolCount', { count: platform.key_pools.length }) }}
                  </span>
                </div>
                <button
                  type="button"
                  class="rounded-md p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                  :title="t('admin.upstreamChannels.form.removePlatform')"
                  @click="removePlatform(platformIndex)"
                >
                  <Icon name="trash" size="sm" />
                </button>
              </div>

              <div class="space-y-5 p-4">
                <div class="grid gap-4 lg:grid-cols-[180px_160px_minmax(0,1fr)_minmax(0,1.4fr)]">
                  <div>
                    <label class="input-label">{{ t('admin.upstreamChannels.form.provider') }}</label>
                    <Select
                      v-model="platform.provider"
                      :options="providerOptionsForPlatform(platform)"
                      @change="applyProviderDefaults(platform)"
                    />
                  </div>
                  <div>
                    <label class="input-label">{{ t('admin.upstreamChannels.form.status') }}</label>
                    <Select v-model="platform.status" :options="statusOptions" />
                  </div>
                  <div>
                    <label class="input-label">{{ t('admin.upstreamChannels.form.displayName') }}</label>
                    <input v-model="platform.display_name" type="text" class="input" />
                  </div>
                  <div>
                    <label class="input-label">{{ t('admin.upstreamChannels.form.baseUrl') }}</label>
                    <input v-model="platform.base_url" type="url" class="input" />
                  </div>
                </div>

                <div class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-800">
                  <div>
                    <h5 class="text-sm font-medium text-gray-900 dark:text-white">
                      {{ t('admin.upstreamChannels.form.groups') }}
                    </h5>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{ t('admin.upstreamChannels.form.groupsHint') }}
                    </p>
                  </div>
                  <button
                    type="button"
                    class="btn btn-secondary px-3 py-1.5"
                    @click="addPool(platform)"
                  >
                    <Icon name="plus" size="sm" class="mr-1 inline-block" />
                    {{ t('admin.upstreamChannels.form.addPool') }}
                  </button>
                </div>

                <div v-if="platform.key_pools.length === 0" class="rounded-lg border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
                  {{ t('admin.upstreamChannels.form.noPools') }}
                </div>

                <div v-else class="space-y-4">
                  <section
                    v-for="(pool, poolIndex) in platform.key_pools"
                    :key="pool.client_uid"
                    class="rounded-lg bg-gray-50 p-4 dark:bg-dark-800/60"
                  >
                    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <div class="text-sm font-medium text-gray-900 dark:text-white">
                          {{ pool.name || t('admin.upstreamChannels.form.unnamedPool') }}
                        </div>
                        <div class="text-xs text-gray-500 dark:text-gray-400">
                          {{ pool.group_name || '-' }}
                        </div>
                      </div>
                      <button
                        type="button"
                        class="rounded-md p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                        :title="t('admin.upstreamChannels.form.removePool')"
                        @click="removePool(platform, poolIndex)"
                      >
                        <Icon name="trash" size="sm" />
                      </button>
                    </div>

                    <div class="space-y-4">
                      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_150px_140px]">
                        <div>
                          <label class="input-label">{{ t('admin.upstreamChannels.form.poolName') }}</label>
                          <input v-model="pool.name" type="text" class="input" />
                        </div>
                        <div>
                          <label class="input-label">{{ t('admin.upstreamChannels.form.groupName') }}</label>
                          <Select
                            :model-value="groupSelectValue(platform, pool)"
                            :options="groupOptionsForPlatform(platform)"
                            :placeholder="t('admin.upstreamChannels.form.groupNamePlaceholder')"
                            :searchable="true"
                            :disabled="groupsLoading"
                            @change="(value, option) => handleGroupSelect(platform, pool, value, option)"
                          />
                          <input
                            v-if="isCreatingGroup(platform, pool)"
                            v-model="pool.group_name"
                            type="text"
                            class="input mt-2"
                            :placeholder="t('admin.upstreamChannels.form.newGroupNamePlaceholder')"
                          />
                        </div>
                        <div>
                          <label class="input-label">{{ t('admin.upstreamChannels.form.status') }}</label>
                          <Select v-model="pool.status" :options="statusOptions" />
                        </div>
                        <div>
                          <label class="input-label">{{ t('admin.upstreamChannels.form.syncedGroup') }}</label>
                          <div class="readonly-field">{{ formatSyncId(pool.synced_group_id) }}</div>
                        </div>
                      </div>

                      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
                        <div>
                          <label class="input-label">{{ t('admin.upstreamChannels.form.groupMultiplier') }}</label>
                          <input v-model.number="pool.group_rate_multiplier" type="number" min="0" step="0.0001" class="input" />
                        </div>
                        <div>
                          <label class="input-label">{{ t('admin.upstreamChannels.form.accountMultiplier') }}</label>
                          <input v-model.number="pool.account_rate_multiplier" type="number" min="0" step="0.0001" class="input" />
                        </div>
                        <div>
                          <label class="input-label">{{ t('admin.upstreamChannels.form.loadFactor') }}</label>
                          <input v-model.number="pool.load_factor" type="number" min="0" step="1" class="input" />
                        </div>
                        <div>
                          <label class="input-label">{{ t('admin.upstreamChannels.form.concurrency') }}</label>
                          <input v-model.number="pool.concurrency" type="number" min="0" step="1" class="input" />
                        </div>
                      </div>

                      <div class="border-t border-gray-200 pt-4 dark:border-dark-700">
                        <div class="mb-3">
                          <h6 class="text-sm font-medium text-gray-900 dark:text-white">
                            {{ t('admin.upstreamChannels.form.accountKey') }}
                          </h6>
                          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                            {{ t('admin.upstreamChannels.form.accountKeyHint') }}
                          </p>
                        </div>

                        <div
                          v-for="accountKey in [poolAccountKey(pool)]"
                          :key="accountKey.client_uid"
                        >
                          <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_150px_140px]">
                            <div>
                              <label class="input-label">{{ t('admin.upstreamChannels.form.accountName') }}</label>
                              <input v-model="accountKey.name" type="text" class="input" />
                            </div>
                            <div>
                              <label class="input-label">{{ t('admin.upstreamChannels.form.apiKey') }}</label>
                              <input
                                v-model="accountKey.api_key"
                                type="password"
                                autocomplete="new-password"
                                class="input"
                                :placeholder="accountKey.api_key_masked || t('admin.upstreamChannels.form.apiKeyPlaceholder')"
                              />
                            </div>
                            <div>
                              <label class="input-label">{{ t('admin.upstreamChannels.form.status') }}</label>
                              <Select v-model="accountKey.status" :options="statusOptions" />
                            </div>
                            <div>
                              <label class="input-label">{{ t('admin.upstreamChannels.form.syncedAccount') }}</label>
                              <div class="readonly-field">{{ formatSyncId(accountKey.synced_account_id) }}</div>
                            </div>
                          </div>

                          <div class="mt-3 text-sm text-gray-600 dark:text-gray-400">
                            <span>{{ t('admin.upstreamChannels.form.lastTest') }}: {{ formatKeyTest(accountKey) }}</span>
                            <span v-if="accountKey.last_test_message" class="ml-2 text-xs text-gray-400">
                              {{ accountKey.last_test_message }}
                            </span>
                          </div>
                        </div>
                      </div>
                    </div>
                  </section>
                </div>
              </div>
            </section>
          </div>
        </section>
      </form>

      <template #footer>
        <div class="flex flex-wrap justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeDialog">
            {{ t('common.cancel') }}
          </button>
          <button
            v-if="editingChannel"
            type="button"
            class="btn btn-secondary"
            :disabled="previewingId === editingChannel.id"
            @click="handlePreview(editingChannel)"
          >
            {{ t('admin.upstreamChannels.actions.preview') }}
          </button>
          <button
            v-if="editingChannel"
            type="button"
            class="btn btn-secondary"
            :disabled="syncingId === editingChannel.id"
            @click="handleSync(editingChannel)"
          >
            {{ t('admin.upstreamChannels.actions.sync') }}
          </button>
          <button
            type="submit"
            form="upstream-channel-form"
            class="btn btn-primary"
            :disabled="submitting"
          >
            {{
              submitting
                ? t('common.submitting')
                : editingChannel
                  ? t('common.update')
                  : t('common.create')
            }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showPreviewDialog"
      :title="previewTitle"
      width="wide"
      @close="showPreviewDialog = false"
    >
      <div class="space-y-4">
        <div class="rounded-lg border border-primary-100 bg-primary-50 p-3 text-sm text-primary-800 dark:border-primary-900/50 dark:bg-primary-900/20 dark:text-primary-200">
          {{ t('admin.upstreamChannels.preview.explain') }}
        </div>

        <div v-if="previewSummaryCards.length > 0" class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <div
            v-for="card in previewSummaryCards"
            :key="card.key"
            class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
          >
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ card.label }}</div>
            <div class="mt-1 text-lg font-semibold" :class="card.valueClass">{{ card.value }}</div>
            <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ card.description }}</div>
          </div>
        </div>

        <div v-if="syncPreviewResult?.warnings?.length" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-700 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-300">
          <div class="mb-2 font-medium">{{ t('admin.upstreamChannels.preview.warnings') }}</div>
          <div v-for="warning in syncPreviewResult.warnings" :key="warning">
            {{ syncWarningLabel(warning) }}
          </div>
        </div>

        <div class="grid gap-4 lg:grid-cols-2">
          <section>
            <div class="mb-2 flex items-center justify-between gap-3">
              <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t('admin.upstreamChannels.preview.groups') }}
              </h4>
              <span class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.upstreamChannels.preview.itemCount', { count: syncPreviewResult?.groups?.length || 0 }) }}
              </span>
            </div>
            <div class="max-h-80 overflow-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <div v-if="!syncPreviewResult?.groups?.length" class="p-4 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.upstreamChannels.preview.noChanges') }}
              </div>
              <div
	                v-for="item in syncPreviewResult?.groups || []"
	                :key="`${item.action}-${item.id}-${item.name}`"
	                class="border-b border-gray-100 p-3 last:border-b-0 dark:border-dark-800"
              >
                <div class="flex items-center justify-between gap-3">
                  <span class="font-medium text-gray-900 dark:text-white">{{ item.name || item.id || '-' }}</span>
                  <span class="rounded px-2 py-0.5 text-xs font-medium" :class="syncActionClass(item.action)">
                    {{ syncActionLabel(item.action) }}
                  </span>
                </div>
                <div class="mt-1 flex flex-wrap gap-2 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ syncTargetLabel(item.target || 'group') }}</span>
                  <span v-if="item.status">{{ providerLabel(item.status) }}</span>
                  <span v-if="item.id">{{ t('admin.upstreamChannels.preview.syncedId', { id: item.id }) }}</span>
                </div>
                <div v-if="item.message" class="mt-2 rounded-md bg-amber-50 px-2 py-1.5 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                  {{ syncWarningLabel(item.message) }}
                </div>
              </div>
            </div>
          </section>

          <section>
            <div class="mb-2 flex items-center justify-between gap-3">
              <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t('admin.upstreamChannels.preview.accounts') }}
              </h4>
              <span class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.upstreamChannels.preview.itemCount', { count: syncPreviewResult?.accounts?.length || 0 }) }}
              </span>
            </div>
            <div class="max-h-80 overflow-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <div v-if="!syncPreviewResult?.accounts?.length" class="p-4 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.upstreamChannels.preview.noChanges') }}
              </div>
              <div
	                v-for="item in syncPreviewResult?.accounts || []"
	                :key="`${item.action}-${item.id}-${item.name}`"
	                class="border-b border-gray-100 p-3 last:border-b-0 dark:border-dark-800"
              >
                <div class="flex items-center justify-between gap-3">
                  <span class="font-medium text-gray-900 dark:text-white">{{ item.name || item.id || '-' }}</span>
                  <span class="rounded px-2 py-0.5 text-xs font-medium" :class="syncActionClass(item.action)">
                    {{ syncActionLabel(item.action) }}
                  </span>
                </div>
                <div class="mt-1 flex flex-wrap gap-2 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ syncTargetLabel(item.target || 'account') }}</span>
                  <span v-if="item.status">{{ providerLabel(item.status) }}</span>
                  <span v-if="item.id">{{ t('admin.upstreamChannels.preview.syncedId', { id: item.id }) }}</span>
                </div>
                <div v-if="item.message" class="mt-2 rounded-md bg-amber-50 px-2 py-1.5 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                  {{ syncWarningLabel(item.message) }}
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>
    </BaseDialog>

    <BaseDialog
      :show="showTestDialog"
      :title="t('admin.upstreamChannels.test.title')"
      width="wide"
      @close="showTestDialog = false"
    >
      <div class="space-y-4">
        <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm text-gray-600 dark:border-dark-700 dark:bg-dark-800/60 dark:text-gray-300">
          {{ t('admin.upstreamChannels.test.explain') }}
        </div>
        <div v-if="testSummaryCards.length > 0" class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div
            v-for="card in testSummaryCards"
            :key="card.key"
            class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
          >
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ card.label }}</div>
            <div class="mt-1 text-lg font-semibold" :class="card.valueClass">{{ card.value }}</div>
          </div>
        </div>
        <div v-if="normalizedTestResults.length === 0" class="text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.upstreamChannels.test.noResults') }}
        </div>
        <div
	          v-for="result in normalizedTestResults"
	          :key="`${result.key_id || result.key_name || 'channel'}-${result.status}`"
	          class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
	        >
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <div class="font-medium text-gray-900 dark:text-white">
                {{ testResultTitle(result) }}
              </div>
              <div class="mt-1 flex flex-wrap gap-2 text-xs text-gray-500 dark:text-gray-400">
                <span v-if="result.platform">{{ providerLabel(result.platform) }}</span>
                <span v-if="result.group_name">{{ t('admin.upstreamChannels.test.groupName', { name: result.group_name }) }}</span>
                <span v-if="result.account_id">{{ t('admin.upstreamChannels.test.accountId', { id: result.account_id }) }}</span>
              </div>
            </div>
            <span class="rounded px-2 py-0.5 text-xs font-medium" :class="testStatusClass(result.status)">
              {{ testStatusLabel(result.status) }}
            </span>
          </div>
          <div class="mt-1 text-sm text-gray-600 dark:text-gray-400">
            {{ t('admin.upstreamChannels.test.latency') }}：{{ formatLatency(result.latency_ms) }}
          </div>
          <div v-if="result.message" class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.upstreamChannels.test.response') }}：{{ testMessageLabel(result.message) }}
          </div>
          <div v-if="result.tested_at" class="mt-1 text-xs text-gray-400">
            {{ t('admin.upstreamChannels.test.testedAt') }}：{{ formatDateTime(result.tested_at) }}
          </div>
        </div>
      </div>
    </BaseDialog>

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.upstreamChannels.deleteTitle')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { adminAPI } from '@/api/admin'
import upstreamChannelsAPI from '@/api/admin/upstreamChannels'
import type {
  CreateUpstreamChannelRequest,
  UpstreamChannel,
  UpstreamKeyPayload,
  UpstreamProvider,
  UpstreamResourceStatus,
  UpstreamSyncPreviewResponse,
  UpstreamTestResponse,
  UpstreamTestResult,
  UpdateUpstreamChannelRequest,
} from '@/api/admin/upstreamChannels'
import type { AdminGroup, GroupPlatform } from '@/types'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'

type SupportedProvider = 'anthropic' | 'openai'
const CREATE_GROUP_OPTION_VALUE = '__create_upstream_group__'

interface FormKey {
  client_uid: string
  id?: number
  name: string
  api_key: string
  api_key_masked: string
  status: UpstreamResourceStatus
  synced_account_id: number | null
  last_test_latency_ms: number | null
  last_test_status: string | null
  last_test_message: string | null
}

interface FormPool {
  client_uid: string
  id?: number
  name: string
  group_name: string
  group_rate_multiplier: number
  account_rate_multiplier: number
  load_factor: number
  concurrency: number
  status: UpstreamResourceStatus
  synced_group_id: number | null
  keys: FormKey[]
}

interface FormPlatform {
  client_uid: string
  id?: number
  provider: UpstreamProvider
  display_name: string
  base_url: string
  status: UpstreamResourceStatus
  key_pools: FormPool[]
}

interface SummaryCard {
  key: string
  label: string
  description?: string
  value: number
  valueClass: string
}

const { t } = useI18n()
const appStore = useAppStore()

const supportedProviders: SupportedProvider[] = ['anthropic', 'openai']
const providerDefaults: Record<SupportedProvider, { baseUrl: string; displayNameKey: string }> = {
  anthropic: {
    baseUrl: 'https://api.anthropic.com',
    displayNameKey: 'admin.upstreamChannels.providers.anthropic',
  },
  openai: {
    baseUrl: 'https://api.openai.com/v1',
    displayNameKey: 'admin.upstreamChannels.providers.openai',
  },
}

function isSupportedProvider(provider: UpstreamProvider): provider is SupportedProvider {
  return provider === 'anthropic' || provider === 'openai'
}

let uidCounter = 0
let abortController: AbortController | null = null
let searchTimeout: ReturnType<typeof setTimeout> | null = null

const channels = ref<UpstreamChannel[]>([])
const upstreamGroups = ref<AdminGroup[]>([])
const loading = ref(false)
const groupsLoading = ref(false)
const searchQuery = ref('')
const filters = reactive({
  provider: '',
})
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
})
const sortState = reactive({
  sort_by: 'updated_at',
  sort_order: 'desc' as 'asc' | 'desc',
})

const showDialog = ref(false)
const submitting = ref(false)
const editingChannel = ref<UpstreamChannel | null>(null)
const editingLoadingId = ref<number | null>(null)

const showDeleteDialog = ref(false)
const deletingChannel = ref<UpstreamChannel | null>(null)
const previewingId = ref<number | null>(null)
const syncingId = ref<number | null>(null)
const testingId = ref<number | null>(null)
const showPreviewDialog = ref(false)
const showTestDialog = ref(false)
const syncPreviewResult = ref<UpstreamSyncPreviewResponse | null>(null)
const previewTitle = ref('')
const testResult = ref<UpstreamTestResponse | null>(null)

const form = reactive({
  name: '',
  description: '',
  platforms: [] as FormPlatform[],
})

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.upstreamChannels.columns.name'), sortable: true },
  { key: 'platforms', label: t('admin.upstreamChannels.columns.platforms'), sortable: false },
  { key: 'key_count', label: t('admin.upstreamChannels.columns.keys'), sortable: false },
  { key: 'updated_at', label: t('admin.upstreamChannels.columns.updatedAt'), sortable: true },
  { key: 'actions', label: t('admin.upstreamChannels.columns.actions'), sortable: false },
])

const statusOptions = computed(() => [
  { value: 'active', label: t('admin.upstreamChannels.status.active') },
  { value: 'disabled', label: t('admin.upstreamChannels.status.disabled') },
])

const providerOptions = computed(() => supportedProviders.map(provider => ({
  value: provider,
  label: providerLabel(provider),
})))

const providerFilterOptions = computed(() => [
  { value: '', label: t('admin.upstreamChannels.filters.allProviders') },
  ...providerOptions.value,
])

const deleteConfirmMessage = computed(() => {
  const name = deletingChannel.value?.name || ''
  return t('admin.upstreamChannels.deleteConfirm', { name })
})

const previewSummaryCards = computed<SummaryCard[]>(() => {
  const summary = syncPreviewResult.value?.summary || {}
  const orderedKeys = [
    'create_group',
    'update_group',
    'create_account',
    'update_account',
    'skip_account',
    'error',
  ]
  const keys = [
    ...orderedKeys.filter(key => summary[key] != null),
    ...Object.keys(summary).filter(key => !orderedKeys.includes(key)),
  ]
  return keys.map(key => ({
    key,
    label: syncSummaryLabel(key),
    description: syncSummaryDescription(key),
    value: Number(summary[key] || 0),
    valueClass: syncSummaryValueClass(key),
  }))
})

const normalizedTestResults = computed<UpstreamTestResult[]>(() => {
  const result = testResult.value
  if (!result) return []
  if (result.results?.length) return result.results
  if (result.status || result.message || result.latency_ms != null) {
    return [{
      status: result.status || 'unknown',
      latency_ms: result.latency_ms ?? null,
      message: result.message,
    }]
  }
  return []
})

const testSummaryCards = computed<SummaryCard[]>(() => {
  const counts = normalizedTestResults.value.reduce<Record<string, number>>((acc, result) => {
    const key = testStatusKey(result.status)
    acc[key] = (acc[key] || 0) + 1
    return acc
  }, {})
  return ['operational', 'authError', 'failed', 'unknown']
    .filter(key => counts[key] > 0)
    .map(key => ({
      key,
      label: testSummaryLabel(key),
      value: counts[key],
      valueClass: testSummaryValueClass(key),
    }))
})

function nextUid(prefix: string): string {
  uidCounter += 1
  return `${prefix}-${Date.now()}-${uidCounter}`
}

function providerLabel(provider: UpstreamProvider): string {
  if (isSupportedProvider(provider)) {
    return t(providerDefaults[provider].displayNameKey)
  }
  return provider || t('admin.upstreamChannels.providers.unknown')
}

function providerToGroupPlatform(provider: UpstreamProvider): GroupPlatform | null {
  switch (normalizeProviderValue(provider)) {
    case 'anthropic':
    case 'openai':
    case 'gemini':
    case 'antigravity':
    case 'grok':
      return normalizeProviderValue(provider) as GroupPlatform
    default:
      return null
  }
}

function providerBadgeClass(provider: UpstreamProvider): string {
  if (provider === 'anthropic') {
    return 'bg-orange-50 text-orange-700 dark:bg-orange-900/20 dark:text-orange-300'
  }
  if (provider === 'openai') {
    return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
  }
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
}

function createKeyForm(seed?: Partial<FormKey>): FormKey {
  return {
    client_uid: nextUid('key'),
    id: seed?.id,
    name: seed?.name || '',
    api_key: '',
    api_key_masked: seed?.api_key_masked || '',
    status: seed?.status || 'active',
    synced_account_id: seed?.synced_account_id ?? null,
    last_test_latency_ms: seed?.last_test_latency_ms ?? null,
    last_test_status: seed?.last_test_status ?? null,
    last_test_message: seed?.last_test_message ?? null,
  }
}

function normalizedPoolKeys(seed?: Partial<FormPool>): FormKey[] {
  const keys = seed?.keys || []
  if (keys.length > 0) return [keys[0]]
  return [createKeyForm()]
}

function createPoolForm(provider: UpstreamProvider, seed?: Partial<FormPool>): FormPool {
  const label = providerLabel(provider)
  return {
    client_uid: nextUid('pool'),
    id: seed?.id,
    name: seed?.name || `${label} ${t('admin.upstreamChannels.form.defaultPoolSuffix')}`,
    group_name: seed?.group_name || `${label} ${t('admin.upstreamChannels.form.defaultGroupSuffix')}`,
    group_rate_multiplier: seed?.group_rate_multiplier ?? 1,
    account_rate_multiplier: seed?.account_rate_multiplier ?? 1,
    load_factor: seed?.load_factor ?? 1,
    concurrency: seed?.concurrency ?? 1,
    status: seed?.status || 'active',
    synced_group_id: seed?.synced_group_id ?? null,
    keys: normalizedPoolKeys(seed),
  }
}

function createPlatformForm(provider: UpstreamProvider, seed?: Partial<FormPlatform>): FormPlatform {
  const supported = isSupportedProvider(provider) ? provider : null
  const label = supported ? providerLabel(supported) : providerLabel(provider)
  const baseUrl = supported ? providerDefaults[supported].baseUrl : ''
  return {
    client_uid: nextUid('platform'),
    id: seed?.id,
    provider,
    display_name: seed?.display_name || label,
    base_url: seed?.base_url || baseUrl,
    status: seed?.status || 'active',
    key_pools: seed?.key_pools || [createPoolForm(provider)],
  }
}

function apiToForm(channel: UpstreamChannel): FormPlatform[] {
  return (channel.platforms || []).map(platform => createPlatformForm(platform.provider, {
    id: platform.id,
    display_name: platform.display_name,
    base_url: platform.base_url,
    status: platform.status,
    key_pools: (platform.key_pools || []).map(pool => createPoolForm(platform.provider, {
      id: pool.id,
      name: pool.name,
      group_name: pool.group_name,
      group_rate_multiplier: pool.group_rate_multiplier,
      account_rate_multiplier: pool.account_rate_multiplier,
      load_factor: pool.load_factor,
      concurrency: pool.concurrency,
      status: pool.status,
      synced_group_id: pool.synced_group_id,
      keys: (pool.keys || []).map(key => createKeyForm({
        id: key.id,
        name: key.name,
        api_key_masked: key.api_key_masked,
        status: key.status,
        synced_account_id: key.synced_account_id,
        last_test_latency_ms: key.last_test_latency_ms,
        last_test_status: key.last_test_status,
        last_test_message: key.last_test_message,
      })),
    })),
  }))
}

function resetForm() {
  form.name = ''
  form.description = ''
  form.platforms = [
    createPlatformForm('anthropic'),
    createPlatformForm('openai'),
  ]
}

function assignForm(channel: UpstreamChannel) {
  form.name = channel.name || ''
  form.description = channel.description || ''
  form.platforms = apiToForm(channel)
}

function countKeys(channel: UpstreamChannel): number {
  return (channel.platforms || []).reduce((total, platform) => {
    return total + (platform.key_pools || []).reduce((poolTotal, pool) => {
      return poolTotal + (pool.keys || []).length
    }, 0)
  }, 0)
}

function formatDateTime(value: string): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function formatSyncId(value: number | null | undefined): string {
  return value ? `#${value}` : '-'
}

function formatLatency(value: number | null | undefined): string {
  return value == null ? '-' : `${value} ms`
}

function formatKeyTest(key: FormKey): string {
  if (!key.last_test_status && key.last_test_latency_ms == null) return '-'
  const status = testStatusLabel(key.last_test_status || 'unknown')
  const latency = formatLatency(key.last_test_latency_ms)
  return latency === '-' ? status : `${status} / ${latency}`
}

function toNumber(value: unknown, fallback: number): number {
  const num = Number(value)
  return Number.isFinite(num) ? num : fallback
}

function normalizeProviderValue(provider: UpstreamProvider): string {
  return String(provider || '').trim().toLowerCase()
}

function normalizeNameValue(name: string): string {
  return name.trim().toLowerCase()
}

function uniqueFormName(base: string, existingNames: string[]): string {
  const normalized = new Set(existingNames.map(normalizeNameValue).filter(Boolean))
  const trimmed = base.trim()
  if (!normalized.has(normalizeNameValue(trimmed))) return trimmed
  for (let index = 2; ; index += 1) {
    const candidate = `${trimmed} ${index}`
    if (!normalized.has(normalizeNameValue(candidate))) return candidate
  }
}

function providerOptionsForPlatform(current: FormPlatform) {
  const currentProvider = normalizeProviderValue(current.provider)
  const usedProviders = new Set(
    form.platforms
      .filter(platform => platform.client_uid !== current.client_uid)
      .map(platform => normalizeProviderValue(platform.provider))
  )
  return supportedProviders.map(provider => ({
    value: provider,
    label: providerLabel(provider),
    disabled: usedProviders.has(provider) && provider !== currentProvider,
  }))
}

function groupsForPlatform(platform: FormPlatform): AdminGroup[] {
  const groupPlatform = providerToGroupPlatform(platform.provider)
  if (!groupPlatform) return []
  return upstreamGroups.value.filter(group => group.platform === groupPlatform)
}

function findGroupByName(platform: FormPlatform, name: string): AdminGroup | undefined {
  const normalized = normalizeNameValue(name)
  if (!normalized) return undefined
  return groupsForPlatform(platform).find(group => normalizeNameValue(group.name) === normalized)
}

function groupSelectValue(platform: FormPlatform, pool: FormPool): string {
  const groups = groupsForPlatform(platform)
  if (pool.synced_group_id && groups.some(group => group.id === pool.synced_group_id)) {
    return `group:${pool.synced_group_id}`
  }
  const matchedGroup = findGroupByName(platform, pool.group_name)
  if (matchedGroup) {
    return `group:${matchedGroup.id}`
  }
  return CREATE_GROUP_OPTION_VALUE
}

function groupOptionsForPlatform(platform: FormPlatform) {
  const groups = groupsForPlatform(platform)
  const options: Array<{
    value: string
    label: string
    description: string
    group?: AdminGroup
  }> = groups.map(group => ({
    value: `group:${group.id}`,
    label: group.name,
    description: `${providerLabel(group.platform)} · x${group.rate_multiplier}`,
    group,
  }))
  options.push({
    value: CREATE_GROUP_OPTION_VALUE,
    label: t('admin.upstreamChannels.form.createGroupOption'),
    description: t('admin.upstreamChannels.form.createGroupDescription'),
  })
  return options
}

function isCreatingGroup(platform: FormPlatform, pool: FormPool): boolean {
  return groupSelectValue(platform, pool) === CREATE_GROUP_OPTION_VALUE
}

function handleGroupSelect(platform: FormPlatform, pool: FormPool, value: string | number | boolean | null, option: unknown) {
  const selectedGroup = (option as { group?: AdminGroup } | null)?.group
  if (selectedGroup) {
    pool.group_name = selectedGroup.name
    pool.synced_group_id = selectedGroup.id
    pool.group_rate_multiplier = selectedGroup.rate_multiplier
    return
  }
  if (value === CREATE_GROUP_OPTION_VALUE && findGroupByName(platform, pool.group_name)) {
    pool.group_name = ''
  }
  pool.synced_group_id = null
}

async function reload() {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  loading.value = true
  try {
    const response = await upstreamChannelsAPI.list({
      page: pagination.page,
      page_size: pagination.page_size,
      provider: filters.provider || undefined,
      search: searchQuery.value.trim() || undefined,
      sort_by: sortState.sort_by,
      sort_order: sortState.sort_order,
    }, { signal: ctrl.signal })

    if (ctrl.signal.aborted || abortController !== ctrl) return
    channels.value = response.items || []
    pagination.total = response.total || 0
  } catch (error: unknown) {
    const e = error as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamChannels.loadError')))
  } finally {
    if (abortController === ctrl) {
      loading.value = false
      abortController = null
    }
  }
}

async function loadUpstreamGroups() {
  groupsLoading.value = true
  try {
    upstreamGroups.value = await adminAPI.groups.getAll()
  } catch (error: unknown) {
    console.error('Error loading upstream group options:', error)
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamChannels.groupsLoadError')))
  } finally {
    groupsLoading.value = false
  }
}

function handleSearch() {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    reload()
  }, 300)
}

function handlePageChange(page: number) {
  pagination.page = page
  reload()
}

function handlePageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  reload()
}

function handleSort(key: string, order: 'asc' | 'desc') {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  reload()
}

function openCreateDialog() {
  editingChannel.value = null
  resetForm()
  showDialog.value = true
}

async function openEditDialog(row: UpstreamChannel) {
  editingLoadingId.value = row.id
  try {
    const channel = await upstreamChannelsAPI.get(row.id)
    editingChannel.value = channel
    assignForm(channel)
    showDialog.value = true
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamChannels.loadOneError')))
  } finally {
    editingLoadingId.value = null
  }
}

function closeDialog() {
  showDialog.value = false
  editingChannel.value = null
  resetForm()
}

function hasProvider(provider: UpstreamProvider): boolean {
  return form.platforms.some(platform => normalizeProviderValue(platform.provider) === normalizeProviderValue(provider))
}

function addPlatform(provider: UpstreamProvider) {
  if (hasProvider(provider)) return
  form.platforms.push(createPlatformForm(provider))
}

function removePlatform(index: number) {
  form.platforms.splice(index, 1)
}

function applyProviderDefaults(platform: FormPlatform) {
  if (!isSupportedProvider(platform.provider)) return
  platform.display_name = providerLabel(platform.provider)
  platform.base_url = providerDefaults[platform.provider].baseUrl
}

function addPool(platform: FormPlatform) {
  const pool = createPoolForm(platform.provider)
  pool.name = uniqueFormName(pool.name, platform.key_pools.map(item => item.name))
  pool.group_name = uniqueFormName(pool.group_name, platform.key_pools.map(item => item.group_name))
  platform.key_pools.push(pool)
}

function removePool(platform: FormPlatform, index: number) {
  platform.key_pools.splice(index, 1)
}

function poolAccountKey(pool: FormPool): FormKey {
  if (pool.keys.length === 0) {
    pool.keys.push(createKeyForm({
      name: pool.group_name || pool.name,
      status: pool.status,
    }))
  }
  if (pool.keys.length > 1) {
    pool.keys.splice(1)
  }
  return pool.keys[0]
}

function keyNameFromSecret(secret: string, index: number): string {
  const trimmed = secret.trim()
  if (trimmed.length <= 10) return `Key ${index}`
  return `Key ${trimmed.slice(0, 4)}...${trimmed.slice(-4)}`
}

function validateForm(): boolean {
  if (!form.name.trim()) {
    appStore.showError(t('admin.upstreamChannels.validation.nameRequired'))
    return false
  }
  if (form.platforms.length === 0) {
    appStore.showError(t('admin.upstreamChannels.validation.platformRequired'))
    return false
  }

  const seenProviders = new Set<string>()
  for (const platform of form.platforms) {
    const providerKey = normalizeProviderValue(platform.provider)
    if (seenProviders.has(providerKey)) {
      appStore.showError(t('admin.upstreamChannels.validation.platformDuplicate', { platform: providerLabel(platform.provider) }))
      return false
    }
    seenProviders.add(providerKey)
    if (!platform.display_name.trim() || !platform.base_url.trim()) {
      appStore.showError(t('admin.upstreamChannels.validation.platformInvalid', { platform: providerLabel(platform.provider) }))
      return false
    }
    const seenPoolNames = new Set<string>()
    for (const pool of platform.key_pools) {
      if (!pool.name.trim() || !pool.group_name.trim()) {
        appStore.showError(t('admin.upstreamChannels.validation.poolInvalid', { platform: providerLabel(platform.provider) }))
        return false
      }
      const poolNameKey = normalizeNameValue(pool.name)
      if (seenPoolNames.has(poolNameKey)) {
        appStore.showError(t('admin.upstreamChannels.validation.poolDuplicate', { pool: pool.name }))
        return false
      }
      seenPoolNames.add(poolNameKey)
      if (
        toNumber(pool.group_rate_multiplier, -1) < 0 ||
        toNumber(pool.account_rate_multiplier, -1) < 0 ||
        toNumber(pool.load_factor, -1) < 0 ||
        toNumber(pool.concurrency, -1) < 0
      ) {
        appStore.showError(t('admin.upstreamChannels.validation.poolNumbersInvalid', { pool: pool.name }))
        return false
      }
      const accountKey = poolAccountKey(pool)
      if (!accountKey.id && !accountKey.api_key.trim()) {
        appStore.showError(t('admin.upstreamChannels.validation.newKeyRequired', { pool: pool.name }))
        return false
      }
    }
  }

  return true
}

function formToPayload(): CreateUpstreamChannelRequest {
  return {
    name: form.name.trim(),
    description: form.description.trim() || undefined,
    platforms: form.platforms.map(platform => ({
      id: platform.id,
      provider: platform.provider,
      display_name: platform.display_name.trim(),
      base_url: platform.base_url.trim(),
      status: platform.status,
      key_pools: platform.key_pools.map(pool => {
        const matchedGroup = findGroupByName(platform, pool.group_name)
        const accountKey = poolAccountKey(pool)
        return {
          id: pool.id,
          name: pool.name.trim(),
          group_name: pool.group_name.trim(),
          group_rate_multiplier: toNumber(pool.group_rate_multiplier, 1),
          account_rate_multiplier: toNumber(pool.account_rate_multiplier, 1),
          load_factor: Math.trunc(toNumber(pool.load_factor, 1)),
          concurrency: toNumber(pool.concurrency, 1),
          status: pool.status,
          synced_group_id: pool.synced_group_id ?? matchedGroup?.id ?? undefined,
          keys: [accountKey]
            .filter(key => key.id || key.name.trim() || key.api_key.trim())
            .map((key): UpstreamKeyPayload => {
              const payload: UpstreamKeyPayload = {
                id: key.id,
                name: key.name.trim() || pool.group_name.trim() || keyNameFromSecret(key.api_key, 1),
                status: key.status,
                synced_account_id: key.synced_account_id ?? undefined,
              }
              if (key.api_key.trim()) {
                payload.api_key = key.api_key.trim()
              }
              return payload
            }),
        }
      }),
    })),
  }
}

async function handleSubmit() {
  if (submitting.value || !validateForm()) return

  submitting.value = true
  try {
    const payload = formToPayload()
    if (editingChannel.value) {
      await upstreamChannelsAPI.update(editingChannel.value.id, payload as UpdateUpstreamChannelRequest)
      appStore.showSuccess(t('admin.upstreamChannels.updateSuccess'))
    } else {
      await upstreamChannelsAPI.create(payload)
      appStore.showSuccess(t('admin.upstreamChannels.createSuccess'))
    }
    closeDialog()
    reload()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(
      error,
      editingChannel.value ? t('admin.upstreamChannels.updateError') : t('admin.upstreamChannels.createError')
    ))
  } finally {
    submitting.value = false
  }
}

function handleDelete(channel: UpstreamChannel) {
  deletingChannel.value = channel
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (!deletingChannel.value) return
  try {
    await upstreamChannelsAPI.remove(deletingChannel.value.id)
    appStore.showSuccess(t('admin.upstreamChannels.deleteSuccess'))
    showDeleteDialog.value = false
    deletingChannel.value = null
    reload()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamChannels.deleteError')))
  }
}

async function handlePreview(channel: UpstreamChannel) {
  if (previewingId.value != null) return
  previewingId.value = channel.id
  try {
    syncPreviewResult.value = await upstreamChannelsAPI.syncPreview(channel.id)
    previewTitle.value = t('admin.upstreamChannels.preview.title', { name: channel.name })
    showPreviewDialog.value = true
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamChannels.preview.failed')))
  } finally {
    previewingId.value = null
  }
}

async function handleSync(channel: UpstreamChannel) {
  if (syncingId.value != null) return
  syncingId.value = channel.id
  try {
    syncPreviewResult.value = await upstreamChannelsAPI.sync(channel.id)
    previewTitle.value = t('admin.upstreamChannels.syncResultTitle', { name: channel.name })
    showPreviewDialog.value = true
    appStore.showSuccess(t('admin.upstreamChannels.syncSuccess'))
    loadUpstreamGroups()
    reload()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamChannels.syncFailed')))
  } finally {
    syncingId.value = null
  }
}

async function handleTest(channel: UpstreamChannel) {
  if (testingId.value != null) return
  testingId.value = channel.id
  try {
    testResult.value = await upstreamChannelsAPI.test(channel.id)
    showTestDialog.value = true
    appStore.showSuccess(t('admin.upstreamChannels.test.success'))
    reload()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.upstreamChannels.test.failed')))
  } finally {
    testingId.value = null
  }
}

function readableKey(value: string): string {
  return value
    .split('_')
    .filter(Boolean)
    .map(part => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function syncActionLabel(action: string): string {
  switch (action) {
    case 'create_group':
      return t('admin.upstreamChannels.preview.actions.createGroup')
    case 'update_group':
      return t('admin.upstreamChannels.preview.actions.updateGroup')
    case 'create_account':
      return t('admin.upstreamChannels.preview.actions.createAccount')
    case 'update_account':
      return t('admin.upstreamChannels.preview.actions.updateAccount')
    case 'skip_account':
      return t('admin.upstreamChannels.preview.actions.skipAccount')
    case 'error':
      return t('admin.upstreamChannels.preview.actions.error')
    default:
      return readableKey(action)
  }
}

function syncSummaryLabel(action: string): string {
  return syncActionLabel(action)
}

function syncSummaryDescription(action: string): string {
  switch (action) {
    case 'create_group':
      return t('admin.upstreamChannels.preview.descriptions.createGroup')
    case 'update_group':
      return t('admin.upstreamChannels.preview.descriptions.updateGroup')
    case 'create_account':
      return t('admin.upstreamChannels.preview.descriptions.createAccount')
    case 'update_account':
      return t('admin.upstreamChannels.preview.descriptions.updateAccount')
    case 'skip_account':
      return t('admin.upstreamChannels.preview.descriptions.skipAccount')
    case 'error':
      return t('admin.upstreamChannels.preview.descriptions.error')
    default:
      return t('admin.upstreamChannels.preview.descriptions.other')
  }
}

function syncActionClass(action: string): string {
  if (action.startsWith('create_')) {
    return 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
  }
  if (action.startsWith('update_')) {
    return 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'
  }
  if (action.startsWith('skip_')) {
    return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  }
  if (action === 'error') {
    return 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'
  }
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
}

function syncSummaryValueClass(action: string): string {
  if (action.startsWith('create_')) return 'text-primary-700 dark:text-primary-300'
  if (action.startsWith('update_')) return 'text-amber-700 dark:text-amber-300'
  if (action.startsWith('skip_')) return 'text-gray-600 dark:text-gray-300'
  if (action === 'error') return 'text-red-700 dark:text-red-300'
  return 'text-gray-900 dark:text-white'
}

function syncTargetLabel(target: string): string {
  switch (target) {
    case 'group':
      return t('admin.upstreamChannels.preview.targets.group')
    case 'account':
      return t('admin.upstreamChannels.preview.targets.account')
    default:
      return readableKey(target)
  }
}

function syncWarningLabel(message: string): string {
  const parts = message.split(';').map(part => part.trim()).filter(Boolean)
  if (parts.length > 1) {
    return parts.map(syncWarningLabel).join('；')
  }
  switch (message.trim()) {
    case 'missing api key':
      return t('admin.upstreamChannels.preview.warningMessages.missingApiKey')
    case 'missing or undecryptable api key':
      return t('admin.upstreamChannels.preview.warningMessages.missingOrInvalidApiKey')
    case 'previous synced group no longer exists':
      return t('admin.upstreamChannels.preview.warningMessages.groupMissing')
    case 'failed to lookup existing account by upstream key id':
      return t('admin.upstreamChannels.preview.warningMessages.accountLookupFailed')
    default:
      return message
  }
}

function testStatusKey(status: string): string {
  const normalized = status.toLowerCase()
  if (normalized === 'ok' || normalized === 'success' || normalized === 'active' || normalized === 'operational') {
    return 'operational'
  }
  if (normalized === 'auth_error' || normalized === 'unauthorized' || normalized === 'forbidden') {
    return 'authError'
  }
  if (normalized === 'error' || normalized === 'failed' || normalized === 'disabled') {
    return 'failed'
  }
  return 'unknown'
}

function testStatusLabel(status: string): string {
  switch (testStatusKey(status)) {
    case 'operational':
      return t('admin.upstreamChannels.test.status.operational')
    case 'authError':
      return t('admin.upstreamChannels.test.status.authError')
    case 'failed':
      return t('admin.upstreamChannels.test.status.failed')
    default:
      return t('admin.upstreamChannels.test.status.unknown')
  }
}

function testSummaryLabel(key: string): string {
  switch (key) {
    case 'operational':
      return t('admin.upstreamChannels.test.summary.operational')
    case 'authError':
      return t('admin.upstreamChannels.test.summary.authError')
    case 'failed':
      return t('admin.upstreamChannels.test.summary.failed')
    default:
      return t('admin.upstreamChannels.test.summary.unknown')
  }
}

function testSummaryValueClass(key: string): string {
  switch (key) {
    case 'operational':
      return 'text-emerald-700 dark:text-emerald-300'
    case 'authError':
      return 'text-amber-700 dark:text-amber-300'
    case 'failed':
      return 'text-red-700 dark:text-red-300'
    default:
      return 'text-gray-700 dark:text-gray-300'
  }
}

function testStatusClass(status: string): string {
  const key = testStatusKey(status)
  if (key === 'operational') {
    return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
  }
  if (key === 'authError') {
    return 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'
  }
  if (key === 'failed') {
    return 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'
  }
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
}

function testResultTitle(result: UpstreamTestResult): string {
  return result.key_name ||
    result.group_name ||
    result.pool_name ||
    (result.key_id ? t('admin.upstreamChannels.test.keyId', { id: result.key_id }) : t('admin.upstreamChannels.test.channel'))
}

function testMessageLabel(message: string): string {
  switch (message.trim()) {
    case 'missing or undecryptable api key':
      return t('admin.upstreamChannels.test.messages.missingOrInvalidApiKey')
    default:
      return message
  }
}

onMounted(() => {
  resetForm()
  loadUpstreamGroups()
  reload()
})

onUnmounted(() => {
  if (searchTimeout) clearTimeout(searchTimeout)
  abortController?.abort()
})
</script>

<style scoped>
.upstream-dialog-body {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
  max-height: 72vh;
  overflow-y: auto;
}

.table-action {
  @apply flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-dark-700 dark:hover:text-primary-400;
}

.table-action-danger {
  @apply hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400;
}

.readonly-field {
  @apply flex h-[42px] w-full items-center rounded-xl border border-gray-200 bg-gray-50 px-4 text-sm text-gray-500 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-400;
}
</style>
