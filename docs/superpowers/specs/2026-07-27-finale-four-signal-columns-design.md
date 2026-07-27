# 终章页新增四信号提示列（仅展示）设计

日期：2026-07-27

## 背景

本项目管理后台的「终章 · 未来比赛预测」页（`admin/src/views/statistics/Finale.vue`，
接口 `go_admin/handlers/statistics_finale.go`）目前只展示汇总信号（四信号齐备 /
主推≥70%）。需要把四个信号各自的推荐方向和具体数值逐场展示出来，**仅展示、
不参与预测**（与「盈亏提示（仅展示）」列同一模式，符合「前瞻只报告」约定）。

## 方案（已确认）

### 后端 `statistics_finale.go`

四个信号方向均已在现有代码中算出，仅新增四个展示字段（字符串，无数据为空）：

- `sig_base`：前端主推方向 + 概率，如 `客胜 72.3%`（有概率即展示，不限于≥70%）
- `sig_pro`：专业信号（凯体同向）方向，如 `客胜`
- `sig_recent`：近期状态让球方向 + 让球差，如 `客胜 -0.85`（>0 主胜，<0 客胜，=0 仅显示数值）
- `sig_comp`：亚盘综合均值方向 + 期望值，如 `客胜 -0.42`（方向沿用 `statisticsOutcomeFromValue` + `statisticsHandicapBand` 口径）

现有 `pick`/`signal` 预测逻辑与其余字段不变。

### 前端 `Finale.vue`

- 表格移除旧的「主推概率 / 让球差 / 综合期望」3 个纯数值列（用户已确认）。
- 新增 4 列：「主推 / 专业 / 让球 / 综合」，表头区注明仅展示；单元格按方向着色：
  红=主胜（text-error）、绿=客胜（text-success）、平局/无方向为默认色，空值显示 `-`。
- 日期分组行 colspan 由 11 改为 12。

## 追加（同日确认）

- 再增两列：亚盘盘口（即时）`ah_line`、大小球盘口（即时）`ou_line`，取
  `statisticsPankouLinePair` 的即时值（bet365_asia / bet365_dxq），仅展示。
- 四信号列与两个盘口列均改用 tag（v-chip）显示：信号 tag 主胜=红、客胜=绿
  （tonal），盘口 tag 为中性 outlined。

## 验证

- `cd go_admin && go build ./... && go test ./handlers/`
- `cd admin && npx vue-tsc --noEmit`
- 页面刷新后每行可见四信号提示，预测（pick/signal）行为不变。
