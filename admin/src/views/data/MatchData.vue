<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { deleteCrawlerMatch, getCrawlerMatchDetail, getCrawlerMatches } from '@/api'

interface Match {
  id: number
  match_id: string
  date: string
  league: string
  home: string
  guest: string
  scores: string
  home_score: number
  guest_score: number
  status: string
  home_logo: string
  guest_logo: string
  match_time: string
}

type CrawlerRecord = Record<string, unknown>

interface MatchDetail {
  match: CrawlerRecord
  history: CrawlerRecord
  odds: CrawlerRecord
  pankou: CrawlerRecord
}

const matches = ref<Match[]>([])
const total = ref(0)
const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const selectedDate = ref('')
const selectedLeague = ref('')
const leagues = ref<string[]>([])
const dates = ref<string[]>([])

const detailDialog = ref(false)
const detailLoading = ref(false)
const detailTab = ref('match')
const matchDetail = ref<MatchDetail | null>(null)

const deleteDialog = ref(false)
const deleteMatchId = ref('')

const historyHeaders = [
  { title: '时间', key: 'matchTime' },
  { title: '联赛', key: 'league' },
  { title: '主队', key: 'home' },
  { title: '比分', key: 'score' },
  { title: '客队', key: 'guest' },
  { title: '半场', key: 'halfScore' },
]

const oddsHeaders = [
  { title: '公司', key: 'companyName' },
  { title: '初赔', key: 'firstOdds' },
  { title: '即时', key: 'odds' },
  { title: '初始返还率', key: 'firstReturnRatio' },
  { title: '返还率', key: 'returnRatio' },
]

const pankouHeaders = [
  { title: '公司', key: 'companyName' },
  { title: '初盘', key: 'firstPankou' },
  { title: '即时盘', key: 'pankou' },
  { title: '初赔', key: 'firstOdds' },
  { title: '即时赔', key: 'odds' },
  { title: '返还率', key: 'returnRatio' },
]

async function fetchMatches() {
  loading.value = true
  try {
    const { data } = await getCrawlerMatches({
      page: page.value,
      page_size: pageSize.value,
      keyword: keyword.value,
      date: selectedDate.value,
      league: selectedLeague.value,
    })
    matches.value = data.list || []
    total.value = data.total || 0
    leagues.value = data.leagues || []
    dates.value = data.dates || []
  } finally {
    loading.value = false
  }
}

onMounted(fetchMatches)

function handleSearch() {
  page.value = 1
  fetchMatches()
}

async function showDetail(matchId: string) {
  detailLoading.value = true
  detailDialog.value = true
  detailTab.value = 'match'
  try {
    const { data } = await getCrawlerMatchDetail(matchId)
    matchDetail.value = data as MatchDetail
  } finally {
    detailLoading.value = false
  }
}

function openDelete(matchId: string) {
  deleteMatchId.value = matchId
  deleteDialog.value = true
}

async function handleDelete() {
  await deleteCrawlerMatch(deleteMatchId.value)
  deleteDialog.value = false
  fetchMatches()
}

function getStatusColor(status: string) {
  if (status === '已结束' || status === 'finished' || status === '完场') return 'success'
  if (status === '进行中' || status === 'playing') return 'warning'
  return 'info'
}

function logoUrl(path: string) {
  if (!path) return ''
  try {
    const url = new URL(path)
    if (url.hostname.endsWith('vipc.cn') && url.pathname.includes('/vipc-sport/image/')) {
      const filename = url.pathname.split('/').filter(Boolean).pop()
      return filename ? `/footballimg/${filename}` : path
    }
  } catch {
    return path
  }
  return path
}

function recordEntries(record?: CrawlerRecord) {
  if (!record) return []
  return Object.entries(record)
    .filter(([, value]) => !isComplex(value))
    .map(([key, value]) => ({ key, value: formatValue(value) }))
}

function valueByKeys(record: CrawlerRecord | undefined, keys: string[]) {
  if (!record) return undefined
  for (const key of keys) {
    if (record[key] !== undefined && record[key] !== null) return record[key]
  }
  return undefined
}

function asRecord(value: unknown): CrawlerRecord {
  const parsed = parseMaybeJSON(value)
  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed as CrawlerRecord
  return {}
}

function asArray(value: unknown): CrawlerRecord[] {
  const parsed = parseMaybeJSON(value)
  if (Array.isArray(parsed)) return parsed.filter((item) => item && typeof item === 'object') as CrawlerRecord[]
  if (parsed && typeof parsed === 'object') {
    const record = parsed as CrawlerRecord
    if (Array.isArray(record.list)) return record.list as CrawlerRecord[]
    if (Array.isArray(record.data)) return record.data as CrawlerRecord[]
  }
  return []
}

function parseMaybeJSON(value: unknown): unknown {
  if (typeof value !== 'string') return value
  const trimmed = value.trim()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return value
  try {
    return JSON.parse(trimmed)
  } catch {
    return value
  }
}

function historySummary(record: CrawlerRecord, keys: string[]) {
  return asRecord(parseMaybeJSON(valueByKeys(record, keys)))
}

function historyRows(record: CrawlerRecord, keys: string[]) {
  return asArray(valueByKeys(record, keys)).map((item) => ({
    matchTime: textValue(item.matchTime ?? item.match_time),
    league: textValue(item.league),
    home: textValue(item.home),
    guest: textValue(item.guest),
    score: scoreValue(item.goal ?? item.score),
    halfScore: scoreValue(item.halfGoal ?? item.half_score),
  }))
}

function oddsRows(record: CrawlerRecord) {
  return asArray(valueByKeys(record, ['data', 'Data'])).map((item) => ({
    companyName: textValue(item.companyName ?? item.company_name),
    firstOdds: listValue(item.firstOdds ?? item.first_odds),
    odds: listValue(item.odds),
    firstReturnRatio: textValue(item.firstReturnRatio ?? item.first_return_ratio),
    returnRatio: textValue(item.returnRatio ?? item.return_ratio),
  }))
}

function pankouRows(record: CrawlerRecord, keys: string[]) {
  return asArray(valueByKeys(record, keys)).map((item) => ({
    companyName: textValue(item.companyName ?? item.company_name),
    firstPankou: textValue(item.firstPankou ?? item.first_pankou),
    pankou: textValue(item.pankou),
    firstOdds: listValue(item.firstOdds ?? item.first_odds),
    odds: listValue(item.odds),
    returnRatio: textValue(item.returnRatio ?? item.return_ratio),
  }))
}

function rankValue(record: CrawlerRecord) {
  return parseMaybeJSON(valueByKeys(record, ['rank_data', 'rankData', 'rank']))
}

function rankSections(record: CrawlerRecord) {
  const value = rankValue(record)
  const sections: Array<{ title: string; rows: CrawlerRecord[] }> = []

  if (Array.isArray(value)) {
    sections.push({ title: '积分榜', rows: normalizeRankRows(value) })
    return sections
  }

  if (!value || typeof value !== 'object') return sections

  const root = value as CrawlerRecord
  for (const [key, item] of Object.entries(root)) {
    if (Array.isArray(item)) {
      sections.push({ title: key, rows: normalizeRankRows(item) })
      continue
    }
    if (item && typeof item === 'object') {
      const nested = item as CrawlerRecord
      for (const [nestedKey, nestedItem] of Object.entries(nested)) {
        if (Array.isArray(nestedItem)) {
          sections.push({ title: `${key}.${nestedKey}`, rows: normalizeRankRows(nestedItem) })
        }
      }
    }
  }

  return sections
}

function normalizeRankRows(rows: unknown[]) {
  return rows
    .filter((row) => row && typeof row === 'object')
    .map((row) => row as CrawlerRecord)
}

function rankColumns(rows: CrawlerRecord[]) {
  const keys = new Set<string>()
  for (const row of rows.slice(0, 5)) {
    Object.keys(row).forEach((key) => keys.add(key))
  }
  return Array.from(keys)
}

function summaryItems(summary: CrawlerRecord) {
  return Object.entries(summary).map(([key, value]) => ({ key, value: formatValue(value) }))
}

function rawJSON(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2)
}

function isComplex(value: unknown) {
  const parsed = parseMaybeJSON(value)
  return Boolean(parsed && typeof parsed === 'object')
}

function formatValue(value: unknown) {
  const parsed = parseMaybeJSON(value)
  if (Array.isArray(parsed)) return parsed.join(' / ')
  if (parsed && typeof parsed === 'object') return JSON.stringify(parsed)
  return String(parsed ?? '-')
}

function textValue(value: unknown) {
  return String(value ?? '-')
}

function listValue(value: unknown) {
  const parsed = parseMaybeJSON(value)
  if (Array.isArray(parsed)) return parsed.join(' / ')
  return String(parsed ?? '-')
}

function scoreValue(value: unknown) {
  const parsed = parseMaybeJSON(value)
  if (Array.isArray(parsed)) return parsed.join(':')
  return String(parsed ?? '-')
}
</script>

<template>
  <div>
    <h2 class="text-h5 font-weight-bold mb-4">比赛数据</h2>

    <v-card class="mb-4">
      <v-card-text>
        <v-row>
          <v-col cols="12" md="3">
            <v-text-field
              v-model="keyword"
              label="搜索"
              prepend-inner-icon="mdi-magnify"
              placeholder="球队/联赛名称"
              clearable
              hide-details
              @keyup.enter="handleSearch"
            />
          </v-col>
          <v-col cols="12" md="3">
            <v-select
              v-model="selectedDate"
              :items="['', ...dates]"
              label="日期"
              clearable
              hide-details
              @update:model-value="handleSearch"
            />
          </v-col>
          <v-col cols="12" md="3">
            <v-select
              v-model="selectedLeague"
              :items="['', ...leagues]"
              label="联赛"
              clearable
              hide-details
              @update:model-value="handleSearch"
            />
          </v-col>
          <v-col cols="12" md="2">
            <v-btn color="primary" @click="handleSearch">搜索</v-btn>
            <v-btn class="ml-2" @click="keyword = ''; selectedDate = ''; selectedLeague = ''; handleSearch()">重置</v-btn>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <v-card>
      <v-data-table-server
        :headers="[
          { title: 'Match ID', key: 'match_id', width: 100 },
          { title: '日期', key: 'date', width: 100 },
          { title: '联赛', key: 'league' },
          { title: '主队', key: 'home' },
          { title: '比分', key: 'scores', width: 80 },
          { title: '客队', key: 'guest' },
          { title: '状态', key: 'status', width: 100 },
          { title: '操作', key: 'actions', sortable: false, width: 150 },
        ]"
        :items="matches"
        :items-length="total"
        :loading="loading"
        :items-per-page="pageSize"
        :page="page"
        @update:page="page = $event; fetchMatches()"
        @update:items-per-page="pageSize = $event; fetchMatches()"
      >
        <template #item.league="{ item }">
          <v-chip size="small" color="primary" variant="tonal">{{ item.league || '-' }}</v-chip>
        </template>

        <template #item.home="{ item }">
          <div class="d-flex align-center">
            <v-avatar v-if="item.home_logo" size="24" class="mr-2">
              <v-img :src="logoUrl(item.home_logo)" />
            </v-avatar>
            {{ item.home }}
          </div>
        </template>

        <template #item.scores="{ item }">
          <strong>{{ item.scores || '-' }}</strong>
        </template>

        <template #item.guest="{ item }">
          <div class="d-flex align-center">
            <v-avatar v-if="item.guest_logo" size="24" class="mr-2">
              <v-img :src="logoUrl(item.guest_logo)" />
            </v-avatar>
            {{ item.guest }}
          </div>
        </template>

        <template #item.status="{ item }">
          <v-chip :color="getStatusColor(item.status)" size="small" variant="tonal">
            {{ item.status || '未知' }}
          </v-chip>
        </template>

        <template #item.actions="{ item }">
          <v-btn size="small" variant="text" color="primary" @click="showDetail(item.match_id)">详情</v-btn>
          <v-btn size="small" variant="text" color="error" @click="openDelete(item.match_id)">删除</v-btn>
        </template>
      </v-data-table-server>
    </v-card>

    <v-dialog v-model="detailDialog" max-width="1200" scrollable>
      <v-card>
        <v-card-title class="d-flex align-center">
          比赛详情
          <v-spacer />
          <v-btn icon="mdi-close" variant="text" @click="detailDialog = false" />
        </v-card-title>
        <v-card-text>
          <v-progress-linear v-if="detailLoading" indeterminate />
          <template v-else-if="matchDetail">
            <v-tabs v-model="detailTab" class="mb-3">
              <v-tab value="match">基本信息</v-tab>
              <v-tab value="history">历史战绩</v-tab>
              <v-tab value="rank">排名/积分榜</v-tab>
              <v-tab value="odds">欧赔数据</v-tab>
              <v-tab value="pankou">盘口数据</v-tab>
              <v-tab value="raw">原始 JSON</v-tab>
            </v-tabs>

            <v-window v-model="detailTab">
              <v-window-item value="match">
                <v-table density="compact">
                  <tbody>
                    <tr v-for="row in recordEntries(matchDetail.match)" :key="row.key">
                      <td class="font-weight-bold" style="width: 220px">{{ row.key }}</td>
                      <td>{{ row.value }}</td>
                    </tr>
                  </tbody>
                </v-table>
              </v-window-item>

              <v-window-item value="history">
                <v-row>
                  <v-col cols="12" md="4">
                    <h3 class="text-subtitle-1 font-weight-bold mb-2">双方交锋汇总</h3>
                    <v-table density="compact">
                      <tbody>
                        <tr v-for="row in summaryItems(historySummary(matchDetail.history, ['against_summary', 'againstSummary', 'against']))" :key="row.key">
                          <td>{{ row.key }}</td>
                          <td>{{ row.value }}</td>
                        </tr>
                      </tbody>
                    </v-table>
                  </v-col>
                  <v-col cols="12" md="4">
                    <h3 class="text-subtitle-1 font-weight-bold mb-2">主队近况汇总</h3>
                    <v-table density="compact">
                      <tbody>
                        <tr v-for="row in summaryItems(historySummary(matchDetail.history, ['recent_home_summary', 'recentHomeSummary']))" :key="row.key">
                          <td>{{ row.key }}</td>
                          <td>{{ row.value }}</td>
                        </tr>
                      </tbody>
                    </v-table>
                  </v-col>
                  <v-col cols="12" md="4">
                    <h3 class="text-subtitle-1 font-weight-bold mb-2">客队近况汇总</h3>
                    <v-table density="compact">
                      <tbody>
                        <tr v-for="row in summaryItems(historySummary(matchDetail.history, ['recent_guest_summary', 'recentGuestSummary']))" :key="row.key">
                          <td>{{ row.key }}</td>
                          <td>{{ row.value }}</td>
                        </tr>
                      </tbody>
                    </v-table>
                  </v-col>
                </v-row>

                <h3 class="text-subtitle-1 font-weight-bold mt-5 mb-2">双方交锋记录</h3>
                <v-data-table :headers="historyHeaders" :items="historyRows(matchDetail.history, ['against_list', 'againstList'])" density="compact" />
                <h3 class="text-subtitle-1 font-weight-bold mt-5 mb-2">主队近期比赛</h3>
                <v-data-table :headers="historyHeaders" :items="historyRows(matchDetail.history, ['recent_home_list', 'recentHomeList', 'recent'])" density="compact" />
                <h3 class="text-subtitle-1 font-weight-bold mt-5 mb-2">客队近期比赛</h3>
                <v-data-table :headers="historyHeaders" :items="historyRows(matchDetail.history, ['recent_guest_list', 'recentGuestList'])" density="compact" />
              </v-window-item>

              <v-window-item value="rank">
                <v-alert type="info" variant="tonal" density="compact" class="mb-4">
                  联赛通常返回排名数据；杯赛、欧洲杯等赛事会在同一 rank 接口返回积分榜或小组积分数据。
                </v-alert>
                <template v-if="rankSections(matchDetail.history).length">
                  <div v-for="section in rankSections(matchDetail.history)" :key="section.title" class="mb-5">
                    <h3 class="text-subtitle-1 font-weight-bold mb-2">{{ section.title }}</h3>
                    <v-table density="compact">
                      <thead>
                        <tr>
                          <th v-for="column in rankColumns(section.rows)" :key="column">{{ column }}</th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="(row, index) in section.rows" :key="`${section.title}-${index}`">
                          <td v-for="column in rankColumns(section.rows)" :key="column">
                            {{ formatValue(row[column]) }}
                          </td>
                        </tr>
                      </tbody>
                    </v-table>
                  </div>
                </template>
                <v-alert v-else type="warning" variant="tonal">
                  暂无排名/积分榜数据，请先执行“联赛排名/杯赛积分榜”爬虫任务。
                </v-alert>
              </v-window-item>

              <v-window-item value="odds">
                <h3 class="text-subtitle-1 font-weight-bold mb-2">平均欧赔</h3>
                <v-table density="compact" class="mb-5">
                  <tbody>
                    <tr v-for="row in summaryItems(asRecord(valueByKeys(matchDetail.odds, ['avg_odds', 'avgOdds'])))" :key="row.key">
                      <td>{{ row.key }}</td>
                      <td>{{ row.value }}</td>
                    </tr>
                  </tbody>
                </v-table>

                <h3 class="text-subtitle-1 font-weight-bold mb-2">公司欧赔</h3>
                <v-data-table :headers="oddsHeaders" :items="oddsRows(matchDetail.odds)" density="compact" />
              </v-window-item>

              <v-window-item value="pankou">
                <h3 class="text-subtitle-1 font-weight-bold mb-2">亚盘</h3>
                <v-data-table :headers="pankouHeaders" :items="pankouRows(matchDetail.pankou, ['asia_data', 'asiaData'])" density="compact" />
                <h3 class="text-subtitle-1 font-weight-bold mt-5 mb-2">大小球</h3>
                <v-data-table :headers="pankouHeaders" :items="pankouRows(matchDetail.pankou, ['dxq_data', 'dxqData'])" density="compact" />
              </v-window-item>

              <v-window-item value="raw">
                <v-expansion-panels multiple>
                  <v-expansion-panel title="基本信息 JSON">
                    <v-expansion-panel-text><pre>{{ rawJSON(matchDetail.match) }}</pre></v-expansion-panel-text>
                  </v-expansion-panel>
                  <v-expansion-panel title="历史战绩 JSON">
                    <v-expansion-panel-text><pre>{{ rawJSON(matchDetail.history) }}</pre></v-expansion-panel-text>
                  </v-expansion-panel>
                  <v-expansion-panel title="欧赔数据 JSON">
                    <v-expansion-panel-text><pre>{{ rawJSON(matchDetail.odds) }}</pre></v-expansion-panel-text>
                  </v-expansion-panel>
                  <v-expansion-panel title="盘口数据 JSON">
                    <v-expansion-panel-text><pre>{{ rawJSON(matchDetail.pankou) }}</pre></v-expansion-panel-text>
                  </v-expansion-panel>
                </v-expansion-panels>
              </v-window-item>
            </v-window>
          </template>
        </v-card-text>
      </v-card>
    </v-dialog>

    <v-dialog v-model="deleteDialog" max-width="400">
      <v-card>
        <v-card-title>确认删除</v-card-title>
        <v-card-text>确定要删除该比赛的所有数据吗？（包括历史、赔率、盘口）</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="deleteDialog = false">取消</v-btn>
          <v-btn color="error" @click="handleDelete">删除</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>