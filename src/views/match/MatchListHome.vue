<template>
  <div class="min-h-screen pb-24 bg-[#0b1020] text-white">
    <header class="sticky top-0 z-50 border-b border-slate-800 bg-[#0b1020]/95 backdrop-blur">
      <div class="px-4 py-3 max-w-5xl mx-auto">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0">
            <p class="text-[11px] text-slate-400 font-bold">核心页面 · 竞彩足球 {{ selectedDateLabel }}</p>
            <h1 class="text-xl font-black truncate">比赛分析工作台</h1>
          </div>
          <button
            class="size-10 rounded-lg bg-primary flex items-center justify-center disabled:opacity-60"
            :disabled="loading"
            @click="loadData"
          >
            <span class="material-symbols-outlined" :class="{ 'animate-spin': loading }">sync</span>
          </button>
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
            <p class="text-[10px] text-slate-500 font-bold">竞彩足球</p>
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
                <p class="mt-1 text-[11px] font-bold text-slate-400">按完场结果统计小球组、追大组、主推和反向推；大小球统一按半球盘口计算。</p>
              </div>
              <table class="w-full table-fixed text-sm">
                <thead class="bg-slate-50 text-xs font-black text-slate-500">
                  <tr>
                    <th class="px-2 py-2 text-left">项目</th>
                    <th class="px-2 py-2 text-center">小球组</th>
                    <th class="px-2 py-2 text-center">追大组</th>
                    <th class="px-2 py-2 text-center">主推</th>
                    <th class="px-2 py-2 text-center">反向推</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100">
                  <tr v-for="row in accuracyStats.evilCultRows" :key="row.label">
                    <td class="px-2 py-3 font-black text-slate-500">{{ row.label }}</td>
                    <td class="px-2 py-3 text-center font-black text-red-600">{{ accuracyRateText(row.underCorrect, row.sample) }}</td>
                    <td class="px-2 py-3 text-center font-black text-emerald-700">{{ accuracyRateText(row.overCorrect, row.sample) }}</td>
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
              <Metric label="散户心理" :value="joinText(item.sanhuxinli?.slice(0, 3))" />
              <Metric label="凯利预测" :value="joinText(item.kailiresult)" />
              <Metric label="体彩预测" :value="joinText(item.ticairesult)" />
            </div>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'minimal'" title="盘口信号" icon="tune">
            <GuideMetaTable :rows="guideMetaRows(item)" />
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
                <p class="text-[11px] font-black text-slate-500">主推</p>
                <p class="mt-1 text-sm font-black" :class="evilCultClass(evilCultPrediction(item).goalTone)">{{ evilCultPrediction(item).mainPick }}</p>
                <p class="mt-1 text-[11px] font-bold text-slate-500">{{ evilCultPrediction(item).mainReason }}</p>
                <p class="mt-2 text-[11px] font-black text-slate-500">反向推</p>
                <p class="mt-1 text-sm font-black" :class="evilCultClass(evilCultPrediction(item).reverseTone)">{{ evilCultPrediction(item).reversePick }}</p>
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
              <Metric label="官方来源" :value="marketNames(item)" />
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
              <p class="border-t border-slate-100 px-3 py-2 text-[11px] text-slate-500">注：本地测算仅使用平均欧赔与散户心理，不使用竞彩投注数据</p>
            </div>

            <div v-if="sportteryMarket(item)" class="mt-3 overflow-hidden rounded-md border border-slate-200 bg-white text-xs">
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
              <div class="flex items-center justify-between gap-2 border-l-2 border-red-500 bg-slate-50 px-3 py-2">
                <p class="font-black text-slate-900">{{ market.name }}交易盈亏</p>
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
              <p class="border-t px-3 py-2 text-[11px] font-black" :class="marketProfitAlertClass(market)">
                {{ marketProfitAlertText(market) }}
              </p>
            </div>
          </AnalysisSection>

          <AnalysisSection v-if="viewMode === 'full'" title="赔率与凯利" icon="query_stats">
            <div class="grid grid-cols-2 gap-2 text-sm">
              <Metric label="平均欧赔" :value="joinText(item.detail.test8)" />
              <Metric label="散户心理" :value="joinText(item.sanhuxinli?.slice(0, 3))" />
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

  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, onMounted, type PropType, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import analysisApi from '@/api/analysis'
import type { AnalysisRuleSnapshot } from '@/api/analysis'
import { resolveAssetUrl } from '@/api/request'
import type { AnalysisMatch, BookmakerMarket, BookmakerOutcome, DirectionValues } from '@/types/analysis'

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

interface AccuracyStatsRow {
  label: string
  sample: number
  bookmakerCorrect: number
  platformCorrect: number
  bothCorrect: number
}

interface EvilCultAccuracyRow {
  label: string
  sample: number
  underCorrect: number
  overCorrect: number
  mainCorrect: number
  reverseCorrect: number
}

interface AccuracyOverallStats {
  sample: number
  bookmakerCorrect: number
  platformCorrect: number
}

interface AccuracyCommonRule {
  value: string
  sample: number
  bothCorrect: number
  rate: number
}

interface AccuracyCommonRow {
  label: string
  sample: number
  rules: AccuracyCommonRule[]
}

interface AccuracyFitSummary {
  label: string
  tone: StatRow['tone']
  score: number
  ruleCount: number
  rate: number
  sample: number
}

interface AccuracyMatchRow {
  matchId: string
  date: string
  matchTitle: string
  league: string
  time: string
  outcomeFit: AccuracyFitSummary
  goalFit: AccuracyFitSummary
  scoreFit: AccuracyFitSummary
  conclusion: string
  tone: StatRow['tone']
  evidence: string
  resultSummary: string
  resultTone: StatRow['tone']
}

interface AccuracyStatsSummary {
  startDate: string
  endDate: string
  total: number
  overall: AccuracyOverallStats
  rows: AccuracyStatsRow[]
  evilCultRows: EvilCultAccuracyRow[]
  commonRows: AccuracyCommonRow[]
  generatedCommonRows: AccuracyCommonRow[]
  matchRows: AccuracyMatchRow[]
  settledFitRows: AccuracyMatchRow[]
}

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
      h('p', { class: 'font-bold break-words leading-snug' }, props.value || '-'),
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
const showScore = ref(restoredState?.showScore ?? true)
const viewMode = ref<AnalysisViewMode>(restoredState?.viewMode || 'simple')
const selectedItem = ref<AnalysisMatch | null>(null)
const dialogMode = ref<'plan' | 'south'>('plan')
const dialogPublishTitle = ref('')
const dialogPublicContent = ref('')
const dialogDetailContent = ref<string[]>([])
const copying = ref(false)
const copiedSection = ref<DialogCopySection | null>(null)
const selectedDate = ref(validDateString(String(route.query.date || '')) || restoredState?.selectedDate || todayString)
const localBookmakerTotalStake = 50000000
const accuracyHistoryStartDate = '2026-05-28'
const guideSectionTitle = computed(() => '庄家 / 平台预测')
const accuracyStatsLoading = ref(false)
const accuracyStatsError = ref('')
const accuracyStats = ref<AccuracyStatsSummary>(emptyAccuracyStats(selectedDate.value))
const accuracyCommonRows = ref<AccuracyCommonRow[]>(emptyAccuracyCommonRows())
const analysisViewModes: Array<{ value: AnalysisViewMode; label: string }> = [
  { value: 'simple', label: '简化版' },
  { value: 'minimal', label: '邪修版' },
  { value: 'full', label: '复杂版' },
  { value: 'stats', label: '统计页' },
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

interface AnalysisPageState {
  selectedDate: string
  showScore: boolean
  viewMode: AnalysisViewMode
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

const accuracyStatsRangeText = computed(() => `${accuracyStats.value.startDate} 至 ${accuracyStats.value.endDate}`)
const accuracyMatchRowsById = computed(() => {
  const rows = accuracyCommonRows.value.length ? accuracyCommonRows.value : accuracyStats.value.commonRows
  return new Map(buildAccuracyMatchRows(list.value, rows, 'all').map((row) => [row.matchId, row]))
})

const highConfidenceCount = computed(() => list.value.filter((item) => item.confidence === '高信心').length)

const accuracyLabel = computed(() => {
  const settled = list.value.filter((item) => item.prediction && item.displayState === '完场')
  if (!settled.length) return 0
  const correct = settled.filter((item) => {
    if (item.homeScore > item.guestScore) return item.prediction === '主胜'
    if (item.homeScore < item.guestScore) return item.prediction === '客胜'
    return item.prediction === '平局'
  }).length
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
    const [matchesResponse, snapshotResponse] = await Promise.all([
      analysisApi.getAnalysisMatches({ date: selectedDate.value }),
      analysisApi.getAnalysisRuleSnapshot().catch(() => ({ data: null as AnalysisRuleSnapshot | null })),
    ])
    list.value = matchesResponse.data ?? []
    accuracyCommonRows.value = snapshotCommonRows(snapshotResponse.data) || emptyAccuracyCommonRows()
    restoreDialogFromState()
    restoreScrollFromState()
    persistAnalysisPageState()
  } finally {
    loading.value = false
  }
}

async function loadAccuracyStats() {
  accuracyStatsLoading.value = true
  accuracyStatsError.value = ''
  const dates = accuracyFixedRuleDateRange()
  accuracyStats.value = emptyAccuracyStats(selectedDate.value)
  try {
    const [responses, currentResponse, snapshotResponse] = await Promise.all([
      Promise.all(dates.map((date) => analysisApi.getAnalysisMatches({ date }))),
      analysisApi.getAnalysisMatches({ date: selectedDate.value }),
      analysisApi.getAnalysisRuleSnapshot().catch(() => ({ data: null as AnalysisRuleSnapshot | null })),
    ])
    const matches = responses.flatMap((response) => response.data ?? []).filter(isSettledMatch)
    const currentMatches = currentResponse.data ?? []
    list.value = currentMatches
    accuracyStats.value = buildAccuracyStats(matches, dates[0] || selectedDate.value, dates[dates.length - 1] || selectedDate.value, currentMatches, snapshotResponse.data)
    accuracyCommonRows.value = accuracyStats.value.commonRows
  } catch {
    accuracyStatsError.value = '历史统计加载失败，请稍后重试。'
  } finally {
    accuracyStatsLoading.value = false
  }
}

function emptyAccuracyStats(endDate: string): AccuracyStatsSummary {
  const dates = accuracyFixedRuleDateRange(endDate)
  return {
    startDate: dates[0] || endDate,
    endDate: dates[dates.length - 1] || endDate,
    total: 0,
    overall: emptyAccuracyOverallStats(),
    rows: [
      emptyAccuracyRow('胜平负'),
      emptyAccuracyRow('大小球'),
      emptyAccuracyRow('比分'),
    ],
    evilCultRows: emptyEvilCultAccuracyRows(),
    commonRows: emptyAccuracyCommonRows(),
    generatedCommonRows: [],
    matchRows: [],
    settledFitRows: [],
  }
}

function emptyAccuracyCommonRows(): AccuracyCommonRow[] {
  return [
    emptyAccuracyCommonRow('胜平负双中'),
    emptyAccuracyCommonRow('大小球双中'),
    emptyAccuracyCommonRow('比分命中'),
  ]
}

function emptyAccuracyOverallStats(): AccuracyOverallStats {
  return {
    sample: 0,
    bookmakerCorrect: 0,
    platformCorrect: 0,
  }
}

function emptyAccuracyRow(label: string): AccuracyStatsRow {
  return {
    label,
    sample: 0,
    bookmakerCorrect: 0,
    platformCorrect: 0,
    bothCorrect: 0,
  }
}

function emptyEvilCultAccuracyRows(): EvilCultAccuracyRow[] {
  return ['综合', '大小球', '球数', '比分', '胜平负'].map((label) => emptyEvilCultAccuracyRow(label))
}

function emptyEvilCultAccuracyRow(label: string): EvilCultAccuracyRow {
  return {
    label,
    sample: 0,
    underCorrect: 0,
    overCorrect: 0,
    mainCorrect: 0,
    reverseCorrect: 0,
  }
}

function emptyAccuracyCommonRow(label: string): AccuracyCommonRow {
  return { label, sample: 0, rules: [] }
}

function buildAccuracyStats(matches: AnalysisMatch[], startDate: string, endDate: string, currentMatches: AnalysisMatch[] = [], snapshot?: AnalysisRuleSnapshot | null): AccuracyStatsSummary {
  const rows = [
    buildAccuracyRow('胜平负', matches, (item, bookmaker, platform) => {
      const actual = actualMatchOutcome(item)
      return {
        bookmaker: actual !== null && bookmaker.outcome === actual,
        platform: actual !== null && platform.outcome === actual,
      }
    }),
    buildAccuracyRow('大小球', matches, (item, bookmaker, platform) => ({
      bookmaker: goalPredictionCorrect(item, bookmaker.goal),
      platform: goalPredictionCorrect(item, platform.goal),
    })),
    buildAccuracyRow('比分', matches, (item, bookmaker, platform) => ({
      bookmaker: scorePredictionCorrect(item, bookmaker.score),
      platform: scorePredictionCorrect(item, platform.score),
    }), true),
  ]

  const generatedCommonRows = buildAccuracyCommonRows(matches)
  const commonRows = snapshotCommonRows(snapshot) || generatedCommonRows
  return {
    startDate,
    endDate,
    total: matches.length,
    overall: buildAccuracyOverallStats(rows),
    rows,
    evilCultRows: buildEvilCultAccuracyRows(matches),
    commonRows,
    generatedCommonRows,
    matchRows: buildAccuracyMatchRows(currentMatches, commonRows, 'upcoming'),
    settledFitRows: buildAccuracyMatchRows(currentMatches, commonRows, 'settledFit'),
  }
}

function buildAccuracyOverallStats(rows: AccuracyStatsRow[]): AccuracyOverallStats {
  return rows.reduce<AccuracyOverallStats>((overall, row) => {
    overall.sample += row.sample
    overall.bookmakerCorrect += row.bookmakerCorrect
    overall.platformCorrect += row.platformCorrect
    return overall
  }, emptyAccuracyOverallStats())
}

function buildEvilCultAccuracyRows(matches: AnalysisMatch[]): EvilCultAccuracyRow[] {
  const rows = {
    overall: emptyEvilCultAccuracyRow('综合'),
    goal: emptyEvilCultAccuracyRow('大小球'),
    total: emptyEvilCultAccuracyRow('球数'),
    score: emptyEvilCultAccuracyRow('比分'),
    outcome: emptyEvilCultAccuracyRow('胜平负'),
  }

  matches.forEach((item) => {
    const prediction = evilCultPrediction(item)
    const actualOutcome = actualMatchOutcome(item)
    const actualTotal = actualGoalTotal(item)
    const checks = [
      {
        row: rows.goal,
        under: evilCultGoalCorrect(item, 'under', prediction.underGoalLine),
        over: evilCultGoalCorrect(item, 'over', prediction.overGoalLine),
      },
      {
        row: rows.total,
        under: actualTotal === prediction.underTotalValue,
        over: actualTotal === prediction.overTotalValue,
      },
      {
        row: rows.score,
        under: scorePredictionCorrect(item, prediction.underScore),
        over: scorePredictionCorrect(item, prediction.overScore),
      },
      {
        row: rows.outcome,
        under: actualOutcome !== null && actualOutcome === prediction.underOutcome,
        over: actualOutcome !== null && actualOutcome === prediction.overOutcome,
      },
    ]

    checks.forEach(({ row, under, over }) => {
      const main = prediction.goalDirection === 'under' ? under : over
      const reverse = prediction.goalDirection === 'under' ? over : under
      addEvilCultAccuracy(row, under, over, main, reverse)
      addEvilCultAccuracy(rows.overall, under, over, main, reverse)
    })
  })

  return [rows.overall, rows.goal, rows.total, rows.score, rows.outcome]
}

function addEvilCultAccuracy(row: EvilCultAccuracyRow, under: boolean, over: boolean, main: boolean, reverse: boolean) {
  row.sample += 1
  if (under) row.underCorrect += 1
  if (over) row.overCorrect += 1
  if (main) row.mainCorrect += 1
  if (reverse) row.reverseCorrect += 1
}

function evilCultGoalCorrect(item: AnalysisMatch, direction: 'over' | 'under', line: number): boolean {
  const total = actualGoalTotal(item)
  if (!Number.isFinite(total) || !Number.isFinite(line)) return false
  if (direction === 'over') return total > line
  return total < line
}

function buildAccuracyRow(
  label: string,
  matches: AnalysisMatch[],
  judge: (item: AnalysisMatch, bookmaker: GuidePrediction, platform: GuidePrediction) => { bookmaker: boolean; platform: boolean },
  eitherAsBoth = false,
): AccuracyStatsRow {
  return matches.reduce<AccuracyStatsRow>((row, item) => {
    const bookmaker = bookmakerGuidePrediction(item)
    const platform = platformLivePrediction(item)
    const result = judge(item, bookmaker, platform)
    row.sample += 1
    if (result.bookmaker) row.bookmakerCorrect += 1
    if (result.platform) row.platformCorrect += 1
    if (eitherAsBoth ? (result.bookmaker || result.platform) : (result.bookmaker && result.platform)) row.bothCorrect += 1
    return row
  }, emptyAccuracyRow(label))
}

function buildAccuracyCommonRows(matches: AnalysisMatch[]): AccuracyCommonRow[] {
  return [
    buildAccuracyCommonRow('胜平负双中', matches, (item, bookmaker, platform) => {
      const actual = actualMatchOutcome(item)
      return actual !== null && bookmaker.outcome === actual && platform.outcome === actual
    }, resultCommonElements),
    buildAccuracyCommonRow('大小球双中', matches, (item, bookmaker, platform) => (
      goalPredictionCorrect(item, bookmaker.goal) && goalPredictionCorrect(item, platform.goal)
    ), goalCommonElements),
    buildAccuracyCommonRow('比分命中', matches, (item, bookmaker, platform) => (
      scorePredictionCorrect(item, bookmaker.score) || scorePredictionCorrect(item, platform.score)
    ), scoreCommonElements),
  ]
}

function snapshotCommonRows(snapshot?: AnalysisRuleSnapshot | null): AccuracyCommonRow[] | null {
  const rows = snapshot?.commonRows
  if (!Array.isArray(rows) || !rows.length) return null
  const normalized = rows.map((row) => ({
    label: String(row.label || ''),
    sample: Number.isFinite(row.sample) ? Number(row.sample) : 0,
    rules: Array.isArray(row.rules)
      ? row.rules.map((rule) => ({
        value: String(rule.value || ''),
        sample: Number.isFinite(rule.sample) ? Number(rule.sample) : 0,
        bothCorrect: Number.isFinite(rule.bothCorrect) ? Number(rule.bothCorrect) : 0,
        rate: Number.isFinite(rule.rate) ? Number(rule.rate) : 0,
      })).filter((rule) => rule.value && rule.sample > 0)
      : [],
  })).filter((row) => row.label)
  return normalized.some((row) => row.rules.length) ? normalized : null
}

function buildAccuracyCommonRow(
  label: string,
  matches: AnalysisMatch[],
  predicate: (item: AnalysisMatch, bookmaker: GuidePrediction, platform: GuidePrediction) => boolean,
  extractor: (item: AnalysisMatch, bookmaker: GuidePrediction, platform: GuidePrediction) => string[],
): AccuracyCommonRow {
  const samples = matches.map((item) => ({ item, bookmaker: bookmakerGuidePrediction(item), platform: platformLivePrediction(item) }))
  const ruleMap = new Map<string, AccuracyCommonRule>()
  let bothSample = 0

  samples.forEach(({ item, bookmaker, platform }) => {
    const bothCorrect = predicate(item, bookmaker, platform)
    if (bothCorrect) bothSample += 1
    uniqueStrings(extractor(item, bookmaker, platform)).forEach((value) => {
      const current = ruleMap.get(value) ?? { value, sample: 0, bothCorrect: 0, rate: 0 }
      current.sample += 1
      if (bothCorrect) current.bothCorrect += 1
      current.rate = current.sample ? current.bothCorrect / current.sample : 0
      ruleMap.set(value, current)
    })
  })

  const isScoreRow = label.includes('比分')
  const minSample = isScoreRow ? Math.max(4, Math.ceil(matches.length * 0.04)) : Math.max(2, Math.ceil(matches.length * 0.08))
  const minRate = isScoreRow ? 0.28 : 0.45
  return {
    label,
    sample: bothSample,
    rules: Array.from(ruleMap.values())
      .filter((rule) => rule.sample >= minSample && rule.bothCorrect > 0 && rule.rate >= minRate)
      .sort((a, b) => b.rate - a.rate || b.bothCorrect - a.bothCorrect || b.sample - a.sample || a.value.localeCompare(b.value))
      .slice(0, 8),
  }
}

function uniqueStrings(values: string[]): string[] {
  return Array.from(new Set(values.filter((value) => Boolean(value && value !== '-'))))
}

function buildAccuracyMatchRows(matches: AnalysisMatch[], commonRows: AccuracyCommonRow[], mode: 'upcoming' | 'settledFit' | 'all'): AccuracyMatchRow[] {
  const targetMatches = mode === 'upcoming'
    ? matches.filter((item) => !isSettledMatch(item))
    : mode === 'settledFit'
      ? matches.filter(isSettledMatch)
      : matches
  const rows = targetMatches.map((item) => buildAccuracyMatchRow(item, commonRows, isSettledMatch(item) ? 'settled' : 'upcoming'))

  if (mode === 'settledFit') {
    return rows
      .filter(isSettledFitRow)
      .sort((a, b) => `${b.date} ${b.time}`.localeCompare(`${a.date} ${a.time}`))
      .slice(0, 30)
  }

  return rows
}

function buildAccuracyMatchRow(item: AnalysisMatch, commonRows: AccuracyCommonRow[], mode: 'upcoming' | 'settled'): AccuracyMatchRow {
  const [outcomeRules, goalRules, scoreRules] = orderedAccuracyCommonRows(commonRows)
  const bookmaker = bookmakerGuidePrediction(item)
  const platform = platformLivePrediction(item)
  const matchedOutcomeRules = matchAccuracyRules(outcomeRules, resultCommonElements(item, bookmaker, platform))
  const matchedGoalRules = matchAccuracyRules(goalRules, goalCommonElements(item, bookmaker, platform))
  const matchedScoreRules = matchAccuracyRules(scoreRules, scoreCommonElements(item, bookmaker, platform))
  const outcomeFit = accuracyFitSummary(matchedOutcomeRules)
  const goalFit = accuracyFitSummary(matchedGoalRules)
  const scoreFit = accuracyFitSummary(matchedScoreRules)
  const totalScore = outcomeFit.score * 0.45 + goalFit.score * 0.35 + scoreFit.score * 0.2
  const conclusion = accuracyConclusion(totalScore)
  const resultSummary = mode === 'upcoming'
    ? predictedDoubleHitSummary(outcomeFit, goalFit, scoreFit)
    : settledAccuracySummary(item, bookmaker, platform)
  const evidence = [
    ...matchedOutcomeRules.slice(0, 2),
    ...matchedGoalRules.slice(0, 2),
    ...matchedScoreRules.slice(0, 1),
  ].map(accuracyRuleText).join('；')

  return {
    matchId: item.matchId,
    date: matchDateText(item.date),
    matchTitle: `${item.home} vs ${item.guest}`,
    league: item.league || '-',
    time: formatTime(item.matchTime),
    outcomeFit,
    goalFit,
    scoreFit,
    conclusion: conclusion.label,
    tone: conclusion.tone,
    evidence: evidence || '暂无明显高命中规则匹配，按原预测谨慎处理。',
    resultSummary: resultSummary.label,
    resultTone: resultSummary.tone,
  }
}

function orderedAccuracyCommonRows(rows: AccuracyCommonRow[]): AccuracyCommonRow[] {
  const fallback = emptyAccuracyCommonRows()
  return [
    rows.find((row) => row.label.includes('胜平负')) || rows[0] || fallback[0],
    rows.find((row) => row.label.includes('大小球')) || rows[1] || fallback[1],
    rows.find((row) => row.label.includes('比分')) || rows[2] || fallback[2],
  ]
}

function accuracyMatchRow(item: AnalysisMatch): AccuracyMatchRow | null {
  return accuracyMatchRowsById.value.get(item.matchId) ?? null
}

function isSettledFitRow(row: AccuracyMatchRow): boolean {
  const hasRuleMatch = row.tone === 'green' || isPredictableDoubleFit(row.outcomeFit) || isPredictableDoubleFit(row.goalFit) || isPredictableDoubleFit(row.scoreFit)
  return hasRuleMatch && row.resultTone === 'green'
}

function predictedDoubleHitSummary(outcomeFit: AccuracyFitSummary, goalFit: AccuracyFitSummary, scoreFit: AccuracyFitSummary): { label: string; tone: StatRow['tone'] } {
  const doubleValues = [
    isPredictableDoubleFit(outcomeFit) ? '胜平负' : '',
    isPredictableDoubleFit(goalFit) ? '大小球' : '',
  ].filter(Boolean)
  const scoreHit = isPredictableDoubleFit(scoreFit)
  if (!doubleValues.length && !scoreHit) return { label: '暂无命中预测', tone: 'normal' }
  const strongCount = [outcomeFit, goalFit, scoreFit].filter((fit) => fit.score >= 78).length
  const values = [
    doubleValues.length ? `预测双中 ${doubleValues.join('/')}` : '',
    scoreHit ? '预测比分命中' : '',
  ].filter(Boolean)
  return { label: values.join(' + '), tone: strongCount ? 'green' : 'blue' }
}

function isPredictableDoubleFit(fit: AccuracyFitSummary): boolean {
  return fit.ruleCount >= 2 && fit.rate >= 0.62 && fit.score >= 70
}

function settledAccuracySummary(item: AnalysisMatch, bookmaker: GuidePrediction, platform: GuidePrediction): { label: string; tone: StatRow['tone'] } {
  if (!isSettledMatch(item)) return { label: '待赛', tone: 'normal' }
  const actual = actualMatchOutcome(item)
  const values = [
    actual !== null && bookmaker.outcome === actual && platform.outcome === actual ? '胜平负' : '',
    goalPredictionCorrect(item, bookmaker.goal) && goalPredictionCorrect(item, platform.goal) ? '大小球' : '',
    scorePredictionCorrect(item, bookmaker.score) || scorePredictionCorrect(item, platform.score) ? '比分命中' : '',
  ].filter(Boolean)
  if (!values.length) return { label: '未命中', tone: 'red' }
  return { label: values.join('/'), tone: 'green' }
}

function matchAccuracyRules(row: AccuracyCommonRow | undefined, elements: string[]): AccuracyCommonRule[] {
  const elementSet = new Set(uniqueStrings(elements))
  return (row?.rules ?? [])
    .filter((rule) => elementSet.has(rule.value))
    .sort((a, b) => b.rate - a.rate || b.bothCorrect - a.bothCorrect || b.sample - a.sample)
}

function accuracyFitSummary(rules: AccuracyCommonRule[]): AccuracyFitSummary {
  if (!rules.length) return { label: '无匹配', tone: 'normal', score: 0, ruleCount: 0, rate: 0, sample: 0 }
  const sample = rules.reduce((sum, rule) => sum + rule.sample, 0)
  const correct = rules.reduce((sum, rule) => sum + rule.bothCorrect, 0)
  const rate = sample ? correct / sample : 0
  const score = Math.min(100, Math.round(rate * 100 + Math.min(18, rules.length * 4)))
  const label = `${rules.length}条 ${Math.round(rate * 100)}%`
  return { label, tone: accuracyScoreTone(score), score, ruleCount: rules.length, rate, sample }
}

function accuracyScoreTone(score: number): StatRow['tone'] {
  if (score >= 78) return 'green'
  if (score >= 58) return 'blue'
  if (score > 0) return 'red'
  return 'normal'
}

function accuracyConclusion(score: number): { label: string; tone: StatRow['tone'] } {
  if (score >= 78) return { label: '符合历史规律', tone: 'green' }
  if (score >= 58) return { label: '部分符合', tone: 'blue' }
  if (score > 0) return { label: '匹配偏弱', tone: 'red' }
  return { label: '无历史支撑', tone: 'normal' }
}

function accuracyRuleText(rule: AccuracyCommonRule): string {
  return `${rule.value} ${rule.bothCorrect}/${rule.sample} ${Math.round(rule.rate * 100)}%`
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

function resultCommonElements(item: AnalysisMatch, bookmaker: GuidePrediction, platform: GuidePrediction): string[] {
  const sportteryComfort = marketComfortOutcome(item, 'sporttery')
  const rqComfort = marketComfortOutcome(item, 'sportteryRqspf')
  const professionalConsensus = professionalConsensusOutcome(item)
  const drawRisk = drawRiskSignal(item)
  return [
    `庄家${outcomeShortLabel(bookmaker.outcome)}`,
    `平台${outcomeShortLabel(platform.outcome)}`,
    bookmaker.outcome === platform.outcome ? `庄平同向${outcomeShortLabel(bookmaker.outcome)}` : '庄平分歧',
    professionalConsensus ? `凯体同向${outcomeShortLabel(professionalConsensus)}` : '',
    `凯利${joinText(item.kailiresult)}`,
    `体彩${joinText(item.ticairesult)}`,
    `亚盘${handicapBucket(item.yapanpankou2)}`,
    `让球热度${heatBucket(item.yapantouzhu?.[0], item.yapantouzhu?.[1], '主热', '客热')}`,
    sportteryComfort ? `竞彩舒服${outcomeShortLabel(sportteryComfort)}` : '',
    rqComfort ? `让球舒服${outcomeShortLabel(rqComfort)}` : '',
    platformSignalWarning(item, platform.outcome).value ? '平台过热' : '',
    handicapPressureSignalLabel(item) ? `让球${handicapPressureSignalLabel(item)}` : '',
    drawRisk.score >= 4 ? '平局风险高' : '',
    drawRisk.score >= 5 ? '平局风险强' : '',
  ]
}

function goalCommonElements(item: AnalysisMatch, bookmaker: GuidePrediction, platform: GuidePrediction): string[] {
  const signal = goalBalanceSignalForItem(item)
  return [
    `庄家${goalDirectionLabel(bookmaker.goal)}`,
    `平台${goalDirectionLabel(platform.goal)}`,
    goalDirectionLabel(bookmaker.goal) === goalDirectionLabel(platform.goal) ? `庄平同向${goalDirectionLabel(bookmaker.goal)}` : '庄平球数分歧',
    `盘口${goalLineBucket(item.qiushupankou2)}`,
    `大小热度${heatBucket(item.qiushutouzhu?.[0], item.qiushutouzhu?.[1], '大热', '小热')}`,
    signal ? `回归${goalBalanceSignalLabel(signal)}` : '',
  ]
}

function scoreCommonElements(item: AnalysisMatch, bookmaker: GuidePrediction, platform: GuidePrediction): string[] {
  const bookmakerShape = scoreShapeLabel(bookmaker.score)
  const platformShape = scoreShapeLabel(platform.score)
  return [
    `庄家${bookmaker.score}`,
    `平台${platform.score}`,
    bookmaker.score === platform.score ? `庄平同比分${bookmaker.score}` : '庄平比分分歧',
    bookmakerShape ? `庄家形态${bookmakerShape}` : '',
    platformShape ? `平台形态${platformShape}` : '',
    bookmakerShape && platformShape && bookmakerShape === platformShape ? `庄平同形态${bookmakerShape}` : '',
    `庄家赛果${outcomeShortLabel(bookmaker.outcome)}`,
    `平台赛果${outcomeShortLabel(platform.outcome)}`,
    `庄家球数${goalDirectionLabel(bookmaker.goal)}`,
    `平台球数${goalDirectionLabel(platform.goal)}`,
    `亚盘${handicapBucket(item.yapanpankou2)}`,
    `大小盘口${goalLineBucket(item.qiushupankou2)}`,
  ]
}

function setDate(value: string) {
  selectedDate.value = validDateString(value) || todayString
  loadData()
  if (viewMode.value === 'stats') void loadAccuracyStats()
}

function shiftDate(days: number) {
  const date = parseLocalDate(selectedDate.value) || new Date()
  date.setDate(date.getDate() + days)
  setDate(localDateString(date))
}

function accuracyFixedRuleDateRange(fallbackEndDate = todayString): string[] {
  return accuracyHistoryDateRange(validDateString(todayString) || fallbackEndDate)
}

function accuracyHistoryDateRange(endDateValue: string): string[] {
  const endDate = parseLocalDate(endDateValue) || new Date()
  const startDate = parseLocalDate(accuracyHistoryStartDate) || endDate
  const safeStart = startDate.getTime() <= endDate.getTime() ? startDate : endDate
  const days = Math.max(1, Math.floor((endDate.getTime() - safeStart.getTime()) / 86400000) + 1)
  return recentDateRange(localDateString(endDate), days)
}

function recentDateRange(endDateValue: string, days: number): string[] {
  const endDate = parseLocalDate(endDateValue) || new Date()
  return Array.from({ length: days }, (_, index) => {
    const date = new Date(endDate)
    date.setDate(endDate.getDate() - (days - 1 - index))
    return localDateString(date)
  })
}

function isSettledMatch(item: AnalysisMatch): boolean {
  const stateText = String(item.displayState || '')
  return stateText.includes('完') || item.status >= 4
}

function actualMatchOutcome(item: AnalysisMatch): DirectionOutcome | null {
  if (!Number.isFinite(item.homeScore) || !Number.isFinite(item.guestScore)) return null
  if (item.homeScore > item.guestScore) return 'home'
  if (item.homeScore < item.guestScore) return 'away'
  return 'draw'
}

function goalPredictionCorrect(item: AnalysisMatch, goal: GuidePrediction['goal']): boolean {
  const total = actualGoalTotal(item)
  if (!Number.isFinite(total) || !Number.isFinite(goal.total)) return false
  const label = String(goal.label || '')
  if (label.includes('以内')) return total <= goal.total
  if (label.includes('以上')) return total >= goal.total
  const range = label.match(/(\d+)\s*-\s*(\d+)球/)
  if (range) {
    const low = Number.parseInt(range[1], 10)
    const high = Number.parseInt(range[2], 10)
    return total >= low && total <= high
  }
  return total === Math.round(goal.total)
}

function scorePredictionCorrect(item: AnalysisMatch, score: string): boolean {
  const parsed = parseScoreText(score)
  if (!parsed) return false
  return parsed.home === item.homeScore && parsed.guest === item.guestScore
}

function actualGoalTotal(item: AnalysisMatch): number {
  return Number(item.homeScore || 0) + Number(item.guestScore || 0)
}

function matchDateText(value: string): string {
  const normalized = validDateString(String(value || '').slice(0, 10))
  return normalized || '-'
}

function parseScoreText(score: string): { home: number; guest: number } | null {
  const match = String(score || '').match(/^(\d+):(\d+)$/)
  if (!match) return null
  return {
    home: Number.parseInt(match[1], 10),
    guest: Number.parseInt(match[2], 10),
  }
}

function accuracyRateText(correct: number, sample: number): string {
  if (!sample) return '-'
  return `${correct}/${sample} ${Math.round((correct / sample) * 100)}%`
}

function outcomeShortLabel(outcome: DirectionOutcome | null): string {
  if (outcome === 'home') return '主胜'
  if (outcome === 'away') return '客胜'
  if (outcome === 'draw') return '平局'
  return '-'
}

function handicapBucket(value: unknown): string {
  const line = parseOptionalNumber(value)
  if (!Number.isFinite(line) || Math.abs(line) < 0.25) return '平浅'
  const side = line > 0 ? '主让' : '客让'
  const abs = Math.abs(line)
  if (abs >= 1) return `${side}深`
  if (abs >= 0.5) return `${side}中`
  return `${side}浅`
}

function goalLineBucket(value: unknown): string {
  const line = parseOptionalNumber(value)
  if (!Number.isFinite(line)) return '-'
  if (line <= 2.25) return '低盘'
  if (line >= 2.75) return '高盘'
  return '中盘'
}

function heatBucket(leftValue: unknown, rightValue: unknown, leftHot: string, rightHot: string): string {
  const left = parseOptionalNumber(leftValue)
  const right = parseOptionalNumber(rightValue)
  if (!Number.isFinite(left) || !Number.isFinite(right)) return '-'
  if (left > 65) return leftHot
  if (right > 65) return rightHot
  if (left - right >= 10) return leftHot
  if (right - left >= 10) return rightHot
  return '均衡'
}

function goalDirectionLabel(goal: GuidePrediction['goal']): string {
  const label = String(goal.label || '')
  if (label.includes('以上')) return '大球'
  if (label.includes('以内')) return '小球'
  return '盘口球'
}

function goalBalanceSignalLabel(signal: ReturnType<typeof goalBalanceSignalForItem>): string {
  if (signal === 'underHidden') return '小球隐藏'
  if (signal === 'under') return '小球'
  if (signal === 'overCorrected') return '大球修正'
  if (signal === 'over') return '大球'
  return '-'
}

function syncDateQuery() {
  const current = String(route.query.date || '')
  if (current === selectedDate.value) return
  router.replace({ query: { ...route.query, date: selectedDate.value } })
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
  const [historyGoals, recentGoals] = splitPair(item.changguiqiushu)
  const historyGoalValue = historyGoalSampleValue(item, historyGoals)
  const recentGoalValue = recentGoalSampleValue(item, recentGoals)
  const combinedGoalValue = combinedGoalAverageValue(historyGoalValue, recentGoalValue)
  const goalAdviceLine = buildGoalAdviceLine(item, historyGoalValue, recentGoalValue, combinedGoalValue)
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

function buildGoalAdviceLine(item: AnalysisMatch, historyGoals: unknown, recentGoals: unknown, combinedGoals: unknown): string {
  const expectedGoals = expectedGoalPair(item)
  const alerts = goalBalanceAlertRows(historyGoals, recentGoals, combinedGoals, item.qiushupankou1)
  const alertText = alerts
    .filter((row) => row.label !== '2.5均衡值')
    .map((row) => `${row.label}：${row.value}`)
    .join('；')
  const balanceRow = alerts.find((row) => row.label === '2.5均衡值')
  const balanceText = balanceRow ? `${balanceRow.label}：${balanceRow.value}；` : ''
  const zeroRiskText = zeroGoalAdviceText(item, expectedGoals)
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

function scoreTriplet(scores: DirectionValues): string {
  return `主${scoreText(scores.home)} 平${scoreText(scores.draw)} 客${scoreText(scores.away)}`
}

function bookmakerMarkets(item: AnalysisMatch): BookmakerMarket[] {
  return (item.roiSimulation?.markets ?? []).filter((market) => market.key === 'sporttery' || market.key === 'sportteryRqspf')
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
  const odds = parseOddsDistribution(item.detail.test8)
  const retailDistribution = parseRetailDistribution(item.sanhuxinli?.slice(0, 3))
  if (!odds || !retailDistribution) return null

  return {
    key: 'localAverageOdds',
    name: '本地测算',
    odds,
    oddsAvailable: true,
    retailDistribution,
    bookmakerByOutcome: buildLocalProfitRows(odds, retailDistribution, localStakeBase(item)),
  }
}

function parseOddsDistribution(values: unknown[] | undefined): DirectionValues | null {
  if (!values || values.length < 3) return null
  const home = parseNumericValue(values[0])
  const draw = parseNumericValue(values[1])
  const away = parseNumericValue(values[2])
  if (home <= 0 || draw <= 0 || away <= 0) return null
  return { home, draw, away }
}

function parseRetailDistribution(values: unknown[] | undefined): DirectionValues | null {
  if (!values || values.length < 3) return null
  const home = parseNumericValue(values[0])
  const draw = parseNumericValue(values[1])
  const away = parseNumericValue(values[2])
  const total = home + draw + away
  if (total <= 0) return null
  return {
    home: roundValue(home / total * 100),
    draw: roundValue(draw / total * 100),
    away: roundValue(away / total * 100),
  }
}

function buildLocalProfitRows(odds: DirectionValues, retailDistribution: DirectionValues, totalStake: number): BookmakerOutcome[] {
  const definitions: Array<{ outcome: BookmakerOutcome['outcome']; outcomeLabel: string; odds: number; share: number }> = [
    { outcome: 'home', outcomeLabel: '主胜打出', odds: odds.home, share: retailDistribution.home },
    { outcome: 'draw', outcomeLabel: '平局打出', odds: odds.draw, share: retailDistribution.draw },
    { outcome: 'away', outcomeLabel: '客胜打出', odds: odds.away, share: retailDistribution.away },
  ]

  return definitions.map((definition) => {
    const betStake = roundValue(totalStake * definition.share / 100)
    const payout = roundValue(betStake * definition.odds)
    const bookmakerProfit = roundValue(totalStake - payout)
    return {
      outcome: definition.outcome,
      outcomeLabel: definition.outcomeLabel,
      retailShare: roundValue(definition.share),
      betStake,
      totalStake,
      odds: roundValue(definition.odds),
      payout,
      bookmakerProfit,
      bookmakerLoss: roundValue(payout - totalStake),
      bookmakerRoi: roundValue(bookmakerProfit / totalStake * 100),
      bookmakerOutcome: bookmakerOutcomeLabel(bookmakerProfit),
      available: true,
    }
  })
}

function parseNumericValue(value: unknown): number {
  if (typeof value === 'number') return Number.isFinite(value) ? value : 0
  const text = String(value || '').trim().replace(/%$/, '')
  const numeric = Number.parseFloat(text)
  return Number.isFinite(numeric) ? numeric : 0
}

function parseOptionalNumber(value: unknown): number {
  if (typeof value === 'number') return Number.isFinite(value) ? value : Number.NaN
  const text = String(value ?? '').trim().replace(/%$/, '')
  if (!text || text === '-') return Number.NaN
  const numeric = Number.parseFloat(text)
  return Number.isFinite(numeric) ? numeric : Number.NaN
}

function roundValue(value: number, fractionDigits = 2): number {
  const factor = 10 ** fractionDigits
  return Math.round(value * factor) / factor
}

function bookmakerOutcomeLabel(value: number): string {
  if (value > 0) return '庄家盈利'
  if (value < 0) return '庄家亏损'
  return '庄家持平'
}

function sportteryMarket(item: AnalysisMatch): BookmakerMarket | undefined {
  return bookmakerMarkets(item).find((market) => market.key === 'sporttery')
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

function bookmakerResultOutcome(item: AnalysisMatch): DirectionOutcome {
  return outcomeFromScores(bookmakerCorrectionScores(item))
}

function bookmakerCorrectionScores(item: AnalysisMatch): Record<DirectionOutcome, number> {
  const scores: Record<DirectionOutcome, number> = { home: 0, draw: 0, away: 0 }
  const add = (outcome: DirectionOutcome | null, weight: number) => {
    if (outcome) scores[outcome] += weight
  }

  const oddsProbabilities = bookmakerOddsProbabilities(item)
  const handicapOutcome = handicapGuideOutcome(item)
  const oddsOutcome = lowestOddsOutcome(item)
  const drawScore = bookmakerDrawScore(item)
  const currentHandicap = parseOptionalNumber(item.yapanpankou2)
  const strongHandicap = Number.isFinite(currentHandicap) && Math.abs(currentHandicap) >= 0.75

  if (oddsProbabilities) {
    scores.home += oddsProbabilities.home * 7
    scores.draw += oddsProbabilities.draw * 7
    scores.away += oddsProbabilities.away * 7
  }
  if (handicapOutcome && oddsOutcome && handicapOutcome === oddsOutcome) add(handicapOutcome, 1.6)
  add(handicapOutcome, strongHandicap ? 1.6 : 0.9)
  if (!oddsProbabilities) add(oddsOutcome, oddsOutcome === 'draw' ? 2.2 : 1.8)
  if (drawScore >= 4) add('draw', 1.8)
  else if (drawScore >= 3 && !strongHandicap) add('draw', 1.2)
  else if (drawScore >= 2 && !handicapOutcome) add('draw', 0.8)
  return scores
}

function bookmakerOddsDistribution(item: AnalysisMatch): DirectionValues | null {
  return parseOddsDistribution(item.sportteryOdds) || parseOddsDistribution(item.detail.test8)
}

function bookmakerOddsProbabilities(item: AnalysisMatch): DirectionValues | null {
  const odds = bookmakerOddsDistribution(item)
  if (!odds) return null
  const home = 1 / odds.home
  const draw = 1 / odds.draw
  const away = 1 / odds.away
  const total = home + draw + away
  if (total <= 0) return null
  return { home: home / total, draw: draw / total, away: away / total }
}

function bookmakerGoalResult(item: AnalysisMatch): { label: string; total: number; tone: StatRow['tone'] } {
  const openingLine = parseOptionalNumber(item.qiushupankou1)
  const currentLine = parseOptionalNumber(item.qiushupankou2)
  const line = Number.isFinite(currentLine) ? currentLine : openingLine
  const delta = currentLine - openingLine

  let lineResult: { label: string; total: number; tone: StatRow['tone'] }
  if (Number.isFinite(delta) && delta >= 0.25) {
    const total = Number.isFinite(line) ? Math.max(3, Math.floor(line + 1.5)) : 3
    lineResult = { label: `${total}球以上`, total, tone: 'green' }
  } else if (Number.isFinite(delta) && delta <= -0.25) {
    const total = Number.isFinite(line) ? Math.max(0, Math.ceil(line) - 1) : 2
    lineResult = { label: `${total}球以内`, total, tone: 'red' }
  } else if (Number.isFinite(line) && line >= 2.75) {
    lineResult = { label: '3球左右', total: 3, tone: 'green' }
  } else if (Number.isFinite(line) && line <= 2.25) {
    lineResult = { label: '2球以内', total: 2, tone: 'red' }
  } else {
    lineResult = { label: '2-3球', total: 2, tone: 'normal' }
  }

  return lineResult
}

function bookmakerFusedScore(item: AnalysisMatch, outcome: DirectionOutcome, total: number): string {
  const normalizedTotal = Math.max(0, Math.round(total))
  if (normalizedTotal <= 0) return '0:0'

  const handicap = parseOptionalNumber(item.yapanpankou2)
  const openingHandicap = parseOptionalNumber(item.yapanpankou1)
  const handicapLine = Number.isFinite(handicap) ? handicap : openingHandicap
  const favorite = Number.isFinite(handicapLine)
    ? (handicapLine > 0 ? 'home' : handicapLine < 0 ? 'away' : outcome)
    : outcome
  const favoriteGoals = Math.max(1, Math.ceil((normalizedTotal + 1) / 2))
  const underdogGoals = Math.max(0, normalizedTotal - favoriteGoals)
  const home = favorite === 'home' ? favoriteGoals : underdogGoals
  const guest = favorite === 'home' ? underdogGoals : favoriteGoals
  return normalizeScoreByOutcome(home, guest, outcome, normalizedTotal)
}

function platformFusedScore(item: AnalysisMatch, outcome: DirectionOutcome, total: number): string {
  const normalizedTotal = Math.max(0, Math.round(total))
  if (normalizedTotal <= 0) return '0:0'
  const goals = allocateIntegerGoalTotal(normalizedTotal, expectedGoalPair(item))
  return normalizeScoreByOutcome(Math.round(goals.home), Math.round(goals.guest), outcome, normalizedTotal)
}

function secondaryGuideScore(item: AnalysisMatch, outcome: DirectionOutcome, goal: GuidePrediction['goal'], source: 'bookmaker' | 'platform'): string {
  const bands = expectedGoalBands(item)
  const band = secondaryGoalBand(item, goal)
  const goals = band === 'over' ? bands.over : bands.under
  const total = Math.max(0, Math.round(goals.home + goals.guest))
  if (!Number.isFinite(goals.home) || !Number.isFinite(goals.guest) || !Number.isFinite(total)) return '-'
  if (source === 'bookmaker') return bookmakerFusedScore(item, outcome, total)
  return normalizeScoreByOutcome(Math.round(goals.home), Math.round(goals.guest), outcome, total)
}

function secondaryGoalBand(item: AnalysisMatch, goal: GuidePrediction['goal']): 'under' | 'over' {
  if (goal.label.includes('以内')) return 'over'
  if (goal.label.includes('以上')) return 'under'
  const line = parseOptionalNumber(item.qiushupankou2)
  if (Number.isFinite(line)) return goal.total <= line ? 'over' : 'under'
  return 'under'
}

function normalizeScoreByOutcome(homeGoals: number, guestGoals: number, outcome: DirectionOutcome, total: number): string {
  let home = Math.max(0, homeGoals)
  let guest = Math.max(0, guestGoals)
  const normalizedTotal = Math.max(0, Math.round(total))

  if (outcome === 'draw') {
    const side = Math.max(0, Math.floor(normalizedTotal / 2))
    return `${side}:${side}`
  }

  const winner = outcome === 'home' ? 'home' : 'guest'
  const currentWinnerGoals = winner === 'home' ? home : guest
  const currentLoserGoals = winner === 'home' ? guest : home
  if (currentWinnerGoals > currentLoserGoals) return `${home}:${guest}`

  const winnerGoals = Math.max(1, Math.ceil((normalizedTotal + 1) / 2))
  const loserGoals = Math.max(0, normalizedTotal - winnerGoals)
  if (winner === 'home') {
    home = winnerGoals
    guest = loserGoals
  } else {
    home = loserGoals
    guest = winnerGoals
  }
  return `${home}:${guest}`
}

function handicapGuideOutcome(item: AnalysisMatch): DirectionOutcome | null {
  const openingLine = parseOptionalNumber(item.yapanpankou1)
  const currentLine = parseOptionalNumber(item.yapanpankou2)
  const delta = currentLine - openingLine
  if (Number.isFinite(delta) && delta >= 0.25) return 'home'
  if (Number.isFinite(delta) && delta <= -0.25) return 'away'
  if (Number.isFinite(currentLine)) {
    if (currentLine >= 0.25) return 'home'
    if (currentLine <= -0.25) return 'away'
  }
  return null
}

function bookmakerDrawScore(item: AnalysisMatch): number {
  let score = 0
  const openingHandicap = parseOptionalNumber(item.yapanpankou1)
  const currentHandicap = parseOptionalNumber(item.yapanpankou2)
  if (Number.isFinite(currentHandicap)) {
    if (Math.abs(currentHandicap) <= 0.25) score += 2
    if (Number.isFinite(openingHandicap) && Math.abs(currentHandicap) < Math.abs(openingHandicap)) score += 1
  }

  const odds = bookmakerOddsDistribution(item)
  if (odds) {
    const homeImplied = 1 / odds.home
    const drawImplied = 1 / odds.draw
    const awayImplied = 1 / odds.away
    const totalImplied = homeImplied + drawImplied + awayImplied
    const drawProbability = totalImplied > 0 ? drawImplied / totalImplied * 100 : 0
    const sideProbability = totalImplied > 0 ? Math.max(homeImplied, awayImplied) / totalImplied * 100 : 0
    if (drawProbability >= sideProbability - 6) score += 2
    else if (drawProbability >= sideProbability - 10) score += 1
    if (odds.draw <= Math.min(odds.home, odds.away) * 1.35) score += 1
  }

  const openingGoalLine = parseOptionalNumber(item.qiushupankou1)
  const currentGoalLine = parseOptionalNumber(item.qiushupankou2)
  if (Number.isFinite(currentGoalLine)) {
    if (currentGoalLine <= 2.25) score += 1
    if (Number.isFinite(openingGoalLine) && currentGoalLine < openingGoalLine) score += 1
  }
  return score
}

function drawRiskSignal(item: AnalysisMatch): { score: number; reasons: string[] } {
  let score = 0
  const reasons: string[] = []
  const add = (condition: boolean, weight: number, reason: string) => {
    if (!condition) return
    score += weight
    reasons.push(reason)
  }

  const openingHandicap = parseOptionalNumber(item.yapanpankou1)
  const currentHandicap = parseOptionalNumber(item.yapanpankou2)
  const absHandicap = Math.abs(currentHandicap)
  add(Number.isFinite(currentHandicap) && absHandicap <= 0.25, 2, '亚盘浅')
  add(Number.isFinite(currentHandicap) && absHandicap > 0.25 && absHandicap <= 0.5, 1, '受让/让球浅盘')
  add(Number.isFinite(openingHandicap) && Number.isFinite(currentHandicap) && Math.abs(openingHandicap) - Math.abs(currentHandicap) >= 0.25, 1.5, '盘口变浅')

  const odds = bookmakerOddsDistribution(item)
  if (odds) {
    const homeImplied = 1 / odds.home
    const drawImplied = 1 / odds.draw
    const awayImplied = 1 / odds.away
    const totalImplied = homeImplied + drawImplied + awayImplied
    const drawProbability = totalImplied > 0 ? drawImplied / totalImplied * 100 : 0
    const sideProbability = totalImplied > 0 ? Math.max(homeImplied, awayImplied) / totalImplied * 100 : 0
    add(odds.draw <= 3.45 && drawProbability >= sideProbability - 10, 1.5, '平赔不高')
    add(odds.draw <= Math.min(odds.home, odds.away) * 1.35, 1, '平赔差距小')
  } else if (bookmakerDrawScore(item) >= 3) {
    add(true, 1, '欧赔平局支撑')
  }

  const goalLine = parseOptionalNumber(item.qiushupankou2)
  add(Number.isFinite(goalLine) && goalLine <= 2.5, 1, '大小球低盘')

  const bookmaker = bookmakerResultOutcome(item)
  const base = basePredictionOutcome(item)
  add(Boolean(bookmaker && base && bookmaker !== base && bookmaker !== 'draw' && base !== 'draw'), 1, '庄平胜负分歧')
  add(Boolean(professionalConflictWarning(item)), 1, '凯利/体彩反差')

  const goals = expectedGoalPair(item)
  const expectedTotal = goals.home + goals.guest
  add(Number.isFinite(expectedTotal) && expectedTotal > 0 && expectedTotal <= 2.5 && goals.home <= 1.5 && goals.guest <= 1.5, 1, '近期进丢球一般')

  const historyOutcome = historyMatchOutcome(item)
  add(historyOutcome === 'draw', 1.5, '历史有平局')
  add(historySmallScore(item), 1, '历史小比分')

  return { score, reasons: uniqueStrings(reasons) }
}

function historySmallScore(item: AnalysisMatch): boolean {
  const record = item.liangduibisai
  if (!record?.length) return false
  const homeScore = parseOptionalNumber(record[3])
  const guestScore = parseOptionalNumber(record[4])
  return Number.isFinite(homeScore) && Number.isFinite(guestScore) && homeScore + guestScore <= 2
}

function lowestOddsOutcome(item: AnalysisMatch): DirectionOutcome | null {
  const odds = bookmakerOddsDistribution(item)
  if (!odds) return null
  const candidates: Array<{ outcome: DirectionOutcome; value: number }> = [
    { outcome: 'home', value: odds.home },
    { outcome: 'draw', value: odds.draw },
    { outcome: 'away', value: odds.away },
  ]
  const rows = candidates.filter((row) => Number.isFinite(row.value) && row.value > 0)
  return rows.sort((a, b) => a.value - b.value)[0]?.outcome ?? null
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

function guideWarningRows(item: AnalysisMatch): GuideWarningRow[] {
  const platform = platformLivePrediction(item)
  const professionalWarning = professionalConflictWarning(item)
  const warnings: GuideWarningRow[] = [
    ...warningRowsFromText(professionalWarning ? professionalWarning.value : '', 'red'),
    ...warningRowsFromText(heatWarningSummary(item), 'red'),
    ...profitAlignmentWarningRows(item),
    ...platformIntegratedWarningRows(item),
    ...warningRowsFromText(platform.warning ? `平台${platform.warning}` : '', 'red'),
  ]
  const seen = new Set<string>()
  return warnings.filter((warning) => {
    if (seen.has(warning.value)) return false
    seen.add(warning.value)
    return true
  })
}

function warningRowsFromText(value: string, tone: StatRow['tone']): GuideWarningRow[] {
  return value ? [{ value: `警示：${value}`, tone }] : []
}

function guideWarningClass(tone: StatRow['tone']): string {
  if (tone === 'green') return 'text-emerald-600'
  if (tone === 'blue') return 'text-sky-600'
  return 'text-red-600'
}

function heatCellTone(...values: unknown[]): StatRow['tone'] {
  return values.some((value) => parseOptionalNumber(value) > 65) ? 'red' : 'normal'
}

function heatWarningSummary(item: AnalysisMatch): string {
  const warnings: string[] = []
  const addHeatWarning = (category: string, name: string, value: unknown) => {
    if (parseOptionalNumber(value) > 65) warnings.push(`${category}${name}${percentText(value)}`)
  }

  addHeatWarning('让球', item.home, item.yapantouzhu?.[0])
  addHeatWarning('让球', item.guest, item.yapantouzhu?.[1])
  addHeatWarning('大小球', '大球', item.qiushutouzhu?.[0])
  addHeatWarning('大小球', '小球', item.qiushutouzhu?.[1])

  return warnings.length ? `投注热度过热：${warnings.join('，')}` : ''
}

function profitAlignmentWarningRows(item: AnalysisMatch): GuideWarningRow[] {
  const local = strongLocalComfortRow(item)
  const sporttery = strongMarketComfortRow(item, 'sporttery')
  const rq = strongMarketComfortRow(item, 'sportteryRqspf')
  const sportteryLoss = strongMarketLossRow(item, 'sporttery')
  const rqLoss = strongMarketLossRow(item, 'sportteryRqspf')
  const warnings: GuideWarningRow[] = []

  if (sporttery && rq && sporttery.outcome === rq.outcome) {
    warnings.push({
      value: `警示：交易盈亏同向：胜平负${outcomeName(sporttery)}${signedMoneyText(sporttery.bookmakerProfit, sporttery.available)}、让球${outcomeName(rq)}${signedMoneyText(rq.bookmakerProfit, rq.available)}，均为庄家舒服项`,
      tone: 'red',
    })
  }

  if (sportteryLoss?.outcome === 'away' && rqLoss?.outcome === 'away') {
    warnings.push({ value: '警示：庄家同向亏损：胜平负负、让球负均为最大亏损项', tone: 'green' })
  }

  if (local) {
    const alignedMarkets = [
      sporttery && sporttery.outcome === local.outcome ? '竞彩' : '',
      rq && rq.outcome === local.outcome ? '让球' : '',
    ].filter(Boolean)
    if (alignedMarkets.length) {
      warnings.push({ value: `警示：本地盈亏同向：本地${outcomeName(local)}与${alignedMarkets.join('、')}一致`, tone: 'red' })
    }
  }

  return warnings
}

function platformIntegratedWarningRows(item: AnalysisMatch): GuideWarningRow[] {
  const rows: GuideWarningRow[] = []
  const handicapLabel = handicapPressureSignalLabel(item)
  const goalSignal = goalBalanceSignalForItem(item)
  const overheat = platformOverheatOutcome(item)
  const hotOutcome = handicapHotOutcome(item)
  const sportteryLoss = strongMarketLossRow(item, 'sporttery')
  const rqLoss = strongMarketLossRow(item, 'sportteryRqspf')
  const professionalWarning = professionalConflictWarning(item)
  const drawRisk = drawRiskSignal(item)

  if (handicapLabel) {
    rows.push({ value: `警示：平台已纳入让球修正：${handicapLabel}`, tone: 'blue' })
  }
  if (hotOutcome) {
    rows.push({ value: `警示：平台已纳入让球热度修正：${outcomeShortLabel(hotOutcome)}过热`, tone: 'blue' })
  }
  if (overheat) {
    rows.push({ value: `警示：平台已纳入过热修正：${outcomeShortLabel(overheat)}信号过热`, tone: 'blue' })
  }
  if (sportteryLoss && rqLoss && sportteryLoss.outcome === rqLoss.outcome) {
    rows.push({ value: `警示：平台已纳入庄家同向亏损修正：${outcomeShortLabel(sportteryLoss.outcome)}`, tone: 'green' })
  }
  if (professionalWarning) {
    rows.push({ value: '警示：平台已纳入凯利/体彩反差修正', tone: 'blue' })
  }
  if (drawRisk.score >= 4) {
    rows.push({ value: `警示：平台已纳入平局风险：${drawRisk.reasons.join('，')}`, tone: drawRisk.score >= 5 ? 'red' : 'blue' })
  }
  if (goalSignal === 'underHidden') {
    rows.push({ value: '警示：平台已纳入大小球修正：回归小球 + 盘口隐藏', tone: 'red' })
  } else if (goalSignal === 'under') {
    rows.push({ value: '警示：平台已纳入大小球修正：回归小球', tone: 'red' })
  } else if (goalSignal === 'overCorrected') {
    rows.push({ value: '警示：平台已纳入大小球修正：回归大球 + 盘口修正', tone: 'green' })
  } else if (goalSignal === 'over') {
    rows.push({ value: '警示：平台已纳入大小球修正：回归大球', tone: 'green' })
  }
  const overGoalHeat = parseOptionalNumber(item.qiushutouzhu?.[0])
  const underGoalHeat = parseOptionalNumber(item.qiushutouzhu?.[1])
  if (overGoalHeat > 65) {
    rows.push({ value: `警示：平台已纳入大小球热度修正：大球过热${percentText(overGoalHeat)}`, tone: 'red' })
  } else if (underGoalHeat > 65) {
    rows.push({ value: `警示：平台已纳入大小球热度修正：小球过热${percentText(underGoalHeat)}`, tone: 'green' })
  }

  return rows
}

function guideWarningPredictionSummary(item: AnalysisMatch): string {
  const prediction = warningAdjustedPrediction(item)
  if (!prediction) return ''
  return `警示后预测：${outcomeLabelByKey(prediction.outcome, item)} / ${prediction.goal.label} / ${prediction.score}`
}

function evilCultRows(item: AnalysisMatch): EvilCultRow[] {
  const prediction = evilCultPrediction(item)
  return [
    {
      label: '大小球',
      primary: prediction.underGoal,
      secondary: prediction.overGoal,
      tone: 'normal',
      primaryTone: 'red',
      secondaryTone: 'green',
    },
    {
      label: '球数',
      primary: prediction.underTotalText,
      secondary: prediction.overTotalText,
      tone: 'normal',
      primaryTone: 'red',
      secondaryTone: 'green',
    },
    {
      label: '比分',
      primary: prediction.underScore,
      secondary: prediction.overScore,
      tone: 'normal',
      primaryTone: scoreTone(prediction.underScore),
      secondaryTone: scoreTone(prediction.overScore),
    },
    {
      label: '胜平负',
      primary: outcomeLabelByKey(prediction.underOutcome, item),
      secondary: outcomeLabelByKey(prediction.overOutcome, item),
      tone: 'normal',
      primaryTone: outcomeTone(prediction.underOutcome),
      secondaryTone: outcomeTone(prediction.overOutcome),
    },
  ]
}

function evilCultPrediction(item: AnalysisMatch): {
  goal: string
  secondaryGoal: string
  total: string
  secondaryTotal: string
  underGoal: string
  overGoal: string
  underTotalText: string
  overTotalText: string
  underTotalValue: number
  overTotalValue: number
  underGoalLine: number
  overGoalLine: number
  underScore: string
  overScore: string
  underOutcome: DirectionOutcome
  overOutcome: DirectionOutcome
  mainPick: string
  reversePick: string
  mainReason: string
  mainTotal: number
  secondaryTotalValue: number
  goalDirection: 'over' | 'under'
  secondaryGoalDirection: 'over' | 'under'
  goalLine: number
  secondaryGoalLine: number
  score: string
  secondaryScore: string
  outcome: DirectionOutcome
  secondaryOutcome: DirectionOutcome
  goalTone: StatRow['tone']
  reverseTone: StatRow['tone']
  note: string
  reason: string
} {
  const line = evilCultGoalLine(item)
  const scores = evilCultGoalScores(item, line)
  const underTotal = evilCultUnderTotal(line, scores.expectedTotal)
  const overTotal = evilCultChaseOverTotal(line, scores.expectedTotal)
  const mainDirection = scores.under >= scores.over ? 'under' : 'over'
  const mainTotal = mainDirection === 'under' ? underTotal : overTotal
  const secondaryTotal = mainDirection === 'under' ? overTotal : underTotal
  const underLine = line
  const overLine = line
  const mainLine = line
  const secondaryLine = line
  const underGoals = evilCultGoalAllocation(item, underTotal)
  const overGoals = evilCultGoalAllocation(item, overTotal)
  const mainGoals = evilCultGoalAllocation(item, mainTotal)
  const secondaryGoals = evilCultGoalAllocation(item, secondaryTotal)
  const score = `${mainGoals.home}:${mainGoals.guest}`
  const secondaryScore = `${secondaryGoals.home}:${secondaryGoals.guest}`
  const underScore = `${underGoals.home}:${underGoals.guest}`
  const overScore = `${overGoals.home}:${overGoals.guest}`
  const goalLineText = trimFixed(line, 2)
  const overLineText = trimFixed(overLine, 2)

  return {
    goal: mainDirection === 'over' ? `追大${goalLineText}` : `小${goalLineText}`,
    secondaryGoal: mainDirection === 'under' ? `追大${goalLineText}` : `小${goalLineText}`,
    total: `${mainTotal}球`,
    secondaryTotal: `${secondaryTotal}球`,
    underGoal: `小${goalLineText}`,
    overGoal: `追大${overLineText}`,
    underTotalText: `${underTotal}球`,
    overTotalText: `${overTotal}球`,
    underTotalValue: underTotal,
    overTotalValue: overTotal,
    underGoalLine: underLine,
    overGoalLine: overLine,
    underScore,
    overScore,
    underOutcome: scoreOutcome(underScore),
    overOutcome: scoreOutcome(overScore),
    mainPick: mainDirection === 'under' ? `小球组：小${goalLineText} / ${underTotal}球 / ${underScore}` : `追大组：追大${goalLineText} / ${overTotal}球 / ${overScore}`,
    reversePick: mainDirection === 'under' ? `追大组：追大${goalLineText} / ${overTotal}球 / ${overScore}` : `小球组：小${goalLineText} / ${underTotal}球 / ${underScore}`,
    mainReason: evilCultReason(scores, line, underTotal, overTotal),
    mainTotal,
    secondaryTotalValue: secondaryTotal,
    goalDirection: mainDirection,
    secondaryGoalDirection: mainDirection === 'under' ? 'over' : 'under',
    goalLine: mainLine,
    secondaryGoalLine: secondaryLine,
    score,
    secondaryScore,
    outcome: scoreOutcome(score),
    secondaryOutcome: scoreOutcome(secondaryScore),
    goalTone: mainDirection === 'over' ? 'green' : 'red',
    reverseTone: mainDirection === 'under' ? 'green' : 'red',
    note: mainDirection === 'under' ? '固定先小，错了追大' : '追大剧本更强，保留小球次选',
    reason: evilCultReason(scores, line, underTotal, overTotal),
  }
}

function evilCultGoalScores(item: AnalysisMatch, line: number): { over: number; under: number; expectedTotal: number } {
  const expected = expectedGoalPair(item)
  const expectedTotal = expected.home + expected.guest
  const [historyGoals, recentGoals] = splitPair(item.changguiqiushu)
  const history = historyGoalSampleValue(item, historyGoals)
  const recent = recentGoalSampleValue(item, recentGoals)
  const homeTotal = parseOptionalNumber(item.qiushuAll?.[0])
  const guestTotal = parseOptionalNumber(item.qiushuAll?.[2])
  const homeConcede = parseOptionalNumber(item.qiushuAll?.[4])
  const guestConcede = parseOptionalNumber(item.qiushuAll?.[5])
  const rawAverage = weightedAverage([
    { value: expectedTotal, weight: 0.35 },
    { value: recent, weight: 0.25 },
    { value: history, weight: 0.15 },
    { value: line, weight: 0.1 },
    { value: weightedAverage([
      { value: homeTotal, weight: 0.25 },
      { value: guestTotal, weight: 0.25 },
      { value: homeConcede, weight: 0.25 },
      { value: guestConcede, weight: 0.25 },
    ]), weight: 0.15 },
  ])
  const average = Number.isFinite(rawAverage) ? rawAverage : line
  let over = 50 + (average - line) * 18
  let under = 50 + (line - average) * 18
  const balance = goalBalanceDirection(goalBalanceSignalForItem(item))
  const base = baseGoalSignal(item)
  if (balance === 'over') over += 8
  if (balance === 'under') under += 8
  if (base === 'over') over += 6
  if (base === 'under') under += 6
  const overReverseWeight = evilCultReverseHeatWeight(item.qiushutouzhu?.[0])
  const underReverseWeight = evilCultReverseHeatWeight(item.qiushutouzhu?.[1])
  if (overReverseWeight > 0) {
    under += overReverseWeight
    over -= overReverseWeight * 0.4
  }
  if (underReverseWeight > 0) {
    over += underReverseWeight
    under -= underReverseWeight * 0.4
  }
  return { over, under, expectedTotal: average }
}

function evilCultReverseHeatWeight(value: unknown): number {
  const heat = parseOptionalNumber(value)
  if (!Number.isFinite(heat) || heat <= 60) return 0
  return (Math.floor((heat - 60) / 5) + 1) * 5
}

function evilCultUnderTotal(line: number, expectedTotal: number): number {
  const maxUnder = Number.isFinite(line) ? Math.max(0, Math.floor(line)) : 2
  const target = Number.isFinite(expectedTotal) ? Math.round(expectedTotal) : maxUnder
  return Math.max(0, Math.min(maxUnder, target))
}

function evilCultChaseOverTotal(line: number, expectedTotal: number): number {
  const base = Number.isFinite(line) ? Math.max(0, Math.floor(line)) : 2
  const candidates = [base + 1, base + 2, base + 4]
  const target = Number.isFinite(expectedTotal) ? Math.max(base + 1, expectedTotal) : base + 1
  return candidates
    .map((total) => ({ total, gap: Math.abs(total - target) }))
    .sort((a, b) => a.gap - b.gap || a.total - b.total)[0].total
}

function evilCultGoalLine(item: AnalysisMatch): number {
  const current = parseOptionalNumber(item.qiushupankou2)
  const opening = parseOptionalNumber(item.qiushupankou1)
  if (Number.isFinite(current) && current > 0) return normalizeGoalHalfLine(current)
  if (Number.isFinite(opening) && opening > 0) return normalizeGoalHalfLine(opening)
  return 2.5
}

function normalizeGoalHalfLine(value: number): number {
  if (!Number.isFinite(value) || value < 0) return 2.5
  return Math.max(0.5, Math.floor(value) + 0.5)
}

function evilCultGoalAllocation(item: AnalysisMatch, total: number): { home: number; guest: number } {
  const expected = expectedGoalPair(item)
  if (!Number.isFinite(expected.home) || !Number.isFinite(expected.guest) || expected.home + expected.guest <= 0) {
    return allocateIntegerGoalTotal(total, { home: 1, guest: 1 }, item.yapanpankou2)
  }
  return allocateIntegerGoalTotal(total, expected, item.yapanpankou2)
}

function evilCultReason(scores: { over: number; under: number; expectedTotal: number }, line: number, underTotal: number, overTotal: number): string {
  const side = scores.over >= scores.under ? '追大' : '先小'
  return `小${trimFixed(line, 2)}覆盖0-${Math.floor(line)}球；追大候选${Math.floor(line) + 1}/${Math.floor(line) + 2}/${Math.floor(line) + 4}球；均值${trimFixed(scores.expectedTotal, 2)}，${side}分${Math.round(Math.max(scores.over, scores.under))}，小球点${underTotal}球/追大点${overTotal}球`
}

function goalBalanceDirection(signal: ReturnType<typeof goalBalanceSignalForItem>): 'over' | 'under' | null {
  if (signal === 'over' || signal === 'overCorrected') return 'over'
  if (signal === 'under' || signal === 'underHidden') return 'under'
  return null
}

function evilCultClass(tone: StatRow['tone']): string {
  if (tone === 'green') return 'text-emerald-700'
  if (tone === 'red') return 'text-red-600'
  if (tone === 'blue') return 'text-sky-700'
  return 'text-slate-800'
}

function bookmakerGuidePrediction(item: AnalysisMatch): GuidePrediction {
  const outcome = bookmakerResultOutcome(item)
  const goal = bookmakerGoalResult(item)
  const score = bookmakerFusedScore(item, outcome, goal.total)
  return {
    outcome,
    goal,
    score,
    secondaryScore: secondaryGuideScore(item, outcome, goal, 'bookmaker'),
  }
}

function platformLivePrediction(item: AnalysisMatch): GuidePrediction {
  const outcome = platformLiveOutcome(item)
  const goal = platformLiveGoalResult(item)
  const score = platformFusedScore(item, outcome, goal.total)
  const warning = platformSignalWarning(item, outcome)
  return {
    outcome,
    goal,
    score,
    secondaryScore: secondaryGuideScore(item, outcome, goal, 'platform'),
    warning: warning.value,
    warningTone: warning.tone,
  }
}

function platformLiveOutcome(item: AnalysisMatch): DirectionOutcome {
  const base = basePredictionOutcome(item)
  const correctionScores = platformLiveCorrectionScores(item)
  const correction = outcomeFromScores(correctionScores)
  if (!base) return correction

  const correctionScore = correctionScores[correction]
  const baseScore = correctionScores[base]
  const hasHandicapAlert = Boolean(handicapPressureSignal(item).outcome)
  const drawRisk = drawRiskSignal(item)
  const drawCanCoverSides = correctionScores.draw >= Math.max(correctionScores.home, correctionScores.away) - 0.8
  if (drawRisk.score >= 5 && drawCanCoverSides) return 'draw'
  const reverseThreshold = hasHandicapAlert ? 4.2 : 5
  const gapThreshold = hasHandicapAlert ? 1.6 : 2.8
  const shouldReverseBase = correction !== base && correctionScore >= reverseThreshold && correctionScore - baseScore >= gapThreshold
  return shouldReverseBase ? correction : base
}

function platformLiveCorrectionScores(item: AnalysisMatch): Record<DirectionOutcome, number> {
  const scores: Record<DirectionOutcome, number> = { home: 0, draw: 0, away: 0 }
  const add = (outcome: DirectionOutcome | null, weight: number) => {
    if (outcome) scores[outcome] += weight
  }
  const kellyOutcomes = platformTextOutcomes(item.kailiresult)
  const ticaiOutcomes = platformTextOutcomes(item.ticairesult)
  const kellyOutcome = kellyOutcomes[0] ?? null
  const ticaiOutcome = ticaiOutcomes[0] ?? null
  const professionalConsensus = professionalConsensusOutcome(item)
  const base = basePredictionOutcome(item)

  add(marketComfortOutcome(item, 'sporttery'), 3)
  add(marketComfortOutcome(item, 'sportteryRqspf'), 2.5)
  add(localComfortOutcome(item), 1.1)
  add(handicapExpectationOutcome(item), 1.8)
  const handicapAlert = handicapPressureSignal(item)
  add(handicapAlert.outcome, handicapAlert.weight * 1.6)
  add(handicapHeatOutcome(item), 0.8)
  add(expectedGoalOutcome(item), 1.2)
  add(kellyOutcome, 2.2)
  add(ticaiOutcome, 2.2)
  add(professionalConsensus, professionalConsensus && professionalConsensus !== base ? 2.2 : 1.2)
  platformTagOutcomes(item).forEach((outcome) => add(outcome, 0.8))
  add(historyMatchOutcome(item), 1.4)
  add(recentTeamOutcome(item.homezuijinbisai, item.home, 'home'), 1)
  add(recentTeamOutcome(item.guestzuijinbisai, item.guest, 'away'), 1)

  if (bookmakerDrawScore(item) >= 3) add('draw', 1.4)
  const drawRisk = drawRiskSignal(item)
  if (drawRisk.score >= 5) add('draw', 3.2)
  else if (drawRisk.score >= 4) add('draw', 2)
  else if (drawRisk.score >= 3) add('draw', 1)
  applyPlatformRiskScores(item, scores)
  return scores
}

function applyPlatformRiskScores(item: AnalysisMatch, scores: Record<DirectionOutcome, number>) {
  const add = (outcome: DirectionOutcome | null, weight: number) => {
    if (outcome) scores[outcome] += weight
  }
  const overheat = platformOverheatOutcome(item)
  if (overheat) {
    scores[overheat] -= 1
    add('draw', 0.6)
    add(oppositeOutcome(overheat), 0.5)
  }

  const handicapHot = handicapHotOutcome(item)
  if (handicapHot) {
    scores[handicapHot] -= 0.8
    add(oppositeOutcome(handicapHot), 0.6)
  }

  const sportteryLoss = strongMarketLossRow(item, 'sporttery')
  const rqLoss = strongMarketLossRow(item, 'sportteryRqspf')
  if (sportteryLoss && rqLoss && sportteryLoss.outcome === rqLoss.outcome) {
    scores[sportteryLoss.outcome] -= 1.2
    add('draw', sportteryLoss.outcome === 'draw' ? 0 : 0.4)
    add(oppositeOutcome(sportteryLoss.outcome), 0.7)
  }

  const professionalWarning = professionalConflictWarning(item)
  const professionalConsensus = professionalConsensusOutcome(item)
  if (professionalWarning && professionalConsensus) {
    const base = basePredictionOutcome(item)
    if (base) scores[base] -= 0.9
    add(professionalConsensus, 1.1)
  }

  const handicapMovement = handicapGuideOutcome(item)
  if (handicapMovement) add(handicapMovement, 0.6)
}

function platformSignalWarning(item: AnalysisMatch, outcome: DirectionOutcome): { value: string; tone?: StatRow['tone'] } {
  if (platformOverheatOutcome(item) === outcome) return { value: '过热', tone: 'blue' }
  return { value: '' }
}

function platformOverheatOutcome(item: AnalysisMatch): DirectionOutcome | null {
  const base = basePredictionOutcome(item)
  const kellyOutcome = primaryTextOutcome(item.kailiresult)
  const ticaiOutcome = primaryTextOutcome(item.ticairesult)
  const sportteryOutcome = marketComfortOutcome(item, 'sporttery')
  const rqOutcome = marketComfortOutcome(item, 'sportteryRqspf')
  const outcomes: DirectionOutcome[] = ['home', 'draw', 'away']
  return outcomes.find((outcome) => [base, kellyOutcome, ticaiOutcome, sportteryOutcome, rqOutcome].filter((signal) => signal === outcome).length >= 4) ?? null
}

function handicapHotOutcome(item: AnalysisMatch): DirectionOutcome | null {
  const homeHeat = parseOptionalNumber(item.yapantouzhu?.[0])
  const guestHeat = parseOptionalNumber(item.yapantouzhu?.[1])
  if (Number.isFinite(homeHeat) && homeHeat > 65) return 'home'
  if (Number.isFinite(guestHeat) && guestHeat > 65) return 'away'
  return null
}

function oppositeOutcome(outcome: DirectionOutcome | null): DirectionOutcome | null {
  if (outcome === 'home') return 'away'
  if (outcome === 'away') return 'home'
  return null
}

function marketComfortOutcome(item: AnalysisMatch, key: string): DirectionOutcome | null {
  return marketComfortRow(bookmakerMarkets(item).find((value) => value.key === key))?.outcome ?? null
}

function localComfortOutcome(item: AnalysisMatch): DirectionOutcome | null {
  return marketComfortRow(localProfitMarket(item))?.outcome ?? null
}

function strongMarketComfortRow(item: AnalysisMatch, key: string): BookmakerOutcome | null {
  return strongComfortRow(marketComfortRow(bookmakerMarkets(item).find((value) => value.key === key)))
}

function strongMarketLossRow(item: AnalysisMatch, key: string): BookmakerOutcome | null {
  return strongLossRow(marketLossRow(bookmakerMarkets(item).find((value) => value.key === key)))
}

function strongLocalComfortRow(item: AnalysisMatch): BookmakerOutcome | null {
  return strongComfortRow(marketComfortRow(localProfitMarket(item)))
}

function marketComfortRow(market: BookmakerMarket | null | undefined): BookmakerOutcome | null {
  const rows = (market?.bookmakerByOutcome ?? []).filter((row) => row.available && Number.isFinite(row.bookmakerProfit))
  return rows.sort((a, b) => b.bookmakerProfit - a.bookmakerProfit)[0] ?? null
}

function marketLossRow(market: BookmakerMarket | null | undefined): BookmakerOutcome | null {
  const rows = (market?.bookmakerByOutcome ?? []).filter((row) => row.available && Number.isFinite(row.bookmakerProfit))
  return rows.sort((a, b) => a.bookmakerProfit - b.bookmakerProfit)[0] ?? null
}

function strongComfortRow(row: BookmakerOutcome | null): BookmakerOutcome | null {
  if (!row || row.bookmakerProfit <= 0) return null
  return Number.isFinite(row.bookmakerRoi) && row.bookmakerRoi >= 8 ? row : null
}

function strongLossRow(row: BookmakerOutcome | null): BookmakerOutcome | null {
  if (!row || row.bookmakerProfit >= 0) return null
  return Number.isFinite(row.bookmakerRoi) && row.bookmakerRoi <= -8 ? row : null
}

function basePredictionOutcome(item: AnalysisMatch): DirectionOutcome | null {
  return textOutcome(item.prediction)
}

function platformTagOutcomes(item: AnalysisMatch): DirectionOutcome[] {
  const tags = Array.isArray(item.detail?.test23) && item.detail.test23.length ? item.detail.test23 : item.tags
  if (!tags?.length) return []
  return tags
    .map((tag) => {
      const text = String(tag || '')
      if (text.includes('客胜')) return 'away'
      if (text.includes('胜平局')) return item.prediction === '平局' ? 'draw' : 'home'
      if (text.includes('闹0区')) return 'draw'
      return null
    })
    .filter((value): value is DirectionOutcome => Boolean(value))
}

function professionalConsensusOutcome(item: AnalysisMatch): DirectionOutcome | null {
  const kellyOutcomes = platformTextOutcomes(item.kailiresult)
  const ticaiOutcomes = platformTextOutcomes(item.ticairesult)
  return kellyOutcomes.find((outcome) => ticaiOutcomes.includes(outcome)) ?? null
}

function professionalConflictWarning(item: AnalysisMatch): { value: string; tone: StatRow['tone'] } | null {
  const base = basePredictionOutcome(item)
  const professionalConsensus = professionalConsensusOutcome(item)
  if (!base || !professionalConsensus || professionalConsensus === base) return null
  return {
    value: `凯体反差：基础${outcomeLabelByKey(base, item)}，凯利/体彩共同指向${outcomeLabelByKey(professionalConsensus, item)}`,
    tone: 'red',
  }
}

function warningAdjustedPrediction(item: AnalysisMatch): GuidePrediction | null {
  const professionalConsensus = professionalConsensusOutcome(item)
  const base = basePredictionOutcome(item)
  if (!professionalConsensus || !base || professionalConsensus === base) return null
  const goal = warningAdjustedGoalResult(item)
  return {
    outcome: professionalConsensus,
    goal,
    score: warningAdjustedScore(professionalConsensus, goal.total),
    secondaryScore: warningAdjustedScore(professionalConsensus, Math.max(1, goal.total + 1)),
  }
}

function warningAdjustedGoalResult(item: AnalysisMatch): { label: string; total: number; tone: StatRow['tone'] } {
  const line = parseOptionalNumber(item.qiushupankou2)
  const total = Number.isFinite(line) ? Math.max(1, Math.ceil(line) - 1) : 2
  return { label: `${total}球以内`, total, tone: 'red' }
}

function warningAdjustedScore(outcome: DirectionOutcome, total: number): string {
  const normalizedTotal = Math.max(1, Math.round(total))
  if (outcome === 'draw') return normalizedTotal <= 1 ? '0:0' : '1:1'
  if (normalizedTotal <= 2) return outcome === 'home' ? '1:0' : '0:1'
  return outcome === 'home' ? '2:1' : '1:2'
}

function platformTextOutcomes(values: unknown[] | undefined): DirectionOutcome[] {
  if (!values?.length) return []
  return values
    .map((value) => textOutcome(value))
    .filter((value): value is DirectionOutcome => Boolean(value))
}

function primaryTextOutcome(values: unknown[] | undefined): DirectionOutcome | null {
  return platformTextOutcomes(values)[0] ?? null
}

function textOutcome(value: unknown): DirectionOutcome | null {
  const text = String(value || '').trim()
  if (!text || text === '-') return null
  if (/平/.test(text)) return 'draw'
  if (/客胜|主负|负/.test(text)) return 'away'
  if (/主胜|胜/.test(text)) return 'home'
  return null
}

function historyMatchOutcome(item: AnalysisMatch): DirectionOutcome | null {
  if (!hasHistoryGoalSample(item)) return null
  return matchOutcomeForCurrentTeams(item.liangduibisai, item.home, item.guest)
}

function recentTeamOutcome(record: unknown[] | undefined, team: string, side: 'home' | 'away'): DirectionOutcome | null {
  const result = matchOutcomeForTeam(record, team)
  if (!result) return null
  if (result === 'draw') return 'draw'
  if (side === 'home') return result === 'win' ? 'home' : 'away'
  return result === 'win' ? 'away' : 'home'
}

function matchOutcomeForCurrentTeams(record: unknown[] | undefined, currentHome: string, currentGuest: string): DirectionOutcome | null {
  if (!record?.length) return null
  const homeTeam = normalizeTeamName(record[1])
  const guestTeam = normalizeTeamName(record[2])
  const scoreA = parseOptionalNumber(record[3])
  const scoreB = parseOptionalNumber(record[4])
  if (!Number.isFinite(scoreA) || !Number.isFinite(scoreB)) return null
  const currentHomeName = normalizeTeamName(currentHome)
  const currentGuestName = normalizeTeamName(currentGuest)
  let homeScore = scoreA
  let guestScore = scoreB

  if (homeTeam.includes(currentGuestName) || guestTeam.includes(currentHomeName)) {
    homeScore = scoreB
    guestScore = scoreA
  } else if (!(homeTeam.includes(currentHomeName) || guestTeam.includes(currentGuestName))) {
    return null
  }

  if (homeScore > guestScore) return 'home'
  if (homeScore < guestScore) return 'away'
  return 'draw'
}

function matchOutcomeForTeam(record: unknown[] | undefined, team: string): 'win' | 'draw' | 'loss' | null {
  if (!record?.length) return null
  const homeTeam = normalizeTeamName(record[1])
  const guestTeam = normalizeTeamName(record[2])
  const scoreA = parseOptionalNumber(record[3])
  const scoreB = parseOptionalNumber(record[4])
  if (!Number.isFinite(scoreA) || !Number.isFinite(scoreB)) return null
  const teamName = normalizeTeamName(team)
  const isHomeTeam = homeTeam.includes(teamName)
  const isGuestTeam = guestTeam.includes(teamName)
  if (!isHomeTeam && !isGuestTeam) return null
  const teamScore = isHomeTeam ? scoreA : scoreB
  const opponentScore = isHomeTeam ? scoreB : scoreA
  if (teamScore > opponentScore) return 'win'
  if (teamScore < opponentScore) return 'loss'
  return 'draw'
}

function normalizeTeamName(value: unknown): string {
  return String(value || '').replace(/\s+/g, '').trim()
}

function handicapExpectationOutcome(item: AnalysisMatch): DirectionOutcome | null {
  const [historyHandicap, recentHandicap] = splitPair(item.changguiyapan)
  const expected = weightedAverage([
    { value: parseOptionalNumber(historyHandicap), weight: 0.45 },
    { value: parseOptionalNumber(recentHandicap), weight: 0.55 },
  ])
  const current = parseOptionalNumber(item.yapanpankou2)
  if (Number.isFinite(expected) && Math.abs(expected) >= 0.25) {
    if (Number.isFinite(current) && Math.abs(expected) - Math.abs(current) >= 0.75) return expected > 0 ? 'home' : 'away'
    return expected > 0 ? 'home' : 'away'
  }
  return null
}

function handicapPressureSignal(item: AnalysisMatch): { outcome: DirectionOutcome | null; weight: number } {
  const [historyHandicap, recentHandicap] = splitPair(item.changguiyapan)
  const history = parseOptionalNumber(historyHandicap)
  const recent = parseOptionalNumber(recentHandicap)
  const currentLine = parseOptionalNumber(item.yapanpankou2)
  if (![history, recent, currentLine].every(Number.isFinite)) return { outcome: null, weight: 0 }

  const expectedLine = weightedAverage([
    { value: history, weight: 0.45 },
    { value: recent, weight: 0.55 },
  ])
  const currentDirection = handicapDirection(currentLine)
  const expectedDirection = handicapDirection(expectedLine)
  const currentAbs = Math.abs(currentLine)
  const expectedAbs = Math.abs(expectedLine)
  const expectedOutcome = handicapDirectionOutcome(expectedDirection)
  const currentOpposite = currentDirection === 'home' ? 'away' : currentDirection === 'guest' ? 'home' : null

  if (currentDirection !== 'level' && expectedDirection !== 'level' && currentDirection !== expectedDirection) {
    return { outcome: expectedOutcome, weight: 1.4 }
  }
  if (currentDirection !== 'level' && currentAbs - expectedAbs >= 0.5) {
    return { outcome: currentOpposite, weight: 1 }
  }
  if (expectedDirection !== 'level' && expectedAbs - currentAbs >= 0.5) {
    return { outcome: expectedOutcome, weight: 1.2 }
  }
  if (Math.min(Math.abs(history - currentLine), Math.abs(recent - currentLine)) > 0.75) {
    return { outcome: expectedOutcome, weight: 1.1 }
  }
  return { outcome: null, weight: 0 }
}

function handicapDirectionOutcome(direction: 'home' | 'guest' | 'level'): DirectionOutcome | null {
  if (direction === 'home') return 'home'
  if (direction === 'guest') return 'away'
  return null
}

function handicapPressureSignalLabel(item: AnalysisMatch): string {
  const [historyHandicap, recentHandicap] = splitPair(item.changguiyapan)
  const history = parseOptionalNumber(historyHandicap)
  const recent = parseOptionalNumber(recentHandicap)
  const currentLine = parseOptionalNumber(item.yapanpankou2)
  if (![history, recent, currentLine].every(Number.isFinite)) return ''

  const expectedLine = weightedAverage([
    { value: history, weight: 0.45 },
    { value: recent, weight: 0.55 },
  ])
  const currentDirection = handicapDirection(currentLine)
  const expectedDirection = handicapDirection(expectedLine)
  const currentAbs = Math.abs(currentLine)
  const expectedAbs = Math.abs(expectedLine)

  if (currentDirection !== 'level' && expectedDirection !== 'level' && currentDirection !== expectedDirection) return '方向反转'
  if (currentDirection !== 'level' && currentAbs - expectedAbs >= 0.5) return '盘口偏深，防夸大强势方'
  if (expectedDirection !== 'level' && expectedAbs - currentAbs >= 0.5) return '盘口偏浅，防隐藏强势方'
  if (Math.min(Math.abs(history - currentLine), Math.abs(recent - currentLine)) > 0.75) return '盘口异常偏离'
  return ''
}

function expectedGoalOutcome(item: AnalysisMatch): DirectionOutcome | null {
  const goals = expectedGoalPair(item)
  if (!Number.isFinite(goals.home) || !Number.isFinite(goals.guest)) return null
  if (goals.home - goals.guest >= 0.55) return 'home'
  if (goals.guest - goals.home >= 0.55) return 'away'
  return 'draw'
}

function handicapHeatOutcome(item: AnalysisMatch): DirectionOutcome | null {
  const homeHeat = parseOptionalNumber(item.yapantouzhu?.[0])
  const guestHeat = parseOptionalNumber(item.yapantouzhu?.[1])
  if (!Number.isFinite(homeHeat) || !Number.isFinite(guestHeat)) return null
  if (homeHeat - guestHeat >= 10) return 'home'
  if (guestHeat - homeHeat >= 10) return 'away'
  return null
}

function platformLiveGoalResult(item: AnalysisMatch): { label: string; total: number; tone: StatRow['tone'] } {
  const bands = expectedGoalBands(item)
  const mainTotal = bands.main.home + bands.main.guest
  const openingLine = parseOptionalNumber(item.qiushupankou1)
  const currentLine = parseOptionalNumber(item.qiushupankou2)
  const line = Number.isFinite(currentLine) ? currentLine : openingLine
  const [historyGoals, recentGoals] = splitPair(item.changguiqiushu)
  const correctionTotal = weightedAverage([
    { value: Number.isFinite(mainTotal) ? mainTotal : Number.NaN, weight: 0.35 },
    { value: recentGoalSampleValue(item, recentGoals), weight: 0.25 },
    { value: historyGoalSampleValue(item, historyGoals), weight: 0.15 },
    { value: line, weight: 0.25 },
  ]) + goalHeatAdjustment(item) + goalBalanceAdjustment(item)
  const baseTotal = baseGoalPredictionValue(item)
  const expectedTotal = Number.isFinite(baseTotal)
    ? weightedAverage([
      { value: baseTotal, weight: 0.3 },
      { value: correctionTotal, weight: 0.7 },
    ])
    : correctionTotal
  const balanceSignal = goalBalanceSignalForItem(item)
  const total = Number.isFinite(expectedTotal)
    ? normalizedPlatformGoalTotal(expectedTotal, balanceSignal)
    : bookmakerGoalResult(item).total
  const signal = baseGoalSignal(item)
  if (signal === 'over' && Number.isFinite(line) && total >= Math.ceil(line)) return { label: `${total}球以上`, total, tone: 'green' }
  if (signal === 'under' && Number.isFinite(line) && total <= Math.floor(line)) return { label: `${total}球以内`, total, tone: 'red' }
  if (Number.isFinite(line) && total > line) return { label: `${total}球以上`, total, tone: 'green' }
  if (Number.isFinite(line) && total < line) return { label: `${total}球以内`, total, tone: 'red' }
  return { label: `${total}球左右`, total, tone: 'normal' }
}

function normalizedPlatformGoalTotal(expectedTotal: number, signal: ReturnType<typeof goalBalanceSignalForItem>): number {
  if (!Number.isFinite(expectedTotal)) return 0
  if (signal === 'underHidden' || signal === 'under') return Math.max(0, Math.floor(expectedTotal))
  if (signal === 'overCorrected' || signal === 'over') return Math.max(0, Math.ceil(expectedTotal))
  return Math.max(0, Math.round(expectedTotal))
}

function goalHeatAdjustment(item: AnalysisMatch): number {
  const overHeat = parseOptionalNumber(item.qiushutouzhu?.[0])
  const underHeat = parseOptionalNumber(item.qiushutouzhu?.[1])
  if (!Number.isFinite(overHeat) || !Number.isFinite(underHeat)) return 0
  if (overHeat > 65) return -0.35
  if (underHeat > 65) return 0.35
  const diff = overHeat - underHeat
  if (Math.abs(diff) < 10) return 0
  return roundValue(Math.max(-0.45, Math.min(0.45, diff / 60)), 2)
}

function goalBalanceAdjustment(item: AnalysisMatch): number {
  const signal = goalBalanceSignalForItem(item)
  if (signal === 'underHidden') return -0.85
  if (signal === 'under') return -0.6
  if (signal === 'overCorrected') return 0.85
  if (signal === 'over') return 0.6
  return 0
}

function goalBalanceSignalForItem(item: AnalysisMatch): 'underHidden' | 'under' | 'overCorrected' | 'over' | null {
  const [historyGoals, recentGoals] = splitPair(item.changguiqiushu)
  const history = parseOptionalNumber(historyGoals)
  const recent = parseOptionalNumber(recentGoals)
  const combined = combinedGoalAverageValue(history, recent)
  const openingLine = parseOptionalNumber(item.qiushupankou1)
  return goalBalanceSignal(history, recent, combined, openingLine)
}

function goalBalanceSignal(history: number, recent: number, combined: number, openingLine: number): 'underHidden' | 'under' | 'overCorrected' | 'over' | null {
  const baseline = 2.5
  const highThreshold = 2.85
  const lowThreshold = 2.15
  const balanceValue = weightedAverage([
    { value: history, weight: 0.2 },
    { value: recent, weight: 0.35 },
    { value: combined, weight: 0.3 },
    { value: openingLine, weight: 0.15 },
  ])
  if (!Number.isFinite(balanceValue)) return null

  const values = [history, recent, combined, openingLine].filter(Number.isFinite)
  const highCount = values.filter((value) => value >= highThreshold).length
  const lowCount = values.filter((value) => value <= lowThreshold).length
  if (balanceValue >= highThreshold || highCount >= 2) {
    return Number.isFinite(openingLine) && openingLine <= baseline ? 'underHidden' : 'under'
  }
  if (balanceValue <= lowThreshold || lowCount >= 2) {
    return Number.isFinite(openingLine) && openingLine >= baseline ? 'overCorrected' : 'over'
  }
  return null
}

function baseGoalPredictionValue(item: AnalysisMatch): number {
  const line = parseOptionalNumber(item.qiushupankou2)
  const signal = baseGoalSignal(item)
  const text = goalSignalText(item)
  if (signal === 'under') return Number.isFinite(line) ? Math.max(0, line - 0.75) : 2
  if (signal === 'over') {
    if (!Number.isFinite(line)) return 3
    if (/裂球/.test(text)) return Math.max(3, line + 1)
    return line + (line <= 2.25 ? 0.5 : 0.75)
  }
  return Number.NaN
}

function baseGoalSignal(item: AnalysisMatch): 'over' | 'under' | null {
  const text = goalSignalText(item)
  if (/小球|闹0区/.test(text)) return 'under'
  if (/大球|裂球/.test(text)) return 'over'
  return null
}

function goalSignalText(item: AnalysisMatch): string {
  return `${item.qiuprediction || ''} ${(item.detail?.test23 || item.tags || []).join(' ')}`
}

function outcomeFromScores(scores: Record<DirectionOutcome, number>): DirectionOutcome {
  return (Object.entries(scores) as Array<[DirectionOutcome, number]>)
    .sort((a, b) => b[1] - a[1])[0]?.[0] ?? 'draw'
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

function scoreShapeLabel(score: string): string {
  const [home, guest] = score.split(':').map((value) => Number.parseInt(value, 10))
  if (!Number.isFinite(home) || !Number.isFinite(guest)) return ''
  const total = home + guest
  const totalLabel = total <= 1 ? '低比分' : total === 2 ? '2球' : total === 3 ? '3球' : '大比分'
  if (home === guest) return `平局${totalLabel}`
  const outcome = home > guest ? '主胜' : '客胜'
  const margin = Math.abs(home - guest)
  const marginLabel = margin === 1 ? '小胜' : margin === 2 ? '中胜' : '大胜'
  return `${outcome}${marginLabel}${totalLabel}`
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

function expectedGoalPair(item: AnalysisMatch): GoalScore {
  const homeRecentAvailable = hasTeamRecentGoalSample(item.homezuijinbisai)
  const guestRecentAvailable = hasTeamRecentGoalSample(item.guestzuijinbisai)
  const homeAttackAverage = homeRecentAvailable ? fallbackAverage(item.liangduiqiushu?.[0], item.qiushuAll?.[0]) : Number.NaN
  const guestAttackAverage = guestRecentAvailable ? fallbackAverage(item.liangduiqiushu?.[1], item.qiushuAll?.[2]) : Number.NaN
  const homeAgainstAverage = homeRecentAvailable ? fallbackAverage(item.liangduiqiushu?.[2], item.qiushuAll?.[4]) : Number.NaN
  const guestAgainstAverage = guestRecentAvailable ? fallbackAverage(item.liangduiqiushu?.[3], item.qiushuAll?.[5]) : Number.NaN
  const homeBase = expectedTeamGoalBase(
    homeAttackAverage,
    item.qiushuAll?.[0],
    item.qiushuAll?.[1],
    guestAgainstAverage,
    item.qiushuAll?.[5],
  )
  const guestBase = expectedTeamGoalBase(
    guestAttackAverage,
    item.qiushuAll?.[2],
    item.qiushuAll?.[3],
    homeAgainstAverage,
    item.qiushuAll?.[4],
  )
  if (!Number.isFinite(homeBase) && !Number.isFinite(guestBase)) return { home: Number.NaN, guest: Number.NaN }

  const safeHomeBase = Number.isFinite(homeBase) ? homeBase : 0
  const safeGuestBase = Number.isFinite(guestBase) ? guestBase : 0
  const baseTotal = safeHomeBase + safeGuestBase
  const [, recentTotalGoalsValue] = splitPair(item.changguiqiushu)
  const totalGoalAnchor = weightedAverage([
    { value: baseTotal, weight: 0.25 },
    { value: recentGoalSampleValue(item, recentTotalGoalsValue), weight: 0.4 },
    { value: parseOptionalNumber(item.qiushupankou2), weight: 0.35 },
  ])
  const finalTotal = Number.isFinite(totalGoalAnchor) ? totalGoalAnchor : baseTotal
  const homeShare = baseTotal > 0 ? safeHomeBase / baseTotal : 0.5
  const rawHome = Math.max(0, finalTotal * homeShare)
  const rawGuest = Math.max(0, finalTotal * (1 - homeShare))

  return {
    home: applyZeroGoalRisk(rawHome, homeAttackAverage, guestAgainstAverage, safeHomeBase, finalTotal, item.yapanpankou2, 'home'),
    guest: applyZeroGoalRisk(rawGuest, guestAttackAverage, homeAgainstAverage, safeGuestBase, finalTotal, item.yapanpankou2, 'guest'),
  }
}

function expectedGoalBands(item: AnalysisMatch): { under: GoalScore; main: GoalScore; over: GoalScore } {
  const main = expectedGoalPair(item)
  const line = parseOptionalNumber(item.qiushupankou2)
  const openingLine = parseOptionalNumber(item.qiushupankou1)
  const goalLine = Number.isFinite(line) ? line : openingLine
  const handicap = parseOptionalNumber(item.yapanpankou2)
  const openingHandicap = parseOptionalNumber(item.yapanpankou1)
  const handicapLine = Number.isFinite(handicap) ? handicap : openingHandicap
  const mainTotal = main.home + main.guest
  const safeMainTotal = Number.isFinite(mainTotal) ? mainTotal : Number.NaN
  const underTotal = Number.isFinite(goalLine) ? Math.max(0, Math.ceil(goalLine) - 1) : safeMainTotal
  const overTotal = Number.isFinite(goalLine) ? Math.max(0, Math.floor(goalLine + 2)) : safeMainTotal + 2

  return {
    under: allocateExpectedGoalTotal(underTotal, main, handicapLine, true),
    main,
    over: allocateExpectedGoalTotal(overTotal, main, handicapLine, true),
  }
}

function allocateExpectedGoalTotal(total: number, base: GoalScore, handicapValue?: number, forceInteger = false): GoalScore {
  if (!Number.isFinite(total)) return { home: Number.NaN, guest: Number.NaN }
  if (forceInteger) return allocateIntegerGoalTotal(Math.max(0, Math.round(total)), base, handicapValue)

  const baseTotal = base.home + base.guest
  const homeShare = Number.isFinite(baseTotal) && baseTotal > 0 ? base.home / baseTotal : 0.5
  return {
    home: Math.max(0, total * homeShare),
    guest: Math.max(0, total * (1 - homeShare)),
  }
}

function allocateIntegerGoalTotal(total: number, base: GoalScore, handicapValue?: number): GoalScore {
  const normalizedTotal = Math.max(0, Math.round(total))
  if (normalizedTotal <= 0) return { home: 0, guest: 0 }

  const baseTotal = base.home + base.guest
  const homeShare = Number.isFinite(baseTotal) && baseTotal > 0 ? base.home / baseTotal : 0.5
  let home = Math.max(0, Math.min(normalizedTotal, Math.round(normalizedTotal * homeShare)))
  let guest = normalizedTotal - home

  const handicap = Number.isFinite(handicapValue) ? Number(handicapValue) : Number.NaN
  if (!Number.isFinite(handicap) || Math.abs(handicap) < 0.5) return { home, guest }

  const favorite = handicap > 0 ? 'home' : 'guest'
  const minGap = Math.min(normalizedTotal, Math.ceil(Math.abs(handicap)))
  const currentGap = favorite === 'home' ? home - guest : guest - home
  if (currentGap >= minGap) return { home, guest }

  const favoriteGoals = Math.min(normalizedTotal, Math.ceil((normalizedTotal + minGap) / 2))
  const underdogGoals = normalizedTotal - favoriteGoals
  if (favorite === 'home') {
    home = favoriteGoals
    guest = underdogGoals
  } else {
    home = underdogGoals
    guest = favoriteGoals
  }
  return { home, guest }
}

function expectedTeamGoalBase(attackAverageValue: unknown, attackTotalValue: unknown, maxGoalValue: unknown, opponentAgainstAverageValue: unknown, opponentAgainstTotalValue: unknown): number {
  const attackAverage = fallbackAverage(attackAverageValue, attackTotalValue)
  const opponentAgainstAverage = fallbackAverage(opponentAgainstAverageValue, opponentAgainstTotalValue)
  const maxGoal = parseOptionalNumber(maxGoalValue)
  if (!Number.isFinite(attackAverage) && !Number.isFinite(opponentAgainstAverage) && !Number.isFinite(maxGoal)) return Number.NaN

  const attackBase = Number.isFinite(attackAverage) ? attackAverage : opponentAgainstAverage
  const againstBase = Number.isFinite(opponentAgainstAverage) ? opponentAgainstAverage : attackBase
  const peakBase = Number.isFinite(maxGoal) ? Math.min(maxGoal, attackBase + 1.5) : attackBase
  return roundValue(Math.max(0, attackBase * 0.5 + againstBase * 0.35 + peakBase * 0.15), 2)
}

function applyZeroGoalRisk(expectedGoal: number, attackAverage: number, opponentAgainstAverage: number, teamBase: number, totalGoalAnchor: number, handicapValue: unknown, side: 'home' | 'guest'): number {
  const risk = zeroGoalRiskScore(attackAverage, opponentAgainstAverage, teamBase, totalGoalAnchor, handicapValue, side)
  if (risk >= 0.65 && expectedGoal < 0.9) return 0.49
  if (risk >= 0.5 && expectedGoal < 1.15) return expectedGoal * 0.7
  return expectedGoal
}

function zeroGoalRiskScore(attackAverage: number, opponentAgainstAverage: number, teamBase: number, totalGoalAnchor: number, handicapValue: unknown, side: 'home' | 'guest'): number {
  let risk = 0
  if (Number.isFinite(attackAverage)) {
    if (attackAverage <= 0.6) risk += 0.35
    else if (attackAverage <= 1) risk += 0.18
  }
  if (Number.isFinite(opponentAgainstAverage)) {
    if (opponentAgainstAverage <= 0.8) risk += 0.3
    else if (opponentAgainstAverage <= 1.1) risk += 0.15
  }
  if (Number.isFinite(teamBase) && teamBase <= 0.75) risk += 0.2
  if (Number.isFinite(totalGoalAnchor) && totalGoalAnchor <= 2.25) risk += 0.15

  const handicap = parseOptionalNumber(handicapValue)
  if (Number.isFinite(handicap)) {
    const unsupportedByHandicap = side === 'home' ? handicap <= -0.25 : handicap >= 0.25
    if (unsupportedByHandicap) risk += 0.15
  }
  return risk
}

function zeroGoalAdviceText(item: AnalysisMatch, expectedGoals: { home: number; guest: number }): string {
  const teams = [
    { name: item.home, value: expectedGoals.home },
    { name: item.guest, value: expectedGoals.guest },
  ].filter((team) => Number.isFinite(team.value) && Math.round(team.value) === 0)
  if (!teams.length) return ''
  return `0球风险：${teams.map((team) => team.name).join('、')}触发低进攻/低失球/盘口压低修正；`
}

function weightedAverage(items: Array<{ value: number; weight: number }>): number {
  const validItems = items.filter((item) => Number.isFinite(item.value) && item.weight > 0)
  const totalWeight = validItems.reduce((sum, item) => sum + item.weight, 0)
  if (totalWeight <= 0) return Number.NaN
  return validItems.reduce((sum, item) => sum + item.value * item.weight, 0) / totalWeight
}

function fallbackAverage(averageValue: unknown, totalValue: unknown): number {
  const average = parseOptionalNumber(averageValue)
  if (Number.isFinite(average)) return average
  const total = parseOptionalNumber(totalValue)
  return Number.isFinite(total) ? total / 5 : Number.NaN
}

function hasHistoryGoalSample(item: AnalysisMatch): boolean {
  const signal = String(item.sanhuxinli?.[4] || '').trim()
  return Boolean(signal && signal !== '样本不足')
}

function hasTeamRecentGoalSample(value: unknown[] | undefined): boolean {
  return Array.isArray(value) && value.length >= 5
}

function hasRecentGoalSample(item: AnalysisMatch): boolean {
  return hasTeamRecentGoalSample(item.homezuijinbisai) || hasTeamRecentGoalSample(item.guestzuijinbisai)
}

function historyGoalSampleValue(item: AnalysisMatch, value: unknown): number {
  return hasHistoryGoalSample(item) ? parseOptionalNumber(value) : Number.NaN
}

function recentGoalSampleValue(item: AnalysisMatch, value: unknown): number {
  return hasRecentGoalSample(item) ? parseOptionalNumber(value) : Number.NaN
}

function combinedGoalAverageValue(historyValue: number, recentValue: number): number {
  return weightedAverage([
    { value: historyValue, weight: 0.45 },
    { value: recentValue, weight: 0.55 },
  ])
}

function goalMetricText(value: number): string {
  if (!Number.isFinite(value)) return '-'
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
    { label: '综合均值', value: pairAverage(item.changguiyapan) },
    { label: '亚盘初盘', value: valueText(item.yapanpankou1) },
    { label: '亚盘即时盘', value: valueText(item.yapanpankou2) },
    { label: '投注主队比例', value: percentText(item.yapantouzhu?.[0]) },
    { label: '投注客队比例', value: percentText(item.yapantouzhu?.[1]) },
    { label: '主队历史主场进球数', value: valueText(item.yapantouzhu?.[10]) },
    { label: '客队历史客场进球数', value: valueText(item.yapantouzhu?.[11]) },
    { label: '压力方向', value: valueText(item.yapantouzhu?.[12]) },
    ...handicapPressureAlertRows(historyHandicap, recentHandicap, item.yapanpankou2, item.home, item.guest),
  ]
}

function expectedGoalRows(item: AnalysisMatch): StatRow[] {
  const [historyGoals, recentGoals] = splitPair(item.changguiqiushu)
  const historyGoalValue = historyGoalSampleValue(item, historyGoals)
  const recentGoalValue = recentGoalSampleValue(item, recentGoals)
  const combinedGoals = combinedGoalAverageValue(historyGoalValue, recentGoalValue)
  return [
    { label: '历史平均球数', value: goalMetricText(historyGoalValue) },
    { label: '近期平均球数', value: goalMetricText(recentGoalValue) },
    { label: '综合均值', value: goalMetricText(combinedGoals) },
    { label: '大小球初盘', value: valueText(item.qiushupankou1) },
    { label: '大小球即时盘', value: valueText(item.qiushupankou2) },
    { label: '投注大球比例', value: percentText(item.qiushutouzhu?.[0]) },
    { label: '投注小球比例', value: percentText(item.qiushutouzhu?.[1]) },
    { label: '最近5场平均进球数', value: valueText(item.qiushutouzhu?.[2]) },
    { label: '最近5场平均丢球数', value: valueText(item.qiushutouzhu?.[3]) },
    { label: '压力方向', value: valueText(item.qiushutouzhu?.[6]) },
    ...goalBalanceAlertRows(historyGoals, recentGoals, combinedGoals, item.qiushupankou1),
  ]
}

function goalBalanceAlertRows(historyValue: unknown, recentValue: unknown, combinedValue: unknown, openingLineValue: unknown): StatRow[] {
  const baseline = 2.5
  const highThreshold = 2.85
  const lowThreshold = 2.15
  const history = parseOptionalNumber(historyValue)
  const recent = parseOptionalNumber(recentValue)
  const combined = parseOptionalNumber(combinedValue)
  const openingLine = parseOptionalNumber(openingLineValue)
  const balanceValue = weightedAverage([
    { value: history, weight: 0.2 },
    { value: recent, weight: 0.35 },
    { value: combined, weight: 0.3 },
    { value: openingLine, weight: 0.15 },
  ])

  if (!Number.isFinite(balanceValue)) {
    return [{
      label: '2.5均衡警示',
      value: '历史、近期、综合均值或初盘不足，暂按盘口变化观察大小球。',
      tone: 'normal',
    }]
  }

  const values = [history, recent, combined, openingLine].filter(Number.isFinite)
  const highCount = values.filter((value) => value >= highThreshold).length
  const lowCount = values.filter((value) => value <= lowThreshold).length
  const rows: StatRow[] = [{
    label: '2.5均衡值',
    value: `测算${trimFixed(balanceValue, 2)}，中轴${trimFixed(baseline, 1)}`,
    tone: 'normal',
  }]
  const missingSamples = [
    Number.isFinite(history) ? '' : '历史样本',
    Number.isFinite(recent) ? '' : '近期样本',
  ].filter(Boolean)
  if (missingSamples.length) {
    rows.push({
      label: '样本修正',
      value: `${missingSamples.join('、')}不足，已从均衡计算中剔除，不按0球处理。`,
      tone: 'normal',
    })
  }

  if (balanceValue >= highThreshold || highCount >= 2) {
    rows.push({
      label: '回归小球警示',
      value: `历史/近期/综合/初盘整体高于${trimFixed(baseline, 1)}，连续大球不可持续，优先防回落到小球。`,
      tone: 'red',
    })
    if (Number.isFinite(openingLine) && openingLine <= baseline) {
      rows.push({
        label: '盘口隐藏提醒',
        value: `均值偏大但初盘未抬高到${trimFixed(baseline, 1)}以上，说明盘口没有追大，回归小球信号更强。`,
        tone: 'blue',
      })
    }
    return rows
  }

  if (balanceValue <= lowThreshold || lowCount >= 2) {
    rows.push({
      label: '回归大球警示',
      value: `历史/近期/综合/初盘整体低于${trimFixed(baseline, 1)}，连续小球不可持续，优先防反弹到大球。`,
      tone: 'green',
    })
    if (Number.isFinite(openingLine) && openingLine >= baseline) {
      rows.push({
        label: '盘口修正提醒',
        value: `均值偏小但初盘仍在${trimFixed(baseline, 1)}附近或以上，盘口可能已提前修正，防大球反弹。`,
        tone: 'blue',
      })
    }
    return rows
  }

  rows.push({
    label: '均衡判断',
    value: `当前没有明显超出阈值，大小球先按${trimFixed(baseline, 1)}上下平衡观察。`,
    tone: 'normal',
  })
  return rows
}

function handicapPressureAlertRows(historyValue: unknown, recentValue: unknown, currentLineValue: unknown, home: string, guest: string): StatRow[] {
  const history = parseOptionalNumber(historyValue)
  const recent = parseOptionalNumber(recentValue)
  const currentLine = parseOptionalNumber(currentLineValue)
  if (![history, recent, currentLine].every(Number.isFinite)) {
    return [{
      label: '注意盘口',
      value: '期望让球或即时盘暂缺，先按主客方向观察。',
      tone: 'normal',
    }]
  }

  const expectedLine = weightedAverage([
    { value: history, weight: 0.45 },
    { value: recent, weight: 0.55 },
  ])
  const currentDirection = handicapDirection(currentLine)
  const expectedDirection = handicapDirection(expectedLine)
  const currentAbs = Math.abs(currentLine)
  const expectedAbs = Math.abs(expectedLine)
  const rows: StatRow[] = []

  rows.push({
    label: '盘口方向',
    value: handicapDirectionText(currentLine, home, guest),
    tone: 'normal',
  })

  if (currentDirection !== 'level' && expectedDirection !== 'level' && currentDirection !== expectedDirection) {
    rows.push({
      label: '方向反转提醒',
      value: `期望更偏${expectedDirection === 'home' ? home : guest}，但即时盘是${handicapDirectionText(currentLine, home, guest)}，需要防盘口方向反做。`,
      tone: 'red',
    })
  } else if (currentDirection !== 'level' && currentAbs - expectedAbs >= 0.5) {
    rows.push({
      label: '重点提醒',
      value: `${handicapDirectionText(currentLine, home, guest)}，比历史/近期期望${trimFixed(expectedAbs, 2)}更深，盘口过于明显，可能夸大强势方。`,
      tone: 'blue',
    })
  } else if (expectedDirection !== 'level' && expectedAbs - currentAbs >= 0.5) {
    rows.push({
      label: '重点提醒',
      value: `历史/近期期望约${trimFixed(expectedAbs, 2)}球，但即时盘只有${handicapDirectionText(currentLine, home, guest)}，盘口偏浅，可能故意隐藏强势方。`,
      tone: 'blue',
    })
  } else {
    rows.push({
      label: '注意盘口',
      value: '期望让球与即时盘没有明显偏离，继续结合盈亏和临场升降观察。',
      tone: 'normal',
    })
  }

  if (Math.min(Math.abs(history - currentLine), Math.abs(recent - currentLine)) > 0.75) {
    rows.push({
      label: '异常提示',
      value: '期望让球与即时盘偏离超过0.75，注意盘口异常。',
      tone: 'green',
    })
  }

  return rows
}

function handicapDirection(value: number): 'home' | 'guest' | 'level' {
  if (!Number.isFinite(value) || Math.abs(value) < 0.01) return 'level'
  return value > 0 ? 'home' : 'guest'
}

function handicapDirectionText(value: number, home: string, guest: string): string {
  if (!Number.isFinite(value) || Math.abs(value) < 0.01) return '平手盘'
  const absolute = trimFixed(Math.abs(value), 2)
  if (value > 0) return `${home}让${absolute}球`
  return `${guest}让${absolute}球（${home}受让${absolute}球）`
}

function labeledRows(labels: string[], values: unknown[] | undefined): StatRow[] {
  return labels.map((label, index) => ({ label, value: valueText(values?.[index]) }))
}

function splitPair(value: string): [string, string] {
  const parts = String(value || '').split(':').map((item) => item.trim())
  return [parts[0] || '', parts[1] || '']
}

function pairAverage(value: string): string {
  const parts = splitPair(value).map((item) => Number.parseFloat(item))
  if (parts.length < 2 || parts.some((item) => Number.isNaN(item))) return '-'
  return ((parts[0] + parts[1]) / 2).toFixed(2)
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

watch([selectedDate, showScore, viewMode], persistAnalysisPageState)

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
