<script setup lang="ts">
interface MatchDetail {
  match_id: string
  date: string
  home: string
  guest: string
  home_score: number
  guest_score: number
  state: string
  pick: string
  result: string
  hit: boolean
  value: number
}

withDefaults(
  defineProps<{
    matches: MatchDetail[]
    total: number
    cap: number
    showValue?: boolean
  }>(),
  { showValue: false },
)
</script>

<template>
  <div>
    <table class="detail-table">
      <thead>
        <tr>
          <th>时间</th>
          <th>主队</th>
          <th class="text-center">比分</th>
          <th>客队</th>
          <th>状态</th>
          <th v-if="showValue" class="text-right">数值</th>
          <th>推荐</th>
          <th>结果</th>
          <th class="text-center">命中</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in matches" :key="m.match_id">
          <td class="text-no-wrap">{{ m.date }}</td>
          <td class="font-weight-medium">{{ m.home }}</td>
          <td class="text-center text-no-wrap">{{ m.home_score }} - {{ m.guest_score }}</td>
          <td class="font-weight-medium">{{ m.guest }}</td>
          <td class="text-medium-emphasis">{{ m.state }}</td>
          <td v-if="showValue" class="text-right">{{ Number(m.value ?? 0).toFixed(2) }}</td>
          <td>{{ m.pick || '-' }}</td>
          <td>{{ m.result || '-' }}</td>
          <td class="text-center">
            <span :class="m.hit ? 'text-success font-weight-bold' : 'text-error'">{{ m.hit ? '✓' : '✗' }}</span>
          </td>
        </tr>
      </tbody>
    </table>
    <div v-if="total > cap" class="text-caption text-medium-emphasis pa-2">
      共 {{ total.toLocaleString() }} 场，仅显示前 {{ cap }} 场。
    </div>
  </div>
</template>

<style scoped>
.detail-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.85rem;
}
.detail-table th,
.detail-table td {
  padding: 6px 12px;
  border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  text-align: left;
  white-space: nowrap;
}
.detail-table th.text-center,
.detail-table td.text-center {
  text-align: center;
}
.detail-table th.text-right,
.detail-table td.text-right {
  text-align: right;
}
</style>
