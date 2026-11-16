<script setup lang="ts">
import { computed } from 'vue'
import type { PublicModels, PublicWaterValues } from '@/api/publicSources'

const props = defineProps<{ models: PublicModels }>()
const rows = computed(() => (props.models.marketContext?.markets || []).flatMap(market => market.phases.map(phase => ({
  ...phase, key: market.key,
  average: phase.macau ? phase.averages.find(value => Math.abs(value.line - phase.macau!.line) < 1e-8) : undefined,
}))))
const names: Record<string, string> = { average: '平均欧赔', william: '威廉', bet365: 'Bet365', pinnacle: '平博' }
function number(value: number | undefined) { return value === undefined || !Number.isFinite(value) ? '—' : String(Number(value.toFixed(4))) }
function water(value: PublicWaterValues | null | undefined, totals: boolean) {
  if (!value) return '—'
  return totals ? `${number(value.underWater)} / ${number(value.overWater)}` : `${number(value.leftWater)} / ${number(value.rightWater)}`
}
function prices(value?: string[] | null) { return value?.length ? value.join(' / ') : '—' }
</script>

<template>
  <v-card v-if="models.marketContext" variant="outlined" class="my-4" title="澳门与平均水位参数">
    <v-card-text class="text-caption">
      <div>{{ models.marketContext.note }}</div>
      <div>均值按同市场、同阶段、同盘口的真实公司等权计算，含澳门，排除平均合成行和重复公司。</div>
      <div v-if="models.marketContext.asianSign">{{ models.marketContext.asianSign }}</div>
      <div>亚盘按主队／客队展示；大小球按小球／大球展示。差值为澳门减同盘口平均值，两侧水差按展示的左值减右值计算。</div>
    </v-card-text>
    <v-table density="compact"><thead><tr><th>市场</th><th>阶段</th><th>澳门盘口</th><th>澳门双水位</th><th>同盘平均双水位</th><th>双水位差值</th><th>两侧水差（澳门／平均）</th><th>公司数</th></tr></thead>
      <tbody><tr v-for="row in rows" :key="`${row.key}-${row.phase}`">
        <td>{{ row.key === 'totals' ? '大小球 · 小／大' : '亚盘 · 主／客' }}</td><td>{{ row.phase === 'opening' ? '初盘' : '即时盘' }}</td>
        <td>{{ row.macau ? number(row.macau.line) : '缺少澳门盘口' }}</td>
        <td>{{ water(row.macau, row.key === 'totals') }}</td><td>{{ water(row.average, row.key === 'totals') }}</td>
        <td v-if="row.macauVsAverage">{{ number(row.key === 'totals' ? row.macauVsAverage.rightWaterDifference : row.macauVsAverage.leftWaterDifference) }} / {{ number(row.key === 'totals' ? row.macauVsAverage.leftWaterDifference : row.macauVsAverage.rightWaterDifference) }}</td><td v-else>—</td>
        <td>{{ number(row.macau ? row.macau.waterGap * (row.key === 'totals' ? -1 : 1) : undefined) }} / {{ number(row.average ? row.average.waterGap * (row.key === 'totals' ? -1 : 1) : undefined) }}</td>
        <td>{{ row.macauVsAverage?.companyCount ?? '—' }}</td>
      </tr></tbody>
    </v-table>
    <v-card-text><details><summary class="text-caption">查看各盘口的平均水位</summary>
      <div v-for="row in rows" :key="`groups-${row.key}-${row.phase}`" class="text-caption mt-2">
        <strong>{{ row.key === 'totals' ? '大小球（小／大）' : '亚盘（主／客）' }} · {{ row.phase === 'opening' ? '初盘' : '即时盘' }}</strong>
        <div v-for="group in row.averages" :key="group.line">盘口 {{ number(group.line) }}：{{ water(group, row.key === 'totals') }}，{{ group.companyCount }} 家</div>
      </div>
    </details></v-card-text>
  </v-card>
  <v-card v-if="models.inputSources?.length" variant="outlined" class="my-4" title="模型实际采用的赔率与凯利">
    <v-card-text class="text-caption">赔率与即时凯利成套取自同一来源。初始凯利按配对赔率使用；若采用完整历史中的最早配对记录，会单独说明初盘来源与时间。</v-card-text>
    <v-table density="compact"><thead><tr><th>公司</th><th>来源</th><th>初赔</th><th>即时赔率</th><th>初始凯利</th><th>即时凯利</th></tr></thead><tbody>
      <tr v-for="source in models.inputSources" :key="source.company">
        <td>{{ names[source.company] || source.company }}</td><td>{{ source.source === 'zgzcw_cp' ? '足彩网 CP 接口' : '原报价来源' }}<div class="text-caption">{{ source.reason }}</div></td>
        <td>{{ prices(source.opening) }}<div v-if="source.openingSource === 'zgzcw_full_quote_history'" class="text-caption">完整历史最早配对记录<br>{{ source.openingAt }}（北京时间）</div></td><td>{{ prices(source.latest) }}</td><td>{{ prices(source.openingKelly) }}</td><td>{{ prices(source.kelly) }}</td>
      </tr>
    </tbody></v-table>
  </v-card>
</template>
