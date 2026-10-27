#!/usr/bin/env python3
"""Build the UI's exploratory draw-trait catalogue from the frozen study.

Rule qualification reads training/calibration only. Held-out outcomes are
copied for inspection, never used to qualify or rank a rule. No DB/network I/O.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path


OBSERVATIONS = ("consistent_da_both_min", "latest_cross_unique_draw")
CONTRASTS = ("heat70_not_win",)


def build(analysis_path: Path, dataset_path: Path) -> dict:
    report = json.loads(analysis_path.read_text(encoding="utf-8-sig"))
    dataset_bytes = dataset_path.read_bytes()
    dataset = json.loads(dataset_bytes.decode("utf-8-sig"))
    digest = hashlib.sha256(dataset_bytes).hexdigest()
    if digest != report["reproducibility"]["datasetSHA256"]:
        raise ValueError("Evidence report and dataset do not refer to the same snapshot")
    rows = dataset["rows"]
    if len(rows) != report["dataset"]["n"] or sum(row["result"] == 1 for row in rows) != report["dataset"]["draws"]:
        raise ValueError("Evidence outcome counts do not match the frozen dataset")

    catalogue = []
    for candidate in report["candidates"]:
        train, calibration = (candidate["partitions"][part] for part in ("train", "calibration"))
        qualified = (train["n"] >= 60 and calibration["n"] >= 15
                     and train["liftPP"] is not None and train["liftPP"] > 0
                     and calibration["liftPP"] is not None and calibration["liftPP"] > 0)
        rule_id = candidate["id"]
        if qualified:
            # This transparent display threshold is descriptive, not a new
            # significance test or a change to the original model selection.
            tier = "suspected" if min(train["liftPP"], calibration["liftPP"]) >= 1 else "weak"
        elif rule_id in OBSERVATIONS:
            tier = "small_sample"
        elif rule_id in CONTRASTS:
            tier = "unsupported"
        else:
            continue
        partitions = {}
        for name, original in candidate["partitions"].items():
            part = dict(original)
            eligible = part["eligible"]
            part["drawOccurrence"] = part["draws"] / eligible["draws"] if eligible["draws"] else None
            non_draws = eligible["n"] - eligible["draws"]
            part["nonDrawOccurrence"] = (part["n"] - part["draws"]) / non_draws if non_draws else None
            partitions[name] = part
        catalogue.append({
            "id": rule_id, "label": candidate["label"], "family": candidate["family"],
            "requirements": candidate["requirements"], "tier": tier, "partitions": partitions,
        })

    # Presentation order follows the type of input; no held-out success rate is
    # consulted. Keep the former normalized rule as a supplementary reference.
    order = ["opening_da_min", "latest_da_min", "consistent_da_late_min",
             "latest_ds_min", "opening_ds_positive", "latest_ds_positive",
             "consistent_ds_late_min", "latest_sa_negative", "opening_da_relative_min"]
    priorities = {rule_id: index for index, rule_id in enumerate(order)}
    catalogue.sort(key=lambda rule: ({"suspected": 0, "small_sample": 1, "weak": 2, "unsupported": 3}[rule["tier"]],
                                      priorities.get(rule["id"], len(order)), rule["id"]))
    return {
        "schemaVersion": 1,
        "source": {
            "generatedAt": report["generatedAt"], "completedMatches": report["dataset"]["n"],
            "drawMatches": report["dataset"]["draws"], "datasetSHA256": digest,
            "firstDate": min(row["date"] for row in rows), "lastDate": max(row["date"] for row in rows),
            "strictPreMatchVerified": 0,
        },
        "policy": {
            "status": "historical_association_only", "selectionUses": ["train", "calibration"],
            "testUsedForSelection": False, "minimumTrainMatches": 60, "minimumCalibrationMatches": 15,
            "candidateCount": report["candidateCount"], "confirmedStrongRules": 0,
            "displayMinimumLiftPP": 1,
            "note": "疑似条件须在训练、校准两区至少各高于同输入基准1个百分点；小幅同向提升列弱关联。1个百分点仅作展示分级，不代表显著性或未来概率；测试仅披露。相关条件重叠，不按条数换算概率。未触发不排除平局。",
            "timingNote": "留存赔率采集时间未确证为赛前，本表为历史关联观察。盈亏新列尚未纳入该回测。",
        },
        "rules": catalogue,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--analysis", type=Path, default=Path("reports/draw-feature-study/analysis.json"))
    parser.add_argument("--dataset", type=Path, default=Path("reports/draw-feature-study/dataset.json"))
    parser.add_argument("--out", type=Path, default=Path("admin/src/data/drawTraitEvidence.json"))
    args = parser.parse_args()
    data = build(args.analysis, args.dataset)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(args.out), "rules": len(data["rules"]),
                      "tiers": {tier: sum(rule["tier"] == tier for rule in data["rules"])
                                for tier in ("suspected", "weak", "small_sample", "unsupported")},
                      "completedMatches": data["source"]["completedMatches"],
                      "drawMatches": data["source"]["drawMatches"]}))


if __name__ == "__main__":
    main()
