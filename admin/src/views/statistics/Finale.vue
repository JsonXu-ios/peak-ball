<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getFinalePredictions } from '@/api'

interface Prediction {
  match_id: string
  date: string
  match_time: string
  league: string
  home: string
  guest: string
  home_logo?: string
  guest_logo?: string
  /** home / away / ''（未命中信号） */
  pick: string
  /** 四信号齐备 / 主推≥70% / '' */
  signal: string
  base_prob: number
  pro_dir: string
  trade_pick: string
  sim_pick: string
  recent_diff?: number
  composite?: number
}

interface Report {
  generated_at: string
  horizon_days: number
  upcoming_total: number
  predictions: Prediction[]
}

const loading = ref(false)
const report = ref<Report | null>(null)
const error = ref('')
/** false=仅显示有预测的（默认），true=显示全部待赛比赛 */
const showAll = ref(false)

const homePicks = computed(() => (report.value?.predictions || []).filter((p) => p.pick === 'home'))
const awayPicks = computed(() => (report.value?.predictions || []).filter((p) => p.pick === 'away'))

const visibleMatches = computed(() =>
  (report.value?.predictions || []).filter((p) => showAll.value || p.pick),
)

/** 按日期分组（接口已按时间升序返回） */
const groupedByDate = computed(() => {
  const groups: Array<{ date: string; rows: Prediction[] }> = []
  for (const row of visibleMatches.value) {
    const last = groups[groups.length - 1]
    if (last && last.date === row.date) last.rows.push(row)
    else groups.push({ date: row.date, rows: [row] })
  }
  return groups
})

function pickColor(pick: string) {
  return pick === 'home' ? 'error' : 'success'
}

function pickLabel(pick: string) {
  return pick === 'home' ? '买主胜' : '买客胜'
}

function logoSrc(path?: string): string {
  return path || ''
}

async function fetchReport() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await getFinalePredictions()
    report.value = data as Report
  } catch (requestError) {
    const err = requestError as { response?: { data?: { error?: string } }; message?: string }
    error.value = err.response?.data?.error || err.message || '加载预测失败'
  } finally {
    loading.value = false
  }
}

onMounted(() => fetchReport())
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <h2 class="text-h5 font-weight-bold">终章 · 未来比赛预测</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">
          未来 {{ report?.horizon_days ?? 14 }} 天待赛比赛中，命中「四信号齐备」或「前端主推≥70%」信号的场次。
          <span class="text-error font-weight-bold">红色=买主胜</span>、<span class="text-success font-weight-bold">绿色=买客胜</span>；交易/模拟盈亏的推荐仅随行展示，不参与预测。
        </div>
      </div>
      <v-spacer />
      <v-btn-toggle v-model="showAll" density="comfortable" color="primary" variant="outlined" mandatory>
        <v-btn :value="false">仅预测（{{ homePicks.length + awayPicks.length }}）</v-btn>
        <v-btn :value="true">全部比赛（{{ report?.upcoming_total ?? 0 }}）</v-btn>
      </v-btn-toggle>
      <v-btn :loading="loading" color="primary" prepend-icon="mdi-refresh" @click="fetchReport">刷新</v-btn>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}</v-alert>

    <template v-if="report">
      <v-row class="mb-1">
        <v-col cols="12" md="3">
          <v-card color="primary" variant="tonal">
            <v-card-text>
              <div class="text-body-2">待赛场次（{{ report.horizon_days }}天内）</div>
              <div class="text-h4 font-weight-bold mt-1">{{ report.upcoming_total.toLocaleString() }}</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card color="error" variant="tonal">
            <v-card-text>
              <div class="text-body-2">买主胜</div>
              <div class="text-h4 font-weight-bold mt-1">{{ homePicks.length }}</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card color="success" variant="tonal">
            <v-card-text>
              <div class="text-body-2">买客胜</div>
              <div class="text-h4 font-weight-bold mt-1">{{ awayPicks.length }}</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card variant="tonal">
            <v-card-text>
              <div class="text-body-2 text-medium-emphasis">生成时间</div>
              <div class="text-subtitle-1 font-weight-medium mt-1">{{ report.generated_at }}</div>
              <div class="text-caption text-medium-emphasis">每次打开实时计算</div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card>
        <v-card-text>
          <div v-if="!visibleMatches.length" class="text-medium-emphasis text-center py-10">
            {{ showAll ? '当前没有待赛比赛。' : '当前没有符合信号的待赛比赛，可切换「全部比赛」查看。' }}
          </div>
          <v-table v-else density="comfortable" class="finale-table">
            <thead>
              <tr>
                <th>时间</th>
                <th>联赛</th>
                <th>主队</th>
                <th class="text-center">VS</th>
                <th>客队</th>
                <th class="text-center">预测</th>
                <th>信号</th>
                <th class="text-right">主推概率</th>
                <th class="text-right">让球差</th>
                <th class="text-right">综合期望</th>
                <th>盈亏提示（仅展示）</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="group in groupedByDate" :key="group.date">
                <tr class="date-row">
                  <td colspan="11" class="font-weight-bold">
                    {{ group.date }}
                    <span class="text-caption text-medium-emphasis ml-2">{{ group.rows.length }} 场</span>
                  </td>
                </tr>
                <tr
                  v-for="p in group.rows"
                  :key="p.match_id"
                  :class="p.pick === 'home' ? 'row-home' : p.pick === 'away' ? 'row-away' : ''"
                >
                  <td class="text-no-wrap">{{ p.match_time || p.date }}</td>
                  <td class="text-no-wrap league-cell">{{ p.league || '-' }}</td>
                  <td class="font-weight-medium">
                    <span class="team-cell">
                      <span class="team-logo">
                        <img v-if="logoSrc(p.home_logo)" :src="logoSrc(p.home_logo)" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
                      </span>
                      {{ p.home }}
                    </span>
                  </td>
                  <td class="text-center text-medium-emphasis">vs</td>
                  <td class="font-weight-medium">
                    <span class="team-cell">
                      <span class="team-logo">
                        <img v-if="logoSrc(p.guest_logo)" :src="logoSrc(p.guest_logo)" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
                      </span>
                      {{ p.guest }}
                    </span>
                  </td>
                  <td class="text-center">
                    <v-chip v-if="p.pick" :color="pickColor(p.pick)" variant="flat" size="large" class="font-weight-bold px-5">
                      {{ pickLabel(p.pick) }}
                    </v-chip>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                  <td class="text-no-wrap">
                    <v-chip v-if="p.signal" :color="p.signal === '四信号齐备' ? 'warning' : 'primary'" size="small" variant="tonal">
                      {{ p.signal }}
                    </v-chip>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                  <td class="text-right">{{ p.base_prob ? p.base_prob.toFixed(1) + '%' : '-' }}</td>
                  <td class="text-right">{{ typeof p.recent_diff === 'number' ? p.recent_diff.toFixed(2) : '-' }}</td>
                  <td class="text-right">{{ typeof p.composite === 'number' ? p.composite.toFixed(2) : '-' }}</td>
                  <td class="text-no-wrap">
                    <v-chip v-if="p.trade_pick" size="x-small" variant="outlined" class="mr-1">交易:{{ p.trade_pick }}</v-chip>
                    <v-chip v-if="p.sim_pick" size="x-small" variant="outlined">模拟:{{ p.sim_pick }}</v-chip>
                    <span v-if="!p.trade_pick && !p.sim_pick" class="text-medium-emphasis">-</span>
                  </td>
                </tr>
              </template>
            </tbody>
          </v-table>
        </v-card-text>
      </v-card>
    </template>
  </div>
</template>

<style scoped>
.finale-table th {
  white-space: nowrap;
}
.date-row {
  background: rgba(var(--v-theme-primary), 0.08);
}
.row-home {
  background: rgba(var(--v-theme-error), 0.06);
}
.row-away {
  background: rgba(var(--v-theme-success), 0.06);
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
