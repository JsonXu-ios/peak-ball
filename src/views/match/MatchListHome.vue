<template>
  <div class="min-h-screen pb-24 bg-[#0b1020] text-white">
    <header class="sticky top-0 z-50 border-b border-slate-800 bg-[#0b1020]/95 backdrop-blur">
      <div class="px-4 py-3 max-w-5xl mx-auto">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0">
            <p class="text-[11px] text-slate-400 font-bold">核心页面 · {{ matchScopeLabel }} {{ selectedDateLabel }}</p>
            <h1 class="text-xl font-black truncate">比赛分析工作台</h1>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <div class="flex rounded-lg border border-slate-700 bg-slate-900/80 p-1" aria-label="比赛范围">
              <button
                v-for="scope in matchScopes"
                :key="scope.value"
                class="h-8 rounded-md px-2.5 text-xs font-black transition disabled:opacity-60"
                :class="matchScope === scope.value ? 'bg-primary text-white shadow-sm' : 'text-slate-400 hover:text-white'"
                :aria-pressed="matchScope === scope.value"
                :disabled="loading"
                @click="setMatchScope(scope.value)"
              >
                {{ scope.label }}
              </button>
            </div>
            <select
              v-model="selectedLeague"
              class="h-10 w-24 rounded-lg border border-slate-700 bg-slate-900/80 px-2 text-xs font-black text-white [color-scheme:dark] disabled:opacity-60 sm:w-32"
              :disabled="loading"
              aria-label="选择联赛"
              title="选择联赛"
              @change="applyLeagueSelection"
            >
              <option value="all">全部联赛</option>
              <option v-for="league in availableLeagues" :key="league" :value="league">{{ league }}</option>
            </select>
            <button
              class="size-10 rounded-lg border border-slate-700 bg-slate-900/80 flex items-center justify-center"
              title="投注补录"
              @click="router.push('/picks')"
            >
              <span class="material-symbols-outlined text-base">edit_note</span>
            </button>
            <button
              class="size-10 rounded-lg bg-primary flex items-center justify-center disabled:opacity-60"
              :disabled="loading"
              title="刷新比赛"
              @click="loadData"
            >
              <span class="material-symbols-outlined" :class="{ 'animate-spin': loading }">sync</span>
            </button>
          </div>
        </div>

        <div class="mt-3 grid grid-cols-[auto_1fr_auto_auto] gap-2 items-center">
          <button
            class="size-10 rounded-md border border-slate-700 bg-slate-900/80 flex items-center justify-center disabled:opacity-60"
            :disabled="loading"
            title="前一天"
            @click="shiftDate(-1)"
          >
            <span class="material-symbols-outlined text-base">chevron_left</span>
          </button>
          <input
            v-model="selectedDate"
            type="date"
            class="h-10 min-w-0 rounded-md border border-slate-700 bg-slate-900/80 px-3 text-sm font-bold text-white [color-scheme:dark]"
            :disabled="loading"
            @change="setDate(selectedDate)"
          />
          <button
            class="h-10 rounded-md border border-slate-700 bg-slate-900/80 px-3 text-xs font-black disabled:opacity-60"
            :disabled="loading || selectedDate === todayString"
            @click="setDate(todayString)"
          >
            今天
          </button>
          <button
            class="size-10 rounded-md border border-slate-700 bg-slate-900/80 flex items-center justify-center disabled:opacity-60"
            :disabled="loading"
            title="后一天"
            @click="shiftDate(1)"
          >
            <span class="material-symbols-outlined text-base">chevron_right</span>
          </button>
        </div>

        <div class="mt-3 grid grid-cols-4 gap-2 rounded-md border border-slate-800 bg-slate-950/60 p-1">
          <button
            v-for="mode in analysisViewModes"
            :key="mode.value"
            class="h-9 rounded px-2 text-xs font-black transition"
            :class="viewMode === mode.value ? 'bg-primary text-white shadow-sm' : 'text-slate-400 hover:bg-slate-900 hover:text-white'"
            @click="setViewMode(mode.value)"
          >
            {{ mode.label }}
          </button>
        </div>

        <div v-if="viewMode !== 'minimal' && viewMode !== 'stats'" class="mt-3 grid grid-cols-3 gap-2">
          <div class="rounded-md border border-slate-800 bg-slate-900/80 px-3 py-2">
            <p class="text-[10px] text-slate-500 font-bold">{{ matchScopeLabel }}</p>
            <p class="text-lg font-black">{{ list.length }}</p>
          </div>
          <div class="rounded-md border border-slate-800 bg-slate-900/80 px-3 py-2">
            <p class="text-[10px] text-slate-500 font-bold">高信心</p>
            <p class="text-lg font-black text-emerald-300">{{ highConfidenceCount }}</p>
          </div>
          <div class="rounded-md border border-slate-800 bg-slate-900/80 px-3 py-2">
            <p class="text-[10px] text-slate-500 font-bold">完场命中</p>
            <p class="text-lg font-black text-primary">{{ accuracyLabel }}%</p>
          </div>
        </div>
      </div>
    </header>

    <main class="max-w-5xl mx-auto px-3 py-4 space-y-4">
      <div v-if="loading && viewMode !== 'stats'" class="flex justify-center py-16">
        <span class="material-symbols-outlined text-4xl text-primary animate-spin">progress_activity</span>
      </div>

      <section v-if="viewMode === 'stats'" class="rounded-lg border border-slate-800 bg-white text-slate-950 overflow-hidden shadow-sm">
        <div class="flex items-center justify-between gap-3 border-b border-slate-200 bg-slate-100 px-3 py-3">
          <div>
            <p class="text-[11px] font-black text-slate-500">历史完赛累计</p>
            <h2 class="text-base font-black">庄家 / 平台命中统计</h2>
            <p class="mt-1 text-xs font-bold text-slate-500">{{ accuracyStatsRangeText }}</p>
          </div>
          <button
            class="size-10 rounded-md bg-slate-950 text-white flex items-center justify-center disabled:opacity-60"
            :disabled="accuracyStatsLoading"
            @click="loadAccuracyStats"
          >
            <span class="material-symbols-outlined" :class="{ 'animate-spin': accuracyStatsLoading }">sync</span>
          </button>
        </div>

        <div class="p-3">
          <div v-if="accuracyStatsLoading" class="flex justify-center py-10">
            <span class="material-symbols-outlined text-3xl text-primary animate-spin">progress_activity</span>
          </div>
          <div v-else-if="accuracyStatsError" class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm font-bold text-red-700">
            {{ accuracyStatsError }}
          </div>
          <div v-else>
            <div class="mb-3 grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
              <Metric label="完场样本" :value="String(accuracyStats.total)" />
              <Metric label="统计周期" :value="accuracyStatsRangeText" />
              <Metric label="庄家综合正确率" :value="accuracyRateText(accuracyStats.overall.bookmakerCorrect, accuracyStats.overall.sample)" />
              <Metric label="平台综合正确率" :value="accuracyRateText(accuracyStats.overall.platformCorrect, accuracyStats.overall.sample)" />
            </div>
            <div class="overflow-hidden rounded-md border border-slate-200">
              <table class="w-full table-fixed text-sm">
                <thead class="bg-slate-50 text-xs font-black text-slate-500">
                  <tr>
                    <th class="px-2 py-2 text-left">项目</th>
                    <th class="px-2 py-2 text-center">庄家</th>
                    <th class="px-2 py-2 text-center">平台</th>
                    <th class="px-2 py-2 text-center">双中/比分命中</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100">
                  <tr v-for="row in accuracyStats.rows" :key="row.label">
                    <td class="px-2 py-3 font-black text-slate-500">{{ row.label }}</td>
                    <td class="px-2 py-3 text-center font-black text-slate-900">{{ accuracyRateText(row.bookmakerCorrect, row.sample) }}</td>
                    <td class="px-2 py-3 text-center font-black text-slate-900">{{ accuracyRateText(row.platformCorrect, row.sample) }}</td>
                    <td class="px-2 py-3 text-center font-black text-emerald-700">{{ accuracyRateText(row.bothCorrect, row.sample) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="mt-3 overflow-hidden rounded-md border border-slate-200">
              <div class="bg-slate-50 px-3 py-2">
                <p class="text-xs font-black text-slate-500">邪修正确率</p>
                <p class="mt-1 text-[11px] font-bold text-slate-400">按完场结果统计小球组、追大组、一推、反诱导二推和反向推；大小球统一按半球盘口计算。</p>
              </div>
              <table class="w-full table-fixed text-sm">
                <thead class="bg-slate-50 text-xs font-black text-slate-500">
                  <tr>
                    <th class="px-2 py-2 text-left">项目</th>
                    <th class="px-2 py-2 text-center">小球组</th>
                    <th class="px-2 py-2 text-center">追大组</th>
                    <th class="px-2 py-2 text-center">一推</th>
                    <th class="px-2 py-2 text-center">二推</th>
                    <th class="px-2 py-2 text-center">反向推</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100">
                  <tr v-for="row in accuracyStats.evilCultRows" :key="row.label">
                    <td class="px-2 py-3 font-black text-slate-500">{{ row.label }}</td>
                    <td class="px-2 py-3 text-center font-black text-red-600">{{ accuracyRateText(row.underCorrect, row.sample) }}</td>
                    <td class="px-2 py-3 text-center font-black text-emerald-700">{{ accuracyRateText(row.overCorrect, row.sample) }}</td>
                    <td class="px-2 py-3 text-center font-black text-slate-700">{{ accuracyRateText(row.firstCorrect, row.sample) }}</td>
                    <td class="px-2 py-3 text-center font-black text-slate-900">{{ accuracyRateText(row.mainCorrect, row.sample) }}</td>
                    <td class="px-2 py-3 text-center font-black text-primary">{{ accuracyRateText(row.reverseCorrect, row.sample) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="mt-3 overflow-hidden rounded-md border border-slate-200">
              <div class="bg-slate-50 px-3 py-2">
                <p class="text-xs font-black text-slate-500">选择日期符合规律的完赛</p>
                <p class="mt-1 text-[11px] font-bold text-slate-400">固定规则池不随选择日期重算，只用来匹配当前日期里的已完赛，并标出实际命中结果。</p>
              </div>
              <div v-if="accuracyStats.settledFitRows.length" class="divide-y divide-slate-100">
                <div v-for="row in accuracyStats.settledFitRows" :key="`settled-${row.matchId}`" class="px-3 py-3">
                  <div class="mb-2 flex items-center justify-between gap-2">
                    <div class="min-w-0">
                      <p class="truncate text-sm font-black text-slate-800">{{ row.matchTitle }}</p>
                      <p class="text-xs font-bold text-slate-400">{{ row.date }} {{ row.league }} {{ row.time }}</p>
                    </div>
                    <span class="shrink-0 rounded px-2 py-1 text-xs font-black" :class="accuracyFitClass(row.resultTone)">{{ row.resultSummary }}</span>
                  </div>
                  <div class="grid grid-cols-3 gap-2 text-xs">
                    <div class="rounded border border-slate-100 bg-slate-50 px-2 py-2">
                      <p class="font-black text-slate-500">胜平负</p>
                      <p class="mt-1 font-black" :class="accuracyFitTextClass(row.outcomeFit.tone)">{{ row.outcomeFit.label }}</p>
                    </div>
                    <div class="rounded border border-slate-100 bg-slate-50 px-2 py-2">
                      <p class="font-black text-slate-500">大小球</p>
                      <p class="mt-1 font-black" :class="accuracyFitTextClass(row.goalFit.tone)">{{ row.goalFit.label }}</p>
                    </div>
                    <div class="rounded border border-slate-100 bg-slate-50 px-2 py-2">
                      <p class="font-black text-slate-500">比分</p>
                      <p class="mt-1 font-black" :class="accuracyFitTextClass(row.scoreFit.tone)">{{ row.scoreFit.label }}</p>
                    </div>
                  </div>
                  <p class="mt-2 text-xs font-bold text-slate-500">{{ row.evidence }}</p>
                </div>
              </div>
              <p v-else class="px-3 py-4 text-xs font-bold text-slate-400">当前历史池里暂无匹配度足够高的完赛。</p>
            </div>
            <p class="mt-2 text-xs font-bold text-slate-500">注：胜平负按赛果判断；大小球按预测“以上/以内/左右/区间”和实际总进球判断；比分按精确比分判断，庄家或平台任一命中即算命中。</p>
          </div>
        </div>
      </section>

      <template v-else>
      <article
        v-for="item in list"
        :key="item.matchId"
        class="rounded-lg border border-slate-800 bg-white text-slate-950 overflow-hidden shadow-sm"
      >
        <div v-if="viewMode !== 'minimal'" class="flex flex-wrap items-center gap-2 px-3 py-2 bg-slate-100 border-b border-slate-200">
          <span class="px-2 py-1 rounded bg-slate-950 text-white text-[11px] font-bold">{{ item.league || '-' }}</span>
          <span v-if="item.jingcaiId" class="px-2 py-1 rounded bg-primary/10 text-primary text-[11px] font-black">{{ item.jingcaiId }}</span>
          <span class="text-xs font-bold text-slate-500">{{ formatTime(item.matchTime) }}</span>
          <span class="text-xs font-bold text-slate-500">{{ item.displayState || '未开赛' }}</span>
          <span class="ml-auto px-2 py-1 rounded bg-emerald-50 text-emerald-700 text-[11px] font-black">{{ item.confidence }}</span>
        </div>

        <section class="grid grid-cols-12 gap-2 px-3 py-4 items-center">
          <div class="col-span-5 text-center min-w-0">
            <div class="mx-auto mb-2 size-12 rounded-full bg-slate-100 border border-slate-200 overflow-hidden flex items-center justify-center">
              <img v-if="item.homeLogo" :src="logoUrl(item.homeLogo)" :alt="item.home" class="h-full w-full object-contain p-1" />
              <span v-else class="text-sm font-black text-slate-400">{{ teamInitial(item.home) }}</span>
            </div>
            <p class="text-lg font-black truncate">{{ item.home }}</p>
            <p v-if="viewMode !== 'minimal'" class="text-xs text-slate-500 mt-1">{{ rankLabel(item.homeRank) }}</p>
          </div>
          <div class="col-span-2 text-center">
            <p class="text-[11px] text-slate-500 font-bold">{{ matchScoreText(item) }}</p>
            <template v-if="viewMode !== 'minimal'">
              <p class="text-xl font-black text-primary leading-tight">{{ item.prediction }}</p>
              <p class="text-xs font-bold text-[#a60056]">{{ item.qiuprediction }}</p>
            </template>
          </div>
          <div class="col-span-5 text-center min-w-0">
            <div class="mx-auto mb-2 size-12 rounded-full bg-slate-100 border border-slate-200 overflow-hidden flex items-center justify-center">
              <img v-if="item.guestLogo" :src="logoUrl(item.guestLogo)" :alt="item.guest" class="h-full w-full object-contain p-1" />
              <span v-else class="text-sm font-black text-slate-400">{{ teamInitial(item.guest) }}</span>
            </div>
            <p class="text-lg font-black truncate">{{ item.guest }}</p>
            <p v-if="viewMode !== 'minimal'" class="text-xs text-slate-500 mt-1">{{ rankLabel(item.guestRank) }}</p>
          </div>
        </section>

        <section v-if="viewMode !== 'minimal'" class="px-3 pb-4">
          <div class="grid grid-cols-3 gap-2">
            <ProbabilityBar label="主胜" :value="item.winProbability" color="bg-emerald-500" />
            <ProbabilityBar label="平局" :value="item.drawProbability" color="bg-amber-500" />
            <ProbabilityBar label="客胜" :value="item.loseProbability" color="bg-sky-500" />
          </div>
        </section>

        <section class="border-t border-slate-200 divide-y divide-slate-200">
          <AnalysisSection v-if="viewMode === 'full'" title="结论" icon="psychology">
            <p class="text-sm font-bold">主推 {{ item.prediction }}，大小球倾向 {{ item.qiuprediction }}。</p>
            <p v-if="item.warnings?.length" class="text-xs text-amber-700 mt-1">{{ joinText(item.warnings) }}</p>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'full' && item.goddessWoman" :title="item.goddessWoman.title || '上帝的女人'" icon="woman">
            <div class="grid grid-cols-3 gap-2">
              <ProbabilityBar label="胜" :value="item.goddessWoman.probabilities.home" color="bg-rose-500" />
              <ProbabilityBar label="平" :value="item.goddessWoman.probabilities.draw" color="bg-amber-500" />
              <ProbabilityBar label="负" :value="item.goddessWoman.probabilities.away" color="bg-fuchsia-500" />
            </div>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'minimal'" title="赔率与凯利" icon="query_stats">
            <div class="grid grid-cols-2 gap-2 text-sm">
              <Metric label="平均欧赔" :value="joinText(item.detail.test8)" />
              <Metric label="散户心理" :value="sanhuCompareText(item)" />
              <Metric label="凯利预测" :value="joinText(item.kailiresult)" />
              <Metric label="体彩预测" :value="joinText(item.ticairesult)" />
            </div>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'minimal'" title="盘口信号" icon="tune">
            <GuideMetaTable :rows="guideMetaRows(item)" />
          </AnalysisSection>

          <AnalysisSection v-if="viewMode !== 'minimal' && item.myAngle && item.myAngle.totalPicks > 0" title="我的镜像" icon="person_search">
            <div class="space-y-1 text-xs">
              <p v-for="row in myAngleRows(item)" :key="row.label" class="flex items-center justify-between gap-2">
                <span class="font-black text-slate-500">{{ row.label }}</span>
                <span class="font-bold text-slate-700">{{ row.stat }}</span>
                <span class="shrink-0 rounded px-2 py-0.5 font-black" :class="row.verdictClass">{{ row.verdictText }}</span>
              </p>
              <p class="text-[11px] font-bold text-slate-400">按你已录 {{ item.myAngle.totalPicks }} 条选择，在本场同类盘型下的历史表现；红区≥65%可信直觉，黑区≤35%考虑反向（样本≥5才判定）。</p>
            </div>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode !== 'minimal'" :title="guideSectionTitle" icon="ssid_chart">
            <GuideCompareTable :rows="guideCompareRows(item)" :minimal="false" />
            <GuideMetaTable :rows="guideMetaRows(item)" />
            <div v-if="guideWarningRows(item).length" class="mt-1 space-y-1 text-xs font-black">
              <p v-for="warning in guideWarningRows(item)" :key="warning.value" :class="guideWarningClass(warning.tone)">{{ warning.value }}</p>
            </div>
            <p v-if="guideWarningPredictionSummary(item)" class="mt-1 text-xs font-black text-slate-800">{{ guideWarningPredictionSummary(item) }}</p>
          </AnalysisSection>

          <AnalysisSection title="邪修" icon="sports_soccer">
            <div class="overflow-hidden rounded-md border border-slate-200">
              <table class="w-full table-fixed text-xs">
                <thead class="bg-slate-50 text-slate-500">
                  <tr>
                    <th class="w-[24%] px-2 py-2 text-left font-black">项目</th>
                    <th class="px-2 py-2 text-left font-black">小球组</th>
                    <th class="px-2 py-2 text-left font-black">追大组</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100">
                  <tr v-for="row in evilCultRows(item)" :key="`${item.matchId}-${row.label}`" :class="row.variant === 'note' ? 'bg-slate-50' : ''">
                    <td class="px-2 py-2 font-black text-slate-500">{{ row.label }}</td>
                    <td class="px-2 py-2 font-black" :class="evilCultClass(row.primaryTone || row.tone)">{{ row.primary }}</td>
                    <td class="px-2 py-2 font-bold" :class="evilCultClass(row.secondaryTone || 'normal')">{{ row.secondary }}</td>
                  </tr>
                </tbody>
              </table>
              <div class="border-t border-slate-200 bg-slate-50 px-3 py-2">
                <p class="text-[11px] font-black text-slate-500">一推</p>
                <p class="mt-1 text-sm font-black" :class="evilCultClass(evilCultPrediction(item).firstDirection === 'over' ? 'green' : 'red')">{{ evilCultPrediction(item).firstPick }}</p>
                <p class="mt-2 text-[11px] font-black text-slate-500">二推（最终主推）</p>
                <p class="mt-1 text-sm font-black" :class="evilCultClass(evilCultPrediction(item).goalTone)">{{ evilCultPrediction(item).mainPick }}</p>
                <p class="mt-1 text-[11px] font-bold" :class="evilCultPrediction(item).secondPassReversed ? 'text-blue-700' : 'text-slate-500'">{{ evilCultPrediction(item).secondPassReason }}</p>
                <p class="mt-2 text-[11px] font-black text-slate-500">反向推</p>
                <p class="mt-1 text-sm font-black" :class="evilCultClass(evilCultPrediction(item).reverseTone)">{{ evilCultPrediction(item).reversePick }}</p>
                <button
                  class="mt-3 flex h-9 w-full items-center justify-center gap-1 rounded-md border border-slate-300 bg-white text-xs font-black text-slate-700"
                  @click.stop="openEvilCultAudit(item)"
                >
                  <span class="material-symbols-outlined text-base">account_tree</span>
                  查看主推逻辑
                </button>
              </div>
            </div>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode !== 'minimal' && accuracyMatchRow(item)" title="匹配历史规律" icon="rule">
            <div class="flex items-center justify-between gap-2">
              <span class="rounded px-2 py-1 text-xs font-black" :class="accuracyFitClass(accuracyMatchRow(item)!.tone)">{{ accuracyMatchRow(item)!.conclusion }}</span>
              <span class="rounded px-2 py-1 text-xs font-black" :class="accuracyFitClass(accuracyMatchRow(item)!.resultTone)">{{ accuracyMatchRow(item)!.resultSummary }}</span>
            </div>
            <div class="mt-2 grid grid-cols-3 gap-2 text-xs">
              <div class="rounded border border-slate-100 bg-slate-50 px-2 py-2">
                <p class="font-black text-slate-500">胜平负</p>
                <p class="mt-1 font-black" :class="accuracyFitTextClass(accuracyMatchRow(item)!.outcomeFit.tone)">{{ accuracyMatchRow(item)!.outcomeFit.label }}</p>
              </div>
              <div class="rounded border border-slate-100 bg-slate-50 px-2 py-2">
                <p class="font-black text-slate-500">大小球</p>
                <p class="mt-1 font-black" :class="accuracyFitTextClass(accuracyMatchRow(item)!.goalFit.tone)">{{ accuracyMatchRow(item)!.goalFit.label }}</p>
              </div>
              <div class="rounded border border-slate-100 bg-slate-50 px-2 py-2">
                <p class="font-black text-slate-500">比分</p>
                <p class="mt-1 font-black" :class="accuracyFitTextClass(accuracyMatchRow(item)!.scoreFit.tone)">{{ accuracyMatchRow(item)!.scoreFit.label }}</p>
              </div>
            </div>
            <p class="mt-2 text-xs font-bold text-slate-500">{{ accuracyMatchRow(item)!.evidence }}</p>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'full' && showBookmakerSection(item)" title="庄家盈亏" icon="monitoring">
            <div class="grid grid-cols-2 gap-2 text-sm">
              <Metric label="测算基数" :value="moneyCompactText(localStakeBase(item))" />
              <Metric label="本地欧赔" :value="localOddsTriplet(item)" />
              <Metric label="散户心理" :value="localRetailTriplet(item)" />
              <Metric label="盈亏来源" :value="marketNames(item)" />
            </div>

            <div v-if="hasLocalProfitMarket(item)" class="mt-3 overflow-hidden rounded-md border border-emerald-200 bg-white text-xs">
              <div class="border-l-2 border-emerald-500 bg-emerald-50 px-3 py-2 font-black text-slate-900">本地庄家盈亏</div>
              <div class="overflow-x-auto">
                <table class="min-w-[760px] w-full text-center">
                  <thead class="bg-emerald-50 text-slate-500">
                    <tr>
                      <th class="px-2 py-2 font-bold">赛果</th>
                      <th class="px-2 py-2 font-bold">平均欧赔</th>
                      <th class="px-2 py-2 font-bold">散户心理</th>
                      <th class="px-2 py-2 font-bold">交易额</th>
                      <th class="px-2 py-2 font-bold">庄家赔付</th>
                      <th class="px-2 py-2 font-bold">本地庄家盈亏</th>
                      <th class="px-2 py-2 font-bold">本地 ROI</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-slate-100">
                    <tr v-for="row in localProfitRows(item)" :key="`${item.matchId}-${row.outcome}-local-profit`">
                      <td class="px-2 py-2 font-black" :class="outcomeClass(row)">{{ outcomeName(row) }}</td>
                      <td class="px-2 py-2">{{ oddsText(row) }}</td>
                      <td class="px-2 py-2 font-black" :class="supportClass(row.retailShare)">{{ formatShare(row.retailShare) }}</td>
                      <td class="px-2 py-2">{{ moneyCompactText(row.betStake) }}</td>
                      <td class="px-2 py-2">{{ moneyCompactText(row.payout) }}</td>
                      <td class="px-2 py-2 font-black" :class="bookmakerClass(row)">{{ signedMoneyText(row.bookmakerProfit, row.available) }}</td>
                      <td class="px-2 py-2 font-black" :class="roiClass(row.bookmakerRoi, row.available)">{{ formatRoi(row.bookmakerRoi, row.available) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <p class="border-t border-slate-100 px-3 py-2 text-[11px] text-slate-500">注：本地测算仅使用平均欧赔与加权散户心理（隐含概率经亚盘/大小球/历史/近况/均值权重偏移，再做羊群放大），不使用竞彩投注数据</p>
            </div>

            <div v-if="sportteryRatioMarket(item)" class="mt-3 overflow-hidden rounded-md border border-slate-200 bg-white text-xs">
              <div class="border-l-2 border-red-500 bg-slate-50 px-3 py-2 font-black text-slate-900">竞彩投注比例</div>
              <div class="overflow-x-auto">
                <table class="min-w-[640px] w-full text-center">
                  <thead class="bg-slate-50 text-slate-500">
                    <tr>
                      <th class="px-2 py-2 font-bold">赛果</th>
                      <th class="px-2 py-2 font-bold">指数</th>
                      <th class="px-2 py-2 font-bold">概率</th>
                      <th class="px-2 py-2 font-bold">彩民支持率</th>
                      <th class="px-2 py-2 font-bold">误差</th>
                      <th class="px-2 py-2 font-bold">心理误差</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-slate-100">
                    <tr v-for="(row, index) in sportteryRows(item)" :key="`${item.matchId}-${row.outcome}-sporttery-ratio`">
                      <td class="px-2 py-2 font-black" :class="outcomeClass(row)">{{ outcomeName(row) }}</td>
                      <td class="px-2 py-2">{{ oddsText(row) }}</td>
                      <td class="px-2 py-2">{{ percentValueText(row.probability) }}</td>
                      <td class="px-2 py-2 font-black" :class="supportClass(row.retailShare)">{{ formatShare(row.retailShare) }}</td>
                      <td class="px-2 py-2">{{ signedPercentText(row.error) }}</td>
                      <td v-if="index === 0" class="px-2 py-2 font-black text-slate-700" :rowspan="sportteryRows(item).length">
                        {{ sportteryPsychologyLabel(item) }}
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <p class="border-t border-slate-100 px-3 py-2 text-[11px] text-slate-500">注：投注比例数据来源于竞彩官方</p>
            </div>

            <div v-for="market in bookmakerMarkets(item)" :key="`${item.matchId}-${market.key}-market`" class="mt-3 overflow-hidden rounded-md border border-slate-200 bg-white text-xs">
              <div class="flex items-center justify-between gap-2 border-l-2 bg-slate-50 px-3 py-2" :class="market.simulated ? 'border-amber-500' : 'border-red-500'">
                <p class="font-black text-slate-900">
                  {{ market.name }}交易盈亏
                  <span v-if="market.simulated" class="ml-1 rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-black text-amber-700">模拟</span>
                </p>
                <p class="text-slate-500">总投注额 {{ moneyCompactText(item.roiSimulation?.totalStake || 0) }}</p>
              </div>
              <div class="overflow-x-auto">
                <table class="min-w-[900px] w-full text-center">
                  <thead class="bg-slate-50 text-slate-500">
                    <tr>
                      <th class="px-2 py-2 font-bold">赛果</th>
                      <th class="px-2 py-2 font-bold">指数</th>
                      <th class="px-2 py-2 font-bold">彩民支持率</th>
                      <th class="px-2 py-2 font-bold">交易额</th>
                      <th class="px-2 py-2 font-bold">庄家赔付</th>
                      <th class="px-2 py-2 font-bold">本地庄家盈亏</th>
                      <th class="px-2 py-2 font-bold">官方庄家盈亏率</th>
                      <th class="px-2 py-2 font-bold">冷热指数</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-slate-100">
                    <tr v-for="row in market.bookmakerByOutcome" :key="`${item.matchId}-${market.key}-${row.outcome}-profit`">
                      <td class="px-2 py-2 font-black" :class="outcomeClass(row)">{{ outcomeName(row) }}</td>
                      <td class="px-2 py-2">{{ oddsText(row) }}</td>
                      <td class="px-2 py-2">{{ formatShare(row.retailShare) }}</td>
                      <td class="px-2 py-2">{{ moneyCompactText(row.betStake) }}</td>
                      <td class="px-2 py-2">{{ row.available ? moneyCompactText(row.payout) : '赔率不足' }}</td>
                      <td class="px-2 py-2 font-black" :class="bookmakerClass(row)">{{ signedMoneyText(row.bookmakerProfit, row.available) }}</td>
                      <td class="px-2 py-2 font-black" :class="profitRateClass(row)">{{ profitRateText(row) }}</td>
                      <td class="px-2 py-2" :class="hotColdClass(row.hotColdIndex)">{{ hotColdText(row.hotColdIndex) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <p v-if="market.simulationNote" class="border-t border-amber-100 bg-amber-50 px-3 py-2 text-[11px] font-bold text-amber-700">
                注：{{ market.simulationNote }}
              </p>
              <p class="border-t px-3 py-2 text-[11px] font-black" :class="marketProfitAlertClass(market)">
                {{ marketProfitAlertText(market) }}
              </p>
            </div>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'full'" title="赔率与凯利" icon="query_stats">
            <div class="grid grid-cols-2 gap-2 text-sm">
              <Metric label="平均欧赔" :value="joinText(item.detail.test8)" />
              <Metric label="散户心理" :value="sanhuCompareText(item)" />
              <Metric label="凯利预测" :value="joinText(item.kailiresult)" />
              <Metric label="体彩预测" :value="joinText(item.ticairesult)" />
            </div>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'full'" title="历史与近况" icon="history">
            <p class="text-sm font-bold text-red-600">{{ item.sanhuxinli?.[4] || '样本不足' }}</p>
            <p class="text-xs text-slate-600 mt-1">{{ historyLine(item.liangduibisai) }}</p>
            <div class="grid grid-cols-2 gap-2 mt-3 mb-3 text-sm">
              <Metric label="主队近期" :value="`${matchMini(item.homezuijinbisai)} ${scoreMini(item.homezuijinbisai)}`" />
              <Metric label="客队近期" :value="`${matchMini(item.guestzuijinbisai)} ${scoreMini(item.guestzuijinbisai)}`" />
            </div>
            <DataList title="两队历史" :rows="historyStatRows(item)" />
            <GoalStatTable title="球数统计" :rows="goalStatRows(item)" />
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'full'" title="让球压力" icon="balance">
            <DataList title="期望让球" :rows="expectedHandicapRows(item)" />
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'full'" title="大小球" icon="sports_soccer">
            <DataList title="期望球数" :rows="expectedGoalRows(item)" />
          </AnalysisSection>
        </section>

        <div v-if="viewMode !== 'minimal'" class="flex flex-wrap gap-2 px-3 py-3 bg-slate-50 border-t border-slate-200">
          <button class="px-4 py-2 rounded-md bg-slate-900 text-white text-sm font-bold" @click.stop="openDialog(item, 'plan')">方案</button>
          <button class="px-4 py-2 rounded-md bg-slate-900 text-white text-sm font-bold" @click.stop="openDialog(item, 'south')">南派</button>
          <button class="ml-auto px-4 py-2 rounded-md border border-slate-300 text-sm font-bold" @click.stop="goToMatch(item.matchId)">单场页</button>
        </div>
      </article>

      <div v-if="!loading && list.length === 0" class="text-center py-16 text-slate-500">
        <span class="material-symbols-outlined text-4xl block mb-2">sports_soccer</span>
        <p>暂无符合条件的比赛</p>
      </div>
      </template>
    </main>

    <div v-if="selectedItem" class="fixed inset-0 z-[80] bg-slate-950/90 overflow-y-auto" role="dialog" aria-modal="true" @click.stop>
      <div class="sticky top-0 bg-slate-950 text-white flex items-center gap-3 p-4 border-b border-slate-800">
        <button class="size-9 rounded-md bg-white/10 flex items-center justify-center" title="关闭" @click="closeDialog">
          <span class="material-symbols-outlined">close</span>
        </button>
        <h2 class="font-bold">{{ dialogTitle }}</h2>
      </div>

      <div class="p-3 max-w-2xl mx-auto">
        <div class="bg-white text-slate-950 rounded-lg p-4 text-sm space-y-3">
          <div class="rounded-md border border-slate-200 bg-slate-50 p-3">
            <div class="flex items-center justify-between gap-2">
              <p class="text-[11px] font-black text-slate-500">发布标题</p>
              <button
                class="h-8 rounded-md border border-slate-300 bg-white px-2 text-[11px] font-black text-slate-700 flex items-center gap-1 disabled:opacity-70"
                :disabled="copying"
                @click="copyDialogSection('title')"
              >
                <span class="material-symbols-outlined text-sm">{{ copiedSection === 'title' ? 'done' : 'content_copy' }}</span>
                {{ copiedSection === 'title' ? '已复制' : '复制' }}
              </button>
            </div>
            <p class="mt-1 text-base font-black leading-snug">{{ dialogPublishTitle }}</p>
          </div>

          <div class="rounded-md border border-slate-200 bg-slate-50 p-3">
            <div class="flex items-center justify-between gap-2">
              <p class="text-[11px] font-black text-slate-500">公开内容</p>
              <button
                class="h-8 rounded-md border border-slate-300 bg-white px-2 text-[11px] font-black text-slate-700 flex items-center gap-1 disabled:opacity-70"
                :disabled="copying"
                @click="copyDialogSection('public')"
              >
                <span class="material-symbols-outlined text-sm">{{ copiedSection === 'public' ? 'done' : 'content_copy' }}</span>
                {{ copiedSection === 'public' ? '已复制' : '复制' }}
              </button>
            </div>
            <p class="mt-1 leading-relaxed text-slate-800">{{ dialogPublicContent }}</p>
          </div>

          <div class="rounded-md border border-slate-200 bg-white p-3">
            <div class="mb-3 flex items-center justify-between gap-2">
              <p class="text-[11px] font-black text-slate-500">具体详情</p>
              <button
                class="h-8 rounded-md border border-slate-300 bg-white px-2 text-[11px] font-black text-slate-700 flex items-center gap-1 disabled:opacity-70"
                :disabled="copying"
                @click="copyDialogSection('detail')"
              >
                <span class="material-symbols-outlined text-sm">{{ copiedSection === 'detail' ? 'done' : 'content_copy' }}</span>
                {{ copiedSection === 'detail' ? '已复制' : '复制' }}
              </button>
            </div>

            <div class="space-y-3 leading-relaxed text-slate-800">
              <p
                v-for="(line, index) in dialogDetailContent"
                :key="`${selectedItem.matchId}-${dialogMode}-${index}`"
                :class="{ 'font-bold text-slate-950': index === dialogDetailContent.length - 1 }"
              >
                {{ line }}
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="evilCultAuditItem && evilCultAudit" class="fixed inset-0 z-[90] overflow-y-auto bg-slate-950/90" role="dialog" aria-modal="true" @click.stop>
      <div class="sticky top-0 z-10 flex items-center gap-3 border-b border-slate-800 bg-slate-950 p-4 text-white">
        <button class="flex size-9 items-center justify-center rounded-md bg-white/10" title="关闭" @click="closeEvilCultAudit">
          <span class="material-symbols-outlined">close</span>
        </button>
        <div class="min-w-0">
          <h2 class="truncate font-black">邪修主推逻辑</h2>
          <p class="truncate text-xs font-bold text-slate-400">{{ evilCultAuditItem.home }} vs {{ evilCultAuditItem.guest }}</p>
        </div>
      </div>

      <div class="mx-auto max-w-3xl p-3">
        <div class="overflow-hidden rounded-lg bg-white text-slate-950">
          <section class="border-b border-slate-200 p-3">
            <div class="grid grid-cols-3 gap-2 text-center">
              <div class="rounded-md bg-slate-950 px-2 py-3 text-white">
                <p class="text-[10px] font-black text-slate-400">最终主推</p>
                <p class="mt-1 text-sm font-black">{{ evilCultAudit.prediction.mainPick }}</p>
              </div>
              <div class="rounded-md bg-emerald-50 px-2 py-3 text-emerald-700">
                <p class="text-[10px] font-black">一推追大得分</p>
                <p class="mt-1 text-xl font-black">{{ evilCultScoreText(evilCultAudit.scores.over) }}</p>
              </div>
              <div class="rounded-md bg-red-50 px-2 py-3 text-red-600">
                <p class="text-[10px] font-black">一推先小得分</p>
                <p class="mt-1 text-xl font-black">{{ evilCultScoreText(evilCultAudit.scores.under) }}</p>
              </div>
            </div>

            <div class="mt-3 space-y-2">
              <p class="text-[10px] font-black text-slate-400">评分占比（用于比较方向，不代表真实概率）</p>
              <div>
                <div class="mb-1 flex justify-between text-xs font-black text-emerald-700">
                  <span>追大</span>
                  <span>{{ evilCultScorePercent(evilCultAudit.scores, 'over') }}%</span>
                </div>
                <div class="h-2 overflow-hidden rounded bg-slate-100">
                  <div class="h-full bg-emerald-500" :style="{ width: `${evilCultScorePercent(evilCultAudit.scores, 'over')}%` }"></div>
                </div>
              </div>
              <div>
                <div class="mb-1 flex justify-between text-xs font-black text-red-600">
                  <span>先小</span>
                  <span>{{ evilCultScorePercent(evilCultAudit.scores, 'under') }}%</span>
                </div>
                <div class="h-2 overflow-hidden rounded bg-slate-100">
                  <div class="h-full bg-red-500" :style="{ width: `${evilCultScorePercent(evilCultAudit.scores, 'under')}%` }"></div>
                </div>
              </div>
            </div>

            <div class="mt-3 rounded-md border px-3 py-3" :class="evilCultAudit.prediction.secondPassReversed ? 'border-blue-200 bg-blue-50' : 'border-slate-200 bg-slate-50'">
              <div class="grid grid-cols-2 gap-2 text-xs">
                <div>
                  <p class="font-black text-slate-500">一推结论</p>
                  <p class="mt-1 font-black text-slate-900">{{ evilCultAudit.prediction.firstPick }}</p>
                </div>
                <div>
                  <p class="font-black text-slate-500">二推评分（大 : 小）</p>
                  <p class="mt-1 font-black text-slate-900">{{ evilCultScoreText(evilCultAudit.prediction.secondOverScore) }} : {{ evilCultScoreText(evilCultAudit.prediction.secondUnderScore) }}</p>
                </div>
              </div>
              <p class="mt-2 text-xs font-black leading-relaxed" :class="evilCultAudit.prediction.secondPassReversed ? 'text-blue-700' : 'text-slate-600'">{{ evilCultAudit.prediction.secondPassReason }}</p>
            </div>
          </section>

          <section class="border-b border-slate-200 p-3">
            <h3 class="mb-2 text-sm font-black">参与计算的数据</h3>
            <div class="overflow-hidden rounded-md border border-slate-200">
              <table class="w-full table-fixed text-xs">
                <thead class="bg-slate-50 text-slate-500">
                  <tr>
                    <th class="w-[30%] px-2 py-2 text-left font-black">数据项</th>
                    <th class="w-[25%] px-2 py-2 text-left font-black">当前值</th>
                    <th class="px-2 py-2 text-left font-black">作用</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100">
                  <tr v-for="row in evilCultAudit.inputs" :key="row.label">
                    <td class="px-2 py-2 font-black text-slate-500">{{ row.label }}</td>
                    <td class="px-2 py-2 font-black text-slate-900 break-words">{{ row.value }}</td>
                    <td class="px-2 py-2 font-bold text-slate-600 break-words">{{ row.detail }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section class="p-3">
            <h3 class="mb-2 text-sm font-black">逐步评分</h3>
            <div class="overflow-x-auto rounded-md border border-slate-200">
              <table class="min-w-[620px] w-full text-xs">
                <thead class="bg-slate-50 text-slate-500">
                  <tr>
                    <th class="px-2 py-2 text-left font-black">步骤</th>
                    <th class="px-2 py-2 text-left font-black">依据</th>
                    <th class="px-2 py-2 text-center font-black">追大变动</th>
                    <th class="px-2 py-2 text-center font-black">先小变动</th>
                    <th class="px-2 py-2 text-center font-black">累计比分</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100">
                  <tr v-for="step in evilCultAudit.scores.steps" :key="step.label">
                    <td class="px-2 py-2 font-black text-slate-700">{{ step.label }}</td>
                    <td class="px-2 py-2 font-bold text-slate-500">{{ step.detail }}</td>
                    <td class="px-2 py-2 text-center font-black text-emerald-700">{{ evilCultDeltaText(step.overDelta) }}</td>
                    <td class="px-2 py-2 text-center font-black text-red-600">{{ evilCultDeltaText(step.underDelta) }}</td>
                    <td class="px-2 py-2 text-center font-black text-slate-900">{{ evilCultScoreText(step.overScore) }} : {{ evilCultScoreText(step.underScore) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p class="mt-3 rounded-md bg-slate-100 px-3 py-2 text-xs font-black leading-relaxed text-slate-700">{{ evilCultAudit.prediction.mainReason }}</p>
          </section>
        </div>
      </div>
    </div>

  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, onMounted, type PropType, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import analysisApi from '@/api/analysis'
import { resolveAssetUrl } from '@/api/request'
import type {
  AccuracyMatchRow,
  AccuracyStatsSummary,
  AnalysisMatch,
  BookmakerMarket,
  BookmakerOutcome,
  DirectionValues,
  PlatformEvilCultPrediction,
  PlatformGoalPair,
} from '@/types/analysis'

interface StatRow {
  label: string
  value: string
  tone?: 'blue' | 'green' | 'red' | 'normal'
}

interface GoalStatTableRow {
  label: string
  homeValue: string
  guestValue: string
  homeTone?: 'red' | 'green' | 'normal'
  guestTone?: 'red' | 'green' | 'normal'
}

interface GuideCompareRow {
  label: string
  bookmakerValue: string
  platformValue: string
  bookmakerTone?: StatRow['tone']
  platformTone?: StatRow['tone']
  variant?: 'secondary'
}

interface GuideMetaRow {
  label: string
  leftValue: string
  rightValue: string
  leftTone?: StatRow['tone']
  rightTone?: StatRow['tone']
}

interface GuideWarningRow {
  value: string
  tone: StatRow['tone']
}

interface EvilCultRow {
  label: string
  primary: string
  secondary: string
  tone: StatRow['tone']
  primaryTone?: StatRow['tone']
  secondaryTone?: StatRow['tone']
  variant?: 'note'
}

interface GuidePrediction {
  outcome: DirectionOutcome
  goal: { label: string; total: number; tone: StatRow['tone'] }
  score: string
  secondaryScore: string
  warning?: string
  warningTone?: StatRow['tone']
}

type GoalScore = { home: number; guest: number }
type DirectionOutcome = 'home' | 'draw' | 'away'

interface PlanContext {
  item: AnalysisMatch
  matchTitle: string
  league: string
  matchTime: string
  direction: string
  resultLabel: string
  confidenceTone: string
  probabilityLine: string
  signalLine: string
  historyLine: string
  teamLine: string
  bookmakerLine: string
  bookmakerPressure: string
  handicapLine: string
  handicapAdvice: string
  goalLine: string
  goalAdvice: string
  riskLine: string
  conclusionLine: string
}

const Metric = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: String, default: '-' },
  },
  setup(props) {
    return () => h('div', { class: 'border-l-2 border-slate-200 pl-2 min-w-0' }, [
      h('p', { class: 'text-[11px] text-slate-500 font-bold' }, props.label),
      h('p', { class: 'font-bold break-words leading-snug whitespace-pre-line' }, props.value || '-'),
    ])
  },
})

const DataList = defineComponent({
  props: {
    title: { type: String, required: true },
    rows: { type: Array as PropType<StatRow[]>, default: () => [] },
  },
  setup(props) {
    return () => h('div', { class: 'mt-3 rounded-md border border-slate-200 overflow-hidden' }, [
      h('div', { class: 'px-3 py-2 bg-slate-50 text-xs font-black text-slate-500' }, props.title),
      h('div', { class: 'divide-y divide-slate-100' }, props.rows.map((row) => h('div', { class: ['grid grid-cols-12 gap-2 px-3 py-2 text-sm', statRowClass(row)] }, [
        h('span', { class: ['col-span-6 font-bold', statRowLabelClass(row)] }, row.label),
        h('span', { class: ['col-span-6 font-black break-words text-right', statRowValueClass(row)] }, row.value),
      ]))),
    ])
  },
})

const GoalStatTable = defineComponent({
  props: {
    title: { type: String, required: true },
    rows: { type: Array as PropType<GoalStatTableRow[]>, default: () => [] },
  },
  setup(props) {
    return () => h('div', { class: 'mt-3 rounded-md border border-slate-200 overflow-hidden' }, [
      h('div', { class: 'px-3 py-2 bg-slate-50 text-xs font-black text-slate-500' }, props.title),
      h('div', { class: 'overflow-x-auto' }, [
        h('table', { class: 'w-full text-sm' }, [
          h('thead', { class: 'bg-slate-50/70 text-slate-500 text-xs font-black' }, [
            h('tr', {}, [
              h('th', { class: 'px-3 py-2 text-left' }, '统计项'),
              h('th', { class: 'px-3 py-2 text-right' }, '主队'),
              h('th', { class: 'px-3 py-2 text-right' }, '客队'),
            ]),
          ]),
          h('tbody', { class: 'divide-y divide-slate-100' }, props.rows.map((row) => h('tr', {}, [
            h('td', { class: 'px-3 py-2 text-slate-500 font-bold' }, row.label),
            h('td', { class: ['px-3 py-2 text-right font-black', goalStatToneClass(row.homeTone)] }, row.homeValue),
            h('td', { class: ['px-3 py-2 text-right font-black', goalStatToneClass(row.guestTone)] }, row.guestValue),
          ]))),
        ]),
      ]),
    ])
  },
})

const GuideCompareTable = defineComponent({
  props: {
    rows: { type: Array as PropType<GuideCompareRow[]>, default: () => [] },
    minimal: { type: Boolean, default: false },
  },
  setup(props) {
    const visibleRows = () => props.minimal ? props.rows.filter((row) => row.variant !== 'secondary') : props.rows
    return () => h('div', { class: 'mt-3 rounded-md border border-slate-200 overflow-hidden' }, [
      h('table', { class: 'w-full text-sm table-fixed' }, [
        h('thead', { class: 'bg-slate-50 text-xs font-black text-slate-500' }, [
            h('tr', {}, [
              h('th', { class: 'w-[28%] px-3 py-2 text-left' }, '项目'),
            h('th', { class: 'px-3 py-2 text-center' }, props.minimal ? '黄老板' : '庄家'),
            h('th', { class: 'px-3 py-2 text-center' }, props.minimal ? '范总' : '平台'),
            ]),
        ]),
        h('tbody', { class: 'divide-y divide-slate-100' }, visibleRows().map((row) => h('tr', { class: guideRowClass(row) }, [
          h('td', { class: ['px-3 py-3 font-black', guideRowLabelClass(row)] }, row.label),
          h('td', { class: ['px-3 py-3 text-center font-black break-words', guideRowValueClass(row, row.bookmakerTone)] }, row.bookmakerValue),
          h('td', { class: ['px-3 py-3 text-center font-black break-words', guideRowValueClass(row, row.platformTone)] }, row.platformValue),
        ]))),
      ]),
    ])
  },
})

const GuideMetaTable = defineComponent({
  props: {
    rows: { type: Array as PropType<GuideMetaRow[]>, default: () => [] },
  },
  setup(props) {
    return () => h('div', { class: 'mt-2 rounded-md border border-slate-200 overflow-hidden' }, [
      h('table', { class: 'w-full text-xs table-fixed' }, [
        h('tbody', { class: 'divide-y divide-slate-100' }, props.rows.map((row) => h('tr', {}, [
          h('td', { class: 'w-[24%] px-3 py-2 font-black text-slate-500 bg-slate-50/70' }, row.label),
          h('td', { class: ['px-3 py-2 font-bold break-words', guideCellClass(row.leftTone)] }, row.leftValue),
          h('td', { class: ['px-3 py-2 font-bold break-words', guideCellClass(row.rightTone)] }, row.rightValue),
        ]))),
      ]),
    ])
  },
})

const ProbabilityBar = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: Number, required: true },
    color: { type: String, required: true },
  },
  setup(props) {
    return () => h('div', { class: 'min-w-0' }, [
      h('div', { class: 'flex justify-between text-xs font-bold mb-1' }, [
        h('span', {}, props.label),
        h('span', {}, `${Math.round(props.value)}%`),
      ]),
      h('div', { class: 'h-2 rounded bg-slate-200 overflow-hidden' }, [
        h('div', { class: `h-full ${props.color}`, style: { width: `${Math.max(0, Math.min(100, props.value))}%` } }),
      ]),
    ])
  },
})

const AnalysisSection = defineComponent({
  props: {
    title: { type: String, required: true },
    icon: { type: String, required: true },
  },
  setup(props, { slots }) {
    return () => h('section', { class: 'px-3 py-3' }, [
      h('div', { class: 'flex items-center gap-2 mb-2 min-w-0' }, [
        h('span', { class: 'material-symbols-outlined text-primary text-xl shrink-0 leading-none' }, safeSectionIcon(props.icon)),
        h('h3', { class: 'font-black text-sm leading-none truncate' }, props.title),
      ]),
      slots.default?.(),
    ])
  },
})

function safeSectionIcon(icon: string): string {
  return icon || 'analytics'
}

const router = useRouter()
const route = useRoute()
const analysisPageStateKey = 'peak-ball:analysis-page-state'
const todayString = localDateString(new Date())
const restoredState = readAnalysisPageState()
const loading = ref(false)
const list = ref<AnalysisMatch[]>([])
const allMatches = ref<AnalysisMatch[]>([])
const showScore = ref(restoredState?.showScore ?? true)
const viewMode = ref<AnalysisViewMode>(restoredState?.viewMode || 'simple')
const selectedItem = ref<AnalysisMatch | null>(null)
const evilCultAuditItem = ref<AnalysisMatch | null>(null)
const dialogMode = ref<'plan' | 'south'>('plan')
const dialogPublishTitle = ref('')
const dialogPublicContent = ref('')
const dialogDetailContent = ref<string[]>([])
const copying = ref(false)
const copiedSection = ref<DialogCopySection | null>(null)
const selectedDate = ref(validDateString(String(route.query.date || '')) || restoredState?.selectedDate || todayString)
const matchScope = ref<MatchScope>(normalizeMatchScope(route.query.scope || restoredState?.matchScope))
const selectedLeague = ref(normalizeLeague(route.query.league || restoredState?.selectedLeague))
const localBookmakerTotalStake = 50000000
const accuracyHistoryStartDate = '2026-05-28'
const guideSectionTitle = computed(() => '庄家 / 平台预测')
const accuracyStatsLoading = ref(false)
const accuracyStatsError = ref('')
const accuracyStats = ref<AccuracyStatsSummary>(emptyAccuracyStats(todayString))
const analysisViewModes: Array<{ value: AnalysisViewMode; label: string }> = [
  { value: 'simple', label: '简化版' },
  { value: 'minimal', label: '邪修版' },
  { value: 'full', label: '复杂版' },
  { value: 'stats', label: '统计页' },
]
const matchScopes: Array<{ value: MatchScope; label: string }> = [
  { value: 'sporttery', label: '竞彩' },
  { value: 'all', label: '全部' },
]

const publicContentPhrases = [
  '我的习惯是先看状态和位置，再结合指数变化判断热度，重点观察两队节奏差和临场资金倾向。',
  '这场会从主客队攻防状态、历史交锋和盘口压力入手，思路以稳为主，避免只看单一赔率。',
  '个人分析偏重数据结构和冷热分布，会把基本面、凯利变化、散户心理放在一起交叉验证。',
  '本场重点看节奏控制、让球压力和大小球预期，先判断风险区间，再给出更适合跟进的方向。',
  '我会先拆两队近期表现，再看指数与资金是否一致，若出现冷热背离，会优先控制选择范围。',
  '这场不单看排名和名气，主要结合进球预期、历史样本和盘口变化，判断哪一侧更值得关注。',
  '分析会围绕基本面、胜平负分布和庄家赔付压力展开，重点看结论是否能被多组数据同时支撑。',
  '这场先做信息面和数据面交叉，再看临场热度有没有偏离合理区间，方向上会更重视风险控制。',
]

type DialogCopySection = 'title' | 'public' | 'detail'
type AnalysisViewMode = 'simple' | 'minimal' | 'full' | 'stats'
type MatchScope = 'sporttery' | 'all'

interface AnalysisPageState {
  selectedDate: string
  showScore: boolean
  viewMode: AnalysisViewMode
  matchScope?: MatchScope
  selectedLeague?: string
  coreOnlyMode?: boolean
  scrollY: number
  dialog?: {
    matchId: string
    mode: 'plan' | 'south'
    title: string
    publicContent: string
    detailContent: string[]
  }
}

const selectedDateLabel = computed(() => {
  if (selectedDate.value === todayString) return '今天'
  return selectedDate.value
})
const matchScopeLabel = computed(() => matchScope.value === 'all' ? '全部比赛' : '竞彩足球')
const availableLeagues = computed(() => Array.from(new Set(allMatches.value.map((item) => item.league).filter(Boolean))).sort((left, right) => left.localeCompare(right, 'zh-CN')))

const accuracyStatsRangeText = computed(() => `${accuracyStats.value.startDate} 至 ${accuracyStats.value.endDate}`)
const evilCultAudit = computed(() => {
  const item = evilCultAuditItem.value
  if (!item?.platform) return null
  const block = item.platform.evilCult
  return {
    line: block.line,
    scores: block.scores,
    prediction: block.prediction,
    inputs: block.inputs,
  }
})

const highConfidenceCount = computed(() => list.value.filter((item) => item.confidence === '高信心').length)

// 命中与否由后端 predictionHit 标记，前端只做展示统计。
const accuracyLabel = computed(() => {
  const settled = list.value.filter((item) => typeof item.predictionHit === 'boolean' && item.displayState === '完场')
  if (!settled.length) return 0
  const correct = settled.filter((item) => item.predictionHit).length
  return Math.round((correct / settled.length) * 100)
})

const dialogTitle = computed(() => {
  if (dialogMode.value === 'plan') return '方案详情'
  return '南派方案'
})

async function loadData() {
  loading.value = true
  try {
    syncDateQuery()
    const matchesResponse = await analysisApi.getAnalysisMatches(analysisQueryParams())
    setCurrentMatches(matchesResponse.data ?? [])
    restoreDialogFromState()
    restoreScrollFromState()
    persistAnalysisPageState()
  } finally {
    loading.value = false
  }
}

// 命中率统计全部由后端 /analysis/accuracy-stats 计算，前端只负责请求与渲染。
async function loadAccuracyStats() {
  accuracyStatsLoading.value = true
  accuracyStatsError.value = ''
  accuracyStats.value = emptyAccuracyStats(todayString)
  try {
    const league = selectedLeague.value === 'all' ? undefined : selectedLeague.value
    const [statsResponse, currentResponse] = await Promise.all([
      analysisApi.getAccuracyStats({ ...analysisQueryParams(), league }),
      analysisApi.getAnalysisMatches(analysisQueryParams()),
    ])
    setCurrentMatches(currentResponse.data ?? [])
    accuracyStats.value = statsResponse.data
  } catch {
    accuracyStatsError.value = '历史统计加载失败，请稍后重试。'
  } finally {
    accuracyStatsLoading.value = false
  }
}

function emptyAccuracyStats(endDate: string): AccuracyStatsSummary {
  const emptyRow = (label: string) => ({ label, sample: 0, bookmakerCorrect: 0, platformCorrect: 0, bothCorrect: 0 })
  return {
    startDate: accuracyHistoryStartDate,
    endDate,
    total: 0,
    overall: { sample: 0, bookmakerCorrect: 0, platformCorrect: 0 },
    rows: [emptyRow('胜平负'), emptyRow('大小球'), emptyRow('比分')],
    evilCultRows: [],
    commonRows: [],
    generatedCommonRows: [],
    matchRows: [],
    settledFitRows: [],
  }
}

// "匹配历史规律"由后端随列表返回（item.accuracyFit），前端只读取。
function accuracyMatchRow(item: AnalysisMatch): AccuracyMatchRow | null {
  return item.accuracyFit ?? null
}

function accuracyFitClass(tone: StatRow['tone']): string {
  if (tone === 'green') return 'bg-emerald-50 text-emerald-700'
  if (tone === 'blue') return 'bg-sky-50 text-sky-700'
  if (tone === 'red') return 'bg-red-50 text-red-700'
  return 'bg-slate-100 text-slate-500'
}

function accuracyFitTextClass(tone: StatRow['tone']): string {
  if (tone === 'green') return 'text-emerald-700'
  if (tone === 'blue') return 'text-sky-700'
  if (tone === 'red') return 'text-red-700'
  return 'text-slate-500'
}

interface MyAngleRow {
  label: string
  stat: string
  verdictText: string
  verdictClass: string
}

// 我的镜像：后端按我的历史选择算出的同盘型表现，前端只渲染。
function myAngleRows(item: AnalysisMatch): MyAngleRow[] {
  const angle = item.myAngle
  if (!angle) return []
  const build = (label: string, market: NonNullable<AnalysisMatch['myAngle']>['spf']): MyAngleRow => {
    const verdictText = market.verdict === 'red' ? '红区·可信' : market.verdict === 'black' ? '黑区·防反' : market.sample ? '中性' : '无样本'
    const verdictClass = market.verdict === 'red'
      ? 'bg-red-50 text-red-600'
      : market.verdict === 'black'
        ? 'bg-slate-900 text-white'
        : 'bg-slate-100 text-slate-500'
    return {
      label: `${label} @ ${market.bucket}`,
      stat: market.sample ? `${market.hit}/${market.sample} · ${market.accuracy.toFixed(0)}%` : '-',
      verdictText,
      verdictClass,
    }
  }
  return [
    build('胜平负', angle.spf),
    build('让球', angle.rqspf),
    build('大小球', angle.dxq),
  ]
}

function setDate(value: string) {
  selectedDate.value = validDateString(value) || todayString
  loadData()
  if (viewMode.value === 'stats') void loadAccuracyStats()
}

function setMatchScope(scope: MatchScope) {
  if (matchScope.value === scope) return
  matchScope.value = scope
  loadData()
  if (viewMode.value === 'stats') void loadAccuracyStats()
}

function applyLeagueSelection() {
  list.value = filterMatchesByLeague(allMatches.value)
  syncDateQuery()
  persistAnalysisPageState()
  if (viewMode.value === 'stats') void loadAccuracyStats()
}

function setCurrentMatches(matches: AnalysisMatch[]) {
  allMatches.value = matches
  if (selectedLeague.value !== 'all' && !matches.some((item) => item.league === selectedLeague.value)) {
    selectedLeague.value = 'all'
    syncDateQuery()
  }
  list.value = filterMatchesByLeague(matches)
}

function filterMatchesByLeague(matches: AnalysisMatch[]) {
  if (selectedLeague.value === 'all') return matches
  return matches.filter((item) => item.league === selectedLeague.value)
}

function analysisQueryParams(date = selectedDate.value) {
  return {
    date,
    scope: matchScope.value,
  }
}

function shiftDate(days: number) {
  const date = parseLocalDate(selectedDate.value) || new Date()
  date.setDate(date.getDate() + days)
  setDate(localDateString(date))
}

function accuracyRateText(correct: number, sample: number): string {
  if (!sample) return '-'
  return `${correct}/${sample} ${Math.round((correct / sample) * 100)}%`
}

function syncDateQuery() {
  const current = String(route.query.date || '')
  const currentScope = String(route.query.scope || '')
  const currentLeague = String(route.query.league || '')
  const nextScope = matchScope.value === 'all' ? 'all' : undefined
  const nextLeague = selectedLeague.value === 'all' ? undefined : selectedLeague.value
  if (current === selectedDate.value && currentScope === (nextScope || '') && currentLeague === (nextLeague || '')) return
  router.replace({ query: { ...route.query, date: selectedDate.value, scope: nextScope, league: nextLeague } })
}

function openEvilCultAudit(item: AnalysisMatch) {
  evilCultAuditItem.value = item
  void hydrateEvilCultAuditDetail(item.matchId)
}

async function hydrateEvilCultAuditDetail(matchId: string) {
  try {
    const { data } = await analysisApi.getAnalysisDetail(matchId)
    if (!evilCultAuditItem.value || evilCultAuditItem.value.matchId !== matchId) return
    evilCultAuditItem.value = data
    const index = list.value.findIndex((item) => item.matchId === matchId)
    if (index >= 0) list.value[index] = { ...list.value[index], ...data }
  } catch {
    // The list payload already contains the inputs required by the audit dialog.
  }
}

function closeEvilCultAudit() {
  evilCultAuditItem.value = null
}

function openDialog(item: AnalysisMatch, mode: 'plan' | 'south') {
  selectedItem.value = item
  dialogMode.value = mode
  dialogPublishTitle.value = buildPublishTitle(item)
  dialogPublicContent.value = buildPublicContent(item)
  dialogDetailContent.value = buildDialogDetailLines(item, mode)
  copiedSection.value = null
  persistAnalysisPageState()
  void hydrateDialogAnalysisDetail(item.matchId, mode)
}

async function hydrateDialogAnalysisDetail(matchId: string, mode: 'plan' | 'south') {
  try {
    const { data } = await analysisApi.getAnalysisDetail(matchId)
    if (!selectedItem.value || selectedItem.value.matchId !== matchId || dialogMode.value !== mode) return
    selectedItem.value = data
    const index = list.value.findIndex((item) => item.matchId === matchId)
    if (index >= 0) {
      list.value[index] = { ...list.value[index], ...data }
    }
    dialogDetailContent.value = buildDialogDetailLines(data, mode)
    persistAnalysisPageState()
  } catch {
    // Keep the already generated local plan when the detail refresh or network fallback fails.
  }
}

function closeDialog() {
  selectedItem.value = null
  dialogPublishTitle.value = ''
  dialogPublicContent.value = ''
  dialogDetailContent.value = []
  copiedSection.value = null
  persistAnalysisPageState()
}

function restoreDialogFromState() {
  if (!restoredState?.dialog || selectedItem.value) return
  const item = list.value.find((match) => match.matchId === restoredState.dialog?.matchId)
  if (!item) return

  selectedItem.value = item
  dialogMode.value = restoredState.dialog.mode
  dialogPublishTitle.value = restoredState.dialog.title || buildPublishTitle(item)
  dialogPublicContent.value = restoredState.dialog.publicContent || buildPublicContent(item)
  dialogDetailContent.value = restoredState.dialog.detailContent.length ? restoredState.dialog.detailContent : buildDialogDetailLines(item, restoredState.dialog.mode)
}

function restoreScrollFromState() {
  const scrollY = restoredState?.scrollY || 0
  if (scrollY <= 0) return
  window.requestAnimationFrame(() => window.scrollTo({ top: scrollY }))
}

function persistAnalysisPageState() {
  const state: AnalysisPageState = {
    selectedDate: selectedDate.value,
    showScore: showScore.value,
    viewMode: viewMode.value,
    matchScope: matchScope.value,
    selectedLeague: selectedLeague.value,
    scrollY: window.scrollY || 0,
  }

  if (selectedItem.value) {
    state.dialog = {
      matchId: selectedItem.value.matchId,
      mode: dialogMode.value,
      title: dialogPublishTitle.value,
      publicContent: dialogPublicContent.value,
      detailContent: dialogDetailContent.value,
    }
  }

  try {
    window.sessionStorage.setItem(analysisPageStateKey, JSON.stringify(state))
  } catch {
    // Ignore storage failures in private browsing or restricted webviews.
  }
}

function readAnalysisPageState(): AnalysisPageState | null {
  try {
    const raw = window.sessionStorage.getItem(analysisPageStateKey)
    if (!raw) return null
    const state = JSON.parse(raw) as Partial<AnalysisPageState>
    const selectedDate = validDateString(String(state.selectedDate || ''))
    if (!selectedDate) return null

    return {
      selectedDate,
      showScore: state.showScore !== false,
      viewMode: normalizeViewMode(state.viewMode, state.coreOnlyMode),
      matchScope: normalizeMatchScope(state.matchScope),
      selectedLeague: normalizeLeague(state.selectedLeague),
      scrollY: typeof state.scrollY === 'number' && Number.isFinite(state.scrollY) ? state.scrollY : 0,
      dialog: normalizeDialogState(state.dialog),
    }
  } catch {
    return null
  }
}

function normalizeDialogState(value: unknown): AnalysisPageState['dialog'] {
  if (!value || typeof value !== 'object') return undefined
  const dialog = value as Partial<NonNullable<AnalysisPageState['dialog']>>
  const matchId = String(dialog.matchId || '').trim()
  if (!matchId) return undefined
  return {
    matchId,
    mode: dialog.mode === 'south' ? 'south' : 'plan',
    title: String(dialog.title || ''),
    publicContent: String(dialog.publicContent || ''),
    detailContent: Array.isArray(dialog.detailContent) ? dialog.detailContent.map((item) => String(item)) : [],
  }
}

function normalizeViewMode(value: unknown, legacyCoreOnlyMode?: unknown): AnalysisViewMode {
  if (value === 'full' || value === 'minimal' || value === 'simple' || value === 'stats') return value
  if (legacyCoreOnlyMode === true) return 'simple'
  return 'simple'
}

function normalizeMatchScope(value: unknown): MatchScope {
  return value === 'all' ? 'all' : 'sporttery'
}

function normalizeLeague(value: unknown): string {
  const league = Array.isArray(value) ? value[0] : value
  return String(league || '').trim() || 'all'
}

function setViewMode(mode: AnalysisViewMode) {
  viewMode.value = mode
  if (mode === 'stats') void loadAccuracyStats()
  persistAnalysisPageState()
}

async function copyDialogSection(section: DialogCopySection) {
  if (!selectedItem.value || copying.value) return
  copying.value = true
  try {
    await copyText(dialogSectionText(selectedItem.value, section))
    copiedSection.value = section
    window.setTimeout(() => {
      if (copiedSection.value === section) {
        copiedSection.value = null
      }
    }, 1600)
  } finally {
    copying.value = false
  }
}

async function copyText(text: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text)
    return
  }

  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  textarea.style.top = '0'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  document.execCommand('copy')
  document.body.removeChild(textarea)
}

function buildPublishTitle(item: AnalysisMatch): string {
  const event = titleEventName(item)
  const home = shortTeamName(item.home)
  const guest = shortTeamName(item.guest)
  const titles = [
    `${event} ${home}vs${guest} 三项推荐`,
    `${event}${home}战${guest} 内含三推`,
    `${event} ${home}${guest} 胜平负比分球数`,
    `${event}${home}vs${guest} 三路答案`,
    `${event} ${home}对${guest} 推荐合集`,
    `${event}${home}${guest} 胜负比分球数`,
    `${event} ${home}战${guest} 三种思路`,
    `${event}${home}vs${guest} 内含三种推荐`,
    `${event} ${home}${guest} 赛果比分球数`,
  ]
  return titles[variantIndex(item, 'publish-title', titles.length)]
}

function titleEventName(item: AnalysisMatch): string {
  const league = String(item.league || '').trim()
  if (league.includes('世界杯')) return '世界杯'
  return league || '竞彩足球'
}

function shortTeamName(name: string): string {
  const cleaned = String(name || '').replace(/\s+/g, '')
  if (!cleaned) return '主队'
  return Array.from(cleaned).slice(0, 4).join('')
}

function buildPublicContent(item: AnalysisMatch): string {
  const phrase = publicContentPhrases[Math.floor(Math.random() * publicContentPhrases.length)]
  return `${item.home}vs${item.guest}，${phrase}`
}

function dialogSectionText(item: AnalysisMatch, section: DialogCopySection): string {
  if (section === 'title') return dialogPublishTitle.value
  if (section === 'public') return dialogPublicContent.value
  return dialogDetailContent.value.length ? dialogDetailContent.value.join('\n') : buildDialogDetailLines(item, dialogMode.value).join('\n')
}

function buildDialogDetailLines(item: AnalysisMatch, mode: 'plan' | 'south'): string[] {
  const lines = mode === 'plan' ? planDetailLines(item) : southDetailLines(item)
  return [...lines, ...recommendationPlaceholderLines()]
}

function recommendationPlaceholderLines(): string[] {
  return [
    '【推荐】',
    '大小球：',
    '比分：',
    '让球：',
    '某彩：',
    '胜平负：',
  ]
}

function planDetailLines(item: AnalysisMatch): string[] {
  const context = buildPlanContext(item)
  const frameworks: Array<(context: PlanContext) => string[]> = [
    planByInformationEdge,
    planByMarketPressure,
    planByConflictCheck,
    planByGameScript,
    planByRiskControl,
    planByDataLedger,
    planByLateChecklist,
    planByCompactPreview,
  ]
  return frameworks[variantIndex(item, 'plan-framework', frameworks.length)](context)
}

function southDetailLines(item: AnalysisMatch): string[] {
  const context = buildPlanContext(item)
  const frameworks: Array<(context: PlanContext) => string[]> = [
    southByHeatMap,
    southByPayoutTrap,
    southByKellyFilter,
    southByHandicapGuard,
    southByCrowdSplit,
    southByScenario,
    southByShortSlip,
  ]
  return frameworks[variantIndex(item, 'south-framework', frameworks.length)](context)
}

function buildPlanContext(item: AnalysisMatch): PlanContext {
  const probabilityRank = resultProbabilityRank(item)
  const top = probabilityRank[0]
  const second = probabilityRank[1]
  const gap = top.value - second.value
  const confidenceTone = gap >= 14 ? '主线相对清楚' : gap >= 7 ? '有方向但不能放太满' : '三项差距不大，容错要靠分散'
  const [historyHandicap, recentHandicap] = splitPair(item.changguiyapan)
  const historyGoalValue = item.historyGoalAverage
  const recentGoalValue = item.recentGoalAverage
  const combinedGoalValue = item.combinedGoalAverage
  const goalAdviceLine = buildGoalAdviceLine(item)
  const rows = localProfitRows(item)
  const pressureRow = rows.slice().sort((a, b) => a.bookmakerProfit - b.bookmakerProfit)[0]
  const bestRow = rows.slice().sort((a, b) => b.bookmakerProfit - a.bookmakerProfit)[0]
  const source = item.teamProfiles?.home?.sourceTitle || item.teamProfiles?.guest?.sourceTitle

  return {
    item,
    matchTitle: `${item.home} vs ${item.guest}`,
    league: item.league || '竞彩足球',
    matchTime: formatTime(item.matchTime),
    direction: resultDirectionText(item),
    resultLabel: item.prediction || top.label,
    confidenceTone,
    probabilityLine: `${item.home} ${formatShare(item.winProbability)}，平局 ${formatShare(item.drawProbability)}，${item.guest} ${formatShare(item.loseProbability)}；最高项是${top.side}${formatShare(top.value)}，与第二项差${trimFixed(gap, 2)}个百分点。`,
    signalLine: `凯利筛选${joinText(item.kailiresult)}，体彩筛选${joinText(item.ticairesult)}，平均欧赔${joinText(item.detail.test8)}，庄控分布${joinText(item.detail.test1)}。`,
    historyLine: `历史样本${joinText(item.liangduilishi)}；散户心理${joinText(item.sanhuxinli?.slice(0, 3))}，当前人气备注为${valueText(item.sanhuxinli?.[4])}。`,
    teamLine: `${source ? `资料参考${source}，` : ''}${item.home}：${profileSummary(item.teamProfiles?.home?.summary, item.home)}；${item.guest}：${profileSummary(item.teamProfiles?.guest?.summary, item.guest)}。`,
    bookmakerLine: rows.length
      ? `本地按${moneyCompactText(localStakeBase(item))}资金池测算：${rows.map((row) => `${outcomeName(row)}=${signedMoneyText(row.bookmakerProfit, row.available)} / ${formatRoi(row.bookmakerRoi, row.available)}`).join('，')}。`
      : '本地庄家盈亏暂缺，先以赔率、盘口和散户心理做主判断。',
    bookmakerPressure: pressureRow
      ? `赔付压力最大的是${outcomeName(pressureRow)}打出，庄家本地净值${signedMoneyText(pressureRow.bookmakerProfit, pressureRow.available)}；相对舒服项是${outcomeName(bestRow)}打出，净值${signedMoneyText(bestRow.bookmakerProfit, bestRow.available)}。`
      : '缺少完整赔付测算时，不把盈亏项作为结论核心。',
    handicapLine: `让球从${valueText(item.yapanpankou1)}到${valueText(item.yapanpankou2)}；历史期望${valueText(historyHandicap)}，近期期望${valueText(recentHandicap)}，主客承压约${percentText(item.yapantouzhu?.[0])}/${percentText(item.yapantouzhu?.[1])}。`,
    handicapAdvice: `${valueText(item.yapantouzhu?.[12])}。若临场盘口继续靠近热门方向，信心可保留；若退盘但热度不降，需要把主推降级处理。`,
    goalLine: `球数均值：历史${goalMetricText(historyGoalValue)}，近期${goalMetricText(recentGoalValue)}，综合${goalMetricText(combinedGoalValue)}；盘口${valueText(item.qiushupankou1)}到${valueText(item.qiushupankou2)}，大/小资金${percentText(item.qiushutouzhu?.[0])}/${percentText(item.qiushutouzhu?.[1])}。`,
    goalAdvice: `${item.home}近5场进${valueText(item.qiushuAll?.[0])}失${valueText(item.qiushuAll?.[4])}，${item.guest}近5场进${valueText(item.qiushuAll?.[2])}失${valueText(item.qiushuAll?.[5])}。${goalAdviceLine}`,
    riskLine: buildRiskLine(item, gap, pressureRow),
    conclusionLine: `最终倾向：胜平负看${resultDirectionText(item)}，球数看${item.qiuprediction}；临场重点盯盘口是否反向、热门项赔付是否继续扩大。`,
  }
}

function buildGoalAdviceLine(item: AnalysisMatch): string {
  const expectedGoals = expectedGoalPair(item)
  const alerts = item.platform?.goalBalanceAlertRows ?? []
  const alertText = alerts
    .filter((row) => row.label !== '2.5均衡值')
    .map((row) => `${row.label}：${row.value}`)
    .join('；')
  const balanceRow = alerts.find((row) => row.label === '2.5均衡值')
  const balanceText = balanceRow ? `${balanceRow.label}：${balanceRow.value}；` : ''
  const zeroRiskText = zeroGoalAdviceText(item)
  return `预测本场进球数：${item.home}${expectedGoalText(expectedGoals.home)}，${item.guest}${expectedGoalText(expectedGoals.guest)}。${zeroRiskText}${balanceText}${alertText || `大小球先看${item.qiuprediction}，临场继续按2.5上下平衡观察。`}`
}

function planByInformationEdge(context: PlanContext): string[] {
  return [
    `【信息面先行】${context.league} ${context.matchTitle}，${context.matchTime}开赛。这场先从两队资料和近期样本落笔，避免一上来被赔率牵着走。`,
    `【球队底色】${context.teamLine}`,
    `【概率校准】${context.probabilityLine}${context.confidenceTone}，所以主线暂定${context.direction}。`,
    `【历史与人气】${context.historyLine}`,
    `【指数验证】${context.signalLine}`,
    `【赔付压力】${context.bookmakerLine}${context.bookmakerPressure}`,
    `【盘口落点】${context.handicapLine}${context.handicapAdvice}`,
    `【球数脚本】${context.goalLine}${context.goalAdvice}`,
    `【结论】${context.conclusionLine} ${context.riskLine}`,
  ]
}

function planByMarketPressure(context: PlanContext): string[] {
  return [
    `【资金切入】这场先看庄家舒服不舒服。${context.bookmakerLine}`,
    `【赔付解释】${context.bookmakerPressure} 如果这个方向同时被散户追捧，临场反向动作就要特别敏感。`,
    `【人气分布】${context.historyLine}`,
    `【胜平负主线】${context.probabilityLine}目前更适合围绕${context.direction}做第一判断。`,
    `【凯利与体彩】${context.signalLine}`,
    `【基本面补充】${context.teamLine}`,
    `【让球过滤】${context.handicapLine}${context.handicapAdvice}`,
    `【大小球】${context.goalLine}${context.goalAdvice}`,
    `【执行口径】${context.conclusionLine} ${context.riskLine}`,
  ]
}

function planByConflictCheck(context: PlanContext): string[] {
  return [
    `【先找矛盾】${context.matchTitle}不适合只写一个结论，先看概率、人气、盘口有没有互相打架。`,
    `【概率项】${context.probabilityLine}`,
    `【人气项】${context.historyLine}`,
    `【赔率项】${context.signalLine}`,
    `【赔付项】${context.bookmakerPressure} ${context.bookmakerLine}`,
    `【矛盾处理】如果概率支持${context.direction}，但盘口退让或赔付压力集中到同一边，本场就不能追深；如果盘口同步保护，结论才更稳。`,
    `【让球修正】${context.handicapLine}${context.handicapAdvice}`,
    `【球数修正】${context.goalLine}${context.goalAdvice}`,
    `【落点】${context.conclusionLine} ${context.riskLine}`,
  ]
}

function planByGameScript(context: PlanContext): string[] {
  return [
    `【比赛剧本】${context.league} ${context.matchTitle}，先按节奏推演：如果${context.resultLabel}方向兑现，比赛大概率要符合盘口和球数两条线。`,
    `【第一层：控场】${context.teamLine}`,
    `【第二层：赛果】${context.probabilityLine}${context.signalLine}`,
    `【第三层：热度】${context.historyLine}`,
    `【第四层：庄家账本】${context.bookmakerLine}${context.bookmakerPressure}`,
    `【第五层：让球】${context.handicapLine}${context.handicapAdvice}`,
    `【第六层：进球数】${context.goalLine}${context.goalAdvice}`,
    `【剧本结论】优先剧本是${context.direction}配合${context.item.qiuprediction}，但若临场人气继续单边集中，需要降低重仓思路。`,
  ]
}

function planByRiskControl(context: PlanContext): string[] {
  return [
    `【风控口径】这场不是单纯找最强方向，而是要先确定哪些信号会让方案失效。`,
    `【失效条件一】${context.handicapLine}若临场退盘并且${context.direction}仍热，胜平负信心要下调。`,
    `【失效条件二】${context.bookmakerPressure}赔付压力若继续集中，防守项要提前准备。`,
    `【失效条件三】${context.goalLine}若球数盘口不跟随均值，${context.item.qiuprediction}只能作次级方向。`,
    `【可用支撑】${context.probabilityLine}${context.signalLine}`,
    `【基本面确认】${context.teamLine}`,
    `【人气确认】${context.historyLine}`,
    `【最终方案】${context.conclusionLine} ${context.riskLine}`,
  ]
}

function planByDataLedger(context: PlanContext): string[] {
  return [
    `【数据账本】${context.matchTitle}，先把可量化信息摆开，再给结论。`,
    `【1. 胜平负】${context.probabilityLine}`,
    `【2. 凯利/体彩】${context.signalLine}`,
    `【3. 历史/心理】${context.historyLine}`,
    `【4. 球队资料】${context.teamLine}`,
    `【5. 庄家盈亏】${context.bookmakerLine}${context.bookmakerPressure}`,
    `【6. 让球】${context.handicapLine}${context.handicapAdvice}`,
    `【7. 大小球】${context.goalLine}${context.goalAdvice}`,
    `【账本结论】${context.conclusionLine}`,
  ]
}

function planByLateChecklist(context: PlanContext): string[] {
  return [
    `【临场清单】${context.matchTitle}当前初判是${context.direction}，但需要按清单确认。`,
    `【确认A】概率是否继续支持主线：${context.probabilityLine}`,
    `【确认B】凯利与体彩是否同向：${context.signalLine}`,
    `【确认C】热度是否过满：${context.historyLine}`,
    `【确认D】庄家账本是否出现危险项：${context.bookmakerPressure}`,
    `【确认E】盘口是否配合：${context.handicapLine}${context.handicapAdvice}`,
    `【确认F】球数是否有节奏支撑：${context.goalLine}${context.goalAdvice}`,
    `【确认G】外部资料只作背景：${context.teamLine}`,
    `【清单结果】${context.conclusionLine} ${context.riskLine}`,
  ]
}

function planByCompactPreview(context: PlanContext): string[] {
  return [
    `【一句话定位】${context.matchTitle}，${context.confidenceTone}，先看${context.direction}。`,
    `【为什么】${context.probabilityLine}${context.signalLine}`,
    `【担心什么】${context.bookmakerPressure}${context.historyLine}`,
    `【怎么防】${context.handicapLine}${context.handicapAdvice}`,
    `【球数】${context.goalLine}${context.goalAdvice}`,
    `【资料补充】${context.teamLine}`,
    `【方案】${context.conclusionLine} ${context.riskLine}`,
  ]
}

function southByHeatMap(context: PlanContext): string[] {
  return [
    `【南派热度图】先看人流，不先看结论。${context.historyLine}`,
    `【三边站位】${context.probabilityLine} 当前热度如果继续贴近${context.direction}，要看庄家是否主动防守。`,
    `【庄家态度】${context.bookmakerLine}${context.bookmakerPressure}`,
    `【凯利过滤】${context.signalLine}`,
    `【盘口过滤】${context.handicapLine}${context.handicapAdvice}`,
    `【球数过滤】${context.goalLine}${context.goalAdvice}`,
    `【南派落点】${context.conclusionLine}`,
  ]
}

function southByPayoutTrap(context: PlanContext): string[] {
  return [
    `【南派赔付陷阱】这场先找庄家最怕哪一项打出。${context.bookmakerPressure}`,
    `【账面压力】${context.bookmakerLine}`,
    `【散户去向】${context.historyLine}`,
    `【方向确认】${context.probabilityLine}${context.signalLine}`,
    `【盘口保护】${context.handicapLine}${context.handicapAdvice}`,
    `【球路】${context.goalLine}${context.goalAdvice}`,
    `【结论】若临场没有反向保护，本场南派倾向${context.direction}；若热门继续过载，防守级别提高。`,
  ]
}

function southByKellyFilter(context: PlanContext): string[] {
  return [
    `【南派凯利筛】先用凯利和某彩过滤掉假方向。${context.signalLine}`,
    `【概率再看】${context.probabilityLine}`,
    `【人气再看】${context.historyLine}`,
    `【盈亏再看】${context.bookmakerPressure}`,
    `【让球落点】${context.handicapLine}${context.handicapAdvice}`,
    `【进球落点】${context.goalLine}${context.goalAdvice}`,
    `【南派结论】${context.conclusionLine} ${context.riskLine}`,
  ]
}

function southByHandicapGuard(context: PlanContext): string[] {
  return [
    `【南派盘口门】这场先过盘口这一关。${context.handicapLine}`,
    `【盘口含义】${context.handicapAdvice}`,
    `【人气是否配合】${context.historyLine}`,
    `【庄家是否难受】${context.bookmakerLine}${context.bookmakerPressure}`,
    `【概率是否兜底】${context.probabilityLine}${context.signalLine}`,
    `【大小球】${context.goalLine}${context.goalAdvice}`,
    `【出手方向】${context.conclusionLine}`,
  ]
}

function southByCrowdSplit(context: PlanContext): string[] {
  return [
    `【南派人流分割】${context.matchTitle}先拆三边：主、平、客的心理位置分别看${joinText(context.item.sanhuxinli?.slice(0, 3))}。`,
    `【主线概率】${context.probabilityLine}`,
    `【人流风险】${context.historyLine}`,
    `【庄家账】${context.bookmakerPressure}`,
    `【信息面】${context.teamLine}`,
    `【盘口和球数】${context.handicapLine}${context.goalLine}`,
    `【南派收口】倾向${context.direction}，球数配${context.item.qiuprediction}，临场防单边热度反噬。`,
  ]
}

function southByScenario(context: PlanContext): string[] {
  return [
    `【南派双剧本】剧本一是${context.direction}顺利打出，剧本二是热度过满后反向修正。`,
    `【剧本一依据】${context.probabilityLine}${context.signalLine}`,
    `【剧本二触发】${context.bookmakerPressure}${context.historyLine}`,
    `【盘口裁判】${context.handicapLine}${context.handicapAdvice}`,
    `【球数裁判】${context.goalLine}${context.goalAdvice}`,
    `【资料背景】${context.teamLine}`,
    `【最终取舍】默认走${context.direction}，但只要临场盘口与热度背离，就把防守选项提前。`,
  ]
}

function southByShortSlip(context: PlanContext): string[] {
  return [
    `【南派简案】${context.matchTitle}，先看${context.direction}，大小球看${context.item.qiuprediction}。`,
    `【核心证据】${context.probabilityLine}`,
    `【过滤证据】${context.signalLine}`,
    `【风险证据】${context.bookmakerPressure}`,
    `【盘口证据】${context.handicapLine}${context.handicapAdvice}`,
    `【球数证据】${context.goalLine}${context.goalAdvice}`,
    `【结论】${context.conclusionLine}`,
  ]
}

function profileSummary(value: string | undefined, teamName: string): string {
  const text = String(value || '').replace(/\s+/g, ' ').trim()
  if (!text) return `${teamName}暂无足够外部资料，本场先按本地赛程、排名和近期数据判断`
  return text.length > 90 ? `${text.slice(0, 90)}...` : text
}

function buildRiskLine(item: AnalysisMatch, probabilityGap: number, pressureRow: BookmakerOutcome | undefined): string {
  const warnings = item.warnings?.length ? `已有提示：${joinText(item.warnings)}。` : ''
  const gapText = probabilityGap < 7 ? '胜平负差距偏小，不能把单一方向当成稳胆。' : ''
  const pressureText = pressureRow && pressureRow.bookmakerProfit < 0 ? `${outcomeName(pressureRow)}项一旦打出会形成本地亏损，临场需要看是否有保护动作。` : ''
  return `风险提醒：${warnings}${gapText}${pressureText || '若盘口临场反向或热度突然集中，原方案需要降级。'}`
}

function resultProbabilityRank(item: AnalysisMatch) {
  return [
    { label: '主胜', side: `${item.home}方向`, value: numberOrZero(item.winProbability) },
    { label: '平局', side: '平局方向', value: numberOrZero(item.drawProbability) },
    { label: '客胜', side: `${item.guest}方向`, value: numberOrZero(item.loseProbability) },
  ].sort((a, b) => b.value - a.value)
}

function numberOrZero(value: number): number {
  return Number.isFinite(value) ? value : 0
}

function resultDirectionText(item: AnalysisMatch): string {
  if (item.prediction === '主胜') return `${item.home}方向`
  if (item.prediction === '客胜') return `${item.guest}方向`
  if (item.prediction === '平局') return '平局方向'
  return item.prediction || '-'
}

function matchScoreText(item: AnalysisMatch): string {
  if (viewMode.value !== 'minimal') return showScore.value ? `${item.homeScore}:${item.guestScore}` : 'VS'
  const stateText = String(item.displayState || '')
  const hasResult = stateText.includes('完') || stateText.includes('中') || item.status > 0 || item.homeScore !== 0 || item.guestScore !== 0
  return hasResult ? `${item.homeScore}:${item.guestScore}` : 'VS'
}

function variantIndex(item: AnalysisMatch, salt: string, length: number): number {
  if (length <= 0) return 0
  return Math.abs(hashText(`${selectedDate.value}:${item.matchId}:${item.home}:${item.guest}:${salt}`)) % length
}

function hashText(value: string): number {
  let hash = 0
  for (let index = 0; index < value.length; index += 1) {
    hash = ((hash << 5) - hash + value.charCodeAt(index)) | 0
  }
  return hash
}

function goToMatch(matchId: string) {
  router.push(`/match/${matchId}`)
}

function logoUrl(logo: string): string {
  return resolveAssetUrl(logo)
}

function teamInitial(name: string): string {
  return name?.slice(0, 1) || '-'
}

function rankLabel(rank: string): string {
  const value = String(rank || '').trim()
  return value ? `排名 ${value}` : '排名 -'
}

function formatTime(value: string) {
  if (!value) return '-'
  const directTime = value.match(/(?:T|\s)(\d{2}:\d{2})(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?$/)
  if (directTime) return directTime[1]
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value.slice(10, 16)
  return `${String(date.getUTCHours()).padStart(2, '0')}:${String(date.getUTCMinutes()).padStart(2, '0')}`
}

function localDateString(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function parseLocalDate(value: string): Date | null {
  const normalized = validDateString(value)
  if (!normalized) return null
  const [year, month, day] = normalized.split('-').map(Number)
  return new Date(year, month - 1, day)
}

function validDateString(value: string): string {
  const trimmed = value.trim()
  if (!/^\d{4}-\d{2}-\d{2}$/.test(trimmed)) return ''
  const date = parseLocalDateUnchecked(trimmed)
  if (!date) return ''
  return localDateString(date) === trimmed ? trimmed : ''
}

function parseLocalDateUnchecked(value: string): Date | null {
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return null
  const date = new Date(year, month - 1, day)
  if (Number.isNaN(date.getTime())) return null
  return date
}

function joinText(value: unknown[] | undefined, separator = ' / ') {
  if (!value?.length) return '-'
  return value.map((item) => Array.isArray(item) ? item.join(',') : String(item)).join(separator)
}

// 散户心理两行展示：第一行隐含概率基底（庄家定价），第二行加权后的模拟资金分布。
function sanhuCompareText(item: AnalysisMatch): string {
  const implied = `${Math.round(item.winProbability)}% / ${Math.round(item.drawProbability)}% / ${Math.round(item.loseProbability)}%`
  const weighted = joinText(item.sanhuxinli?.slice(0, 3))
  return `隐含 ${implied}\n加权 ${weighted}`
}

function scoreTriplet(scores: DirectionValues): string {
  return `主${scoreText(scores.home)} 平${scoreText(scores.draw)} 客${scoreText(scores.away)}`
}

const tradeMarketKeys = ['sporttery', 'sportterySim', 'sportteryRqspf', 'sportteryRqspfSim']

function bookmakerMarkets(item: AnalysisMatch): BookmakerMarket[] {
  return (item.roiSimulation?.markets ?? []).filter((market) => tradeMarketKeys.includes(market.key))
}

function showBookmakerSection(item: AnalysisMatch): boolean {
  return hasLocalProfitMarket(item) || bookmakerMarkets(item).length > 0
}

function localStakeBase(item: AnalysisMatch): number {
  const value = item.roiSimulation?.totalStake
  return Number.isFinite(value) && (value || 0) > 0 ? Number(value) : localBookmakerTotalStake
}

function hasLocalProfitMarket(item: AnalysisMatch): boolean {
  return localProfitRows(item).length > 0
}

function localProfitRows(item: AnalysisMatch): BookmakerOutcome[] {
  return localProfitMarket(item)?.bookmakerByOutcome ?? []
}

function localOddsTriplet(item: AnalysisMatch): string {
  const market = localProfitMarket(item)
  return market ? scoreTriplet(market.odds) : '-'
}

function localRetailTriplet(item: AnalysisMatch): string {
  const market = localProfitMarket(item)
  return market ? scoreTriplet(market.retailDistribution) : '-'
}

function localProfitMarket(item: AnalysisMatch): BookmakerMarket | null {
  return item.platform?.localMarket ?? null
}


function parseOptionalNumber(value: unknown): number {
  if (typeof value === 'number') return Number.isFinite(value) ? value : Number.NaN
  const text = String(value ?? '').trim().replace(/%$/, '')
  if (!text || text === '-') return Number.NaN
  const numeric = Number.parseFloat(text)
  return Number.isFinite(numeric) ? numeric : Number.NaN
}



function sportteryMarket(item: AnalysisMatch): BookmakerMarket | undefined {
  return bookmakerMarkets(item).find((market) => market.key === 'sporttery')
}

// 竞彩投注比例表只展示官方数据，模拟盘不渲染。
function sportteryRatioMarket(item: AnalysisMatch): BookmakerMarket | undefined {
  const market = sportteryMarket(item)
  return market && !market.simulated ? market : undefined
}

function sportteryRows(item: AnalysisMatch): BookmakerOutcome[] {
  const market = sportteryMarket(item)
  return market?.bettingRatio?.length ? market.bettingRatio : (market?.bookmakerByOutcome ?? [])
}

function sportteryPsychologyLabel(item: AnalysisMatch): string {
  return sportteryMarket(item)?.psychologyErrorLabel || '-'
}

function marketNames(item: AnalysisMatch): string {
  const names = bookmakerMarkets(item).map((market) => market.name)
  return names.length ? names.join(' / ') : '-'
}

function marketProfitAlert(market: BookmakerMarket): StatRow {
  const rows = market.bookmakerByOutcome.filter((row) => (
    row.available
    && Number.isFinite(row.retailShare)
    && Number.isFinite(row.bookmakerProfit)
  ))
  if (!rows.length) {
    return {
      label: '注意盈亏',
      value: '交易盈亏数据不足，先结合盘口变化观察。',
      tone: 'normal',
    }
  }

  const maxShare = Math.max(...rows.map((row) => row.retailShare))
  const maxSupportRows = rows.filter((row) => Math.abs(row.retailShare - maxShare) < 0.01)
  const profitableRows = maxSupportRows.filter((row) => row.bookmakerProfit > 0)
  if (profitableRows.length) {
    return {
      label: '重点提醒',
      value: `彩民支持率最大的是${profitableRows.map(outcomeName).join('、')}，本地庄家盈亏大于0，此场比赛可能就是这个方向。`,
      tone: 'blue',
    }
  }

  return {
    label: '注意盈亏',
    value: '彩民支持率最大方向没有对应本地庄家盈利，谨防热度与赔付方向背离。',
    tone: 'normal',
  }
}

function marketProfitAlertText(market: BookmakerMarket): string {
  const alert = marketProfitAlert(market)
  return `${alert.label}：${alert.value}`
}

function marketProfitAlertClass(market: BookmakerMarket): string {
  return alertToneClass(marketProfitAlert(market).tone)
}

function scoreText(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return value.toFixed(2).replace(/\.00$/, '')
}

function formatShare(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return `${trimFixed(value, 2)}%`
}

function percentValueText(value: number | undefined): string {
  if (!Number.isFinite(value)) return '-'
  return `${trimFixed(value || 0, 2)}%`
}

function signedPercentText(value: number | undefined): string {
  if (!Number.isFinite(value)) return '-'
  const numeric = value || 0
  const sign = numeric > 0 ? '+' : ''
  return `${sign}${trimFixed(numeric, 2)}%`
}

function formatRoi(value: number, available = true): string {
  if (!available) return '赔率不足'
  if (!Number.isFinite(value)) return '-'
  const sign = value > 0 ? '+' : ''
  return `${sign}${value.toFixed(2)}%`
}

function profitRateText(row: BookmakerOutcome): string {
  return formatRoi(profitRateValue(row), profitRateAvailable(row))
}

function profitRateClass(row: BookmakerOutcome): string {
  return officialRateClass(profitRateValue(row), profitRateAvailable(row))
}

function profitRateValue(row: BookmakerOutcome): number {
  return typeof row.officialProfitRate === 'number' && Number.isFinite(row.officialProfitRate) ? row.officialProfitRate : Number.NaN
}

function profitRateAvailable(row: BookmakerOutcome): boolean {
  return typeof row.officialProfitRate === 'number' && Number.isFinite(row.officialProfitRate)
}

function roiClass(value: number, available = true): string {
  if (!available || Math.abs(value) < 0.01) return 'text-slate-500'
  return value > 0 ? 'text-red-600' : 'text-emerald-600'
}

function officialRateClass(value: number, available = true): string {
  if (!available || Math.abs(value) < 0.01) return 'text-slate-500'
  return value > 0 ? 'text-red-600' : 'text-emerald-600'
}

function bookmakerClass(row: BookmakerOutcome): string {
  return roiClass(row.bookmakerProfit, row.available)
}

function outcomeName(row: BookmakerOutcome): string {
  if (row.outcome === 'home') return '胜'
  if (row.outcome === 'draw') return '平'
  return '负'
}

function outcomeClass(row: BookmakerOutcome): string {
  if (row.outcome === 'home') return 'text-primary'
  if (row.outcome === 'draw') return 'text-slate-800'
  return 'text-slate-800'
}

function supportClass(value: number): string {
  if (!Number.isFinite(value)) return 'text-slate-800'
  return value >= 50 ? 'text-red-600' : 'text-slate-900'
}

function alertToneClass(tone: StatRow['tone']): string {
  if (tone === 'red') return 'border-red-200 bg-red-50 text-red-800'
  if (tone === 'blue') return 'border-sky-200 bg-sky-50 text-sky-800'
  if (tone === 'green') return 'border-emerald-200 bg-emerald-50 text-emerald-800'
  return 'border-slate-100 bg-slate-50 text-slate-600'
}

function statRowClass(row: StatRow): string {
  if (row.tone === 'red') return 'border-l-2 border-red-500 bg-red-50'
  if (row.tone === 'blue') return 'border-l-2 border-sky-500 bg-sky-50'
  if (row.tone === 'green') return 'border-l-2 border-emerald-500 bg-emerald-50'
  return ''
}

function statRowLabelClass(row: StatRow): string {
  if (row.tone === 'red') return 'text-red-700'
  if (row.tone === 'blue') return 'text-sky-700'
  if (row.tone === 'green') return 'text-emerald-700'
  return 'text-slate-500'
}

function statRowValueClass(row: StatRow): string {
  if (row.tone === 'red') return 'text-red-900'
  if (row.tone === 'blue') return 'text-sky-900'
  if (row.tone === 'green') return 'text-emerald-900'
  return 'text-slate-950'
}

function guideCellClass(tone: StatRow['tone']): string {
  if (tone === 'red') return 'bg-red-50 text-red-700'
  if (tone === 'blue') return 'bg-sky-50 text-sky-700'
  if (tone === 'green') return 'bg-emerald-50 text-emerald-700'
  return 'text-slate-950'
}

function guideRowClass(row: GuideCompareRow): string {
  return row.variant === 'secondary' ? 'bg-slate-100' : ''
}

function guideRowLabelClass(row: GuideCompareRow): string {
  return row.variant === 'secondary' ? 'text-sky-700' : 'text-slate-500'
}

function guideRowValueClass(row: GuideCompareRow, tone: StatRow['tone']): string {
  return row.variant === 'secondary' ? 'text-sky-700' : guideCellClass(tone)
}

function hotColdText(value: number | undefined): string {
  if (!Number.isFinite(value)) return '-'
  return trimFixed(value || 0, 2)
}

function hotColdClass(value: number | undefined): string {
  if (!Number.isFinite(value) || Math.abs(value || 0) < 0.01) return 'text-slate-500'
  return (value || 0) > 0 ? 'text-red-600' : 'text-slate-700'
}

function moneyText(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return Math.round(value).toLocaleString('zh-CN')
}

function moneyCompactText(value: number): string {
  if (!Number.isFinite(value)) return '-'
  const absolute = Math.abs(value)
  if (absolute >= 10000) {
    return `${trimFixed(value / 10000, 1)}万`
  }
  return `${moneyText(value)}元`
}

function signedMoneyText(value: number, available = true): string {
  if (!available) return '赔率不足'
  if (!Number.isFinite(value)) return '-'
  const sign = value > 0 ? '+' : value < 0 ? '-' : ''
  return `${sign}${moneyCompactText(Math.abs(value))}`
}

function oddsText(row: BookmakerOutcome): string {
  if (!Number.isFinite(row.odds) || row.odds <= 0) return '-'
  return trimFixed(row.odds, 2)
}

function trimFixed(value: number, fractionDigits: number): string {
  return value.toFixed(fractionDigits).replace(/\.0+$/, '').replace(/(\.\d*[1-9])0+$/, '$1')
}

function historyLine(value: unknown[] | undefined) {
  if (!value?.length) return '暂无历史交锋'
  return `${value[0] || ''} ${value[1] || ''} VS ${value[2] || ''} ${value[3] ?? '-'}:${value[4] ?? '-'} ${value[5] || ''}`
}

function matchMini(value: unknown[] | undefined) {
  if (!value?.length) return '-'
  return `${String(value[1] || '').slice(0, 4)} VS ${String(value[2] || '').slice(0, 4)}`
}

function scoreMini(value: unknown[] | undefined) {
  if (!value?.length) return '-'
  return `${value[3] ?? '-'}:${value[4] ?? '-'}`
}

function outcomeLabelByKey(outcome: DirectionOutcome | null, item: AnalysisMatch): string {
  if (outcome === 'home') return `主胜(${item.home})`
  if (outcome === 'away') return `客胜(${item.guest})`
  return '平局'
}

function guideCompareRows(item: AnalysisMatch): GuideCompareRow[] {
  const bookmaker = bookmakerGuidePrediction(item)
  const platform = platformLivePrediction(item)
  return [
    {
      label: '胜平负',
      bookmakerValue: outcomeLabelByKey(bookmaker.outcome, item),
      platformValue: outcomeLabelByKey(platform.outcome, item),
      bookmakerTone: outcomeTone(bookmaker.outcome),
      platformTone: outcomeTone(platform.outcome),
    },
    {
      label: '进球数',
      bookmakerValue: bookmaker.goal.label,
      platformValue: platform.goal.label,
      bookmakerTone: bookmaker.goal.tone,
      platformTone: platform.goal.tone,
    },
    {
      label: '比分',
      bookmakerValue: bookmaker.score,
      platformValue: platform.score,
      bookmakerTone: scoreTone(bookmaker.score),
      platformTone: scoreTone(platform.score),
    },
    {
      label: '次选比分',
      bookmakerValue: bookmaker.secondaryScore,
      platformValue: platform.secondaryScore,
      bookmakerTone: scoreTone(bookmaker.secondaryScore),
      platformTone: scoreTone(platform.secondaryScore),
      variant: 'secondary',
    },
  ]
}

function guideMetaRows(item: AnalysisMatch): GuideMetaRow[] {
  return [
    {
      label: '盘口',
      leftValue: `亚盘 ${valueText(item.yapanpankou1)}/${valueText(item.yapanpankou2)}`,
      rightValue: `大小球 ${valueText(item.qiushupankou1)}/${valueText(item.qiushupankou2)}`,
    },
    {
      label: '投注热度',
      leftValue: `${percentText(item.yapantouzhu?.[0])}/${percentText(item.yapantouzhu?.[1])}`,
      rightValue: `${percentText(item.qiushutouzhu?.[0])}/${percentText(item.qiushutouzhu?.[1])}`,
      leftTone: heatCellTone(item.yapantouzhu?.[0], item.yapantouzhu?.[1]),
      rightTone: heatCellTone(item.qiushutouzhu?.[0], item.qiushutouzhu?.[1]),
    },
    {
      label: '专业信号',
      leftValue: `凯利 ${joinText(item.kailiresult)}`,
      rightValue: `体彩 ${joinText(item.ticairesult)}`,
    },
  ]
}

// 警示全部由后端 platform 块计算，前端只渲染。
function guideWarningRows(item: AnalysisMatch): GuideWarningRow[] {
  return item.platform?.warningRows ?? []
}

function guideWarningClass(tone: StatRow['tone']): string {
  if (tone === 'green') return 'text-emerald-600'
  if (tone === 'blue') return 'text-sky-600'
  return 'text-red-600'
}

function heatCellTone(...values: unknown[]): StatRow['tone'] {
  return values.some((value) => parseOptionalNumber(value) > 65) ? 'red' : 'normal'
}

function guideWarningPredictionSummary(item: AnalysisMatch): string {
  return item.platform?.warningAdjustedSummary ?? ''
}

// 邪修全部数据由后端 platform.evilCult 提供。
function evilCultRows(item: AnalysisMatch): EvilCultRow[] {
  return (item.platform?.evilCult.rows ?? []) as EvilCultRow[]
}

const emptyEvilCultPrediction: PlatformEvilCultPrediction = {
  goal: '-', secondaryGoal: '-', total: '-', secondaryTotal: '-',
  underGoal: '-', overGoal: '-', underTotalText: '-', overTotalText: '-',
  underTotalValue: 0, overTotalValue: 0, underGoalLine: 2.5, overGoalLine: 2.5,
  underScore: '-', overScore: '-', underOutcome: 'draw', overOutcome: 'draw',
  firstPick: '-', firstDirection: 'under', mainPick: '-', reversePick: '-',
  mainReason: '', secondPassReason: '', secondPassReversed: false, secondPassForced: false,
  secondOverScore: 0, secondUnderScore: 0, mainTotal: 0, secondaryTotalValue: 0,
  goalDirection: 'under', secondaryGoalDirection: 'over', goalLine: 2.5, secondaryGoalLine: 2.5,
  score: '-', secondaryScore: '-', outcome: 'draw', secondaryOutcome: 'draw',
  goalTone: 'red', reverseTone: 'green', note: '', reason: '',
}

function evilCultPrediction(item: AnalysisMatch): PlatformEvilCultPrediction {
  return item.platform?.evilCult.prediction ?? emptyEvilCultPrediction
}

function evilCultScorePercent(scores: { overPercent: number; underPercent: number }, side: 'over' | 'under'): number {
  return side === 'over' ? scores.overPercent : scores.underPercent
}

function evilCultScoreText(value: number): string {
  return Number.isFinite(value) ? trimFixed(value, 1) : '-'
}

function evilCultDeltaText(value: number): string {
  if (!Number.isFinite(value) || Math.abs(value) < 0.01) return '0'
  return `${value > 0 ? '+' : ''}${trimFixed(value, 1)}`
}

function evilCultClass(tone: StatRow['tone'] | string): string {
  if (tone === 'green') return 'text-emerald-700'
  if (tone === 'red') return 'text-red-600'
  if (tone === 'blue') return 'text-sky-700'
  return 'text-slate-800'
}

const emptyGuidePrediction: GuidePrediction = {
  outcome: 'draw',
  goal: { label: '-', total: 0, tone: 'normal' },
  score: '-',
  secondaryScore: '-',
}

// 庄家/平台预测全部由后端 platform 块计算。
function bookmakerGuidePrediction(item: AnalysisMatch): GuidePrediction {
  return item.platform?.bookmaker ?? emptyGuidePrediction
}

function platformLivePrediction(item: AnalysisMatch): GuidePrediction {
  return item.platform?.platform ?? emptyGuidePrediction
}

function outcomeTone(outcome: DirectionOutcome): StatRow['tone'] {
  if (outcome === 'home') return 'red'
  if (outcome === 'away') return 'green'
  return 'blue'
}

function scoreTone(score: string): StatRow['tone'] {
  return outcomeTone(scoreOutcome(score))
}

function scoreOutcome(score: string): DirectionOutcome {
  const [home, guest] = score.split(':').map((value) => Number.parseInt(value, 10))
  if (!Number.isFinite(home) || !Number.isFinite(guest)) return 'draw'
  if (home > guest) return 'home'
  if (home < guest) return 'away'
  return 'draw'
}

function historyStatRows(item: AnalysisMatch): StatRow[] {
  return labeledRows([
    '主队历史胜率',
    '历史平局率',
    '客队历史胜率',
    '历史结论',
    '历史均球',
  ], item.liangduilishi)
}

function goalStatRows(item: AnalysisMatch): GoalStatTableRow[] {
  const expectedGoals = expectedGoalBands(item)

  return [
    {
      label: '总进球数',
      homeValue: valueText(item.qiushuAll?.[0]),
      guestValue: valueText(item.qiushuAll?.[2]),
    },
    {
      label: '最大进球数',
      homeValue: valueText(item.qiushuAll?.[1]),
      guestValue: valueText(item.qiushuAll?.[3]),
    },
    {
      label: '丢球数',
      homeValue: valueText(item.qiushuAll?.[4]),
      guestValue: valueText(item.qiushuAll?.[5]),
    },
    {
      label: '上档预测（小于盘口）',
      homeValue: expectedGoalText(expectedGoals.under.home),
      guestValue: expectedGoalText(expectedGoals.under.guest),
      homeTone: 'red',
      guestTone: 'green',
    },
    {
      label: '预测本场进球数',
      homeValue: expectedGoalText(expectedGoals.main.home),
      guestValue: expectedGoalText(expectedGoals.main.guest),
      homeTone: 'red',
      guestTone: 'green',
    },
    {
      label: '下档预测（盘口+2球）',
      homeValue: expectedGoalText(expectedGoals.over.home),
      guestValue: expectedGoalText(expectedGoals.over.guest),
      homeTone: 'red',
      guestTone: 'green',
    },
  ]
}

function goalStatToneClass(tone: GoalStatTableRow['homeTone']): string {
  if (tone === 'red') return 'bg-red-50 text-red-600'
  if (tone === 'green') return 'bg-emerald-50 text-emerald-600'
  return 'text-slate-950'
}

// 期望进球全部由后端 platform.goals 提供（null 表示样本不足）。
function platformPair(pair: PlatformGoalPair | undefined): GoalScore {
  return { home: pair?.home ?? Number.NaN, guest: pair?.guest ?? Number.NaN }
}

function expectedGoalPair(item: AnalysisMatch): GoalScore {
  return platformPair(item.platform?.goals.main)
}

function expectedGoalBands(item: AnalysisMatch): { under: GoalScore; main: GoalScore; over: GoalScore } {
  return {
    under: platformPair(item.platform?.goals.under),
    main: platformPair(item.platform?.goals.main),
    over: platformPair(item.platform?.goals.over),
  }
}

function zeroGoalAdviceText(item: AnalysisMatch): string {
  return item.platform?.zeroGoalAdvice ?? ''
}

// 球数/让球均值全部由后端计算（historyGoalAverage / recentGoalAverage / combinedGoalAverage / combinedHandicapAverage），null 表示样本不足。
function goalMetricText(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '-'
  return trimFixed(value, 2)
}

function expectedGoalText(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return String(Math.round(value))
}

function expectedHandicapRows(item: AnalysisMatch): StatRow[] {
  const [historyHandicap, recentHandicap] = splitPair(item.changguiyapan)
  return [
    { label: '历史期望让球', value: valueText(historyHandicap) },
    { label: '近期状态让球', value: valueText(recentHandicap) },
    { label: '综合均值', value: goalMetricText(item.combinedHandicapAverage) },
    { label: '亚盘初盘', value: valueText(item.yapanpankou1) },
    { label: '亚盘即时盘', value: valueText(item.yapanpankou2) },
    { label: '投注主队比例', value: percentText(item.yapantouzhu?.[0]) },
    { label: '投注客队比例', value: percentText(item.yapantouzhu?.[1]) },
    { label: '主队历史主场进球数', value: valueText(item.yapantouzhu?.[10]) },
    { label: '客队历史客场进球数', value: valueText(item.yapantouzhu?.[11]) },
    { label: '压力方向', value: valueText(item.yapantouzhu?.[12]) },
    ...((item.platform?.handicapAlertRows ?? []) as StatRow[]),
  ]
}

function expectedGoalRows(item: AnalysisMatch): StatRow[] {
  return [
    { label: '历史平均球数', value: goalMetricText(item.historyGoalAverage) },
    { label: '近期平均球数', value: goalMetricText(item.recentGoalAverage) },
    { label: '综合均值', value: goalMetricText(item.combinedGoalAverage) },
    { label: '大小球初盘', value: valueText(item.qiushupankou1) },
    { label: '大小球即时盘', value: valueText(item.qiushupankou2) },
    { label: '投注大球比例', value: percentText(item.qiushutouzhu?.[0]) },
    { label: '投注小球比例', value: percentText(item.qiushutouzhu?.[1]) },
    { label: '最近5场平均进球数', value: valueText(item.qiushutouzhu?.[2]) },
    { label: '最近5场平均丢球数', value: valueText(item.qiushutouzhu?.[3]) },
    { label: '压力方向', value: valueText(item.qiushutouzhu?.[6]) },
    ...((item.platform?.goalBalanceAlertRows ?? []) as StatRow[]),
  ]
}


function labeledRows(labels: string[], values: unknown[] | undefined): StatRow[] {
  return labels.map((label, index) => ({ label, value: valueText(values?.[index]) }))
}

function splitPair(value: string): [string, string] {
  const parts = String(value || '').split(':').map((item) => item.trim())
  return [parts[0] || '', parts[1] || '']
}

function percentText(value: unknown): string {
  const text = valueText(value)
  if (text === '-' || text.endsWith('%')) return text
  return `${text}%`
}

function valueText(value: unknown): string {
  if (value === null || value === undefined || value === '') return '-'
  if (Array.isArray(value)) return value.map((item) => valueText(item)).join(', ')
  return String(value)
}

function handleAnalysisPageScroll() {
  persistAnalysisPageState()
}

watch([selectedDate, showScore, viewMode, matchScope, selectedLeague], persistAnalysisPageState)

onMounted(() => {
  window.addEventListener('scroll', handleAnalysisPageScroll, { passive: true })
  window.addEventListener('pagehide', persistAnalysisPageState)
  window.addEventListener('visibilitychange', persistAnalysisPageState)
  loadData()
  if (viewMode.value === 'stats') void loadAccuracyStats()
})

onBeforeUnmount(() => {
  persistAnalysisPageState()
  window.removeEventListener('scroll', handleAnalysisPageScroll)
  window.removeEventListener('pagehide', persistAnalysisPageState)
  window.removeEventListener('visibilitychange', persistAnalysisPageState)
})
</script>
