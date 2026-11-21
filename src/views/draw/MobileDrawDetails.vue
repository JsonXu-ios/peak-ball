<script setup lang="ts">
import { computed, ref } from "vue";
import type {
  DrawObservation,
  DrawObservedEuroOdds,
  DrawObservedLine,
  DrawRecentHistory,
} from "@/types/drawHistory";
import {
  drawProfitAmount,
  drawProfitMarkets,
  type DrawProfitMarket,
} from "../../../admin/src/utils/drawProfit";

const props = defineProps<{ row: DrawObservation }>();
const tabs = [
  { key: "history", label: "战绩" },
  { key: "quotes", label: "指数" },
  { key: "profit", label: "盈亏" },
] as const;
type TabKey = (typeof tabs)[number]["key"];
const activeTab = ref<TabKey>("history");
const idPrefix = computed(
  () => `draw-details-${props.row.matchId.replace(/[^a-zA-Z0-9_-]/g, "-")}`,
);
const histories = computed(() => [
  {
    key: "home",
    title: `${props.row.home} · 近10场`,
    history: props.row.homeRecent,
  },
  {
    key: "away",
    title: `${props.row.guest} · 近10场`,
    history: props.row.awayRecent,
  },
  { key: "h2h", title: "双方交锋", history: props.row.headToHead },
]);
const averageRows = computed(() => {
  const stages = props.row.frontendAnalysis?.comparisonStages ?? [];
  return [
    {
      label: "初盘",
      values: stages.find((stage) => stage.key === "opening")?.average,
    },
    {
      label: "即时",
      values:
        stages.find((stage) => stage.key === "latest")?.average ??
        props.row.frontendAnalysis?.averageEuroOdds,
    },
  ];
});
const companyPanels = computed(() => {
  const companies = props.row.companyQuotes ?? [];
  if (companies.length)
    return companies.map((company) => ({
      key: company.key,
      name: company.name,
      euro: company.euroOdds,
      lines: [
        { key: "asian", label: "亚盘", line: company.asianHandicap },
        { key: "goals", label: "大小球", line: company.goalsLine },
      ],
    }));
  return [
    {
      key: "source",
      name: props.row.euroOdds?.company || "已保存公司指数",
      euro: props.row.euroOdds,
      lines: [
        { key: "asian", label: "亚盘", line: props.row.asianHandicap },
        { key: "goals", label: "大小球", line: props.row.goalsLine },
      ],
    },
  ];
});
const mainLines = computed(() => [
  { key: "asian", label: "页面亚盘", line: props.row.asianHandicap },
  { key: "goals", label: "页面大小球", line: props.row.goalsLine },
]);
const profitMarkets = computed(() => [
  ...drawProfitMarkets(props.row, "sporttery", true),
  ...drawProfitMarkets(props.row, "simulated", true),
]);

function finite(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}
function valueText(value: unknown, decimals = 2): string {
  if (finite(value)) return value.toFixed(decimals);
  if (typeof value === "string" && value.trim()) return value.trim();
  return "—";
}
function amountText(value: number | null): string {
  if (!finite(value)) return "暂无";
  const absolute = Math.abs(value);
  const sign = value > 0 ? "+" : value < 0 ? "−" : "";
  if (absolute >= 100000000)
    return `${sign}${(absolute / 100000000).toFixed(2)}亿`;
  if (absolute >= 10000) return `${sign}${(absolute / 10000).toFixed(1)}万`;
  return `${sign}${absolute.toLocaleString("zh-CN", { maximumFractionDigits: 0 })}元`;
}
function percent(value: number | null, signed = false): string {
  return finite(value)
    ? `${signed && value > 0 ? "+" : ""}${value.toFixed(1)}%`
    : "暂无";
}
function profitTone(value: number | null): string {
  return !finite(value) || Math.abs(value) < 0.005
    ? ""
    : value > 0
      ? "is-positive"
      : "is-negative";
}
function historyRows(history: DrawRecentHistory | undefined) {
  return history?.matches ?? [];
}
function sampleText(history: DrawRecentHistory | undefined): string {
  return history && finite(history.sample) && history.sample > 0
    ? `${history.sample}场 · ${history.draws}平`
    : "暂无有效记录";
}
function historyDate(date: string, matchTime: string): string {
  if (date) return date.slice(0, 10);
  const parsed = new Date(matchTime);
  return Number.isNaN(parsed.getTime())
    ? "日期未知"
    : new Intl.DateTimeFormat("zh-CN", {
        timeZone: "Asia/Shanghai",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
      }).format(parsed);
}
function euroHint(euro: DrawObservedEuroOdds | undefined): string {
  if (!euro || euro.source === "missing") return "暂无已采集欧赔";
  if (euro.openingStatus === "unopened") return "源站未开盘";
  if (euro.openingStatus === "not_collected") return "初盘尚未采集";
  if (euro.openingStatus === "no_quotes") return "暂无有效报价";
  return "已保存的公司报价";
}
function lineHint(line: DrawObservedLine | undefined): string {
  if (!line || line.source === "missing") return "暂无盘口";
  if (line.source === "history_moneys") return "历史记录，不作即时盘";
  return line.company ? `${line.company} · 已保存盘口` : "已保存盘口";
}
function lineRows(line: DrawObservedLine | undefined) {
  if (!line || line.source === "missing") return [];
  if (line.source === "history_moneys")
    return [
      {
        key: "historical",
        label: "历史",
        line: line.historical,
        left: null,
        right: null,
      },
    ];
  return [
    {
      key: "opening",
      label: "初盘",
      line: line.opening,
      left: line.openingOdds?.[0],
      right: line.openingOdds?.[1],
    },
    {
      key: "latest",
      label: "即时",
      line: line.latest,
      left: line.latestOdds?.[0],
      right: line.latestOdds?.[1],
    },
  ];
}
function marketKind(market: DrawProfitMarket): string {
  return market.simulated
    ? "模拟胜平负"
    : market.key === "sportteryRqspf"
      ? "真实竞彩让球"
      : "真实竞彩胜平负";
}
function moveTab(event: KeyboardEvent) {
  const current = tabs.findIndex((tab) => tab.key === activeTab.value);
  const next =
    event.key === "ArrowRight"
      ? (current + 1) % tabs.length
      : event.key === "ArrowLeft"
        ? (current + tabs.length - 1) % tabs.length
        : event.key === "Home"
          ? 0
          : event.key === "End"
            ? tabs.length - 1
            : -1;
  if (next < 0) return;
  event.preventDefault();
  activeTab.value = tabs[next]!.key;
  const tablist = (event.currentTarget as HTMLElement).closest(
    '[role="tablist"]',
  );
  tablist?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
}
</script>

<template>
  <section class="draw-details" aria-label="比赛详细数据">
    <div class="detail-tabs" role="tablist" aria-label="比赛数据分类">
      <button
        v-for="tab in tabs"
        :id="`${idPrefix}-${tab.key}-tab`"
        :key="tab.key"
        type="button"
        role="tab"
        :aria-selected="activeTab === tab.key"
        :aria-controls="`${idPrefix}-${tab.key}-panel`"
        :tabindex="activeTab === tab.key ? 0 : -1"
        :class="{ 'is-active': activeTab === tab.key }"
        @click="activeTab = tab.key"
        @keydown="moveTab"
      >
        {{ tab.label }}
      </button>
    </div>

    <div
      v-show="activeTab === 'history'"
      :id="`${idPrefix}-history-panel`"
      class="detail-panel"
      role="tabpanel"
      :aria-labelledby="`${idPrefix}-history-tab`"
    >
      <p class="detail-note">按该场开赛前已完赛记录统计，日期为北京时间。</p>
      <details
        v-for="section in histories"
        :key="section.key"
        class="history-block"
      >
        <summary>
          <span class="history-title">{{ section.title }}</span><span
            class="history-count"
            >{{ sampleText(section.history) }}</span
          ><span class="expand-icon" aria-hidden="true">⌄</span>
        </summary>
        <ol v-if="historyRows(section.history).length" class="history-list">
          <li
            v-for="(match, index) in historyRows(section.history)"
            :key="`${match.matchId}-${index}`"
          >
            <div class="history-meta">
              <time>{{
                historyDate(match.date, match.matchTime)
              }}</time><span>{{ match.league || "赛事未标注" }}</span>
            </div>
            <div class="history-match">
              <span>{{ match.home }}</span><strong
                :class="{ 'draw-score': match.homeScore === match.awayScore }"
                >{{ match.homeScore }} : {{ match.awayScore }}</strong
              ><span>{{ match.guest }}</span>
            </div>
          </li>
        </ol>
        <p v-else class="empty-state">暂无有效历史明细。</p>
      </details>
    </div>

    <div
      v-show="activeTab === 'quotes'"
      :id="`${idPrefix}-quotes-panel`"
      class="detail-panel"
      role="tabpanel"
      :aria-labelledby="`${idPrefix}-quotes-tab`"
    >
      <section class="data-block">
        <h4>平均欧赔</h4>
        <div class="quote-grid quote-heading">
          <span>阶段</span><span>主胜</span><span>平局</span><span>客胜</span>
        </div>
        <div v-for="stage in averageRows" :key="stage.label" class="quote-grid">
          <span class="row-label">{{ stage.label }}</span><span
            v-for="index in 3"
            :key="index"
            >{{ valueText(stage.values?.[index - 1]) }}</span
          >
        </div>
        <p class="detail-note">即时为最后已保存报价；— 表示未提供。</p>
      </section>
      <section v-if="row.companyQuotes?.length" class="data-block">
        <h4>页面盘口</h4>
        <div v-for="market in mainLines" :key="market.key" class="line-section">
          <div class="market-subtitle">{{ market.label }}</div>
          <p class="source-note">{{ lineHint(market.line) }}</p>
          <template v-if="lineRows(market.line).length">
            <div class="quote-grid quote-heading">
              <span>阶段</span
              ><span>{{ market.key === "goals" ? "大水" : "主水" }}</span
              ><span>盘口</span
              ><span>{{ market.key === "goals" ? "小水" : "客水" }}</span>
            </div>
            <div
              v-for="stage in lineRows(market.line)"
              :key="stage.key"
              class="quote-grid"
            >
              <span class="row-label">{{ stage.label }}</span><span>{{
                valueText(stage.left)
              }}</span
              ><span>{{ valueText(stage.line) }}</span
              ><span>{{ valueText(stage.right) }}</span>
            </div>
          </template>
        </div>
      </section>
      <section
        v-for="company in companyPanels"
        :key="company.key"
        class="data-block"
      >
        <h4>{{ company.name || "公司指数" }}</h4>
        <p class="source-note">{{ euroHint(company.euro) }}</p>
        <div class="quote-grid quote-heading">
          <span>欧赔</span><span>主胜</span><span>平局</span><span>客胜</span>
        </div>
        <div class="quote-grid">
          <span class="row-label">初盘</span
          ><span v-for="index in 3" :key="index">{{
            valueText(company.euro?.opening?.[index - 1])
          }}</span>
        </div>
        <div class="quote-grid">
          <span class="row-label">即时</span
          ><span v-for="index in 3" :key="index">{{
            valueText(company.euro?.latest?.[index - 1])
          }}</span>
        </div>
        <div
          v-for="market in company.lines"
          :key="market.key"
          class="line-section"
        >
          <div class="market-subtitle">{{ market.label }}</div>
          <p class="source-note">{{ lineHint(market.line) }}</p>
          <template v-if="lineRows(market.line).length">
            <div class="quote-grid quote-heading">
              <span>阶段</span
              ><span>{{ market.key === "goals" ? "大水" : "主水" }}</span
              ><span>盘口</span
              ><span>{{ market.key === "goals" ? "小水" : "客水" }}</span>
            </div>
            <div
              v-for="stage in lineRows(market.line)"
              :key="stage.key"
              class="quote-grid"
            >
              <span class="row-label">{{ stage.label }}</span><span>{{
                valueText(stage.left)
              }}</span
              ><span>{{ valueText(stage.line) }}</span
              ><span>{{ valueText(stage.right) }}</span>
            </div>
          </template>
        </div>
      </section>
    </div>

    <div
      v-show="activeTab === 'profit'"
      :id="`${idPrefix}-profit-panel`"
      class="detail-panel"
      role="tabpanel"
      :aria-labelledby="`${idPrefix}-profit-tab`"
    >
      <p v-if="!profitMarkets.length" class="empty-state">
        暂无可展示的胜平负或真实竞彩让球盈亏数据。
      </p>
      <template v-else>
        <p class="detail-note">
          盈亏金额按页面本金与支持率测算。正数为庄家盈利，负数为亏损。
        </p>
        <section
          v-for="market in profitMarkets"
          :key="market.key"
          class="data-block profit-block"
        >
          <div class="profit-title">
            <h4>{{ market.name }}</h4>
            <span
              :class="['market-tag', { 'is-simulated': market.simulated }]"
              >{{ marketKind(market) }}</span
            >
          </div>
          <p v-if="market.goal" class="source-note">让球数 {{ market.goal }}</p>
          <p v-if="market.totalStake !== null" class="source-note">
            测算本金 {{ drawProfitAmount(market.totalStake) }}
          </p>
          <div class="profit-grid quote-heading">
            <span>项目</span
            ><span v-for="outcome in market.outcomes" :key="outcome.outcome">{{
              outcome.label
            }}</span>
          </div>
          <div class="profit-grid">
            <span class="row-label">支持率</span
            ><span
              v-for="outcome in market.outcomes"
              :key="outcome.outcome"
              :title="
                outcome.support === null ? outcome.unavailableReason : undefined
              "
              >{{ percent(outcome.support) }}</span
            >
          </div>
          <div class="profit-grid">
            <span class="row-label">盈亏额</span
            ><span
              v-for="outcome in market.outcomes"
              :key="outcome.outcome"
              :class="profitTone(outcome.profit)"
              :title="
                outcome.profit === null
                  ? outcome.unavailableReason
                  : drawProfitAmount(outcome.profit, true)
              "
              >{{ amountText(outcome.profit) }}</span
            >
          </div>
          <div class="profit-grid">
            <span class="row-label">{{
              market.simulated ? "测算ROI" : "源站率"
            }}</span><span
              v-for="outcome in market.outcomes"
              :key="outcome.outcome"
              :class="profitTone(outcome.rate)"
              :title="
                outcome.rate === null ? outcome.unavailableReason : undefined
              "
              >{{ percent(outcome.rate, true) }}</span
            >
          </div>
          <p v-if="!market.simulated" class="detail-note">
            源站率与本地测算金额分别展示，可能采用不同口径。
          </p>
          <details v-if="market.note" class="market-explanation">
            <summary>查看数据说明</summary>
            <p>{{ market.note }}</p>
          </details>
        </section>
      </template>
    </div>
  </section>
</template>

<style scoped>
.draw-details {
  min-width: 0;
  color: #172b4d;
  background: #fff;
  font-size: 15px;
  line-height: 1.55;
}
.draw-details *,
.draw-details *::before,
.draw-details *::after {
  box-sizing: border-box;
}
.detail-tabs {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 4px;
  padding: 4px;
  border: 1px solid #e5eaf2;
  border-radius: 12px;
  background: #f5f8fc;
}
.detail-tabs button {
  min-width: 0;
  min-height: 44px;
  padding: 8px;
  border: 0;
  border-radius: 9px;
  color: #60718b;
  background: transparent;
  font: inherit;
  font-weight: 600;
  cursor: pointer;
  touch-action: manipulation;
}
.detail-tabs button.is-active {
  color: #2563eb;
  background: #fff;
  box-shadow: 0 1px 5px #172b4d0d;
}
.detail-tabs button:focus-visible,
summary:focus-visible {
  outline: 2px solid #2563eb;
  outline-offset: 2px;
}
.detail-panel {
  min-width: 0;
  padding-top: 14px;
}
.detail-note,
.source-note {
  margin: 0 0 10px;
  color: #6b7b91;
  font-size: 13px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}
.source-note {
  margin: 4px 0 8px;
}
.history-block,
.data-block {
  min-width: 0;
  margin: 0 0 10px;
  border: 1px solid #e5eaf2;
  border-radius: 12px;
  background: #fff;
}
.history-block > summary {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 48px;
  padding: 11px 12px;
  list-style: none;
  cursor: pointer;
  touch-action: manipulation;
}
summary::-webkit-details-marker {
  display: none;
}
.history-title {
  min-width: 0;
  flex: 1;
  overflow-wrap: anywhere;
  font-weight: 600;
}
.history-count {
  flex: 0 0 auto;
  font-size: 13px;
  color: #60718b;
}
.expand-icon {
  color: #8896aa;
  transition: transform 0.15s;
}
.history-block[open] > summary .expand-icon {
  transform: rotate(180deg);
}
.history-list {
  list-style: none;
  margin: 0;
  padding: 0 12px;
}
.history-list li {
  padding: 12px 0;
  border-top: 1px solid #edf1f6;
}
.history-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 10px;
  margin-bottom: 7px;
  color: #7b889b;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.history-match {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 54px minmax(0, 1fr);
  align-items: center;
  gap: 6px;
  font-size: 14px;
}
.history-match > span {
  overflow-wrap: anywhere;
}
.history-match > span:last-child {
  text-align: right;
}
.history-match strong {
  text-align: center;
  font-size: 15px;
  font-variant-numeric: tabular-nums;
}
.draw-score {
  color: #2563eb;
}
.empty-state {
  margin: 0;
  padding: 20px 12px;
  color: #7b889b;
  text-align: center;
  font-size: 14px;
  line-height: 1.7;
}
.data-block {
  padding: 14px 10px;
}
.data-block h4 {
  margin: 0 0 8px;
  font-size: 15px;
  font-weight: 650;
  overflow-wrap: anywhere;
}
.quote-grid,
.profit-grid {
  display: grid;
  grid-template-columns: 42px repeat(3, minmax(0, 1fr));
  align-items: center;
  min-height: 40px;
  gap: 4px;
  padding: 6px 0;
  font-size: 14px;
  line-height: 1.55;
  font-variant-numeric: tabular-nums;
}
.quote-grid > span,
.profit-grid > span {
  min-width: 0;
  text-align: center;
  overflow-wrap: anywhere;
}
.quote-grid > span:first-child,
.profit-grid > span:first-child {
  text-align: left;
}
.quote-heading {
  min-height: 34px;
  color: #8190a5;
  font-size: 13px;
  border-bottom: 1px solid #edf1f6;
}
.row-label {
  color: #64758e;
  font-size: 13px;
}
.line-section {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid #edf1f6;
}
.market-subtitle {
  font-size: 15px;
  font-weight: 600;
}
.data-block > .detail-note:last-child {
  margin: 8px 0 0;
}
.profit-title {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
}
.profit-title h4 {
  flex: 1 1 100px;
  margin: 0;
}
.market-tag {
  padding: 4px 7px;
  border-radius: 5px;
  background: #eff6ff;
  color: #2563eb;
  font-size: 13px;
}
.market-tag.is-simulated {
  background: #fff8ed;
  color: #a46b19;
}
.profit-grid {
  grid-template-columns: 48px repeat(3, minmax(0, 1fr));
  font-size: 14px;
}
.is-positive {
  color: #c44949;
}
.is-negative {
  color: #14856b;
}
.market-explanation {
  margin-top: 6px;
  border-top: 1px solid #edf1f6;
}
.market-explanation summary {
  display: flex;
  align-items: center;
  min-height: 44px;
  padding: 6px 0;
  list-style: none;
  color: #2563eb;
  font-size: 13px;
  cursor: pointer;
  touch-action: manipulation;
}
.market-explanation p {
  margin: 0 0 4px;
  color: #6b7b91;
  font-size: 13px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}
@media (max-width: 360px) {
  .data-block {
    padding: 12px 8px;
  }
  .history-block > summary {
    padding: 10px;
    gap: 5px;
  }
  .profit-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 4px 6px;
    padding: 8px 0;
  }
  .profit-grid > span:first-child {
    grid-column: 1 / -1;
  }
  .profit-grid.quote-heading > span:first-child {
    display: none;
  }
  .profit-grid + .profit-grid {
    border-top: 1px solid #edf1f6;
  }
}
@media (prefers-reduced-motion: reduce) {
  .expand-icon {
    transition: none;
  }
}
</style>
