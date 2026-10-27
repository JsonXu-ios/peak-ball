#!/usr/bin/env python3
"""Describe fixed, domain-selected inputs by *actual* draw score subtype.

The subtype is a post-match review label, never an input or selection target.
No model fitting, threshold search, correlation ranking, service, network or
database operation occurs. Existing numeric columns use the frozen extractor;
absolute Asian lines are explicitly documented descriptive-only transforms.
"""
from __future__ import annotations

import argparse
import csv
import hashlib
import html
import json
import math
import sys
from collections import Counter
from pathlib import Path

sys.dont_write_bytecode = True
import numpy as np

from draw_full_features import build_features

ROOT = Path(__file__).resolve().parents[1]
GROUPS = (
    ("nonDraw", "非平局"), ("allDraw", "全部平局"),
    ("draw0_0", "0:0"), ("draw1_1", "1:1"),
    ("draw2_2", "2:2"), ("draw3Plus", "≥3:3"), ("drawOther", "其他平局标签"),
)


def variables():
    """Fixed list chosen from the requested concepts, before viewing results."""
    result = []

    def add(name, label, *, transform=None, full_window=None):
        result.append({"featureName": name, "label": label,
                       "transform": transform, "fullWindow": full_window})

    for phase, label in (("opening", "初盘"), ("latest", "即时盘")):
        add(f"euro.average.{phase}.prob.draw", f"均赔{label}平去水概率")
        add(f"euro.average.{phase}.sideGap", f"均赔{label}主客概率差绝对值")
        add(f"line.selected.asianHandicap.{phase}.line", f"实际亚盘{label}绝对值", transform="abs")
        add(f"line.selected.goalsLine.{phase}.line", f"实际大小球{label}")
    for side, label in (("home", "主队"), ("away", "客队")):
        for key, title in (("n", "实际样本数"), ("drawRate", "平局率"),
                           ("goalsFor", "场均进球"), ("goalsAgainst", "场均失球"),
                           ("totalGoals", "场均总进球")):
            add(f"safeRecent.{side}.10.{key}", f"{label}赛前近10场{title}", full_window=side)
    add("safeRecent.pair.totalGoals.absGap", "双方近期场均总球差绝对值", full_window="both")
    for phase, label in (("opening", "初盘"), ("latest", "即时盘")):
        add(f"kelly.{phase}.draw", f"{label}凯利平值")
        for source, title in (("sporttery", "威廉−竞彩"), ("average", "威廉−均赔")):
            add(f"difference.{phase}.{source}.drawAbsRank", f"{label}{title}平差绝对值降序名次")
    add("heat.goals.over", "大小球计算大热度")
    add("heat.goals.under", "大小球计算小热度")
    add("profit.simulated.roi.draw", "模拟平局庄家ROI")
    add("profit.simulated.share.draw", "模拟平局支持比例")
    add("retail.draw", "散户心理平局比例")
    add("professional.drawRisk.score", "专业平局风险分数")
    return result


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def summarize(values, mask):
    subset = np.asarray(values, float)[mask]
    finite = subset[np.isfinite(subset)]
    n = len(subset)
    quartiles = np.quantile(finite, [.25, .5, .75], method="linear") if len(finite) else [None]*3
    return {"groupN": n, "validN": int(len(finite)), "missingN": int(n-len(finite)),
            "coverage": len(finite)/n if n else None,
            "q25": float(quartiles[0]) if len(finite) else None,
            "median": float(quartiles[1]) if len(finite) else None,
            "q75": float(quartiles[2]) if len(finite) else None}


def group_masks(rows):
    masks = {key: np.zeros(len(rows), bool) for key, _ in GROUPS}
    draw = np.asarray([row.get("result") == 1 for row in rows], bool)
    masks["nonDraw"], masks["allDraw"] = ~draw, draw
    for index, row in enumerate(rows):
        if not draw[index]:
            continue
        home, away = row.get("homeScore"), row.get("awayScore")
        valid = (type(home) in (int, float) and type(away) in (int, float)
                 and math.isfinite(home) and math.isfinite(away)
                 and home == away and home >= 0 and int(home) == home)
        key = (f"draw{int(home)}_{int(home)}" if home in (0, 1, 2) else "draw3Plus") if valid else "drawOther"
        masks[key][index] = True
    assert np.array_equal(sum((masks[key].astype(int) for key in ("draw0_0", "draw1_1", "draw2_2", "draw3Plus", "drawOther"))), draw.astype(int))
    return masks


def report(dataset_path, manifest_path):
    extractor_path = Path(__file__).with_name("draw_full_features.py")
    study_path = manifest_path.with_name("analysis.json")
    recorded_study = json.loads(study_path.read_text(encoding="utf-8-sig")) if study_path.exists() else {}
    if recorded_study.get("extractorSHA256") and digest(extractor_path) != recorded_study["extractorSHA256"]:
        raise RuntimeError("Extractor source differs from the frozen study SHA256; no descriptive report written")
    if recorded_study.get("datasetSHA256") and digest(dataset_path) != recorded_study["datasetSHA256"]:
        raise RuntimeError("Dataset differs from the frozen study SHA256; no descriptive report written")
    data = json.loads(dataset_path.read_text(encoding="utf-8-sig"))
    rows = data["rows"]
    x, manifest = build_features(rows)
    frozen = json.loads(manifest_path.read_text(encoding="utf-8-sig"))
    if manifest != frozen:
        raise RuntimeError("Extractor metadata differs from the frozen feature manifest; no descriptive report written")
    columns = {entry["name"]: index for index, entry in enumerate(manifest)}
    masks = group_masks(rows)
    output = []
    for requested in variables():
        name = requested["featureName"]
        found = name in columns
        metadata = manifest[columns[name]] if found else {}
        values = x[:, columns[name]].copy() if found else np.full(len(rows), np.nan)
        derived = None
        if requested["transform"] == "abs":
            values = np.abs(values)
            derived = "abs(" + name + ")"
        item = {"label": requested["label"], "featureName": name,
                "sourceColumnPresent": found,
                "missingReason": None if found else "The requested input has no corresponding frozen feature column; no substitute or new market metric was invented.",
                "unit": metadata.get("unit"), "family": metadata.get("family"),
                "view": metadata.get("view"), "source": metadata.get("source"),
                "derivedFrom": derived,
                "descriptiveOnlyDerived": derived is not None,
                "groups": {key: summarize(values, mask) for key, mask in masks.items()}}
        if requested["fullWindow"] is not None:
            sides = ("home", "away") if requested["fullWindow"] == "both" else (requested["fullWindow"],)
            complete = np.ones(len(rows), bool)
            for side in sides:
                n_column = columns.get(f"safeRecent.{side}.10.n")
                complete &= x[:, n_column] == 10 if n_column is not None else False
            item["fullTenWindowSensitivity"] = {
                "requires": [f"safeRecent.{side}.10.n == 10" for side in sides],
                "groups": {key: summarize(values, mask & complete) for key, mask in masks.items()}}
        output.append(item)
    counts = {key: int(mask.sum()) for key, mask in masks.items()}
    draws = counts["allDraw"]
    return {
        "version": "draw-subtype-descriptive-review-v1",
        "purpose": "Describe a fixed domain-selected set of existing pre-match/snapshot inputs across actual draw score subtypes and all non-draw controls; no feature selection, model fitting or threshold search.",
        "dataset": {"path": str(dataset_path.relative_to(ROOT)) if dataset_path.is_relative_to(ROOT) else str(dataset_path),
                    "sha256": digest(dataset_path), "rows": len(rows), "draws": draws, "nonDraws": counts["nonDraw"]},
        "frozenFeatureExtractor": {"path": "scripts/draw_full_features.py", "sha256": digest(extractor_path),
                                   "manifestSHA256": digest(manifest_path), "metadataMatchesFrozenManifest": True, "columns": len(manifest),
                                   "recordedStudyExtractorSHA256": recorded_study.get("extractorSHA256"),
                                   "matchesRecordedStudyExtractor": digest(extractor_path) == recorded_study.get("extractorSHA256") if recorded_study.get("extractorSHA256") else None,
                                   "derivedAbsLinesAreNotAddedToX": True},
        "reviewScriptSHA256": digest(Path(__file__)),
        "method": {"variablesFixedBeforeLookingAtSubtypes": True, "noResultsBasedColumnRanking": True,
                   "quantileMethod": "numpy.quantile(method='linear'); finite observations only; no imputation",
                   "subtypeUsesActualTargetScoreAsReviewLabelOnly": True, "subtypeNeverEntersX": True,
                   "currentModelOrFeatureWhitelistChanged": False,
                   "upToTenHistory": "Strict existing as-of records (including kickoff+2h<target and target ID exclusion); fewer than ten retained and denominator reported, with separate exact-ten sensitivity.",
                   "units": "probability is a fraction in JSON (0.25=25%); percent is percentage points already (25=25%); HTML converts probability to percent for reading. ROI is bookmaker perspective, not betting return.",
                   "ties": "drawAbsRank is existing descending rank with ties; rank 1 means draw has the largest absolute difference, not a guaranteed draw."},
        "groups": [{"key": key, "label": label, "n": counts[key],
                    "fractionOfAllFixtures": counts[key]/len(rows) if rows else None,
                    "fractionOfDraws": counts[key]/draws if draws and key not in ("nonDraw",) else None}
                   for key, label in GROUPS],
        "actualDrawScoreBreakdown": dict(sorted(Counter(f"{row.get('homeScore')}:{row.get('awayScore')}" for row in rows if row.get("result")==1).items())),
        "warnings": [
            "Actual 0:0/1:1/2:2/≥3:3 categories are known only after the match and are not candidate pre-match features.",
            "This is a description of all existing historical outcomes, not out-of-time or prospective validation; between-group medians do not measure prediction precision/recall.",
            "Current source capture timestamps are unknown. safeHistory is reconstructed as-of; heat, modeled profits, psychology and professional scores are snapshotDerived and often share inputs.",
            "Finite denominators differ by source; missing data are not zero and should not be compared as equally observed subgroups.",
            "No universally necessary draw condition is asserted. Overlapping quartiles and small higher-score subgroups limit any single hard-rule interpretation."],
        "variables": output,
    }


def display(value, unit):
    if value is None:
        return "暂无"
    if unit == "probability":
        return f"{value*100:.1f}%"
    if unit == "percent":
        return f"{value:.1f}%"
    if unit in ("rank", "count"):
        return f"{value:g}"
    return f"{value:.3f}".rstrip("0").rstrip(".")


def write_html(path, data):
    groups = [group for group in data["groups"] if group["key"] != "drawOther" or group["n"]]

    def table(sensitivity=False):
        heading = "<th>固定复盘变量</th>" + "".join(f"<th>{html.escape(group['label'])}<br>总样本 {group['n']}</th>" for group in groups)
        body = []
        for item in data["variables"]:
            if sensitivity and "fullTenWindowSensitivity" not in item:
                continue
            stats = item["fullTenWindowSensitivity"]["groups"] if sensitivity else item["groups"]
            label = html.escape(item["label"])
            subtitle = {"market": "市场原值", "safeHistory": "严格赛前历史", "snapshotDerived": "当前快照派生"}.get(item["view"], "缺失")
            if item["derivedFrom"]:
                subtitle += " · 仅复盘取绝对值"
            cells = []
            for group in groups:
                stat = stats[group["key"]]
                cells.append("<td>" + display(stat["median"], item["unit"]) + "<small>[" + display(stat["q25"], item["unit"]) + ", " + display(stat["q75"], item["unit"]) + f"]<br>有效 {stat['validN']} / {stat['groupN']}</small></td>")
            body.append(f"<tr><th title='{html.escape(item['derivedFrom'] or item['featureName'],quote=True)}'>{label}<small>{subtitle}</small></th>" + "".join(cells) + "</tr>")
        return "<div class='scroll'><table><thead><tr>" + heading + "</tr></thead><tbody>" + "".join(body) + "</tbody></table></div>"

    cards = "".join(f"<div class='card'><span>{html.escape(g['label'])}</span><strong>{g['n']}</strong></div>" for g in groups)
    page = """<!doctype html><html lang='zh-CN'><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>历史平局比分分型复盘</title><style>
body{font-family:system-ui,'Microsoft Yahei',sans-serif;background:#f4f6f8;color:#152235;margin:0;padding:28px;line-height:1.6}main{max-width:1440px;margin:auto}h1{margin:0 0 10px;font-size:27px}p{max-width:1050px}.note{background:#eaf2fb;border-left:4px solid #2481ce;padding:12px 16px}.cards{display:flex;flex-wrap:wrap;gap:12px;margin:20px 0}.card{background:#fff;border:1px solid #dae2ea;border-radius:8px;padding:12px 18px;min-width:110px}.card span{display:block;color:#526579}.card strong{font-size:26px}.scroll{overflow:auto;border:1px solid #dce3eb;background:white;margin:16px 0}table{border-collapse:collapse;width:100%;min-width:1050px;font-size:13px}th,td{padding:10px 12px;text-align:right;border-bottom:1px solid #e5eaf0;white-space:nowrap}thead th{background:#edf2f7;font-weight:600}tbody th{text-align:left;min-width:240px;white-space:normal}small{display:block;font-size:11px;color:#6a7e93}tbody tr:nth-child(even){background:#fafbfd}details{margin-top:24px}summary{cursor:pointer;font-weight:650}footer{font-size:12px;color:#6a7e93;margin-top:26px;word-break:break-all}a{color:#176caa}
</style><main>"""
    page += "<h1>历史平局比分分型复盘</h1><p>把已完成的平局分为实际 0:0、1:1、2:2、≥3:3，与全部非平局比较同一组预先固定的变量。</p>"
    page += "<p class='note'>实际比分分型是赛后复盘标签，赛前未知，未加入预测输入。没有重新拟合模型、挑最相关的列或搜索阈值。表格显示 <b>中位数 [25%分位，75%分位]</b>，并列出有效分母；这些数字不是命中率。</p>"
    page += "<div class='cards'>" + cards + "</div>"
    page += "<p>近况采用严格排除本场和未来、预留两小时完赛缓冲的最多 10 场。均赔去水概率、近况平率在本页换算为百分比；模拟 ROI、支持率及热度沿用百分数。市场、历史和当前快照派生值分开注明来源，缺失不按零处理。</p>"
    page += table()
    page += "<details><summary>查看历史变量恰好满 10 场的敏感性对照</summary><p>单队变量要求该队有 10 场，双方差值要求两队都满 10 场；有效分母会相应变化。</p>" + table(True) + "</details>"
    page += "<p>不同分型的分布可以重叠。大小球热度、模拟盈亏、散户心理和专业分数会共用输入；采集时点未知，本页仅描述当前历史快照，不能证明未来预测效果。</p>"
    page += "<p><a href='draw-subtypes.json'>完整 JSON</a> · <a href='draw-subtypes.csv'>明细 CSV</a></p>"
    page += "<footer>数据 SHA256：" + data["dataset"]["sha256"] + "<br>冻结提取器 SHA256：" + data["frozenFeatureExtractor"]["sha256"] + "</footer></main></html>"
    path.write_text(page, encoding="utf-8")


def write_csv(path, data):
    columns = ["variable", "feature", "derivedFrom", "view", "unit", "slice", "group", "groupN", "validN", "missingN", "coverage", "q25", "median", "q75"]
    with path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=columns)
        writer.writeheader()
        for item in data["variables"]:
            slices = [("all_available", item["groups"])]
            if "fullTenWindowSensitivity" in item:
                slices.append(("exact_ten_history", item["fullTenWindowSensitivity"]["groups"]))
            for slice_name, stats in slices:
                for key, stat in stats.items():
                    writer.writerow({"variable": item["label"], "feature": item["featureName"], "derivedFrom": item["derivedFrom"], "view": item["view"], "unit": item["unit"], "slice": slice_name, "group": key, **stat})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dir", type=Path, default=ROOT / "reports/draw-full-feature-study")
    args = parser.parse_args()
    directory = args.dir.resolve()
    result = report(directory / "dataset.json", directory / "feature-manifest.json")
    (directory / "draw-subtypes.json").write_text(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")
    write_html(directory / "draw-subtypes.html", result)
    write_csv(directory / "draw-subtypes.csv", result)
    print(json.dumps({"groups": {g["key"]: g["n"] for g in result["groups"]}, "variables": len(result["variables"]), "frozenMetadataMatches": True, "outputs": ["draw-subtypes.json", "draw-subtypes.html", "draw-subtypes.csv"]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
