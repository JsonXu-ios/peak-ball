<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { teamLogoUrl } from '@/utils/teamLogo'

const props = defineProps<{ src?: string; name: string }>()
const source = computed(() => teamLogoUrl(props.src))
const failed = ref(false)
watch(source, () => { failed.value = false })
</script>

<template>
  <span class="team-logo" aria-hidden="true">
    <img v-if="source && !failed" :src="source" alt="" width="24" height="24" loading="lazy" decoding="async" @error="failed = true" />
    <span v-else class="team-logo-fallback">{{ name.trim().slice(0, 1) || '球' }}</span>
  </span>
</template>

<style scoped>
.team-logo { display: inline-flex; align-items: center; justify-content: center; flex: 0 0 24px; width: 24px; height: 24px; }
.team-logo img { width: 100%; height: 100%; object-fit: contain; }
.team-logo-fallback { display: inline-flex; align-items: center; justify-content: center; width: 100%; height: 100%; border-radius: 50%; background: rgba(var(--v-theme-on-surface), .08); color: rgba(var(--v-theme-on-surface), .55); font-size: 12px; }
</style>
