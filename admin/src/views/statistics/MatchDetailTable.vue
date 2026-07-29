<script setup lang="ts">
import { computed } from 'vue'

interface MatchDetail {
  match_id: string
  date: string
  match_time?: string
  league?: string
  home: string
  guest: string
  home_logo?: string
  guest_logo?: string
  home_score: number
  guest_score: number
  state: string
  pick: string
  result: string
  hit: boolean
  value: number
  /** 该场结算用的盘口线（仅盘口类信号有值） */
  line?: string
  /**
   * 期望球数与对照信号各自的方向+数值（仅大小球方向组有值）。
   * exp_goals 如「判大球 3.73」；heat_goals 带信号名前缀，如「热度 判小球 3.50」
   *（#15~#18）或「倾向 判大球 4.00」（#19~#22），因为这一列的来源随信号而变。
   */
  exp_goals?: string
  heat_goals?: string
}

const props = withDefaults(
  defineProps<{
    matches: MatchDetail[]
    total: number
    cap: number
    showValue?: boolean
  }>(),
  { showValue: false },
)

/** 有任何一场带盘口线才显示盘口列 */
const hasLine = computed(() => props.matches.some((m) => m.line))

/** 期望球数/球数热度两列只有大小球方向组才有值 */
const hasGoalsDir = computed(() => props.matches.some((m) => m.exp_goals || m.heat_goals))

/** 判大球=橙、判小球=蓝，和终章、H5 的大小球配色一致 */
function goalsDirClass(text?: string) {
  if (!text) return ''
  if (text.includes('大球')) return 'text-warning'
  if (text.includes('小球')) return 'text-info'
  return ''
}

/** 库里存的是 /footballimg/<id> 相对路径（vite 已代理到前台API），http(s) 原样返回 */
function logoSrc(path?: string): string {
  if (!path) return ''
  return path
}

function timeText(row: MatchDetail): string {
  return row.match_time || row.date
}
</script>

<template>
  <div>
    <table class="detail-table">
      <thead>
        <tr>
          <th>时间</th>
          <th>联赛</th>
          <th>主队</th>
          <th class="text-center">比分</th>
          <th>客队</th>
          <th>状态</th>
          <th v-if="hasLine" class="text-right">盘口</th>
          <th v-if="hasGoalsDir">期望球数</th>
          <th v-if="hasGoalsDir">对照信号</th>
          <th v-if="showValue" class="text-right">数值</th>
          <th>推荐</th>
          <th>结果</th>
          <th class="text-center">命中</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in matches" :key="m.match_id">
          <td class="text-no-wrap">{{ timeText(m) }}</td>
          <td class="text-no-wrap league-cell">{{ m.league || '-' }}</td>
          <td class="font-weight-medium">
            <span class="team-cell">
              <span class="team-logo">
                <img v-if="logoSrc(m.home_logo)" :src="logoSrc(m.home_logo)" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
              </span>
              {{ m.home }}
            </span>
          </td>
          <td class="text-center text-no-wrap">{{ m.home_score }} - {{ m.guest_score }}</td>
          <td class="font-weight-medium">
            <span class="team-cell">
              <span class="team-logo">
                <img v-if="logoSrc(m.guest_logo)" :src="logoSrc(m.guest_logo)" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
              </span>
              {{ m.guest }}
            </span>
          </td>
          <td class="text-medium-emphasis">{{ m.state }}</td>
          <td v-if="hasLine" class="text-right">{{ m.line || '-' }}</td>
          <td v-if="hasGoalsDir" :class="goalsDirClass(m.exp_goals)">{{ m.exp_goals || '-' }}</td>
          <td v-if="hasGoalsDir" :class="goalsDirClass(m.heat_goals)">{{ m.heat_goals || '-' }}</td>
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
.team-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.team-logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  border-radius: 50%;
  background: rgba(var(--v-border-color), 0.12);
  overflow: hidden;
}
.team-logo img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.league-cell {
  max-width: 130px;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
