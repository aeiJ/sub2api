<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="hasHomeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

	  <CompactHomePage
	    v-else-if="compactHomeEnabled"
	    :site-name="siteName"
	    :site-logo="siteLogo"
	    :site-subtitle="siteSubtitle"
	    :doc-url="docUrl"
	    :is-dark="isDark"
	    :is-authenticated="isAuthenticated"
	    :dashboard-path="dashboardPath"
	    :current-year="currentYear"
	    :show-model-plaza-entry="showModelPlazaEntry"
	    @toggle-theme="toggleTheme"
	  />

	  <!-- Default Home Page -->
	  <div
	    v-else
	    class="home-quantum relative flex min-h-screen flex-col overflow-hidden"
	    :class="isDark ? 'is-dark' : 'is-light'"
	  >
	    <!-- Background System -->
	    <div class="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
	      <div class="quantum-grid absolute inset-0"></div>
	      <div class="quantum-vignette absolute inset-0"></div>
	      <div class="quantum-beam quantum-beam-a"></div>
	      <div class="quantum-beam quantum-beam-b"></div>
	      <div class="quantum-scanlines absolute inset-0"></div>
	    </div>

	    <!-- Header -->
	    <header class="relative z-20 px-4 py-4 sm:px-6">
	      <nav
	        class="home-nav mx-auto flex max-w-7xl items-center justify-between px-3 py-3 backdrop-blur-xl sm:px-4"
	      >
	        <!-- Logo -->
	        <div class="flex min-w-0 items-center gap-3">
	          <div
	            class="home-logo-frame flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden"
	          >
	            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
	          </div>
	          <div class="hidden min-w-0 sm:block">
	            <p class="home-brand-name truncate text-sm font-semibold tracking-[0.16em]">{{ siteName }}</p>
	            <p class="home-brand-subtitle truncate text-[11px] uppercase tracking-[0.24em]">Quantum Gateway</p>
	          </div>
	        </div>

	        <!-- Nav Actions -->
        <div class="flex min-w-0 items-center gap-2 sm:gap-3">
          <!-- Language Switcher -->
	          <div class="home-locale">
	            <LocaleSwitcher />
	          </div>

          <!-- Doc Link -->
          <a
            v-if="docUrl"
	            :href="docUrl"
	            target="_blank"
	            rel="noopener noreferrer"
	            class="home-icon-action rounded-lg p-2 transition-colors"
	            :title="t('home.viewDocs')"
	          >
	            <Icon name="book" size="md" />
          </a>

	          <!-- Theme Toggle -->
	          <button
	            @click="toggleTheme"
	            class="home-icon-action rounded-lg p-2 transition-colors"
	            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
	          >
	            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>

          <!-- Model Marketplace Button -->
	          <router-link
	            to="/model-plaza"
	            class="home-secondary-pill inline-flex items-center whitespace-nowrap rounded-full px-2.5 py-1 text-xs font-medium transition-colors"
	          >
	            <Icon name="grid" size="xs" class="mr-1 hidden sm:block" />
	            <span>{{ t('nav.modelPlaza') }}</span>
          </router-link>

          <!-- Login / Dashboard Button -->
          <router-link
	            v-if="isAuthenticated"
	            :to="dashboardPath"
	            class="home-primary-pill inline-flex items-center gap-1.5 rounded-full py-1 pl-1 pr-2.5 transition-colors"
	          >
	            <span
	              class="home-user-initial flex h-5 w-5 items-center justify-center rounded-full text-[10px] font-semibold"
	            >
	              {{ userInitial }}
	            </span>
	            <span class="text-xs font-semibold">{{ t('home.dashboard') }}</span>
	            <svg
	              class="home-primary-arrow h-3 w-3"
	              fill="none"
	              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M4.5 19.5l15-15m0 0H8.25m11.25 0v11.25"
              />
            </svg>
          </router-link>
	          <router-link
	            v-else
	            to="/login"
	            class="home-primary-pill inline-flex items-center rounded-full px-3 py-1 text-xs font-semibold transition-colors"
	          >
	            {{ t('home.login') }}
	          </router-link>
        </div>
      </nav>
    </header>

	    <!-- Main Content -->
	    <main class="relative z-10 flex-1 px-4 py-12 sm:px-6 lg:py-16">
	      <div class="mx-auto max-w-7xl">
	        <!-- Hero Section - Left/Right Layout -->
	        <div class="mb-12 grid items-center gap-10 lg:grid-cols-[minmax(0,1fr)_minmax(420px,0.86fr)] lg:gap-16">
	          <!-- Left: Text Content -->
	          <div class="text-center lg:text-left">
	            <div
	              class="home-eyebrow mb-5 inline-flex items-center gap-2 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.24em]"
	            >
	              <span class="home-status-dot h-1.5 w-1.5 rounded-full"></span>
	              Live API Routing Matrix
	            </div>
	            <h1
	              class="quantum-title mx-auto mb-5 max-w-4xl text-4xl font-black leading-[1.02] sm:text-5xl md:text-6xl lg:mx-0 lg:text-7xl"
	            >
	              {{ siteName }}
	            </h1>
	            <p class="home-hero-subtitle mx-auto mb-8 max-w-2xl text-base leading-8 sm:text-lg md:text-xl lg:mx-0">
	              {{ siteSubtitle }}
	            </p>

	            <!-- CTA Button -->
	            <div class="flex flex-col items-center gap-3 sm:flex-row lg:justify-start">
	              <router-link
	                :to="isAuthenticated ? dashboardPath : '/login'"
	                class="home-cta-primary group inline-flex min-h-[48px] items-center justify-center rounded-full px-7 py-3 text-base font-bold transition-all duration-300 hover:-translate-y-0.5"
	              >
	                {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
	                <Icon name="arrowRight" size="md" class="ml-2 transition-transform group-hover:translate-x-1" :stroke-width="2" />
	              </router-link>
	              <router-link
	                to="/model-plaza"
	                class="home-cta-secondary inline-flex min-h-[48px] items-center justify-center rounded-full px-7 py-3 text-base font-semibold transition-all duration-300"
	              >
	                {{ t('nav.modelPlaza') }}
	              </router-link>
	            </div>
	          </div>

	          <!-- Right: Command Deck -->
	          <div class="flex justify-center lg:justify-end">
	            <div class="command-deck w-full max-w-[520px]">
	              <div class="deck-frame">
	                <div class="deck-orbit" aria-hidden="true"></div>
	                <div class="deck-status">
	                  <span>UPSTREAM SYNC</span>
	                  <strong>99.99%</strong>
	                </div>
	              </div>
	              <div class="terminal-window">
	                <!-- Window header -->
	                <div class="terminal-header">
	                  <div class="terminal-buttons">
                    <span class="btn-close"></span>
                    <span class="btn-minimize"></span>
                    <span class="btn-maximize"></span>
                  </div>
	                  <span class="terminal-title">sub2api.quantum</span>
	                </div>
	                <!-- Terminal content -->
	                <div class="terminal-body">
	                  <div class="code-line line-1">
	                    <span class="code-prompt">$</span>
                    <span class="code-cmd">curl</span>
                    <span class="code-flag">-X POST</span>
                    <span class="code-url">/v1/messages</span>
                  </div>
                  <div class="code-line line-2">
                    <span class="code-comment"># Routing to upstream...</span>
                  </div>
	                  <div class="code-line line-3">
	                    <span class="code-success">200 OK</span>
	                    <span class="code-response">{ "content": "Hello!" }</span>
	                  </div>
	                  <div class="code-line line-4">
	                    <span class="code-prompt">$</span>
	                    <span class="cursor"></span>
	                  </div>
	                </div>
	              </div>
	              <div class="metrics-grid">
	                <div>
	                  <span>Latency</span>
	                  <strong>38ms</strong>
	                </div>
	                <div>
	                  <span>Failover</span>
	                  <strong>Auto</strong>
	                </div>
	                <div>
	                  <span>Billing</span>
	                  <strong>Live</strong>
	                </div>
	              </div>
	            </div>
	          </div>
	        </div>

	        <!-- Feature Tags - Centered -->
	        <div class="mb-12 flex flex-wrap items-center justify-center gap-3 md:gap-4">
	          <div
	            class="home-tag home-tag-cyan inline-flex items-center gap-2.5 rounded-full px-5 py-2.5 backdrop-blur-sm"
	          >
	            <Icon name="swap" size="sm" />
	            <span class="text-sm font-medium">{{
	              t('home.tags.subscriptionToApi')
	            }}</span>
	          </div>
	          <div
	            class="home-tag home-tag-violet inline-flex items-center gap-2.5 rounded-full px-5 py-2.5 backdrop-blur-sm"
	          >
	            <Icon name="shield" size="sm" />
	            <span class="text-sm font-medium">{{
	              t('home.tags.stickySession')
	            }}</span>
	          </div>
	          <div
	            class="home-tag home-tag-fuchsia inline-flex items-center gap-2.5 rounded-full px-5 py-2.5 backdrop-blur-sm"
	          >
	            <Icon name="chart" size="sm" />
	            <span class="text-sm font-medium">{{
	              t('home.tags.realtimeBilling')
	            }}</span>
	          </div>
	        </div>

	        <!-- Features Grid -->
	        <div class="mb-12 grid gap-6 md:grid-cols-3">
	          <!-- Feature 1: Unified Gateway -->
	          <div
	            class="quantum-card group p-6 transition-all duration-300 hover:-translate-y-1"
	          >
	            <div
	              class="home-feature-icon home-feature-icon-cyan mb-4 flex h-12 w-12 items-center justify-center transition-transform group-hover:scale-110"
	            >
	              <Icon name="server" size="lg" />
	            </div>
	            <h3 class="home-section-heading mb-2 text-lg font-semibold">
	              {{ t('home.features.unifiedGateway') }}
	            </h3>
	            <p class="home-muted-text text-sm leading-relaxed">
	              {{ t('home.features.unifiedGatewayDesc') }}
	            </p>
	          </div>

	          <!-- Feature 2: Account Pool -->
	          <div
	            class="quantum-card group p-6 transition-all duration-300 hover:-translate-y-1"
	          >
	            <div
	              class="home-feature-icon home-feature-icon-violet mb-4 flex h-12 w-12 items-center justify-center transition-transform group-hover:scale-110"
	            >
	              <svg
	                class="h-6 w-6"
	                fill="none"
	                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="1.5"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  d="M18 18.72a9.094 9.094 0 003.741-.479 3 3 0 00-4.682-2.72m.94 3.198l.001.031c0 .225-.012.447-.037.666A11.944 11.944 0 0112 21c-2.17 0-4.207-.576-5.963-1.584A6.062 6.062 0 016 18.719m12 0a5.971 5.971 0 00-.941-3.197m0 0A5.995 5.995 0 0012 12.75a5.995 5.995 0 00-5.058 2.772m0 0a3 3 0 00-4.681 2.72 8.986 8.986 0 003.74.477m.94-3.197a5.971 5.971 0 00-.94 3.197M15 6.75a3 3 0 11-6 0 3 3 0 016 0zm6 3a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0zm-13.5 0a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0z"
	                />
	              </svg>
	            </div>
	            <h3 class="home-section-heading mb-2 text-lg font-semibold">
	              {{ t('home.features.multiAccount') }}
	            </h3>
	            <p class="home-muted-text text-sm leading-relaxed">
	              {{ t('home.features.multiAccountDesc') }}
	            </p>
	          </div>

	          <!-- Feature 3: Billing & Quota -->
	          <div
	            class="quantum-card group p-6 transition-all duration-300 hover:-translate-y-1"
	          >
	            <div
	              class="home-feature-icon home-feature-icon-fuchsia mb-4 flex h-12 w-12 items-center justify-center transition-transform group-hover:scale-110"
	            >
	              <svg
	                class="h-6 w-6"
	                fill="none"
	                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="1.5"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  d="M2.25 18.75a60.07 60.07 0 0115.797 2.101c.727.198 1.453-.342 1.453-1.096V18.75M3.75 4.5v.75A.75.75 0 013 6h-.75m0 0v-.375c0-.621.504-1.125 1.125-1.125H20.25M2.25 6v9m18-10.5v.75c0 .414.336.75.75.75h.75m-1.5-1.5h.375c.621 0 1.125.504 1.125 1.125v9.75c0 .621-.504 1.125-1.125 1.125h-.375m1.5-1.5H21a.75.75 0 00-.75.75v.75m0 0H3.75m0 0h-.375a1.125 1.125 0 01-1.125-1.125V15m1.5 1.5v-.75A.75.75 0 003 15h-.75M15 10.5a3 3 0 11-6 0 3 3 0 016 0zm3 0h.008v.008H18V10.5zm-12 0h.008v.008H6V10.5z"
	                />
	              </svg>
	            </div>
	            <h3 class="home-section-heading mb-2 text-lg font-semibold">
	              {{ t('home.features.balanceQuota') }}
	            </h3>
	            <p class="home-muted-text text-sm leading-relaxed">
	              {{ t('home.features.balanceQuotaDesc') }}
	            </p>
	          </div>
	        </div>

	        <!-- Supported Providers -->
	        <div class="mb-8 text-center">
	          <h2 class="home-section-heading mb-3 text-2xl font-bold">
	            {{ t('home.providers.title') }}
	          </h2>
	          <p class="home-muted-text text-sm">
	            {{ t('home.providers.description') }}
	          </p>
	        </div>

	        <div class="mb-16 flex flex-wrap items-center justify-center gap-4">
	          <!-- Claude - Supported -->
	          <div
	            class="provider-chip"
	          >
	            <div
	              class="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-orange-400 to-orange-500"
	            >
	              <span class="text-xs font-bold text-white">C</span>
	            </div>
	            <span class="home-provider-name text-sm font-medium">{{ t('home.providers.claude') }}</span>
	            <span
	              class="home-provider-status rounded px-1.5 py-0.5 text-[10px] font-medium"
	              >{{ t('home.providers.supported') }}</span
	            >
	          </div>
	          <!-- GPT - Supported -->
	          <div
	            class="provider-chip"
	          >
	            <div
	              class="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-green-500 to-green-600"
	            >
	              <span class="text-xs font-bold text-white">G</span>
	            </div>
	            <span class="home-provider-name text-sm font-medium">GPT</span>
	            <span
	              class="home-provider-status rounded px-1.5 py-0.5 text-[10px] font-medium"
	              >{{ t('home.providers.supported') }}</span
	            >
	          </div>
	          <!-- Gemini - Supported -->
	          <div
	            class="provider-chip"
	          >
	            <div
	              class="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-blue-500 to-blue-600"
	            >
	              <span class="text-xs font-bold text-white">G</span>
	            </div>
	            <span class="home-provider-name text-sm font-medium">{{ t('home.providers.gemini') }}</span>
	            <span
	              class="home-provider-status rounded px-1.5 py-0.5 text-[10px] font-medium"
	              >{{ t('home.providers.supported') }}</span
	            >
	          </div>
	          <!-- Antigravity - Supported -->
	          <div
	            class="provider-chip"
	          >
	            <div
	              class="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-rose-500 to-pink-600"
	            >
	              <span class="text-xs font-bold text-white">A</span>
	            </div>
	            <span class="home-provider-name text-sm font-medium">{{ t('home.providers.antigravity') }}</span>
	            <span
	              class="home-provider-status rounded px-1.5 py-0.5 text-[10px] font-medium"
	              >{{ t('home.providers.supported') }}</span
	            >
	          </div>
	          <!-- More - Coming Soon -->
	          <div
	            class="provider-chip opacity-60"
	          >
	            <div
	              class="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-gray-500 to-gray-600"
	            >
	              <span class="text-xs font-bold text-white">+</span>
	            </div>
	            <span class="home-provider-name text-sm font-medium">{{ t('home.providers.more') }}</span>
	            <span
	              class="home-provider-status rounded px-1.5 py-0.5 text-[10px] font-medium"
	              >{{ t('home.providers.soon') }}</span
	            >
	          </div>
        </div>
      </div>
    </main>

	    <!-- Footer -->
	    <footer class="home-footer relative z-10 px-6 py-8">
	      <div
	        class="mx-auto flex max-w-6xl flex-col items-center justify-center gap-4 text-center sm:flex-row sm:text-left"
	      >
	        <p class="home-footer-text text-sm">
	          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
	        </p>
	        <div class="flex items-center gap-4">
          <a
            v-if="docUrl"
	            :href="docUrl"
	            target="_blank"
	            rel="noopener noreferrer"
	            class="home-footer-link text-sm transition-colors"
	          >
	            {{ t('home.docs') }}
	          </a>
          <a
	            :href="githubUrl"
	            target="_blank"
	            rel="noopener noreferrer"
	            class="home-footer-link text-sm transition-colors"
	          >
	            GitHub
	          </a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import CompactHomePage from '@/components/home/CompactHomePage.vue'
import { applyThemeClass } from '@/utils/theme'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// Site settings - directly from appStore (already initialized from injected config)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)

// Check if homeContent is a URL (for iframe display)
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// Theme
const isDark = ref(document.documentElement.classList.contains('dark'))

// GitHub URL
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'

// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
const modelPlazaRequiresAuth = computed(
  () => appStore.cachedPublicSettings?.model_plaza_require_auth === true,
)
const showModelPlazaEntry = computed(
  () => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value),
)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const userInitial = computed(() => {
  const user = authStore.user
  if (!user || !user.email) return ''
  return user.email.charAt(0).toUpperCase()
})

// Current year for footer
const currentYear = computed(() => new Date().getFullYear())

// Toggle theme
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

// Initialize theme
function initTheme() {
  isDark.value = applyThemeClass()
}

onMounted(() => {
  initTheme()

  // Check auth state
  authStore.checkAuth()

  // Ensure public settings are loaded (will use cache if already loaded from injected config)
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
.home-quantum {
  --home-bg: #f6fbff;
  --home-bg-rgb: 246, 251, 255;
  --home-text: #102033;
  --home-heading: #061426;
  --home-muted: rgba(16, 32, 51, 0.68);
  --home-subtle: rgba(16, 32, 51, 0.54);
  --home-grid-rgb: 8, 145, 178;
  --home-cyan: #007a91;
  --home-cyan-bright: #0891b2;
  --home-violet: #6045d8;
  --home-fuchsia: #b91ca8;
  --home-nav-bg: rgba(255, 255, 255, 0.78);
  --home-panel-bg: linear-gradient(145deg, rgba(255, 255, 255, 0.9), rgba(232, 246, 255, 0.78));
  --home-panel-border: rgba(8, 145, 178, 0.22);
  --home-panel-highlight: rgba(8, 145, 178, 0.54);
  --home-panel-shadow: 0 22px 60px rgba(15, 52, 82, 0.14);
  --home-chip-bg: rgba(255, 255, 255, 0.72);
  --home-chip-border: rgba(8, 145, 178, 0.2);
  --home-primary-bg: #073042;
  --home-primary-text: #e7fdff;
  --home-primary-hover: #0b475c;
  --home-secondary-bg: rgba(96, 69, 216, 0.1);
  --home-secondary-text: #3f2bb4;
  --home-secondary-border: rgba(96, 69, 216, 0.24);
  --home-footer-border: rgba(8, 145, 178, 0.16);
  --home-terminal-bg: linear-gradient(145deg, rgba(13, 14, 34, 0.97) 0%, rgba(5, 5, 16, 0.98) 100%);
  --home-terminal-text: #e0e0ff;
  background:
    radial-gradient(ellipse at 50% 0%, rgba(0, 188, 212, 0.18), transparent 38%),
    radial-gradient(ellipse at 90% 26%, rgba(123, 97, 255, 0.14), transparent 34%),
    linear-gradient(180deg, var(--home-bg), #eef8ff 72%, #f8fbff);
  color: var(--home-text);
  isolation: isolate;
}

.home-quantum.is-dark {
  --home-bg: #050510;
  --home-bg-rgb: 5, 5, 16;
  --home-text: #e0e0ff;
  --home-heading: #ffffff;
  --home-muted: rgba(224, 224, 255, 0.64);
  --home-subtle: rgba(224, 224, 255, 0.5);
  --home-grid-rgb: 0, 255, 255;
  --home-cyan: #00ffff;
  --home-cyan-bright: #00ffff;
  --home-violet: #7b61ff;
  --home-fuchsia: #ff00ff;
  --home-nav-bg: rgba(7, 10, 24, 0.7);
  --home-panel-bg: linear-gradient(145deg, rgba(10, 16, 35, 0.82), rgba(8, 8, 24, 0.72));
  --home-panel-border: rgba(0, 255, 255, 0.16);
  --home-panel-highlight: rgba(0, 255, 255, 0.72);
  --home-panel-shadow: 0 20px 54px rgba(0, 0, 0, 0.24);
  --home-chip-bg: rgba(8, 12, 28, 0.72);
  --home-chip-border: rgba(0, 255, 255, 0.16);
  --home-primary-bg: #00ffff;
  --home-primary-text: #050510;
  --home-primary-hover: #a5ffff;
  --home-secondary-bg: rgba(123, 97, 255, 0.1);
  --home-secondary-text: #ede9ff;
  --home-secondary-border: rgba(196, 181, 253, 0.28);
  --home-footer-border: rgba(0, 255, 255, 0.1);
  --home-terminal-bg: linear-gradient(145deg, rgba(13, 14, 34, 0.96) 0%, rgba(5, 5, 16, 0.98) 100%);
  --home-terminal-text: #e0e0ff;
  background: #050510;
}

.quantum-grid {
  background:
    linear-gradient(rgba(var(--home-grid-rgb), 0.09) 1px, transparent 1px),
    linear-gradient(90deg, rgba(var(--home-grid-rgb), 0.09) 1px, transparent 1px),
    linear-gradient(115deg, color-mix(in srgb, var(--home-violet) 18%, transparent), transparent 36%, color-mix(in srgb, var(--home-fuchsia) 12%, transparent));
  background-size: 56px 56px, 56px 56px, 100% 100%;
  mask-image: linear-gradient(to bottom, rgba(0, 0, 0, 0.92), rgba(0, 0, 0, 0.3) 68%, transparent);
  opacity: 0.58;
}

.quantum-vignette {
  background:
    radial-gradient(ellipse at 50% 0%, color-mix(in srgb, var(--home-cyan) 18%, transparent), transparent 36%),
    radial-gradient(ellipse at 85% 36%, color-mix(in srgb, var(--home-violet) 20%, transparent), transparent 34%),
    linear-gradient(180deg, rgba(var(--home-bg-rgb), 0) 0%, rgba(var(--home-bg-rgb), 0.88) 82%);
}

.quantum-scanlines {
  background: repeating-linear-gradient(
    0deg,
    rgba(var(--home-grid-rgb), 0.035) 0,
    rgba(var(--home-grid-rgb), 0.035) 1px,
    transparent 1px,
    transparent 6px
  );
  mix-blend-mode: screen;
  opacity: 0.42;
}

.quantum-beam {
  position: absolute;
  height: 1px;
  width: 58vw;
  background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--home-cyan) 72%, transparent), transparent);
  box-shadow: 0 0 28px color-mix(in srgb, var(--home-cyan) 42%, transparent);
}

.quantum-beam-a {
  right: -12vw;
  top: 24%;
  transform: rotate(-16deg);
}

.quantum-beam-b {
  bottom: 31%;
  left: -14vw;
  transform: rotate(13deg);
  background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--home-fuchsia) 58%, transparent), transparent);
  box-shadow: 0 0 28px color-mix(in srgb, var(--home-fuchsia) 28%, transparent);
}

.quantum-title {
  color: var(--home-heading);
  text-shadow:
    0 0 18px color-mix(in srgb, var(--home-cyan) 28%, transparent),
    0 0 54px color-mix(in srgb, var(--home-violet) 28%, transparent);
}

.home-nav {
  border: 1px solid var(--home-panel-border);
  background: var(--home-nav-bg);
  box-shadow: 0 0 40px color-mix(in srgb, var(--home-cyan) 10%, transparent);
}

.home-logo-frame {
  border: 1px solid color-mix(in srgb, var(--home-cyan) 34%, transparent);
  background: color-mix(in srgb, var(--home-cyan) 12%, transparent);
  box-shadow: 0 0 24px color-mix(in srgb, var(--home-cyan) 22%, transparent);
}

.home-brand-name,
.home-section-heading,
.home-provider-name {
  color: var(--home-heading);
}

.home-brand-subtitle,
.home-hero-subtitle,
.home-muted-text,
.home-footer-text,
.home-footer-link {
  color: var(--home-muted);
}

.home-icon-action {
  border: 1px solid color-mix(in srgb, var(--home-cyan) 14%, transparent);
  background: color-mix(in srgb, var(--home-cyan) 5%, transparent);
  color: var(--home-subtle);
}

.home-icon-action:hover {
  border-color: color-mix(in srgb, var(--home-cyan) 42%, transparent);
  background: color-mix(in srgb, var(--home-cyan) 12%, transparent);
  color: var(--home-heading);
}

.home-primary-pill,
.home-cta-primary {
  background: var(--home-primary-bg);
  color: var(--home-primary-text);
  box-shadow: 0 0 28px color-mix(in srgb, var(--home-cyan) 32%, transparent);
}

.home-primary-pill:hover,
.home-cta-primary:hover {
  background: var(--home-primary-hover);
}

.home-user-initial {
  background: rgba(var(--home-bg-rgb), 0.92);
  color: var(--home-cyan-bright);
}

.home-primary-arrow {
  color: color-mix(in srgb, var(--home-primary-text) 74%, transparent);
}

.home-secondary-pill,
.home-cta-secondary {
  border: 1px solid var(--home-secondary-border);
  background: var(--home-secondary-bg);
  color: var(--home-secondary-text);
}

.home-secondary-pill:hover,
.home-cta-secondary:hover {
  border-color: color-mix(in srgb, var(--home-violet) 58%, transparent);
  background: color-mix(in srgb, var(--home-violet) 16%, transparent);
}

.home-eyebrow,
.home-tag {
  border: 1px solid var(--home-chip-border);
  background: color-mix(in srgb, var(--home-cyan) 10%, transparent);
  color: var(--home-cyan);
  box-shadow: 0 0 24px color-mix(in srgb, var(--home-cyan) 12%, transparent);
}

.home-status-dot {
  background: var(--home-cyan-bright);
  box-shadow: 0 0 12px color-mix(in srgb, var(--home-cyan) 75%, transparent);
}

.home-tag-violet {
  background: color-mix(in srgb, var(--home-violet) 10%, transparent);
  color: var(--home-violet);
  box-shadow: 0 0 18px color-mix(in srgb, var(--home-violet) 12%, transparent);
}

.home-tag-fuchsia {
  background: color-mix(in srgb, var(--home-fuchsia) 10%, transparent);
  color: var(--home-fuchsia);
  box-shadow: 0 0 18px color-mix(in srgb, var(--home-fuchsia) 12%, transparent);
}

.home-feature-icon {
  border: 1px solid color-mix(in srgb, var(--home-cyan) 32%, transparent);
  background: color-mix(in srgb, var(--home-cyan) 12%, transparent);
  color: var(--home-cyan);
  box-shadow: 0 0 26px color-mix(in srgb, var(--home-cyan) 22%, transparent);
}

.home-feature-icon-violet {
  border-color: color-mix(in srgb, var(--home-violet) 32%, transparent);
  background: color-mix(in srgb, var(--home-violet) 12%, transparent);
  color: var(--home-violet);
  box-shadow: 0 0 26px color-mix(in srgb, var(--home-violet) 22%, transparent);
}

.home-feature-icon-fuchsia {
  border-color: color-mix(in srgb, var(--home-fuchsia) 32%, transparent);
  background: color-mix(in srgb, var(--home-fuchsia) 12%, transparent);
  color: var(--home-fuchsia);
  box-shadow: 0 0 26px color-mix(in srgb, var(--home-fuchsia) 20%, transparent);
}

.home-footer {
  border-top: 1px solid var(--home-footer-border);
}

.home-footer-link:hover {
  color: var(--home-heading);
}

.home-locale :deep(button) {
  border: 1px solid color-mix(in srgb, var(--home-cyan) 14%, transparent);
  background: color-mix(in srgb, var(--home-cyan) 5%, transparent);
  color: var(--home-subtle);
}

.home-locale :deep(button:hover) {
  border-color: color-mix(in srgb, var(--home-cyan) 42%, transparent);
  background: color-mix(in srgb, var(--home-cyan) 12%, transparent);
  color: var(--home-heading);
}

.home-locale :deep(.absolute) {
  border-color: color-mix(in srgb, var(--home-cyan) 22%, transparent);
  background: color-mix(in srgb, var(--home-bg) 94%, white);
  box-shadow: 0 20px 48px rgba(15, 52, 82, 0.18), 0 0 30px color-mix(in srgb, var(--home-cyan) 10%, transparent);
}

.quantum-card {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--home-panel-border);
  border-radius: 8px;
  background: var(--home-panel-bg);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.18),
    var(--home-panel-shadow);
  backdrop-filter: blur(18px);
}

.quantum-card::before,
.provider-chip::before {
  position: absolute;
  top: 0;
  left: 12px;
  right: 12px;
  height: 1px;
  background: linear-gradient(90deg, transparent, var(--home-panel-highlight), transparent);
  content: '';
}

.quantum-card:hover {
  border-color: color-mix(in srgb, var(--home-cyan) 36%, transparent);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.18),
    0 24px 64px rgba(15, 52, 82, 0.18),
    0 0 36px color-mix(in srgb, var(--home-cyan) 12%, transparent);
}

.provider-chip {
  position: relative;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  border: 1px solid var(--home-chip-border);
  border-radius: 8px;
  background: var(--home-chip-bg);
  padding: 0.75rem 1.25rem;
  box-shadow: 0 12px 38px rgba(15, 52, 82, 0.14);
  backdrop-filter: blur(16px);
}

.home-provider-status {
  background: color-mix(in srgb, var(--home-cyan) 14%, transparent);
  color: var(--home-cyan);
}

.command-deck {
  position: relative;
}

.deck-frame {
  position: absolute;
  inset: -24px -18px auto auto;
  z-index: -1;
  height: 180px;
  width: 180px;
  border: 1px solid color-mix(in srgb, var(--home-cyan) 20%, transparent);
  border-radius: 8px;
  transform: rotate(8deg);
}

.deck-frame::before,
.deck-frame::after {
  position: absolute;
  border: 1px solid color-mix(in srgb, var(--home-violet) 24%, transparent);
  content: '';
}

.deck-frame::before {
  inset: 22px;
}

.deck-frame::after {
  inset: 50px;
  border-color: color-mix(in srgb, var(--home-fuchsia) 20%, transparent);
}

.deck-orbit {
  position: absolute;
  inset: 72px;
  border: 1px solid color-mix(in srgb, var(--home-cyan) 34%, transparent);
  transform: rotate(45deg);
}

.deck-status {
  position: absolute;
  right: 12px;
  bottom: -18px;
  display: flex;
  min-width: 136px;
  justify-content: space-between;
  gap: 12px;
  border: 1px solid color-mix(in srgb, var(--home-cyan) 24%, transparent);
  background: color-mix(in srgb, var(--home-bg) 88%, black);
  padding: 8px 10px;
  color: var(--home-muted);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 10px;
}

.deck-status strong {
  color: var(--home-cyan-bright);
}

.terminal-window {
  position: relative;
  width: 100%;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--home-cyan) 24%, transparent);
  border-radius: 8px;
  background:
    var(--home-terminal-bg),
    linear-gradient(90deg, color-mix(in srgb, var(--home-cyan) 12%, transparent), color-mix(in srgb, var(--home-violet) 12%, transparent));
  box-shadow:
    0 30px 80px rgba(0, 0, 0, 0.44),
    0 0 0 1px rgba(255, 255, 255, 0.05),
    0 0 48px color-mix(in srgb, var(--home-cyan) 12%, transparent),
    inset 0 1px 0 rgba(255, 255, 255, 0.08);
  transform: perspective(1000px) rotateX(2deg) rotateY(-3deg);
  transition: transform 0.3s ease, box-shadow 0.3s ease;
}

.terminal-window:hover {
  box-shadow:
    0 34px 88px rgba(0, 0, 0, 0.5),
    0 0 58px color-mix(in srgb, var(--home-cyan) 18%, transparent),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
  transform: perspective(1000px) rotateX(0deg) rotateY(0deg) translateY(-4px);
}

.terminal-header {
  display: flex;
  align-items: center;
  border-bottom: 1px solid color-mix(in srgb, var(--home-cyan) 12%, transparent);
  background: rgba(255, 255, 255, 0.035);
  padding: 12px 16px;
}

.terminal-buttons {
  display: flex;
  gap: 8px;
}

.terminal-buttons span {
  height: 10px;
  width: 10px;
  border-radius: 999px;
}

.btn-close {
  background: #ff3333;
  box-shadow: 0 0 12px rgba(255, 51, 51, 0.48);
}

.btn-minimize {
  background: #ff00ff;
  box-shadow: 0 0 12px rgba(255, 0, 255, 0.42);
}

.btn-maximize {
  background: #00ffff;
  box-shadow: 0 0 12px rgba(0, 255, 255, 0.52);
}

.terminal-title {
  flex: 1;
  margin-right: 48px;
  color: rgba(224, 224, 255, 0.56);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  text-align: center;
}

.terminal-body {
  min-height: 198px;
  padding: 24px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 14px;
  line-height: 2;
}

.code-line {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  opacity: 0;
  animation: line-appear 0.5s ease forwards;
}

.line-1 {
  animation-delay: 0.3s;
}

.line-2 {
  animation-delay: 1s;
}

.line-3 {
  animation-delay: 1.8s;
}

.line-4 {
  animation-delay: 2.5s;
}

@keyframes line-appear {
  from {
    opacity: 0;
    transform: translateY(5px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.code-prompt {
  color: #00ffff;
  font-weight: 700;
}

.code-cmd {
  color: var(--home-terminal-text);
}

.code-flag {
  color: #ff00ff;
}

.code-url {
  color: #9b87ff;
}

.code-comment {
  color: rgba(224, 224, 255, 0.52);
  font-style: italic;
}

.code-success {
  border: 1px solid rgba(0, 255, 255, 0.22);
  border-radius: 4px;
  background: rgba(0, 255, 255, 0.1);
  color: #00ffff;
  padding: 2px 8px;
  font-weight: 700;
}

.code-response {
  color: var(--home-terminal-text);
}

.cursor {
  display: inline-block;
  height: 16px;
  width: 8px;
  background: #00ffff;
  box-shadow: 0 0 14px rgba(0, 255, 255, 0.78);
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  0%,
  50% {
    opacity: 1;
  }
  51%,
  100% {
    opacity: 0;
  }
}

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-top: 10px;
}

.metrics-grid div {
  border: 1px solid color-mix(in srgb, var(--home-cyan) 16%, transparent);
  border-radius: 8px;
  background: color-mix(in srgb, var(--home-bg) 82%, black);
  padding: 12px;
  backdrop-filter: blur(12px);
}

.metrics-grid span,
.metrics-grid strong {
  display: block;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.metrics-grid span {
  color: var(--home-muted);
  font-size: 11px;
}

.metrics-grid strong {
  margin-top: 4px;
  color: var(--home-cyan-bright);
  font-size: 16px;
}

@media (max-width: 640px) {
  .quantum-grid {
    background-size: 38px 38px, 38px 38px, 100% 100%;
  }

  .deck-frame {
    display: none;
  }

  .terminal-window {
    transform: none;
  }

  .terminal-window:hover {
    transform: translateY(-2px);
  }

  .terminal-body {
    min-height: 180px;
    padding: 18px;
    font-size: 12px;
  }

  .metrics-grid {
    grid-template-columns: 1fr;
  }
}
</style>
