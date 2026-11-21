# 平局模型更新与后台启动

当前发布模型为 `go_server/internal/drawmodeldata/observation_current.json`，已将 2026 年 11 月 21 日以前的本地已完赛数据纳入拟合。原始 10 月版保留在 `observation_v1.json`，用于复算对比。新旧模型仍采用同一组 391 个输入、相同历史样本保护规则、240 棵深度 3 的树以及原阈值 0.2016984853563883；次选的主客胜方向规则不变。

## 启动顺序

`go_server` 连接数据库后，在启动 HTTP 监听和研究库工作线程之前：

1. 载入随程序发布的模型和本机上次有效模型，优先使用较新的有效版本。
2. 对本地数据库比赛、赔率、盘口和战绩内容计算 SHA256 指纹。不是仅比较 `updated_at`，同时间戳修正及删除也会被识别。
3. 数据内容、模型版本、训练脚本、特征生产代码和统计日期均未变时直接复用，跳过大数据导出和 Python 训练。
4. 有变化时从同一个只读一致性快照提取已确认常规时间赛果和原模型输入，统计新增、修改、删除。只有训练输入或训练配置实际改变才重新拟合。
5. 该树算法不能逐条 `partial_fit`；增量检测后，使用合并后的完整有效样本重新训练。不会只训练新增几场，丢失以前的知识。
6. 新模型须通过结构校验与 Python/Go 分数一致性检查后才原子发布，保留上次版本及按 SHA256 命名的副本。失败时继续使用有效模型，并在状态接口及日志中说明。

已有采集程序负责从外部网站更新数据库。上述流程读取**数据库已取得的最新数据**，不会自行启动采集服务，也不声称完成了外部联网更新。后台程序需要用户自行重启；助手未自动重启任何服务。

## 文件与配置

- `data/draw-model/current.json`：本机当前有效模型。
- `data/draw-model/previous.json`：更新前的有效模型。
- `data/draw-model/artifacts/<sha256>.json`：研究登记对应的内容寻址副本。
- `data/draw-model/row-hashes.json`：各训练样本指纹，不含原始比赛全文。
- `data/draw-model/source-gate.json`：快速无变化检查信息。
- `data/draw-model/training-input.json`：发生训练变动时生成的本机输入。
- `data/draw-model/python-env/`：项目独立训练环境，已在本机准备。

新电脑先运行 `powershell -ExecutionPolicy Bypass -File scripts/setup-draw-model.ps1`。它只准备依赖，不启动服务。可通过 `DRAW_MODEL_ROOT`、`DRAW_MODEL_DATA_DIR`、`DRAW_MODEL_PYTHON` 指定项目根目录、运行数据目录和 Python 路径。

`GET /api/draw/model/status` 返回 `updated` / `unchanged` / `fallback` 状态、核对时间、模型版本、训练截止日期、样本数量、增量数量与说明。首次或数据变化的启动需要读取全库特征，可能等待数分钟；整个更新有 10 分钟超时，失败不会删除已有有效模型。

离线维护命令：在 `go_server` 目录运行 `go run ./cmd/draw-model-update -update`。该命令只读取数据库、更新本地模型文件，完成后退出，不启动服务。

## 如何看命中率

`reports/model-update-20261121/evaluation.json` 同时记录旧版、新版全量拟合后的历史成绩和按日期先训练后验证的 11 月分段回放。全量拟合后的历史命中率是训练内成绩。分段回放也使用本地最终赔率，不能称为已验证的实时赛前命中率。加入 11 月并不保证每个时段或次选命中率都提高，报告保留所有结果，不据11月表现挑阈值。
