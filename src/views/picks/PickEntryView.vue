<template>
  <div class="min-h-screen pb-24 bg-[#0b1020] text-white">
    <header class="sticky top-0 z-50 border-b border-slate-800 bg-[#0b1020]/95 backdrop-blur">
      <div class="px-4 py-3 max-w-3xl mx-auto">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0">
            <p class="text-[11px] text-slate-400 font-bold">仅完赛比赛 · 比分已隐藏，防止结果误导</p>
            <h1 class="text-xl font-black truncate">我的投注补录</h1>
          </div>
          <button
            class="size-10 rounded-lg bg-primary flex items-center justify-center disabled:opacity-60"
            :disabled="loading"
            title="刷新"
            @click="loadData"
          >
            <span class="material-symbols-outlined" :class="{ 'animate-spin': loading }">sync</span>
          </button>
        </div>

        <div class="mt-3 grid grid-cols-[auto_1fr_auto] gap-2 items-center">
          <button class="size-10 rounded-md border border-slate-700 bg-slate-900/80 flex items-center justify-center disabled:opacity-60" :disabled="loading" title="前一天" @click="shiftDate(-1)">
            <span class="material-symbols-outlined text-base">chevron_left</span>
          </button>
          <input
            v-model="selectedDate"
            type="date"
            class="h-10 min-w-0 rounded-md border border-slate-700 bg-slate-900/80 px-3 text-sm font-bold text-white [color-scheme:dark]"
            :disabled="loading"
            @change="loadData"
          />
          <button class="size-10 rounded-md border border-slate-700 bg-slate-900/80 flex items-center justify-center disabled:opacity-60" :disabled="loading" title="后一天" @click="shiftDate(1)">
            <span class="material-symbols-outlined text-base">chevron_right</span>
          </button>
        </div>

        <div class="mt-3 flex items-center gap-2">
          <div class="flex rounded-lg border border-slate-700 bg-slate-900/80 p-1">
            <button
              v-for="scope in scopes"
              :key="scope.value"
              class="h-8 rounded-md px-3 text-xs font-black transition"
              :class="matchScope === scope.value ? 'bg-primary text-white' : 'text-slate-400'"
              :disabled="loading"
              @click="setScope(scope.value)"
            >
              {{ scope.label }}
            </button>
          </div>
          <p class="text-xs font-bold text-slate-400">共 {{ list.length }} 场完赛 · 已录 {{ recordedCount }} 场</p>
        </div>
      </div>
    </header>

    <main class="max-w-3xl mx-auto px-3 py-4 space-y-4">
      <div v-if="loading" class="flex justify-center py-16">
        <span class="material-symbols-outlined text-4xl text-primary animate-spin">progress_activity</span>
      </div>

      <article
        v-for="item in list"
        :key="item.matchId"
        class="rounded-lg border border-slate-800 bg-white text-slate-950 overflow-hidden shadow-sm"
      >
        <div class="flex flex-wrap items-center gap-2 px-3 py-2 bg-slate-100 border-b border-slate-200">
          <span class="px-2 py-1 rounded bg-slate-950 text-white text-[11px] font-bold">{{ item.league || '-' }}</span>
          <span v-if="item.jingcaiId" class="px-2 py-1 rounded bg-primary/10 text-primary text-[11px] font-black">{{ item.jingcaiId }}</span>
          <span class="text-xs font-bold text-slate-500">{{ formatTime(item.matchTime) }}</span>
          <span
            class="ml-auto px-2 py-1 rounded text-[11px] font-black"
            :class="item.settled ? 'bg-slate-200 text-slate-600' : 'bg-emerald-100 text-emerald-700'"
          >
            {{ item.settled ? '已完赛 · 比分隐藏' : '未开赛 · 赛前记录' }}
          </span>
        </div>

        <section class="grid grid-cols-12 gap-2 px-3 py-3 items-center">
          <p class="col-span-5 text-center text-base font-black truncate">{{ item.home }}</p>
          <p class="col-span-2 text-center text-lg font-black text-slate-300">VS</p>
          <p class="col-span-5 text-center text-base font-black truncate">{{ item.guest }}</p>
        </section>

        <!-- 快速参考行 -->
        <section class="border-t border-slate-200 px-3 py-2 text-xs space-y-1 bg-slate-50">
          <div class="flex flex-wrap gap-x-4 gap-y-1 font-bold text-slate-600">
            <span>亚盘 {{ lineText(item.yapanpankou1) }}/{{ lineText(item.yapanpankou2) }}</span>
            <span>大小球 {{ lineText(item.qiushupankou1) }}/{{ lineText(item.qiushupankou2) }}</span>
            <span v-if="rqspfGoal(item)">让球线 {{ rqspfGoal(item) }}</span>
            <span>主胜{{ Math.round(item.winProbability) }}% 平{{ Math.round(item.drawProbability) }}% 客胜{{ Math.round(item.loseProbability) }}%</span>
          </div>
          <div v-if="item.platform" class="flex flex-wrap gap-x-4 gap-y-1 font-bold">
            <span class="text-slate-500">庄家：<b class="text-slate-900">{{ outcomeLabel(item.platform.bookmaker.outcome, item) }} / {{ item.platform.bookmaker.goal.label }} / {{ item.platform.bookmaker.score }}</b></span>
            <span class="text-slate-500">平台：<b class="text-slate-900">{{ outcomeLabel(item.platform.platform.outcome, item) }} / {{ item.platform.platform.goal.label }} / {{ item.platform.platform.score }}</b></span>
          </div>
          <button
            class="mt-1 flex h-8 w-full items-center justify-center gap-1 rounded-md border border-slate-300 bg-white text-xs font-black text-slate-700"
            @click="toggleDetail(item.matchId)"
          >
            <span class="material-symbols-outlined text-base">{{ expanded[item.matchId] ? 'expand_less' : 'expand_more' }}</span>
            {{ expanded[item.matchId] ? '收起完整分析' : '展开完整分析（复杂版）' }}
          </button>
        </section>

        <!-- 完整复杂版分析（全部后端数据） -->
        <section v-if="expanded[item.matchId] && item.platform" class="border-t border-slate-200 divide-y divide-slate-100 text-xs">
          <!-- 庄家/平台预测 -->
          <div class="px-3 py-3">
            <p class="mb-2 text-[11px] font-black text-slate-400">庄家 / 平台预测</p>
            <table class="w-full table-fixed">
              <thead class="text-slate-400">
                <tr><th class="w-[22%] py-1 text-left font-black">项目</th><th class="py-1 text-left font-black">庄家</th><th class="py-1 text-left font-black">平台</th></tr>
              </thead>
              <tbody class="divide-y divide-slate-100">
                <tr>
                  <td class="py-1.5 font-black text-slate-500">胜平负</td>
                  <td class="py-1.5 font-black" :class="toneClass(outcomeTone(item.platform.bookmaker.outcome))">{{ outcomeLabel(item.platform.bookmaker.outcome, item) }}</td>
                  <td class="py-1.5 font-black" :class="toneClass(outcomeTone(item.platform.platform.outcome))">{{ outcomeLabel(item.platform.platform.outcome, item) }}{{ item.platform.platform.warning ? `（${item.platform.platform.warning}）` : '' }}</td>
                </tr>
                <tr>
                  <td class="py-1.5 font-black text-slate-500">进球数</td>
                  <td class="py-1.5 font-black" :class="toneClass(item.platform.bookmaker.goal.tone)">{{ item.platform.bookmaker.goal.label }}</td>
                  <td class="py-1.5 font-black" :class="toneClass(item.platform.platform.goal.tone)">{{ item.platform.platform.goal.label }}</td>
                </tr>
                <tr>
                  <td class="py-1.5 font-black text-slate-500">比分</td>
                  <td class="py-1.5 font-black">{{ item.platform.bookmaker.score }}</td>
                  <td class="py-1.5 font-black">{{ item.platform.platform.score }}</td>
                </tr>
                <tr>
                  <td class="py-1.5 font-black text-slate-400">次选比分</td>
                  <td class="py-1.5 font-bold text-slate-600">{{ item.platform.bookmaker.secondaryScore }}</td>
                  <td class="py-1.5 font-bold text-slate-600">{{ item.platform.platform.secondaryScore }}</td>
                </tr>
              </tbody>
            </table>
            <div v-if="item.platform.warningRows.length" class="mt-2 space-y-1 font-black">
              <p v-for="warning in item.platform.warningRows" :key="warning.value" :class="warningClass(warning.tone)">{{ warning.value }}</p>
            </div>
            <p v-if="item.platform.warningAdjustedSummary" class="mt-1 font-black text-slate-800">{{ item.platform.warningAdjustedSummary }}</p>
          </div>

          <!-- 邪修 -->
          <div class="px-3 py-3">
            <p class="mb-2 text-[11px] font-black text-slate-400">邪修</p>
            <table class="w-full table-fixed">
              <thead class="text-slate-400">
                <tr><th class="w-[22%] py-1 text-left font-black">项目</th><th class="py-1 text-left font-black">小球组</th><th class="py-1 text-left font-black">追大组</th></tr>
              </thead>
              <tbody class="divide-y divide-slate-100">
                <tr v-for="row in item.platform.evilCult.rows" :key="row.label">
                  <td class="py-1.5 font-black text-slate-500">{{ row.label }}</td>
                  <td class="py-1.5 font-black" :class="toneClass(row.primaryTone)">{{ row.primary }}</td>
                  <td class="py-1.5 font-bold" :class="toneClass(row.secondaryTone)">{{ row.secondary }}</td>
                </tr>
              </tbody>
            </table>
            <div class="mt-2 rounded-md bg-slate-50 px-2 py-2 space-y-1">
              <p><b class="text-slate-400">一推</b> <b :class="toneClass(item.platform.evilCult.prediction.firstDirection === 'over' ? 'green' : 'red')">{{ item.platform.evilCult.prediction.firstPick }}</b></p>
              <p><b class="text-slate-400">二推</b> <b :class="toneClass(item.platform.evilCult.prediction.goalTone)">{{ item.platform.evilCult.prediction.mainPick }}</b></p>
              <p><b class="text-slate-400">反向推</b> <b :class="toneClass(item.platform.evilCult.prediction.reverseTone)">{{ item.platform.evilCult.prediction.reversePick }}</b></p>
              <p class="font-bold text-slate-500">{{ item.platform.evilCult.prediction.secondPassReason }}</p>
            </div>
          </div>

          <!-- 让球压力 -->
          <div class="px-3 py-3">
            <p class="mb-2 text-[11px] font-black text-slate-400">让球压力</p>
            <div class="grid grid-cols-2 gap-x-4 gap-y-1 font-bold text-slate-600">
              <span>历史期望让球：{{ pairPart(item.changguiyapan, 0) }}</span>
              <span>近期状态让球：{{ pairPart(item.changguiyapan, 1) }}</span>
              <span>综合均值：{{ averageText(item.combinedHandicapAverage) }}</span>
              <span>亚盘 初/即：{{ lineText(item.yapanpankou1) }}/{{ lineText(item.yapanpankou2) }}</span>
              <span>投注主队比例：{{ heatText(item.yapantouzhu?.[0]) }}</span>
              <span>投注客队比例：{{ heatText(item.yapantouzhu?.[1]) }}</span>
              <span class="col-span-2">压力方向：{{ textValue(item.yapantouzhu?.[12]) }}</span>
            </div>
            <div class="mt-2 space-y-1">
              <p v-for="row in item.platform.handicapAlertRows" :key="row.label + row.value" class="font-bold" :class="warningClass(row.tone)">
                <b>{{ row.label }}：</b>{{ row.value }}
              </p>
            </div>
          </div>

          <!-- 大小球 -->
          <div class="px-3 py-3">
            <p class="mb-2 text-[11px] font-black text-slate-400">大小球</p>
            <div class="grid grid-cols-2 gap-x-4 gap-y-1 font-bold text-slate-600">
              <span>历史平均球数：{{ pairPart(item.changguiqiushu, 0) }}</span>
              <span>近期平均球数：{{ pairPart(item.changguiqiushu, 1) }}</span>
              <span>综合均值：{{ averageText(item.combinedGoalAverage) }}</span>
              <span>大小球 初/即：{{ lineText(item.qiushupankou1) }}/{{ lineText(item.qiushupankou2) }}</span>
              <span>投注大球比例：{{ heatText(item.qiushutouzhu?.[0]) }}</span>
              <span>投注小球比例：{{ heatText(item.qiushutouzhu?.[1]) }}</span>
              <span>近5场均进：{{ textValue(item.qiushutouzhu?.[2]) }}</span>
              <span>近5场均失：{{ textValue(item.qiushutouzhu?.[3]) }}</span>
              <span class="col-span-2">压力方向：{{ textValue(item.qiushutouzhu?.[6]) }}</span>
              <span class="col-span-2">
                预测进球（上/主/下档）：
                {{ goalPairText(item.platform.goals.under) }} /
                <b class="text-slate-900">{{ goalPairText(item.platform.goals.main) }}</b> /
                {{ goalPairText(item.platform.goals.over) }}
              </span>
              <span v-if="item.platform.zeroGoalAdvice" class="col-span-2 text-red-600">{{ item.platform.zeroGoalAdvice }}</span>
            </div>
            <div class="mt-2 space-y-1">
              <p v-for="row in item.platform.goalBalanceAlertRows" :key="row.label + row.value" class="font-bold" :class="warningClass(row.tone)">
                <b>{{ row.label }}：</b>{{ row.value }}
              </p>
            </div>
          </div>

          <!-- 历史与近况 -->
          <div class="px-3 py-3">
            <p class="mb-2 text-[11px] font-black text-slate-400">历史与近况</p>
            <p class="font-black text-red-600">{{ item.sanhuxinli?.[4] || '样本不足' }}</p>
            <p class="mt-1 font-bold text-slate-600">{{ historyLine(item.liangduibisai) }}</p>
            <div class="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 font-bold text-slate-600">
              <span>主队近期：{{ recentMini(item.homezuijinbisai) }}</span>
              <span>客队近期：{{ recentMini(item.guestzuijinbisai) }}</span>
              <span>主历史胜率：{{ textValue(item.liangduilishi?.[0]) }}</span>
              <span>历史平局率：{{ textValue(item.liangduilishi?.[1]) }}</span>
              <span>客历史胜率：{{ textValue(item.liangduilishi?.[2]) }}</span>
              <span>历史均球：{{ textValue(item.liangduilishi?.[4]) }}</span>
              <span>主近5场进/失：{{ textValue(item.qiushuAll?.[0]) }}/{{ textValue(item.qiushuAll?.[4]) }}</span>
              <span>客近5场进/失：{{ textValue(item.qiushuAll?.[2]) }}/{{ textValue(item.qiushuAll?.[5]) }}</span>
            </div>
          </div>

          <!-- 赔率与凯利 -->
          <div class="px-3 py-3">
            <p class="mb-2 text-[11px] font-black text-slate-400">赔率与凯利</p>
            <div class="grid grid-cols-2 gap-x-4 gap-y-1 font-bold text-slate-600">
              <span>平均欧赔：{{ joinText(item.detail?.test8) }}</span>
              <span>散户心理：{{ joinText(item.sanhuxinli?.slice(0, 3)) }}</span>
              <span>凯利预测：{{ joinText(item.kailiresult) }}</span>
              <span>体彩预测：{{ joinText(item.ticairesult) }}</span>
            </div>
          </div>

          <!-- 庄家盈亏 -->
          <div class="px-3 py-3">
            <p class="mb-2 text-[11px] font-black text-slate-400">庄家盈亏（本地测算 + 竞彩交易）</p>
            <div v-if="item.platform.localMarket" class="overflow-x-auto">
              <table class="min-w-[430px] w-full text-center">
                <thead class="text-slate-400"><tr><th class="py-1 font-black text-left">本地测算</th><th class="py-1 font-black">欧赔</th><th class="py-1 font-black">散户</th><th class="py-1 font-black">庄家盈亏</th><th class="py-1 font-black">ROI</th></tr></thead>
                <tbody class="divide-y divide-slate-100">
                  <tr v-for="row in item.platform.localMarket.bookmakerByOutcome" :key="`local-${row.outcome}`">
                    <td class="py-1 text-left font-black" :class="toneClass(outcomeTone(row.outcome))">{{ shortOutcome(row.outcome) }}</td>
                    <td class="py-1">{{ row.odds.toFixed(2) }}</td>
                    <td class="py-1">{{ row.retailShare.toFixed(1) }}%</td>
                    <td class="py-1 font-black" :class="row.bookmakerProfit >= 0 ? 'text-emerald-700' : 'text-red-600'">{{ money(row.bookmakerProfit) }}</td>
                    <td class="py-1 font-black">{{ row.bookmakerRoi.toFixed(1) }}%</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-for="market in tradeMarkets(item)" :key="market.key" class="mt-2 overflow-x-auto">
              <table class="min-w-[430px] w-full text-center">
                <thead class="text-slate-400"><tr><th class="py-1 font-black text-left">{{ market.name }}</th><th class="py-1 font-black">指数</th><th class="py-1 font-black">支持率</th><th class="py-1 font-black">庄家盈亏</th><th class="py-1 font-black">官方盈亏率</th></tr></thead>
                <tbody class="divide-y divide-slate-100">
                  <tr v-for="row in market.bookmakerByOutcome" :key="`${market.key}-${row.outcome}`">
                    <td class="py-1 text-left font-black" :class="toneClass(outcomeTone(row.outcome))">{{ shortOutcome(row.outcome) }}</td>
                    <td class="py-1">{{ row.available ? row.odds.toFixed(2) : '-' }}</td>
                    <td class="py-1">{{ row.retailShare.toFixed(1) }}%</td>
                    <td class="py-1 font-black" :class="row.bookmakerProfit >= 0 ? 'text-emerald-700' : 'text-red-600'">{{ row.available ? money(row.bookmakerProfit) : '-' }}</td>
                    <td class="py-1 font-black">{{ row.officialProfitRate !== undefined && row.officialProfitRate !== null ? `${row.officialProfitRate}%` : '-' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </section>

        <!-- 选择录入 -->
        <section class="border-t border-slate-200 divide-y divide-slate-100">
          <div class="px-3 py-2">
            <div class="flex items-center gap-2">
              <p class="w-14 shrink-0 text-xs font-black text-slate-500">胜平负</p>
              <div class="flex flex-1 gap-1">
                <button
                  v-for="option in spfOptions"
                  :key="option"
                  class="h-8 flex-1 rounded-md border text-xs font-black transition"
                  :class="draft(item).spf.pick === option ? 'border-primary bg-primary text-white' : 'border-slate-200 bg-white text-slate-600'"
                  @click="togglePick(item, 'spf', option)"
                >
                  {{ option }}
                </button>
              </div>
            </div>
          </div>

          <div class="px-3 py-2">
            <div class="flex items-center gap-2">
              <p class="w-14 shrink-0 text-xs font-black text-slate-500">让球</p>
              <input
                v-model="draft(item).rqspf.line"
                type="text"
                placeholder="线"
                class="h-8 w-14 shrink-0 rounded-md border border-slate-200 px-1 text-center text-xs font-black text-slate-700"
              />
              <div class="flex flex-1 gap-1">
                <button
                  v-for="option in rqspfOptions"
                  :key="option"
                  class="h-8 flex-1 rounded-md border text-xs font-black transition"
                  :class="draft(item).rqspf.pick === option ? 'border-primary bg-primary text-white' : 'border-slate-200 bg-white text-slate-600'"
                  @click="togglePick(item, 'rqspf', option)"
                >
                  {{ option }}
                </button>
              </div>
            </div>
          </div>

          <div class="px-3 py-2">
            <div class="flex items-center gap-2">
              <p class="w-14 shrink-0 text-xs font-black text-slate-500">大小球</p>
              <input
                v-model="draft(item).dxq.line"
                type="text"
                placeholder="盘口"
                class="h-8 w-14 shrink-0 rounded-md border border-slate-200 px-1 text-center text-xs font-black text-slate-700"
              />
              <div class="flex flex-1 gap-1">
                <button
                  v-for="option in dxqOptions"
                  :key="option"
                  class="h-8 flex-1 rounded-md border text-xs font-black transition"
                  :class="draft(item).dxq.pick === option ? 'border-primary bg-primary text-white' : 'border-slate-200 bg-white text-slate-600'"
                  @click="togglePick(item, 'dxq', option)"
                >
                  {{ option }}
                </button>
              </div>
            </div>
          </div>

          <div class="px-3 py-2">
            <div class="flex items-center gap-2">
              <p class="w-14 shrink-0 text-xs font-black text-slate-500">比分</p>
              <input
                v-model="draft(item).score.pick"
                type="text"
                placeholder="如 2:1（可多个，逗号分隔）"
                class="h-8 flex-1 rounded-md border border-slate-200 px-2 text-xs font-black text-slate-700"
              />
            </div>
          </div>

          <div class="px-3 py-2 flex items-center gap-2">
            <p class="w-14 shrink-0 text-xs font-black text-slate-500">信心</p>
            <div class="flex gap-1">
              <button
                v-for="level in [1, 2, 3]"
                :key="level"
                class="h-8 w-10 rounded-md border text-xs font-black transition"
                :class="draft(item).confidence === level ? 'border-amber-500 bg-amber-500 text-white' : 'border-slate-200 bg-white text-slate-500'"
                @click="draft(item).confidence = draft(item).confidence === level ? 0 : level"
              >
                {{ '★'.repeat(level) }}
              </button>
            </div>
            <span v-if="savedLabel(item)" class="ml-1 text-[11px] font-black text-emerald-600">{{ savedLabel(item) }}</span>
            <button
              class="ml-auto h-9 rounded-md bg-slate-950 px-4 text-xs font-black text-white disabled:opacity-50"
              :disabled="savingId === item.matchId || !hasAnyPick(item)"
              @click="saveMatch(item)"
            >
              {{ savingId === item.matchId ? '保存中…' : '保存本场' }}
            </button>
          </div>
        </section>
      </article>

      <div v-if="!loading && list.length === 0" class="text-center py-16 text-slate-500">
        <span class="material-symbols-outlined text-4xl block mb-2">sports_soccer</span>
        <p>该日期暂无完赛比赛</p>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import picksApi from '@/api/picks'
import type { AnalysisMatch, BookmakerMarket, PickEntryMatch, PlatformGoalPair } from '@/types/analysis'

type Market = 'spf' | 'rqspf' | 'dxq' | 'score'

interface MarketDraft {
  pick: string
  line: string
  savedId: number | null
}

interface MatchDraft {
  spf: MarketDraft
  rqspf: MarketDraft
  dxq: MarketDraft
  score: MarketDraft
  confidence: number
}

const spfOptions = ['主胜', '平', '客胜']
const rqspfOptions = ['让胜', '让平', '让负']
const dxqOptions = ['大', '小']
const scopes: Array<{ value: 'sporttery' | 'all'; label: string }> = [
  { value: 'sporttery', label: '竞彩' },
  { value: 'all', label: '全部' },
]

const loading = ref(false)
const savingId = ref('')
const selectedDate = ref(localDateString(new Date()))
const matchScope = ref<'sporttery' | 'all'>('all')
const list = ref<PickEntryMatch[]>([])
const drafts = reactive<Record<string, MatchDraft>>({})
const expanded = reactive<Record<string, boolean>>({})
const recordedCount = ref(0)

function toggleDetail(matchId: string) {
  expanded[matchId] = !expanded[matchId]
}

function emptyMarketDraft(): MarketDraft {
  return { pick: '', line: '', savedId: null }
}

function draft(item: PickEntryMatch): MatchDraft {
  if (!drafts[item.matchId]) {
    const next: MatchDraft = {
      spf: emptyMarketDraft(),
      rqspf: emptyMarketDraft(),
      dxq: emptyMarketDraft(),
      score: emptyMarketDraft(),
      confidence: 0,
    }
    next.rqspf.line = rqspfGoal(item)
    next.dxq.line = lineText(item.qiushupankou2) === '-' ? '' : lineText(item.qiushupankou2)
    for (const pick of item.picks || []) {
      const slot = next[pick.market]
      if (!slot) continue
      slot.pick = pick.pick
      slot.line = pick.line === null || pick.line === undefined ? slot.line : String(pick.line)
      slot.savedId = pick.id
      if (pick.confidence) next.confidence = pick.confidence
    }
    drafts[item.matchId] = next
  }
  return drafts[item.matchId]
}

function togglePick(item: PickEntryMatch, market: Exclude<Market, 'score'>, option: string) {
  const slot = draft(item)[market]
  slot.pick = slot.pick === option ? '' : option
}

function hasAnyPick(item: PickEntryMatch): boolean {
  const current = draft(item)
  return Boolean(current.spf.pick || current.rqspf.pick || current.dxq.pick || current.score.pick.trim())
}

function savedLabel(item: PickEntryMatch): string {
  const current = drafts[item.matchId]
  if (!current) {
    return (item.picks?.length || 0) > 0 ? `已录 ${item.picks.length} 项` : ''
  }
  const saved = (['spf', 'rqspf', 'dxq', 'score'] as Market[]).filter((market) => current[market].savedId !== null).length
  return saved > 0 ? `已录 ${saved} 项` : ''
}

async function saveMatch(item: PickEntryMatch) {
  const current = draft(item)
  savingId.value = item.matchId
  try {
    for (const market of ['spf', 'rqspf', 'dxq', 'score'] as Market[]) {
      const slot = current[market]
      const pickText = slot.pick.trim()
      if (!pickText) {
        if (slot.savedId !== null) {
          await picksApi.deletePick(slot.savedId)
          slot.savedId = null
        }
        continue
      }
      const lineNumber = Number.parseFloat(slot.line)
      const { data } = await picksApi.savePick({
        matchId: item.matchId,
        market,
        pick: pickText,
        line: Number.isFinite(lineNumber) ? lineNumber : null,
        direction: 'self',
        confidence: current.confidence,
        source: item.settled ? 'backfill' : 'live',
      })
      slot.savedId = data.id
    }
    refreshRecordedCount()
  } finally {
    savingId.value = ''
  }
}

function refreshRecordedCount() {
  recordedCount.value = list.value.filter((item) => {
    const current = drafts[item.matchId]
    if (current) {
      return (['spf', 'rqspf', 'dxq', 'score'] as Market[]).some((market) => current[market].savedId !== null)
    }
    return (item.picks?.length || 0) > 0
  }).length
}

async function loadData() {
  loading.value = true
  try {
    const { data } = await picksApi.getPickEntryMatches({ date: selectedDate.value, scope: matchScope.value })
    list.value = data ?? []
    Object.keys(drafts).forEach((key) => delete drafts[key])
    Object.keys(expanded).forEach((key) => delete expanded[key])
    refreshRecordedCount()
  } finally {
    loading.value = false
  }
}

function setScope(scope: 'sporttery' | 'all') {
  matchScope.value = scope
  loadData()
}

function shiftDate(days: number) {
  const date = new Date(`${selectedDate.value}T00:00:00`)
  date.setDate(date.getDate() + days)
  selectedDate.value = localDateString(date)
  loadData()
}

function localDateString(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function formatTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

// ---- 展示格式化（只格式化后端数字，不做任何决策计算） ----

function lineText(value: number | undefined): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return '-'
  return String(value)
}

function heatText(value: unknown): string {
  const number = typeof value === 'number' ? value : Number.parseFloat(String(value ?? ''))
  if (!Number.isFinite(number)) return '-'
  return `${Math.round(number)}%`
}

function textValue(value: unknown): string {
  if (value === null || value === undefined || value === '') return '-'
  return String(value)
}

function joinText(values: unknown[] | undefined): string {
  return values?.length ? values.map((item) => String(item)).join('/') : '-'
}

function pairPart(value: string, index: number): string {
  const parts = String(value || '').split(':')
  return parts[index]?.trim() || '-'
}

// 综合均值由后端计算（null = 样本不足），这里只格式化。
function averageText(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '-'
  return value.toFixed(2)
}

function outcomeLabel(outcome: 'home' | 'draw' | 'away', item: AnalysisMatch): string {
  if (outcome === 'home') return `主胜(${item.home})`
  if (outcome === 'away') return `客胜(${item.guest})`
  return '平局'
}

function shortOutcome(outcome: string): string {
  if (outcome === 'home') return '主胜'
  if (outcome === 'away') return '客胜'
  return '平局'
}

function outcomeTone(outcome: string): string {
  if (outcome === 'home') return 'red'
  if (outcome === 'away') return 'green'
  return 'blue'
}

function toneClass(tone: string | undefined): string {
  if (tone === 'green') return 'text-emerald-700'
  if (tone === 'red') return 'text-red-600'
  if (tone === 'blue') return 'text-sky-700'
  return 'text-slate-800'
}

function warningClass(tone: string | undefined): string {
  if (tone === 'green') return 'text-emerald-600'
  if (tone === 'blue') return 'text-sky-600'
  if (tone === 'red') return 'text-red-600'
  return 'text-slate-600'
}

function goalPairText(pair: PlatformGoalPair | undefined): string {
  if (!pair || pair.home === null || pair.guest === null) return '-'
  return `${Math.round(pair.home)}:${Math.round(pair.guest)}`
}

function money(value: number): string {
  const absolute = Math.abs(value)
  const sign = value < 0 ? '-' : '+'
  if (absolute >= 100000000) return `${sign}${(absolute / 100000000).toFixed(2)}亿`
  if (absolute >= 10000) return `${sign}${(absolute / 10000).toFixed(0)}万`
  return `${sign}${absolute.toFixed(0)}`
}

function historyLine(value: unknown[] | undefined): string {
  if (!value?.length) return '暂无历史交锋'
  return `${value[0] || ''} ${value[1] || ''} VS ${value[2] || ''} ${value[3] ?? '-'}:${value[4] ?? '-'} ${value[5] || ''}`
}

function recentMini(value: unknown[] | undefined): string {
  if (!value?.length) return '-'
  return `${String(value[1] || '').slice(0, 4)} VS ${String(value[2] || '').slice(0, 4)} ${value[3] ?? '-'}:${value[4] ?? '-'}`
}

function tradeMarkets(item: PickEntryMatch): BookmakerMarket[] {
  return (item.roiSimulation?.markets ?? []).filter((market) => ['sporttery', 'sportterySim', 'sportteryRqspf', 'sportteryRqspfSim'].includes(market.key))
}

function rqspfGoal(item: AnalysisMatch): string {
  const market = item.roiSimulation?.markets?.find((entry) => entry.key === 'sportteryRqspf')
  return market?.goal ? String(market.goal) : ''
}

onMounted(loadData)
</script>
