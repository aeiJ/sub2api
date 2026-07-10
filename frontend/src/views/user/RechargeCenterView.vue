<template>
  <AppLayout>
    <div class="h-[calc(100vh-8rem)] min-h-[560px] overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
      <iframe
        class="h-full w-full border-0"
        :src="rechargeUrl"
        :title="t('nav.rechargeCenter')"
        referrerpolicy="no-referrer-when-downgrade"
        allow="payment *; clipboard-read *; clipboard-write *"
      ></iframe>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
const rechargeUrl = 'https://pay.ldxp.cn/shop/QQUDP4IE'
</script>
