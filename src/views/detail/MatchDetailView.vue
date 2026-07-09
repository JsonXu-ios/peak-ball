<template>
  <div class="min-h-screen pb-24">
    <!-- Top Navigation Bar -->
    <div class="sticky top-0 z-50 bg-background-dark/80 backdrop-blur-md border-b border-slate-800">
      <div class="flex items-center p-4 justify-between max-w-md mx-auto">
        <div class="flex items-center gap-3">
          <span class="material-symbols-outlined cursor-pointer" @click="router.back()">arrow_back_ios</span>
          <div>
            <h2 class="text-sm font-bold leading-tight">{{ match?.league ?? '...' }}</h2>
            <p class="text-[10px] text-slate-500 uppercase tracking-widest">Matchday</p>
          </div>
        </div>
        <div class="flex gap-4">
          <button class="p-1 rounded-full hover:bg-slate-800" @click="router.push('/notifications')">
            <span class="material-symbols-outlined text-primary">notifications</span>
          </button>
          <button class="p-1 rounded-full hover:bg-slate-800">
            <span class="material-symbols-outlined">share</span>
          </button>
        </div>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center items-center py-40">
      <span class="material-symbols-outlined text-4xl text-primary animate-spin">progress_activity</span>
    </div>

    <template v-else-if="match">
      <!-- Match Hero Header -->
      <div class="max-w-md mx-auto px-4 py-6">
        <div class="flex justify-between items-center mb-6">
          <div class="flex flex-col items-center gap-2 flex-1">
            <div class="size-16 rounded-xl bg-white/5 p-2 flex items-center justify-center">
              <img v-if="match.homeLogo" :src="logoUrl(match.homeLogo)" class="w-full h-auto" alt="" />
            </div>
            <span class="font-bold text-sm text-center">{{ match.home }}</span>
          </div>
          <div class="flex flex-col items-center gap-1 flex-1">
            <div v-if="match.status === 1" class="bg-primary/10 text-primary px-3 py-1 rounded-full text-[10px] font-bold uppercase tracking-wider mb-2">
              LIVE
            </div>
            <div v-else-if="match.displayState === '完场'" class="bg-slate-800 text-slate-400 px-3 py-1 rounded-full text-[10px] font-bold uppercase tracking-wider mb-2">
              FT
            </div>
            <div class="text-4xl font-black flex gap-3">
              <span>{{ match.homeScore }}</span>
              <span class="text-slate-500">-</span>
              <span>{{ match.guestScore }}</span>
            </div>
          </div>
          <div class="flex flex-col items-center gap-2 flex-1">
            <div class="size-16 rounded-xl bg-white/5 p-2 flex items-center justify-center">
              <img v-if="match.guestLogo" :src="logoUrl(match.guestLogo)" class="w-full h-auto" alt="" />
            </div>
            <span class="font-bold text-sm text-center">{{ match.guest }}</span>
          </div>
        </div>

        <!-- Navigation Tabs -->
        <div class="flex border-b border-slate-800 mb-6">
          <button
            v-for="tab in tabs"
            :key="tab"
            class="flex-1 pb-3 text-sm font-medium border-b-2"
            :class="activeTab === tab
              ? 'font-bold border-primary text-primary'
              : 'text-slate-500 border-transparent'"
            @click="activeTab = tab"
          >
            {{ tab }}
          </button>
        </div>

        <!-- Analysis Tab -->
        <template v-if="activeTab === 'Analysis'">
          <!-- AI Win Probability -->
          <div class="bg-slate-900/50 rounded-2xl p-5 border border-slate-800 mb-6 shadow-sm">
            <div class="flex items-center justify-between mb-4">
              <h3 class="font-bold flex items-center gap-2">
                <span class="material-symbols-outlined text-primary text-lg">psychology</span>
                AI Probability Insight
              </h3>
            </div>
            <div class="space-y-4">
              <div class="flex h-3 w-full bg-slate-800 rounded-full overflow-hidden">
                <div class="bg-primary h-full" :style="{ width: homeWinPct + '%' }"></div>
                <div class="bg-slate-600 h-full border-x border-white/10" :style="{ width: drawPct + '%' }"></div>
                <div class="bg-rose-500 h-full" :style="{ width: awayWinPct + '%' }"></div>
              </div>
              <div class="flex justify-between text-xs font-bold px-1">
                <div class="flex items-center gap-1.5">
                  <span class="size-2 rounded-full bg-primary"></span> {{ match.home }} {{ homeWinPct }}%
                </div>
                <div class="flex items-center gap-1.5">
                  <span class="size-2 rounded-full bg-slate-400"></span> Draw {{ drawPct }}%
                </div>
                <div class="flex items-center gap-1.5">
                  <span class="size-2 rounded-full bg-rose-500"></span> {{ match.guest }} {{ awayWinPct }}%
                </div>
              </div>
            </div>
          </div>

          <!-- Today's Conclusion -->
          <div class="bg-slate-900/50 rounded-2xl p-5 border border-slate-800 mb-6 shadow-sm">
            <div class="flex items-center justify-between gap-3 mb-3">
              <h3 class="font-bold flex items-center gap-2">
                <span class="material-symbols-outlined text-primary text-lg">fact_check</span>
                今日结论
              </h3>
              <span class="shrink-0 text-[10px] font-bold px-2.5 py-1 rounded-full" :class="confidenceClass">
                {{ confidenceLabel }}
              </span>
            </div>
            <p class="text-sm leading-6 text-slate-300 mb-4">{{ conclusionText }}</p>
            <div class="grid grid-cols-3 gap-2 text-center">
              <div class="bg-slate-800/80 rounded-xl px-2 py-3">
                <p class="text-[10px] text-slate-500 font-bold uppercase mb-1">Odds</p>
                <p class="text-xs font-bold text-white">{{ oddsSignal }}</p>
              </div>
              <div class="bg-slate-800/80 rounded-xl px-2 py-3">
                <p class="text-[10px] text-slate-500 font-bold uppercase mb-1">Form</p>
                <p class="text-xs font-bold text-white">{{ formSignal }}</p>
              </div>
              <div class="bg-slate-800/80 rounded-xl px-2 py-3">
                <p class="text-[10px] text-slate-500 font-bold uppercase mb-1">H2H</p>
                <p class="text-xs font-bold text-white">{{ h2hSignal }}</p>
              </div>
            </div>
          </div>

          <!-- Recent Form -->
          <div v-if="history" class="mb-8">
            <h3 class="text-xs font-bold text-slate-500 uppercase tracking-widest mb-4 px-1">Recent Form (Last 5)</h3>
            <div class="space-y-4">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium">{{ match.home }}</span>
                <div class="flex gap-1.5">
                  <div
                    v-for="(r, i) in homeRecentForm"
                    :key="i"
                    class="size-7 rounded-lg flex items-center justify-center text-[10px] font-bold text-white"
                    :class="formColor(r)"
                  >
                    {{ r }}
                  </div>
                </div>
              </div>
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium text-slate-400">{{ match.guest }}</span>
                <div class="flex gap-1.5">
                  <div
                    v-for="(r, i) in guestRecentForm"
                    :key="i"
                    class="size-7 rounded-lg flex items-center justify-center text-[10px] font-bold text-white"
                    :class="formColor(r)"
                  >
                    {{ r }}
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- H2H Stats -->
          <div v-if="history?.againstSummary" class="mb-8">
            <h3 class="text-xs font-bold text-slate-500 uppercase tracking-widest mb-4 px-1">Head-to-Head</h3>
            <div class="space-y-5 bg-slate-900/30 p-4 rounded-2xl">
              <div class="space-y-2">
                <div class="flex justify-between text-xs font-bold">
                  <span>{{ history.againstSummary.win }}</span>
                  <span class="text-slate-500 uppercase">Wins</span>
                  <span>{{ history.againstSummary.lose }}</span>
                </div>
                <div class="flex h-1.5 w-full bg-slate-800 rounded-full overflow-hidden">
                  <div class="bg-primary h-full" :style="{ width: h2hHomePct + '%' }"></div>
                  <div class="bg-rose-500 h-full ml-auto" :style="{ width: h2hAwayPct + '%' }"></div>
                </div>
              </div>
              <div class="space-y-2">
                <div class="flex justify-between text-xs font-bold">
                  <span>{{ history.againstSummary.winGoal }}</span>
                  <span class="text-slate-500 uppercase">Goals</span>
                  <span>{{ history.againstSummary.loseGoal }}</span>
                </div>
                <div class="flex h-1.5 w-full bg-slate-800 rounded-full overflow-hidden">
                  <div class="bg-primary h-full" :style="{ width: h2hGoalHomePct + '%' }"></div>
                  <div class="bg-rose-500 h-full ml-auto" :style="{ width: h2hGoalAwayPct + '%' }"></div>
                </div>
              </div>
            </div>
          </div>
        </template>

        <!-- H2H Tab -->
        <template v-if="activeTab === 'H2H' && history?.againstList">
          <div class="space-y-3">
            <div
              v-for="(m, i) in history.againstList.slice(0, 10)"
              :key="i"
              class="bg-slate-900/30 p-3 rounded-xl flex items-center justify-between"
            >
              <div class="text-xs text-slate-400">{{ m.matchTime?.slice(0, 10) }}</div>
              <div class="text-sm font-bold text-center flex-1">
                {{ m.home }} {{ m.goal?.[0] }} - {{ m.goal?.[1] }} {{ m.guest }}
              </div>
              <div class="text-xs text-slate-500">{{ m.league }}</div>
            </div>
          </div>
        </template>

        <!-- Expert Tips Tab Placeholder -->
        <template v-if="activeTab === 'Expert Tips'">
          <div class="bg-primary rounded-2xl p-5 text-white relative overflow-hidden">
            <div class="absolute top-0 right-0 -translate-y-1/2 translate-x-1/4 size-32 bg-white/10 rounded-full"></div>
            <div class="relative z-10">
              <div class="flex items-center gap-2 mb-3">
                <span class="material-symbols-outlined text-white text-xl">verified</span>
                <h3 class="font-bold">Expert Prediction</h3>
              </div>
              <p class="text-sm font-medium mb-4 opacity-90">敬请期待更多专家预测...</p>
            </div>
          </div>
        </template>

        <!-- Lineups Placeholder -->
        <template v-if="activeTab === 'Lineups'">
          <div class="relative w-full aspect-[4/3] bg-emerald-800 rounded-xl overflow-hidden shadow-inner border-2 border-emerald-900/50 flex items-center justify-center">
            <p class="text-white/60 text-sm">阵容数据待接入</p>
          </div>
        </template>
      </div>
    </template>

    <!-- Bottom Action Bar -->
    <div class="fixed bottom-0 left-0 right-0 bg-slate-900/95 backdrop-blur-md border-t border-slate-800 pb-8 pt-4 px-4 z-50">
      <div class="max-w-md mx-auto flex items-center gap-4">
        <div class="flex flex-col items-center px-2 cursor-pointer">
          <span class="material-symbols-outlined text-slate-400">equalizer</span>
          <span class="text-[10px] font-bold text-slate-400 mt-1">Live Stats</span>
        </div>
        <button class="flex-1 bg-primary text-white py-3.5 rounded-xl font-bold text-sm shadow-lg shadow-primary/30 flex items-center justify-center gap-2">
          <span class="material-symbols-outlined text-lg">edit_square</span>
          Place Your Prediction
        </button>
        <div class="flex flex-col items-center px-2 cursor-pointer">
          <span class="material-symbols-outlined text-slate-400">bookmark</span>
          <span class="text-[10px] font-bold text-slate-400 mt-1">Save</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { resolveAssetUrl } from '@/api/request'
import { useMatchStore } from '@/store/match'

const route = useRoute()
const router = useRouter()
const matchStore = useMatchStore()

const tabs = ['Analysis', 'Lineups', 'H2H', 'Expert Tips']
const activeTab = ref('Analysis')

const matchId = computed(() => route.params.id as string)
const match = computed(() => matchStore.currentMatch)
const history = computed(() => matchStore.currentHistory)
const oddsEuro = computed(() => matchStore.currentOddsEuro)
const loading = computed(() => matchStore.loading)

function parseOddsValue(value: string | undefined): number {
  if (!value) return 0
  return parseFloat(value) || 0
}

const averageOdds = computed(() => {
  if (oddsEuro.value?.avgOdds?.odds?.length) {
    return oddsEuro.value.avgOdds
  }

  const odds = oddsEuro.value?.data ?? []
  const sums = [0, 0, 0]
  const counts = [0, 0, 0]

  for (const odd of odds) {
    if (!odd.odds?.length) continue

    for (let index = 0; index < 3; index += 1) {
      const value = parseOddsValue(odd.odds[index])
      if (value <= 0) continue

      sums[index] += value
      counts[index] += 1
    }
  }

  if (counts.some((count) => count === 0)) {
    return null
  }

  return {
    companyId: 'average',
    companyName: '平均欧赔',
    odds: sums.map((sum, index) => (sum / counts[index]).toFixed(2)),
    firstOdds: [],
    returnRatio: '',
    firstReturnRatio: '',
    ratio: [],
    firstRatio: [],
    oddsTrend: [],
    ratioTrend: [],
    firstKelly: null,
    kelly: null,
  }
})

/** 从欧赔算出胜平负概率 */
const homeWinPct = computed(() => {
  const avg = averageOdds.value
  if (!avg?.odds?.length) return 33
  const h = parseOddsValue(avg.odds[0])
  const d = parseOddsValue(avg.odds[1])
  const a = parseOddsValue(avg.odds[2])
  if (!h || !d || !a) return 33
  const total = 1 / h + 1 / d + 1 / a
  return Math.round((1 / h / total) * 100)
})

const drawPct = computed(() => {
  const avg = averageOdds.value
  if (!avg?.odds?.length) return 34
  const h = parseOddsValue(avg.odds[0])
  const d = parseOddsValue(avg.odds[1])
  const a = parseOddsValue(avg.odds[2])
  if (!h || !d || !a) return 34
  const total = 1 / h + 1 / d + 1 / a
  return Math.round((1 / d / total) * 100)
})

const awayWinPct = computed(() => 100 - homeWinPct.value - drawPct.value)

const strongestProbability = computed(() => Math.max(homeWinPct.value, drawPct.value, awayWinPct.value))

const predictedPick = computed(() => {
  if (homeWinPct.value >= drawPct.value && homeWinPct.value >= awayWinPct.value) {
    return match.value?.home ?? '主队方向'
  }
  if (awayWinPct.value >= homeWinPct.value && awayWinPct.value >= drawPct.value) {
    return match.value?.guest ?? '客队方向'
  }
  return '平局方向'
})

const confidenceLabel = computed(() => {
  if (strongestProbability.value >= 50) return '高信心'
  if (strongestProbability.value >= 42) return '中信心'
  return '谨慎观察'
})

const confidenceClass = computed(() => {
  if (strongestProbability.value >= 50) return 'bg-emerald-500/15 text-emerald-300'
  if (strongestProbability.value >= 42) return 'bg-primary/15 text-primary'
  return 'bg-amber-500/15 text-amber-300'
})

const oddsSignal = computed(() => `${predictedPick.value} ${strongestProbability.value}%`)

/** 近期战绩提取 W/D/L */
function extractForm(list: any[] | null | undefined, teamName: string | undefined): string[] {
  if (!list?.length) return []
  const normalizedTeamName = teamName?.trim()
  return list.slice(0, 5).map((m: any) => {
    const homeGoal = m.goal?.[0] ?? 0
    const awayGoal = m.goal?.[1] ?? 0

    if (normalizedTeamName && m.guest === normalizedTeamName) {
      if (awayGoal > homeGoal) return 'W'
      if (awayGoal < homeGoal) return 'L'
      return 'D'
    }

    if (homeGoal > awayGoal) return 'W'
    if (homeGoal < awayGoal) return 'L'
    return 'D'
  })
}

const homeRecentForm = computed(() => extractForm(history.value?.recentHomeList, match.value?.home))
const guestRecentForm = computed(() => extractForm(history.value?.recentGuestList, match.value?.guest))

function scoreForm(form: string[]): number {
  return form.reduce((score, result) => {
    if (result === 'W') return score + 3
    if (result === 'D') return score + 1
    return score
  }, 0)
}

const homeFormScore = computed(() => scoreForm(homeRecentForm.value))
const guestFormScore = computed(() => scoreForm(guestRecentForm.value))

const formSignal = computed(() => {
  if (!homeRecentForm.value.length && !guestRecentForm.value.length) return '样本不足'
  const diff = homeFormScore.value - guestFormScore.value
  if (diff >= 3) return `${match.value?.home ?? '主队'}更稳`
  if (diff <= -3) return `${match.value?.guest ?? '客队'}更稳`
  return '接近均衡'
})

function formColor(r: string): string {
  if (r === 'W') return 'bg-emerald-500'
  if (r === 'L') return 'bg-rose-500'
  return 'bg-slate-500'
}

/** H2H 百分比 */
const h2hHomePct = computed(() => {
  const s = history.value?.againstSummary
  if (!s?.all) return 50
  return Math.round((s.win / s.all) * 100)
})
const h2hAwayPct = computed(() => {
  const s = history.value?.againstSummary
  if (!s?.all) return 50
  return Math.round((s.lose / s.all) * 100)
})
const h2hGoalHomePct = computed(() => {
  const s = history.value?.againstSummary
  const total = (s?.winGoal ?? 0) + (s?.loseGoal ?? 0)
  if (!total) return 50
  return Math.round(((s?.winGoal ?? 0) / total) * 100)
})
const h2hGoalAwayPct = computed(() => 100 - h2hGoalHomePct.value)

const h2hSignal = computed(() => {
  const summary = history.value?.againstSummary
  if (!summary?.all) return '样本不足'
  if (summary.win > summary.lose) return `${match.value?.home ?? '主队'}占优`
  if (summary.lose > summary.win) return `${match.value?.guest ?? '客队'}占优`
  return '交锋均衡'
})

const conclusionText = computed(() => {
  return `综合平均欧赔、近况和交锋记录，当前倾向 ${predictedPick.value}。赔率隐含概率最高为 ${strongestProbability.value}%，近期状态为${formSignal.value}，历史交锋为${h2hSignal.value}。`
})

function logoUrl(logo: string): string {
  return resolveAssetUrl(logo)
}

onMounted(() => {
  matchStore.fetchMatchAll(matchId.value)
})

onUnmounted(() => {
  matchStore.resetCurrent()
})
</script>
