<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  getDrawObservations,
  loadDrawStandings,
  clearDrawStandingsCache,
  type DrawStandings,
} from "@/api/drawObservations";
import { resolveAssetUrl } from "@/api/request";
import {
  beijingToday,
  cardPresentation,
  isConfirmed,
  kickoffTime,
  shiftDay,
} from "@/utils/drawObservationPresentation";
import type {
  DrawObservation,
  DrawObservationQuery,
  DrawObservationsResponse,
  DrawMatchStatusFilter,
  DrawSelectionFilter,
} from "@/types/drawHistory";
import {
  drawMatchStatusOptions,
  validateDrawObservationFilters,
} from "../../../admin/src/utils/drawObservationFilters";
import MobileDrawDetails from "./MobileDrawDetails.vue";
import { drawWilliamSportteryDifferences } from "../../../admin/src/utils/drawFrontend";

const router = useRouter();
const route = useRoute();
function validDay(value: unknown): value is string {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value))
    return false;
  const day = new Date(`${value}T12:00:00Z`);
  return (
    Number.isFinite(day.getTime()) && day.toISOString().slice(0, 10) === value
  );
}
const date = ref(
  validDay(route.query.date) ? String(route.query.date) : beijingToday(),
);
const scope = ref<"sporttery" | "all">("sporttery");
const mode = ref<"today" | "history" | "counts">("today");
const selection = ref<DrawSelectionFilter>("all");
const status = ref<DrawMatchStatusFilter>("all");
const league = ref("");
const search = ref("");
const report = ref<DrawObservationsResponse | null>(null);
const items = ref<DrawObservation[]>([]);
const page = ref(1);
const loading = ref(false);
const loadingMore = ref(false);
const error = ref("");
const showMetrics = ref(false);
const expanded = reactive(new Set<string>());
const standings = reactive<Record<string, DrawStandings>>({});
const standingErrors = reactive<Record<string, boolean>>({});
const logoErrors = reactive(new Set<string>());
const countRanges = reactive({ home: ["", ""], away: ["", ""], h2h: ["", ""] });
const rangeError = ref("");
const filterDraft = reactive({
  league: "",
  status: "all" as DrawMatchStatusFilter,
  search: "",
});
const filterButton = ref<HTMLButtonElement | null>(null);
const filterDialog = ref<HTMLDialogElement | null>(null);
const modalFirstInput = ref<HTMLInputElement | null>(null);
const countLabels = [
  { key: "home" as const, label: "主队近 10 场", max: 10 },
  { key: "away" as const, label: "客队近 10 场", max: 10 },
  { key: "h2h" as const, label: "双方历史交锋", max: undefined },
];
const modes = [
  { value: "today" as const, label: "比赛观察" },
  { value: "history" as const, label: "历史平局" },
  { value: "counts" as const, label: "平局次数" },
];
let sequence = 0;
let controller: AbortController | undefined;
let appliedRanges: Partial<DrawObservationQuery> = {};

const metrics = computed(() => report.value?.observationMetrics);
const cards = computed(() =>
  items.value.map((row) => ({
    row,
    view: cardPresentation(row),
    priceDifferences: drawWilliamSportteryDifferences(row.companyQuotes),
  })),
);
const hasMore = computed(() => items.value.length < (report.value?.total || 0));
const statuses = computed(() =>
  drawMatchStatusOptions(
    report.value?.matchStatusOptions || [],
    filterDraft.status,
  ),
);
const activeFilters = computed(
  () =>
    Number(!!league.value) +
    Number(status.value !== "all") +
    Number(!!search.value),
);
const precision = computed(() => {
  const m = metrics.value;
  return m?.status === "available" &&
    m.original.settled > 0 &&
    typeof m.original.precision === "number"
    ? `${(m.original.precision * 100).toFixed(1)}%`
    : "—";
});
const dateLabel = computed(() => date.value.replace(/-/g, "."));
const metricFootnote = computed(() =>
  metrics.value?.original.settled
    ? `${metrics.value.original.hits} / ${metrics.value.original.settled} 场已结算`
    : "入选比赛尚无结算",
);

function countQuery(): Partial<DrawObservationQuery> | null {
  if (mode.value !== "counts") return {};
  const result: Record<string, number> = {};
  for (const group of countLabels) {
    const values = countRanges[group.key].map((v) =>
      String(v ?? "").trim() === "" ? undefined : Number(v),
    );
    if (
      values.some(
        (v) =>
          v !== undefined &&
          (!Number.isSafeInteger(v) ||
            v < 0 ||
            (group.max !== undefined && v > group.max)),
      ) ||
      (values[0] !== undefined &&
        values[1] !== undefined &&
        values[0] > values[1])
    ) {
      rangeError.value = `${group.label}请填写有效次数，最少不能大于最多。`;
      return null;
    }
    if (values[0] !== undefined) result[`${group.key}_draws_min`] = values[0];
    if (values[1] !== undefined) result[`${group.key}_draws_max`] = values[1];
  }
  rangeError.value = "";
  return result;
}

async function supplementStandings(rows: DrawObservation[], request: number) {
  const pending = rows
    .filter((row) => !standings[row.matchId])
    .map((row) => row.matchId);
  async function worker() {
    while (pending.length && request === sequence) {
      const id = pending.shift()!;
      try {
        const value = await loadDrawStandings(id);
        if (request !== sequence) return;
        standings[id] = value;
        delete standingErrors[id];
      } catch {
        if (request === sequence) standingErrors[id] = true;
      }
    }
  }
  await Promise.all(
    Array.from({ length: Math.min(4, pending.length) }, worker),
  );
}

async function load(append = false, refresh = false) {
  if (append && (loading.value || loadingMore.value || !hasMore.value)) return;
  const ranges = append ? appliedRanges : countQuery();
  if (!ranges || !validDay(date.value)) return;
  const request = ++sequence;
  controller?.abort();
  controller = new AbortController();
  const requestedPage = append ? page.value + 1 : 1;
  const query: DrawObservationQuery = {
    mode: mode.value === "today" ? "today" : "history",
    scope: scope.value,
    date: date.value,
    match_status: status.value,
    selection: selection.value,
    league: league.value || undefined,
    search: search.value || undefined,
    page: requestedPage,
    page_size: 20,
    ...ranges,
    ...(refresh ? { refresh: 1 as const } : {}),
  };
  error.value = "";
  if (append) loadingMore.value = true;
  else {
    loading.value = true;
    loadingMore.value = false;
    items.value = [];
    report.value = null;
    page.value = 1;
    expanded.clear();
  }
  if (refresh) {
    clearDrawStandingsCache();
    for (const id of Object.keys(standings)) delete standings[id];
    for (const id of Object.keys(standingErrors)) delete standingErrors[id];
  }
  try {
    const { data } = await getDrawObservations(query, controller.signal);
    if (request !== sequence) return;
    if (!data || !Array.isArray(data.items))
      throw new Error("比赛数据暂时无法读取，请重试。");
    validateDrawObservationFilters(data, query);
    report.value = data;
    appliedRanges = { ...ranges };
    items.value = append
      ? [
          ...new Map(
            [...items.value, ...data.items].map((row) => [row.matchId, row]),
          ).values(),
        ]
      : data.items;
    page.value = requestedPage;
    void supplementStandings(items.value, request);
    void router.replace({ query: { ...route.query, date: date.value } });
  } catch (cause: unknown) {
    if (request !== sequence || controller.signal.aborted) return;
    const e = cause as {
      response?: { data?: { error?: string } };
      message?: string;
    };
    error.value =
      e.response?.data?.error ||
      (e.message === "Network Error"
        ? "暂时无法连接分析服务，请稍后重试。"
        : e.message) ||
      "比赛加载失败，请重试。";
  } finally {
    if (request === sequence) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
}

function chooseScope(value: "all" | "sporttery") {
  scope.value = value;
  if (value === "sporttery" && selection.value === "non_sporttery")
    selection.value = "all";
}
function shiftDate(offset: number) {
  date.value = shiftDay(date.value, offset);
}
function today() {
  const value = beijingToday();
  if (date.value === value) void load();
  else date.value = value;
}
function toggleDetails(id: string) {
  expanded.has(id) ? expanded.delete(id) : expanded.add(id);
}
function openFilters() {
  Object.assign(filterDraft, {
    league: league.value,
    status: status.value,
    search: search.value,
  });
  nextTick(() => {
    filterDialog.value?.showModal();
    modalFirstInput.value?.focus();
  });
}
function closeFilters() {
  filterDialog.value?.close();
  filterButton.value?.focus();
}
function applyFilters() {
  league.value = filterDraft.league;
  status.value = filterDraft.status;
  search.value = filterDraft.search.trim();
  closeFilters();
}
function clearFilters() {
  const willReload =
    !!league.value ||
    status.value !== "all" ||
    !!search.value ||
    selection.value !== "all";
  league.value = "";
  status.value = "all";
  search.value = "";
  selection.value = "all";
  for (const group of countLabels) countRanges[group.key] = ["", ""];
  rangeError.value = "";
  if (!willReload) void load();
}
function shortState(row: DrawObservation) {
  return isConfirmed(row) ? "完赛" : row.displayState || "未开始";
}
function standingValue(
  id: string,
  side: "home" | "away",
  key: "rank" | "played",
) {
  return standings[id]?.[side][key] ?? "—";
}
function standingHint(id: string) {
  return standingErrors[id]
    ? "排名读取失败，可刷新重试"
    : "当前保存的联赛排名与已赛，历史记录不一定是本场赛前值";
}
function ratio(history?: DrawObservation["homeRecent"]) {
  return history?.sample ? `${history.draws}/${history.sample}` : "暂无";
}
function shortLabel(value: string, prefix: string) {
  return value.startsWith(prefix) ? value.slice(prefix.length).trim() : value;
}
function percent(value: number | null) {
  return value === null ? "—" : `${Number(value.toFixed(1))}%`;
}
function quote(value: unknown) {
  return value === null || value === undefined || value === ""
    ? "—"
    : String(value);
}

watch(
  [date, scope, mode, selection, status, league, search],
  () => {
    void load();
  },
  { flush: "post" },
);
watch(mode, () => {
  rangeError.value = "";
});
onMounted(() => {
  void load();
});
onBeforeUnmount(() => {
  sequence++;
  controller?.abort();
  filterDialog.value?.close();
});
</script>

<template>
  <div class="draw-mobile">
    <header class="observation-header">
      <div class="header-top">
        <div class="brand-lockup">
          <span class="brand-mark" aria-hidden="true">=</span>
          <div>
            <p class="eyebrow">比赛分析工作台</p>
            <h1>平局比赛观察</h1>
          </div>
        </div>
        <div class="header-actions">
          <RouterLink to="/analysis/classic" class="old-version"
            >旧版<span aria-hidden="true"> ↗</span></RouterLink
          ><button
            class="icon-button refresh"
            aria-label="刷新比赛数据"
            :disabled="loading"
            @click="load(false, true)"
          >
            <span
              class="material-symbols-outlined"
              :class="{ spinning: loading }"
              >refresh</span
            >
          </button>
        </div>
      </div>
      <div class="scope-row">
        <div class="scope-switch" aria-label="赛事范围">
          <button
            :aria-pressed="scope === 'sporttery'"
            :class="{ active: scope === 'sporttery' }"
            @click="chooseScope('sporttery')"
          >
            竞彩</button
          ><button
            :aria-pressed="scope === 'all'"
            :class="{ active: scope === 'all' }"
            @click="chooseScope('all')"
          >
            全部比赛
          </button>
        </div>
        <span class="timezone"><span class="live-dot"></span>北京时间</span>
      </div>
      <div class="date-navigation">
        <button class="icon-button" aria-label="前一天" @click="shiftDate(-1)">
          <span class="material-symbols-outlined">chevron_left</span></button
        ><label class="date-picker"
          ><span class="material-symbols-outlined" aria-hidden="true"
            >calendar_month</span
          ><input v-model="date" type="date" aria-label="比赛日期" /></label
        ><button class="today-button" @click="today">今天</button
        ><button class="icon-button" aria-label="后一天" @click="shiftDate(1)">
          <span class="material-symbols-outlined">chevron_right</span>
        </button>
      </div>
      <nav class="mode-tabs" aria-label="观察方式">
        <button
          v-for="tab in modes"
          :key="tab.value"
          :class="{ active: mode === tab.value }"
          :aria-pressed="mode === tab.value"
          @click="mode = tab.value"
        >
          {{ tab.label }}
        </button>
      </nav>
      <div class="metric-grid" aria-label="所选日期的统计">
        <div class="metric">
          <span>赛程比赛</span
          ><strong
            >{{ loading ? "—" : (metrics?.scopeMatches ?? report?.total ?? "—")
            }}<small>场</small></strong
          >
          <p>{{ scope === "sporttery" ? "竞彩赛事" : "全部赛事" }}</p>
        </div>
        <div class="metric">
          <span>竞彩入选平</span
          ><strong class="blue-number"
            >{{ loading ? "—" : (metrics?.original.selected ?? "—")
            }}<small>场</small></strong
          >
          <p>
            {{
              metrics ? `${metrics.original.pending} 场待结算` : "等待比赛数据"
            }}
          </p>
        </div>
        <button
          class="metric metric-action"
          :aria-expanded="showMetrics"
          @click="showMetrics = !showMetrics"
        >
          <span>入选平命中<span aria-hidden="true"> ↗</span></span
          ><strong
            :class="{ 'green-number': (metrics?.original.precision || 0) > 0 }"
            >{{ loading ? "—" : precision }}</strong
          >
          <p>{{ metricFootnote }}</p>
        </button>
      </div>
      <div v-if="showMetrics" class="metric-explanation">
        <p>
          <strong>统计口径</strong> ·
          所选日期、范围、联赛和搜索条件下的完整赛程，不限当前页。
        </p>
        <p>
          入选与命中统计仅针对竞彩；命中率＝入选中实际平局数 ÷
          已结算入选数。历史平局模式也使用完整赛程作比较。
        </p>
        <p v-if="metrics">
          二次保留 {{ metrics.secondary.selected }} 场，其中平局命中
          {{ metrics.secondary.hits }}/{{ metrics.secondary.settled }}
          场。“二次保留”是再次筛选平局，和主胜/客胜次选不同。
        </p>
      </div>
    </header>

    <main class="observation-content">
      <section
        v-if="mode === 'counts'"
        class="counts-panel"
        aria-labelledby="counts-title"
      >
        <div class="section-heading">
          <h2 id="counts-title">按平局次数筛选</h2>
          <span>历史实际平局</span>
        </div>
        <div v-for="group in countLabels" :key="group.key" class="count-row">
          <label :for="`count-${group.key}-min`">{{ group.label }}</label><input
            :id="`count-${group.key}-min`"
            v-model="countRanges[group.key][0]"
            inputmode="numeric"
            type="number"
            min="0"
            :max="group.max"
            :aria-label="`${group.label}最少平局次数`"
            placeholder="最少"
          /><span>—</span
          ><input
            v-model="countRanges[group.key][1]"
            inputmode="numeric"
            type="number"
            min="0"
            :max="group.max"
            :aria-label="`${group.label}最多平局次数`"
            placeholder="最多"
          />
        </div>
        <p v-if="rangeError" class="form-error" role="alert">
          {{ rangeError }}
        </p>
        <button class="primary-button" @click="load()">应用次数筛选</button>
      </section>
      <section class="list-toolbar" aria-label="比赛筛选">
        <div class="selection-tabs">
          <button
            :class="{ active: selection === 'all' }"
            :aria-pressed="selection === 'all'"
            @click="selection = 'all'"
          >
            全部</button
          ><button
            :class="{ active: selection === 'sporttery' }"
            :aria-pressed="selection === 'sporttery'"
            @click="selection = 'sporttery'"
          >
            竞彩入选</button
          ><button
            v-if="scope === 'all'"
            :class="{ active: selection === 'non_sporttery' }"
            :aria-pressed="selection === 'non_sporttery'"
            @click="selection = 'non_sporttery'"
          >
            非竞彩入选
          </button>
        </div>
        <button
          ref="filterButton"
          class="filter-button"
          :class="{ applied: activeFilters > 0 }"
          @click="openFilters"
        >
          <span class="material-symbols-outlined" aria-hidden="true">tune</span
          >筛选<span v-if="activeFilters" class="filter-count">{{
            activeFilters
          }}</span>
        </button>
      </section>
      <div class="list-meta">
        <span
          >{{ mode === "today" ? "当日赛程" : "历史实际平局"
          }}<b>{{ report?.total ?? "—" }}</b
          >场</span
        ><span>{{ dateLabel }}</span>
      </div>
      <div v-if="activeFilters" class="active-filter-row">
        <span v-if="league">{{ league }}</span><span v-if="status !== 'all'">{{
          statuses.find((item) => item.value === status)?.title || status
        }}</span
        ><span v-if="search">“{{ search }}”</span
        ><button @click="clearFilters">清除</button>
      </div>
      <div v-if="error" class="error-panel" role="alert">
        <span class="material-symbols-outlined" aria-hidden="true"
          >wifi_off</span
        >
        <h2>暂时没有加载成功</h2>
        <p>{{ error }}</p>
        <button class="primary-button" @click="load(!!items.length)">
          重新加载
        </button>
      </div>
      <div
        v-if="loading"
        class="skeleton-list"
        role="status"
        aria-live="polite"
        aria-label="正在加载比赛"
      >
        <div v-for="n in 3" :key="n" class="skeleton-card">
          <div class="skeleton-line"></div>
          <div class="skeleton-teams"><i></i><i></i></div>
          <div class="skeleton-line wide"></div>
          <div class="skeleton-line"></div>
        </div>
        <span class="sr-only">正在加载比赛数据</span>
      </div>
      <div v-else-if="!error && !items.length" class="empty-panel">
        <div class="empty-icon">
          <span class="material-symbols-outlined" aria-hidden="true"
            >sports_soccer</span
          >
        </div>
        <h2>当前条件下暂无比赛</h2>
        <p>试试其他日期，或调整入选和联赛筛选。</p>
        <div>
          <button @click="clearFilters">清除筛选</button
          ><button @click="today">回到今天</button>
        </div>
      </div>

      <div v-else class="match-cards" :aria-busy="loadingMore">
        <article
          v-for="{ row, view, priceDifferences } in cards"
          :key="row.matchId"
          class="match-card"
          :class="{ 'selected-card': view.selected }"
          :data-match-id="row.matchId"
        >
          <div class="card-topline">
            <div class="match-identification">
              <span class="league-badge">{{ row.league || "赛事" }}</span><span
                v-if="row.jingcaiId?.trim()"
                class="jc-number"
                >{{ row.jingcaiId }}</span
              ><span class="kickoff">{{ kickoffTime(row.matchTime) }}</span>
            </div>
            <span class="match-state" :class="{ finished: isConfirmed(row) }">{{
              shortState(row)
            }}</span>
          </div>
          <div class="teams-score">
            <div class="team">
              <div class="team-logo">
                <img
                  v-if="row.homeLogo && !logoErrors.has(`${row.matchId}-h`)"
                  :src="resolveAssetUrl(row.homeLogo)"
                  alt=""
                  loading="lazy"
                  @error="logoErrors.add(`${row.matchId}-h`)"
                /><span v-else>{{ row.home.slice(0, 2) }}</span>
              </div>
              <h2>{{ row.home }}</h2>
              <p :title="standingHint(row.matchId)">
                <span
                  >排名 {{ standingValue(row.matchId, "home", "rank") }}</span
                >
                <span
                  >已赛 {{ standingValue(row.matchId, "home", "played") }}</span
                >
              </p>
            </div>
            <div class="scoreboard">
              <strong v-if="isConfirmed(row)"
                >{{ row.homeScore }}<span>:</span>{{ row.awayScore }}</strong
              ><strong v-else class="versus">VS</strong
              ><span
                class="selection-badge"
                :data-tone="view.selected ? 'primary' : 'neutral'"
                >{{ view.selected ? "入选平" : view.primary.label }}</span
              ><span class="settlement-note">{{
                isConfirmed(row) ? "常规时间" : "待赛果"
              }}</span>
            </div>
            <div class="team">
              <div class="team-logo">
                <img
                  v-if="row.guestLogo && !logoErrors.has(`${row.matchId}-a`)"
                  :src="resolveAssetUrl(row.guestLogo)"
                  alt=""
                  loading="lazy"
                  @error="logoErrors.add(`${row.matchId}-a`)"
                /><span v-else>{{ row.guest.slice(0, 2) }}</span>
              </div>
              <h2>{{ row.guest }}</h2>
              <p :title="standingHint(row.matchId)">
                <span
                  >排名 {{ standingValue(row.matchId, "away", "rank") }}</span
                >
                <span
                  >已赛 {{ standingValue(row.matchId, "away", "played") }}</span
                >
              </p>
            </div>
          </div>
          <div class="prediction-grid">
            <div class="prediction primary-pick">
              <span class="prediction-caption">{{
                row.jingcaiId?.trim() ? "原模型 · 平局" : "非竞彩 · 平局"
              }}</span><strong
                :data-tone="view.primary.color"
                :title="view.primary.detail"
                >{{ view.primary.label }}</strong
              >
            </div>
            <div class="prediction">
              <span class="prediction-caption">胜负次选</span
              ><strong
                :data-tone="view.alternative.color"
                :title="view.alternative.detail"
                >{{ shortLabel(view.alternative.label, "次选") }}</strong
              ><small v-if="view.alternative.outcomeLabel">{{
                view.alternative.outcomeLabel
              }}</small>
            </div>
            <div class="prediction">
              <span class="prediction-caption">大小球</span
              ><strong :title="view.totals.reason">{{
                view.totals.label
              }}</strong
              ><small v-if="view.totals.outcomeLabel">{{
                view.totals.outcomeLabel
              }}</small>
            </div>
          </div>
          <div class="score-reference">
            <span
              ><span class="material-symbols-outlined" aria-hidden="true"
                >scoreboard</span
              >平局比分参考</span
            ><strong :title="view.score.detail">{{ view.score.label }}</strong>
          </div>
          <div class="probability-row" aria-label="胜平负参考概率">
            <div
              v-for="(label, index) in ['主胜', '平局', '客胜']"
              :key="label"
              :class="`probability-${index}`"
            >
              <p>
                <span>{{ label }}</span><b>{{
                  percent(view.probabilities[index] ?? null)
                }}</b>
              </p>
              <div class="probability-track">
                <i
                  :style="{
                    width: `${Math.max(0, Math.min(100, view.probabilities[index] || 0))}%`,
                  }"
                ></i>
              </div>
            </div>
          </div>
          <div class="recent-summary">
            <span
              >近10场平局 <b>{{ ratio(row.homeRecent) }}</b><span
                class="recent-divider"
                >/</span
              ><b>{{ ratio(row.awayRecent) }}</b></span
            ><span
              >交锋平局 <b>{{ ratio(row.headToHead) }}</b></span
            >
          </div>
          <div
            class="price-difference-row"
            aria-label="即时赔率差值：威廉减体彩，依次为胜、平、负"
            title="威廉即时赔率 − 体彩即时赔率；缺少报价显示 —"
          >
            <span class="price-difference-label">威廉−体彩</span>
            <span
              v-for="item in priceDifferences"
              :key="item.outcome"
              class="price-difference-outcome"
            >
              <span>{{ item.outcome }}</span>
              <strong
                :class="{
                  'difference-positive': item.difference !== null && item.difference > 0,
                  'difference-negative': item.difference !== null && item.difference < 0,
                }"
              >{{
                item.difference === null
                  ? "—"
                  : `${item.difference > 0 ? "+" : ""}${item.difference.toFixed(2)}`
              }}</strong>
            </span>
          </div>
          <div class="card-footer">
            <span class="secondary-chip" :title="view.secondary.detail">{{
              view.secondary.label
            }}</span><button
              :aria-expanded="expanded.has(row.matchId)"
              :aria-controls="`detail-${row.matchId}`"
              @click="toggleDetails(row.matchId)"
            >
              {{ expanded.has(row.matchId) ? "收起详情" : "战绩 / 指数 / 盈亏"
              }}<span class="material-symbols-outlined" aria-hidden="true">{{
                expanded.has(row.matchId) ? "expand_less" : "expand_more"
              }}</span>
            </button>
          </div>
          <div
            v-if="expanded.has(row.matchId)"
            :id="`detail-${row.matchId}`"
            class="card-expanded"
          >
            <MobileDrawDetails :row="row" />
            <div class="detail-meta">
              <p>
                模型评分
                {{
                  quote(
                    (row.jingcaiId?.trim()
                      ? row.frozenDrawSignal
                      : row.nonSportterySignal
                    )?.score?.toFixed(4),
                  )
                }}
                · 评分不等于命中概率
              </p>
              <p>{{ standingHint(row.matchId) }}</p>
              <p v-if="view.primary.detail">{{ view.primary.detail }}</p>
              <RouterLink :to="`/match/${row.matchId}`"
                >打开比赛资料 <span aria-hidden="true">↗</span></RouterLink
              >
            </div>
          </div>
        </article>
      </div>
      <button
        v-if="hasMore && !loading"
        class="load-more"
        :disabled="loadingMore"
        @click="load(true)"
      >
        {{
          loadingMore
            ? "正在加载…"
            : `加载更多（已显示 ${items.length} / ${report?.total}）`
        }}
      </button>
      <div v-if="items.length && !hasMore && !loading" class="list-end">
        <span></span>已显示全部 {{ items.length }} 场<span></span>
      </div>
      <p v-if="report?.generatedAt" class="updated-at">
        数据更新于 {{ kickoffTime(report.generatedAt) }} · 向下浏览更多比赛
      </p>
    </main>

    <nav class="mobile-nav" aria-label="页面导航">
      <RouterLink to="/"
        ><span class="material-symbols-outlined" aria-hidden="true"
          >sports_soccer</span
        ><span>赛程</span></RouterLink
      ><RouterLink to="/analysis" class="current" aria-current="page"
        ><span class="material-symbols-outlined" aria-hidden="true"
          >equalizer</span
        ><span>平局观察</span></RouterLink
      ><RouterLink to="/picks"
        ><span class="material-symbols-outlined" aria-hidden="true"
          >edit_note</span
        ><span>我的记录</span></RouterLink
      ><RouterLink to="/analysis/classic"
        ><span class="material-symbols-outlined" aria-hidden="true"
          >dashboard</span
        ><span>旧版工作台</span></RouterLink
      >
    </nav>

    <dialog
      ref="filterDialog"
      class="filter-dialog"
      aria-labelledby="filter-title"
      @cancel="filterButton?.focus()"
      @click="
        (event) => {
          if (event.target === filterDialog) closeFilters();
        }
      "
    >
      <div class="filter-sheet">
        <div class="sheet-handle"></div>
        <div class="sheet-title">
          <h2 id="filter-title">筛选比赛</h2>
          <button
            class="close-filter"
            aria-label="关闭筛选"
            @click="closeFilters"
          >
            <span class="material-symbols-outlined">close</span>
          </button>
        </div>
        <form @submit.prevent="applyFilters">
          <label
            >球队 / 比赛编号<input
              ref="modalFirstInput"
              v-model="filterDraft.search"
              placeholder="搜索球队或比赛 ID" /></label
          ><label
            >联赛<select v-model="filterDraft.league">
              <option value="">全部联赛</option>
              <option
                v-for="name in report?.leagues || []"
                :key="name"
                :value="name"
              >
                {{ name }}
              </option>
            </select></label
          ><label
            >比赛状态<select v-model="filterDraft.status">
              <option
                v-for="item in statuses"
                :key="item.value"
                :value="item.value"
              >
                {{ item.title }}
              </option>
            </select></label
          >
          <div class="sheet-actions">
            <button
              type="button"
              @click="
                Object.assign(filterDraft, {
                  league: '',
                  status: 'all',
                  search: '',
                })
              "
            >
              重置</button
            ><button type="submit" class="primary-button">查看比赛</button>
          </div>
        </form>
      </div>
    </dialog>
  </div>
</template>

<style scoped>
.draw-mobile {
  --blue: #397af6;
  --navy: #0c1426;
  --muted: #8e9bb1;
  min-height: 100dvh;
  background: var(--navy);
  color: #e9eef8;
  padding-bottom: calc(90px + env(safe-area-inset-bottom));
  font-family: Inter, "PingFang SC", "Microsoft YaHei", system-ui, sans-serif;
  font-variant-numeric: tabular-nums;
  font-size: 16px;
  line-height: 1.5;
}
.draw-mobile button,
.draw-mobile input,
.draw-mobile select {
  -webkit-tap-highlight-color: transparent;
  font: inherit;
}
.draw-mobile button {
  cursor: pointer;
}
.draw-mobile button:disabled {
  cursor: wait;
  opacity: 0.6;
}
.draw-mobile button:focus-visible,
.draw-mobile a:focus-visible {
  outline: 3px solid #7eb0ff;
  outline-offset: 3px;
}
.observation-header,
.observation-content {
  max-width: 820px;
  margin: 0 auto;
}
.observation-header {
  padding: 24px 18px 18px;
  background: radial-gradient(ellipse at 95% 0, #18345c66, transparent 65%);
}
.header-top,
.header-actions,
.brand-lockup,
.scope-row {
  display: flex;
  align-items: center;
}
.header-top {
  justify-content: space-between;
  gap: 10px;
}
.brand-lockup {
  gap: 10px;
  min-width: 0;
}
.brand-mark {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 34px;
  height: 36px;
  background: #3478f6;
  border-radius: 11px;
  color: #fff;
  font-size: 30px;
  font-weight: 800;
  line-height: 1;
  transform: rotate(-7deg);
}
.eyebrow {
  font-size: 13px;
  color: #93a5c1;
  letter-spacing: 1.5px;
  margin-bottom: 4px;
}
.header-top h1 {
  font-size: 24px;
  font-weight: 750;
  letter-spacing: -0.6px;
  line-height: 1.3;
  white-space: nowrap;
}
.header-actions {
  gap: 7px;
}
.old-version {
  color: #a7b6cc;
  font-size: 16px;
  text-decoration: none;
  white-space: nowrap;
  min-height: 44px;
  display: flex;
  align-items: center;
}
.icon-button {
  width: 42px;
  height: 44px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  color: #c1cde0;
  background: #172238;
  border: 1px solid #2a3850;
  border-radius: 11px;
}
.icon-button .material-symbols-outlined {
  font-size: 21px;
}
.refresh {
  border-color: #2b4c79;
  background: #1a3456;
  color: #8db8ff;
  width: 38px;
}
.scope-row {
  justify-content: space-between;
  margin-top: 22px;
}
.scope-switch {
  display: flex;
  gap: 3px;
  background: #080f1f;
  border: 1px solid #253149;
  padding: 3px;
  border-radius: 10px;
}
.scope-switch button {
  border: 0;
  background: none;
  color: #8f9eb6;
  padding: 0 16px;
  height: 44px;
  border-radius: 7px;
  font-size: 16px;
  font-weight: 650;
}
.scope-switch .active {
  background: #2b68df;
  color: white;
  box-shadow: 0 2px 8px #135bec30;
}
.timezone {
  font-size: 13px;
  color: #8ea0bc;
  display: flex;
  align-items: center;
  gap: 5px;
}
.live-dot {
  height: 5px;
  width: 5px;
  border-radius: 50%;
  background: #41cba2;
}
.date-navigation {
  display: grid;
  grid-template-columns: 42px minmax(0, 1fr) 48px 42px;
  gap: 7px;
  margin-top: 15px;
}
.date-picker {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 7px;
  background: #172238;
  border: 1px solid #2a3850;
  border-radius: 11px;
  padding: 0 10px;
}
.date-picker span {
  color: #7186a5;
  font-size: 17px;
}
.date-picker input {
  width: 100%;
  min-width: 0;
  max-width: 100%;
  height: 44px;
  background: none;
  color: #edf2fd;
  border: 0;
  font-size: 16px;
  font-weight: 650;
  color-scheme: dark;
  outline-offset: 2px;
}
.date-picker input::-webkit-calendar-picker-indicator {
  width: 12px;
  padding: 0;
  margin: 0;
  opacity: 0.6;
}
.today-button {
  background: #172238;
  border: 1px solid #2a3850;
  border-radius: 11px;
  font-size: 16px;
  color: #bccce3;
}
.mode-tabs {
  display: flex;
  border-bottom: 1px solid #26324a;
  gap: 22px;
  margin-top: 14px;
}
.mode-tabs button {
  position: relative;
  padding: 12px 0 13px;
  min-height: 46px;
  color: #8393ad;
  background: none;
  border: 0;
  font-size: 16px;
  font-weight: 600;
}
.mode-tabs button.active {
  color: #f3f7ff;
}
.mode-tabs button.active:after {
  content: "";
  position: absolute;
  height: 3px;
  background: var(--blue);
  bottom: -1px;
  left: 16%;
  right: 16%;
  border-radius: 4px;
}
.metric-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 9px;
  margin-top: 17px;
}
.metric {
  text-align: left;
  min-width: 0;
  border: 1px solid #29364b;
  background: #142035;
  border-radius: 11px;
  padding: 12px 10px;
}
.metric > span {
  display: block;
  color: #9babc2;
  font-size: 13px;
  line-height: 1.5;
}
.metric > strong {
  display: block;
  font-size: 28px;
  font-weight: 750;
  line-height: 1.3;
  margin: 5px 0;
  letter-spacing: -1px;
}
.metric strong small {
  font-size: 13px;
  color: #7185a3;
  letter-spacing: 0;
  margin-left: 5px;
  font-weight: 400;
}
.metric p {
  font-size: 13px;
  color: #a4b3ca;
  line-height: 1.5;
}
.blue-number {
  color: #77a8ff;
}
.green-number {
  color: #4bd5ac;
}
.metric-explanation {
  font-size: 14px;
  color: #a3b4cc;
  line-height: 1.8;
  background: #15223b;
  border-radius: 10px;
  padding: 12px;
  margin-top: 12px;
}
.metric-explanation p + p {
  margin-top: 7px;
}
.observation-content {
  padding: 0 12px;
}
.list-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
  border-top: 1px solid #202c43;
  padding: 11px 4px 6px;
}
.selection-tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}
.selection-tabs button {
  border: 0;
  background: none;
  color: #91a2bd;
  min-height: 42px;
  font-size: 14px;
  font-weight: 600;
  padding: 0 12px;
  white-space: nowrap;
  border-radius: 20px;
}
.selection-tabs button.active {
  background: #224375;
  color: #bad4ff;
}
.filter-button {
  display: flex;
  align-items: center;
  gap: 4px;
  color: #a8b8d1;
  background: none;
  border: 0;
  font-size: 16px;
  min-height: 44px;
  padding: 0 4px;
  white-space: nowrap;
}
.filter-button .material-symbols-outlined {
  font-size: 18px;
}
.filter-count {
  border-radius: 50%;
  background: #2e6ce4;
  color: white;
  font-size: 13px;
  width: 16px;
  height: 16px;
  display: grid;
  place-items: center;
}
.list-meta {
  display: flex;
  justify-content: space-between;
  font-size: 13px;
  color: #7f90ab;
  padding: 4px 5px 12px;
  letter-spacing: 0.3px;
}
.list-meta b {
  font-weight: 600;
  color: #c3d0e4;
  margin: 0 5px;
}
.match-cards {
  display: grid;
  gap: 14px;
}
.match-card {
  background: #fff;
  border: 1px solid #e6ecf5;
  border-radius: 15px;
  overflow: hidden;
  color: #26364d;
  box-shadow: 0 6px 15px #00000008;
  min-width: 0;
}
.selected-card {
  border-color: #5893ff;
}
.card-topline {
  padding: 10px 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  background: #f4f7fc;
  border-bottom: 1px solid #edf1f7;
  font-size: 13px;
}
.match-identification {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  flex-wrap: wrap;
}
.league-badge {
  color: #40516d;
  font-weight: 700;
}
.jc-number {
  color: #3678eb;
  background: #e5eeff;
  border-radius: 4px;
  padding: 3px 5px;
  font-size: 13px;
  font-weight: 600;
}
.kickoff {
  font-weight: 650;
  color: #7f8ea5;
}
.match-state {
  color: #687c98;
  white-space: nowrap;
  flex-shrink: 0;
}
.match-state.finished {
  color: #73829a;
}
.teams-score {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 74px minmax(0, 1fr);
  gap: 4px;
  padding: 19px 10px 16px;
  align-items: center;
}
.team {
  text-align: center;
  min-width: 0;
}
.team-logo {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  margin: 0 auto 8px;
  background: #f4f7fc;
  border: 1px solid #e9eef6;
  border-radius: 50%;
  color: #7a90b1;
  font-size: 16px;
  font-weight: 600;
}
.team-logo img {
  width: 32px;
  height: 32px;
  object-fit: contain;
}
.team h2 {
  font-size: 18px;
  font-weight: 750;
  line-height: 1.4;
  overflow-wrap: anywhere;
  min-height: 20px;
}
.team p {
  color: #697a93;
  font-size: 13px;
  margin-top: 6px;
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 2px 8px;
}
.team p span {
  white-space: nowrap;
}
.scoreboard {
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
}
.scoreboard > strong {
  font-size: 32px;
  color: #17243a;
  line-height: 1.15;
  letter-spacing: -1px;
  font-weight: 750;
  white-space: nowrap;
}
.scoreboard > strong span {
  color: #bbc6d6;
  margin: 0 5px;
  font-weight: 400;
}
.scoreboard .versus {
  font-size: 26px;
  letter-spacing: 2px;
  color: #a2afc2;
}
.selection-badge {
  display: block;
  font-size: 13px;
  font-weight: 650;
  padding: 3px 7px;
  border-radius: 5px;
  color: #8b9ab0;
  background: #eef2f7;
}
.selection-badge[data-tone="primary"] {
  background: #e8f0ff;
  color: #2969d9;
}
.settlement-note {
  font-size: 13px;
  color: #6f809b;
}
.prediction-grid {
  display: grid;
  grid-template-columns: 1.15fr 1fr 0.85fr;
  margin: 0 12px;
  background: #f6f8fc;
  border: 1px solid #e9eef6;
  border-radius: 9px;
  min-height: 72px;
}
.prediction {
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: flex-start;
  padding: 12px 9px;
  gap: 6px;
}
.prediction + .prediction {
  border-left: 1px solid #e4eaf3;
}
.prediction-caption {
  font-size: 13px;
  color: #697b96;
}
.prediction > strong {
  font-size: 16px;
  font-weight: 700;
  line-height: 1.5;
  overflow-wrap: anywhere;
  color: #314969;
}
.prediction > small {
  font-size: 12px;
  color: #9a7b4c;
  line-height: 1.4;
}
.prediction strong[data-tone="primary"] {
  color: #286add;
}
.prediction strong[data-tone="success"] {
  color: #15936c;
}
.prediction strong[data-tone="warning"],
.prediction strong[data-tone="error"] {
  color: #b77d36;
}
.prediction strong[data-tone="deep-purple"] {
  color: #8059bd;
}
.score-reference {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 10px 14px 0;
  font-size: 13px;
  color: #647793;
  gap: 8px;
  flex-wrap: wrap;
}
.score-reference > span {
  display: flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
}
.score-reference .material-symbols-outlined {
  font-size: 15px;
}
.score-reference strong {
  font-weight: 600;
  color: #6b7f9d;
  font-size: 13px;
  text-align: right;
}
.probability-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 9px;
  margin: 14px 14px 12px;
}
.probability-row p {
  display: flex;
  justify-content: space-between;
  font-size: 13px;
  color: #8190a6;
  margin-bottom: 5px;
  gap: 3px;
  flex-wrap: wrap;
}
.probability-row b {
  color: #4b5e78;
  font-size: 13px;
  font-weight: 650;
}
.probability-track {
  height: 4px;
  border-radius: 5px;
  background: #eaf0f6;
  overflow: hidden;
}
.probability-track i {
  display: block;
  height: 100%;
  border-radius: 5px;
  background: #30b68c;
}
.probability-1 i {
  background: #e6ad41;
}
.probability-2 i {
  background: #4b98f1;
}
.recent-summary {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  margin: 0 14px 12px;
  font-size: 13px;
  color: #687c98;
  flex-wrap: wrap;
}
.recent-summary b {
  color: #526987;
  font-weight: 600;
  margin-left: 3px;
}
.recent-divider {
  margin: 0 4px;
  color: #c3ccda;
}
.price-difference-row {
  display: grid;
  grid-template-columns: auto repeat(3, minmax(0, 1fr));
  align-items: baseline;
  gap: 6px;
  margin: 0 14px 12px;
  color: #526987;
  font-size: 14px;
  line-height: 1.5;
}
.price-difference-label {
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
}
.price-difference-outcome {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: flex-end;
  gap: 2px 4px;
  min-width: 0;
}
.price-difference-outcome strong {
  font-size: 14px;
  font-weight: 600;
  white-space: nowrap;
}
.price-difference-outcome .difference-positive {
  color: #bd3746;
}
.price-difference-outcome .difference-negative {
  color: #17805b;
}
.card-footer {
  border-top: 1px solid #eaf0f7;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  gap: 5px;
  min-height: 47px;
}
.secondary-chip {
  background: #f2f5f9;
  color: #6a7f9b;
  border-radius: 5px;
  padding: 4px 7px;
  font-size: 13px;
  line-height: 1.5;
  max-width: 42%;
}
.card-footer button {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  min-height: 46px;
  color: #5278b2;
  font-size: 13px;
  background: none;
  border: 0;
  text-align: right;
}
.card-footer button span {
  font-size: 18px;
}
.card-expanded {
  border-top: 1px solid #e8eef7;
}
.detail-meta {
  padding: 10px 14px 16px;
  font-size: 13px;
  color: #8c9aaf;
  line-height: 1.8;
  background: #fafbfd;
}
.detail-meta a {
  display: inline-block;
  margin-top: 9px;
  color: #3c78d6;
  min-height: 32px;
}
.active-filter-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin: 0 4px 12px;
  font-size: 13px;
}
.active-filter-row > span {
  background: #20324e;
  color: #bbcee8;
  border-radius: 5px;
  padding: 5px 8px;
}
.active-filter-row button {
  background: none;
  border: 0;
  color: #7097cf;
  min-height: 34px;
  padding: 0 8px;
}
.empty-panel,
.error-panel {
  text-align: center;
  padding: 42px 20px;
  color: #91a2bd;
}
.empty-panel h2,
.error-panel h2 {
  font-size: 16px;
  color: #d4deee;
  font-weight: 650;
  margin-top: 12px;
}
.empty-panel p,
.error-panel p {
  font-size: 16px;
  line-height: 1.8;
  margin: 9px 0 18px;
  overflow-wrap: anywhere;
}
.empty-icon {
  display: grid;
  place-items: center;
  width: 62px;
  height: 62px;
  border-radius: 20px;
  background: #182842;
  margin: auto;
  color: #6289c2;
}
.empty-icon span {
  font-size: 31px;
}
.empty-panel button {
  background: #20334f;
  color: #acc9f2;
  border: 1px solid #334d70;
  font-size: 16px;
  min-height: 44px;
  border-radius: 9px;
  padding: 0 15px;
  margin: 0 5px;
}
.primary-button {
  background: #3478f4;
  color: #fff;
  border: 0;
  border-radius: 10px;
  min-height: 44px;
  padding: 0 18px;
  font-size: 16px;
  font-weight: 650;
}
.load-more {
  width: 100%;
  min-height: 48px;
  background: #18263f;
  color: #a8c7f3;
  border: 1px solid #2a4366;
  border-radius: 11px;
  margin-top: 17px;
  font-size: 16px;
}
.list-end {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  font-size: 13px;
  color: #667c9d;
  padding: 23px 0 10px;
}
.list-end span {
  width: 22px;
  height: 1px;
  background: #2a3a53;
}
.updated-at {
  font-size: 13px;
  text-align: center;
  color: #506685;
  margin: 8px 0 15px;
}
.mobile-nav {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  z-index: 40;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  padding: 9px max(12px, calc((100vw - 780px) / 2))
    calc(9px + env(safe-area-inset-bottom));
  background: #101b2df5;
  border-top: 1px solid #2b3850;
  backdrop-filter: blur(15px);
}
.mobile-nav a {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  font-size: 13px;
  color: #7b8da9;
  min-height: 42px;
  text-decoration: none;
}
.mobile-nav .material-symbols-outlined {
  font-size: 26px;
}
.mobile-nav a.current {
  color: #74a9ff;
}
.mobile-nav a.current .material-symbols-outlined {
  background: #244576;
  border-radius: 8px;
  padding: 2px 12px;
}
.counts-panel {
  border: 1px solid #2b3d5b;
  background: #15223a;
  border-radius: 12px;
  padding: 14px;
  margin-bottom: 12px;
}
.section-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.section-heading h2 {
  font-size: 16px;
  font-weight: 650;
}
.section-heading span {
  font-size: 13px;
  color: #7f95b4;
}
.count-row {
  display: grid;
  grid-template-columns: 1.4fr 1fr 10px 1fr;
  align-items: center;
  gap: 7px;
  margin: 8px 0;
  font-size: 14px;
  color: #a4b8d6;
}
.count-row input {
  width: 100%;
  min-width: 0;
  background: #0d182c;
  border: 1px solid #304360;
  border-radius: 7px;
  color: white;
  padding: 8px;
  height: 40px;
}
.counts-panel > .primary-button {
  width: 100%;
  margin-top: 8px;
}
.form-error {
  font-size: 14px;
  color: #ffb3a2;
  margin: 8px 0;
}
.filter-dialog {
  border: 0;
  background: none;
  padding: 0;
  color: #26364d;
  width: 100%;
  max-width: 520px;
  max-height: 90dvh;
  margin: auto auto 0;
  border-radius: 20px 20px 0 0;
}
.filter-dialog::backdrop {
  background: #020815b8;
  backdrop-filter: blur(3px);
}
.filter-sheet {
  background: #fff;
  padding: 10px 22px calc(24px + env(safe-area-inset-bottom));
}
.sheet-handle {
  height: 4px;
  width: 36px;
  border-radius: 4px;
  background: #dce3ed;
  margin: 0 auto 12px;
}
.sheet-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 17px;
}
.sheet-title h2 {
  font-size: 22px;
  font-weight: 700;
}
.close-filter {
  display: grid;
  place-items: center;
  height: 44px;
  width: 44px;
  background: #f2f5f9;
  border: 0;
  border-radius: 50%;
  color: #7d8da4;
}
.filter-sheet form > label {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 17px 0;
  font-size: 16px;
  color: #657b98;
}
.filter-sheet input,
.filter-sheet select {
  width: 100%;
  height: 48px;
  padding: 0 12px;
  border: 1px solid #dfe6f0;
  border-radius: 9px;
  background: #f9fbfd;
  color: #283d5b;
  font-size: 16px;
}
.sheet-actions {
  display: grid;
  grid-template-columns: 1fr 2fr;
  gap: 10px;
  margin-top: 25px;
}
.sheet-actions button:first-child {
  min-height: 48px;
  border: 1px solid #dce4f0;
  background: #f0f4fa;
  border-radius: 10px;
  color: #6880a0;
  font-size: 16px;
}
.skeleton-list {
  display: grid;
  gap: 14px;
}
.skeleton-card {
  height: 285px;
  background: #17243a;
  border-radius: 15px;
  padding: 20px;
  animation: pulse 1.4s ease-in-out infinite;
}
.skeleton-line {
  height: 13px;
  width: 55%;
  background: #24344e;
  border-radius: 7px;
  margin-bottom: 18px;
}
.skeleton-line.wide {
  width: 100%;
  height: 60px;
}
.skeleton-teams {
  display: flex;
  justify-content: space-around;
  margin: 25px 0;
}
.skeleton-teams i {
  height: 50px;
  width: 50px;
  background: #24344e;
  border-radius: 50%;
}
.spinning {
  animation: spin 1.2s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
@keyframes pulse {
  50% {
    opacity: 0.5;
  }
}
@media (min-width: 640px) {
  .observation-header {
    padding: 30px 24px 20px;
  }
  .header-top h1 {
    font-size: 27px;
  }
  .observation-content {
    padding: 0 20px;
  }
  .metric-grid {
    gap: 14px;
  }
  .metric {
    padding: 16px;
  }
  .metric > span {
    font-size: 16px;
  }
  .metric > strong {
    font-size: 30px;
  }
  .metric p {
    font-size: 14px;
  }
  .match-cards {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: start;
  }
  .scope-switch button {
    height: 44px;
  }
  .mode-tabs {
    gap: 30px;
  }
  .mode-tabs button {
    font-size: 18px;
  }
  .filter-dialog {
    margin: auto;
    border-radius: 20px;
  }
  .filter-sheet {
    padding-bottom: 24px;
  }
  .teams-score {
    grid-template-columns: minmax(0, 1fr) 66px minmax(0, 1fr);
  }
}
@media (max-width: 359px) {
  .observation-header {
    padding-left: 12px;
    padding-right: 12px;
  }
  .header-top h1 {
    font-size: 22px;
  }
  .brand-mark {
    width: 28px;
    height: 31px;
    font-size: 28px;
  }
  .brand-lockup {
    gap: 7px;
  }
  .header-actions {
    gap: 4px;
  }
  .date-navigation {
    grid-template-columns: 36px minmax(0, 1fr) 42px 36px;
    gap: 5px;
  }
  .date-navigation > .icon-button {
    width: 36px;
  }
  .date-picker {
    padding: 0 7px;
  }
  .date-picker > span {
    display: none;
  }
  .metric {
    padding: 11px 8px;
  }
  .metric > strong {
    font-size: 26px;
  }
  .metric p {
    font-size: 12px;
  }
  .observation-content {
    padding: 0 9px;
  }
  .selection-tabs button {
    padding: 0 8px;
    font-size: 13px;
  }
  .team h2 {
    font-size: 16px;
  }
  .team p {
    font-size: 13px;
  }
  .prediction {
    padding-left: 7px;
    padding-right: 5px;
  }
  .prediction > strong {
    font-size: 16px;
  }
  .jc-number {
    font-size: 12px;
  }
  .recent-summary {
    font-size: 12px;
  }
  .card-footer button {
    font-size: 13px;
  }
}
@media (prefers-reduced-motion: reduce) {
  .spinning,
  .skeleton-card {
    animation: none;
  }
}
</style>
