#!/usr/bin/env python3
"""Render the real research-store overview into a complete local schema manual.

No database connections or credentials are accepted. Input is the sanitized JSON
produced by `go run ./cmd/research-store -action schema -out ...`.
"""
import argparse
import json
from pathlib import Path


def cell(value):
    return str(value or "").replace("|", "\\|").replace("\n", " ")


def render(data):
    lines = [
        "# 独立研究与特征库：完整数据结构",
        "",
        f"数据库：`{data['database']}`；迁移版本：`{data['schemaVersion']}`。",
        "",
        "本文从真实 MySQL 的 information_schema 生成，包含全部表、字段、索引和外键。表中不包含连接凭据。",
        "",
        "## 数据链路与边界",
        "",
        "```mermaid",
        "flowchart LR",
        "  A[原始采集 ingest_events] --> B[标准身份 entities / matches]",
        "  A --> C[报价序列 market_quotes]",
        "  A --> D[清洗审计 audit_runs / audit_issues]",
        "  B --> E[特征定义 feature_definitions]",
        "  C --> F[不可变特征快照 feature_snapshots]",
        "  E --> F",
        "  F --> G[来源血缘 snapshot_sources]",
        "  F --> H[封存预测 predictions]",
        "  H --> M[列表读取同一输入版本的预测]",
        "  I[90分钟赛果 match_results] --> J[赛后结算 settlements]",
        "  H --> J",
        "  F --> K[固定数据集 datasets / dataset_members]",
        "  I --> K",
        "  K --> L[模型登记 model_runs / 效果 evaluations]",
        "```",
        "",
        "- 业务库保持原有读写；本流程只读业务库，研究事实写入独立库。运行时连接不执行DDL。",
        "- 源北京时间赛程日与UTC时间戳分开保存；没有来源依据的时间保持未知。",
        "- `reconstructed`：对历史的当前数据重建，不冒充当时的赛前预测。",
        "- `live`：实际采集事件，或开赛前封存的本地可用输入。仍需结合 source、quality、quality_flags 判断源报价时点及完整性；不等于真实资金流，也不等于所有来源时间已确认。",
        "- 快照缺失值为NULL，真实零值保留。训练中位数仅在原模型推理时使用，不写回清洗数据。",
        "- `values_json`用于数值查询，遵循MySQL JSON的浮点序列化格式。精确输入证据保存在`ingest_events.payload`二进制原文中；模型重放从该原文按固定计算版本重建，并核对分数与决定。",
        "- 相同输入版本保留首次快照；输入改变形成新版本。预测不可覆盖，赛后追加标签和结算。",
        "- 本地大原文按内容去重；真实HTTP观察按实际接收时间追加。同步批次与固定数据集记录本次引用的快照版本。",
        "- 后续同步先核对原始数据与计算代码的完整内容指纹；未变化时沿用已封存版本，不重新计算全部特征。",
        "- 真实赛前效果按每场第一份有效赛前预测评估，一场只计一次；未完赛不进入命中率分母。历史重建另行统计。",
        "- 当前比分方法 `draw-score-baseline-v1`：仅二次保留竞彩推荐1:1和0:0，赛果仅用于事后核验，没有声称比分概率已校准。",
        "- 列表接管版本`research-takeover-v1`：按当前输入内容指纹、模型/特征/规则/比分版本精确读取预测；无匹配时计算一次并封存回读。不会把真实赛前评估的第一份预测直接替换最新输入结果。",
        "- 原模型及二次0.225独立于比分推荐。研究库故障时保留原计算结果，并明确标记回退；来源正常的日缓存可长期复用，旧日缓存按日升级，回退缓存短期重试。",
        "- 首选与前二命中分别统计，分母是实际推荐且已确认常规时间赛果的比赛；评估核对当时保存的有序比分数组，不硬编码只命中0:0/1:1。",
        "",
        "## 表关系全貌",
        "",
        "```mermaid",
        "erDiagram",
    ]
    for table in data["tables"]:
        for fk in table.get("foreignKeys", []):
            nullable = next((c["nullable"] for c in table["columns"] if c["name"] == fk["column"]), False)
            parent = "o|" if nullable else "||"
            lines.append(f"  {fk['referencedTable']} {parent}--o{{ {table['name']} : {fk['column']}")
    lines += ["```", "", "## 全部表", "", "| 表 | 分层 | 用途 |", "|---|---|---|"]
    for table in data["tables"]:
        lines.append(f"| `{table['name']}` | {cell(table['layer'])} | {cell(table['description'])} |")
    for table in data["tables"]:
        lines += ["", f"## {table['name']} · {table['label']}", "", cell(table["description"]), "",
                  "| 字段 | MySQL类型 | 允许NULL | 主键 | 说明 |", "|---|---|---|---|---|"]
        for col in table["columns"]:
            lines.append(f"| `{col['name']}` | `{col['type']}` | {'是' if col['nullable'] else '否'} | {'是' if col.get('primaryKey') else ''} | {cell(col['comment'])} |")
        lines += ["", "索引：", ""]
        for index in table.get("indexes", []):
            lines.append(f"- `{index['name']}`：{'唯一；' if index['unique'] else ''}`({', '.join(index['columns'])})`。")
        if table.get("foreignKeys"):
            lines += ["", "外键：", ""]
            for fk in table["foreignKeys"]:
                lines.append(f"- `{fk['column']}` → `{fk['referencedTable']}.{fk['referencedColumn']}`。")
    lines += ["", "## 初始化、同步与查看", "", "在 `go_server` 目录执行有限维护命令（不会监听端口或启停服务）：", "", "```powershell",
              "go run ./cmd/research-store -action init",
              "go run ./cmd/research-store -action sync",
              "go run ./cmd/research-store -action overview",
              'go run ./cmd/research-store -action takeover -out "$env:TEMP/peak-ball-research-takeover-verification.json"',
              'go run ./cmd/research-store -action takeover-inspect -date 2026-10-24 -out "$env:TEMP/peak-ball-research-takeover-response.json"',
              'go run ./cmd/research-store -action schema -out "$env:TEMP/peak-ball-research-overview.json"',
              "```", "", "运行连接参数：`RESEARCH_BASE_DSN` 默认沿用业务连接配置；`RESEARCH_DB_NAME` 默认 `football_research`。目标拒绝业务库和系统库。",
              "", "后台路径：`/statistics/research-store`。API与后台服务加载新代码后可查看真实结构、质量分层及同步状态。程序由用户自行重启。",
              "", "API进程中的同步间隔默认15分钟；`RESEARCH_SYNC_INTERVAL=0`关闭定期同步。同步通过MySQL互斥锁避免与维护命令并发，数据已封存版本不会被覆盖。",
              "", "爬虫研究写入失败时，原始响应写入本地outbox，重放保留原接收时间。目录由 `RESEARCH_INGEST_OUTBOX` 指定，默认 `data/research-ingest-pending`，已从Git排除。",
              "", "数据库数据、采集原文和训练样本不提交Git；代码、DDL迁移、特征字典和本文保留在仓库。数据库与outbox应纳入正常备份。", ""]
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--overview", required=True, type=Path)
    parser.add_argument("--out", default=Path("doc/research-feature-store-schema.md"), type=Path)
    args = parser.parse_args()
    data = json.loads(args.overview.read_text(encoding="utf-8-sig"))
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(render(data), encoding="utf-8")
    print(f"tables={len(data['tables'])} fields={sum(len(t['columns']) for t in data['tables'])} output={args.out}")


if __name__ == "__main__":
    main()
