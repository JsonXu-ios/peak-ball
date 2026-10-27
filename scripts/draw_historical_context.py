#!/usr/bin/env python3
"""Small, label-blind context adapter for the frozen historical draw study.

Only files are read/written. Rankings are source snapshots, not historical
standings. Observed history density is not a complete fixture calendar.
"""
from __future__ import annotations

import argparse
import csv
import hashlib
import json
import math
import re
from datetime import timedelta
from pathlib import Path

import numpy as np

from draw_full_features import safe_records, timestamp


def parse_rank(value):
    """Return an exact textual competition prefix and a positive rank.

    Compound rankings such as '澳昆甲3-3' are deliberately rejected. Two
    bare integers are syntactically comparable, but their ranking system and
    common as-of time remain unverified.
    """
    if not isinstance(value, str):
        return None
    match = re.fullmatch(r"([^\d]*)([1-9]\d*)", value.strip())
    return (match.group(1), int(match.group(2))) if match else None


def build_context_features(rows, X, names):
    """Return an n-by-18 array and aligned provenance metadata.

    Target labels, scores and status are never consulted. Target ID/time only
    guard historical records. Missing histories never become zero rest days.
    Form/goal composites require five valid previous matches for both teams.
    """
    X = np.asarray(X, dtype=float)
    if X.shape != (len(rows), len(names)) or len(set(names)) != len(names):
        raise ValueError("X rows/columns and unique names must align")
    lookup = {name: j for j, name in enumerate(names)}
    columns, metadata = [], []

    def emit(name, label, values, unit, source, view, guards, family="historicalContext"):
        values = np.asarray(values, dtype=float)
        if values.shape != (len(rows),):
            raise ValueError(name + " must have one value per row")
        if not np.isfinite(values[~np.isnan(values)]).all():
            raise ValueError(name + " contains an infinite value")
        columns.append(values)
        metadata.append({"name": name, "label": label, "unit": unit,
                         "source": source, "view": view, "family": family,
                         "guards": list(guards), "requires": list(guards)})

    def col(name):
        return X[:, lookup[name]] if name in lookup else np.full(len(rows), np.nan)

    ranks = np.full((len(rows), 2), np.nan)
    for i, row in enumerate(rows):
        inputs = row.get("analysisInputs") or {}
        a, b = parse_rank(inputs.get("homeRank")), parse_rank(inputs.get("guestRank"))
        if a is not None and b is not None and a[0] == b[0]:
            ranks[i] = (a[1], b[1])
    rank_guard = ["both positive integer rank suffixes", "identical exact textual prefixes (including both empty)",
                  "ranking system, common season and as-of time are not independently verified"]
    rank_source = "analysisInputs.homeRank / guestRank raw source snapshot; no points or qualification inference"
    for j, side in enumerate(("home", "away")):
        emit("context.rank." + side, ("主" if side == "home" else "客") + "队可比格式排名", ranks[:, j],
             "rank", rank_source, "snapshotDerived", rank_guard)
    emit("context.rank.absGap", "双方可比格式排名差绝对值", abs(ranks[:, 0] - ranks[:, 1]),
         "rank", rank_source, "snapshotDerived", rank_guard)

    schedule = {side: {key: np.full(len(rows), np.nan) for key in ("restDays", "known7Days", "known14Days")}
                for side in ("home", "away")}
    for i, row in enumerate(rows):
        target = timestamp(row.get("matchTime"))
        if target is None:
            continue
        for side, block in (("home", "homeRecent"), ("away", "awayRecent")):
            records = safe_records(row, block)
            if not records:
                continue
            schedule[side]["restDays"][i] = (target - records[0][0]).total_seconds() / 86400
            for days in (7, 14):
                schedule[side]["known%dDays" % days][i] = sum(
                    target - timedelta(days=days) <= rec[0] for rec in records)
    schedule_guard = ["known timestamp with timezone", "previous kickoff + 2 hours < target kickoff",
                      "target match ID excluded; historical IDs deduplicated; valid integer past scores",
                      "at least one safe recorded previous match; supplied history is at most 10 matches",
                      "counts describe recorded matches, not complete fixture/calendar or actual rotation"]
    for side in ("home", "away"):
        cn = "主" if side == "home" else "客"
        emit("context.schedule." + side + ".restDays", cn + "队距上一场已记录比赛天数",
             schedule[side]["restDays"], "days", "preMatchHistory safe_records; actual matchTime", "safeHistory", schedule_guard)
        for days in (7, 14):
            emit("context.schedule." + side + ".known%dDays" % days, cn + "队近%d天已记录比赛数" % days,
                 schedule[side]["known%dDays" % days], "count", "preMatchHistory safe_records; actual matchTime", "safeHistory", schedule_guard)
    emit("context.schedule.restDays.absGap", "双方已记录间隔天数差绝对值",
         abs(schedule["home"]["restDays"] - schedule["away"]["restDays"]), "days",
         "preMatchHistory safe_records; actual matchTime", "safeHistory", schedule_guard)

    ns = {side: col("safeRecent." + side + ".10.n") for side in ("home", "away")}
    safe = np.isfinite(ns["home"]) & np.isfinite(ns["away"]) & (ns["home"] >= 5) & (ns["away"] >= 5)
    form_guard = ["safeRecent.home.10.n >= 5", "safeRecent.away.10.n >= 5",
                  "component features finite; previous matches only; target excluded"]
    ppg = {}
    for side in ("home", "away"):
        wins, draws = col("safeRecent." + side + ".10.wins"), col("safeRecent." + side + ".10.draws")
        with np.errstate(invalid="ignore", divide="ignore"):
            ppg[side] = np.where(safe, (3 * wins + draws) / ns[side], np.nan)
        emit("context.form." + side + ".pointsPerGame", ("主" if side == "home" else "客") + "队近10场场均赛果积分",
             ppg[side], "pointsPerMatch", "safeRecent.*.10 wins/draws/n; synthetic 3-1-0 form score, not league standing points",
             "safeHistory", form_guard)
    emit("context.form.pointsPerGame.absGap", "双方近10场场均赛果积分差绝对值", abs(ppg["home"] - ppg["away"]),
         "pointsPerMatch", "safeRecent.*.10 wins/draws/n", "safeHistory", form_guard)
    emit("context.form.pointsPerGame.sum", "双方近10场场均赛果积分合计", ppg["home"] + ppg["away"],
         "pointsPerMatch", "safeRecent.*.10 wins/draws/n", "safeHistory", form_guard)

    # Arithmetic matchup indices: they are not a fitted expected-goals model.
    home_goal_index = np.where(safe, (col("safeRecent.home.10.goalsFor") + col("safeRecent.away.10.goalsAgainst")) / 2, np.nan)
    away_goal_index = np.where(safe, (col("safeRecent.away.10.goalsFor") + col("safeRecent.home.10.goalsAgainst")) / 2, np.nan)
    goal_source = "arithmetic average of team's safeRecent goalsFor and opponent goalsAgainst; not xG or a calibrated goal forecast"
    for side, value in (("home", home_goal_index), ("away", away_goal_index)):
        emit("context.goals." + side + ".attackDefenseIndex", ("主" if side == "home" else "客") + "队历史攻守算术指数",
             value, "goalsPerMatch", goal_source, "safeHistory", form_guard)
    emit("context.goals.attackDefenseIndex.sum", "双方历史攻守算术指数合计", home_goal_index + away_goal_index,
         "goalsPerMatch", goal_source, "safeHistory", form_guard)
    emit("context.goals.attackDefenseIndex.absGap", "双方历史攻守算术指数差绝对值", abs(home_goal_index - away_goal_index),
         "goalsPerMatch", goal_source, "safeHistory", form_guard)
    Z = np.column_stack(columns)
    assert Z.shape == (len(rows), 18)
    assert len({m["name"] for m in metadata}) == 18
    return Z, metadata


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def self_test():
    base_time = "2026-10-25T12:00:00+08:00"
    past = {"matchId": "old", "matchTime": "2026-10-20T12:00:00+08:00", "homeScore": 1, "awayScore": 0, "venue": "home"}
    unsafe = dict(past, matchId="too-close", matchTime="2026-10-25T11:00:00+08:00")
    target_copy = dict(past, matchId="target")
    row = {"matchId": "target", "matchTime": base_time,
           "analysisInputs": {"homeRank": "League3", "guestRank": "League8"},
           "preMatchHistory": {k: {"matches": [past, dict(past), unsafe, target_copy]} for k in ("homeRecent", "awayRecent")}}
    names = ["safeRecent." + side + ".10." + key for side in ("home", "away") for key in ("n", "wins", "draws", "goalsFor", "goalsAgainst")]
    X = np.array([[5, 2, 2, 1.2, .8, 5, 1, 3, .8, 1.2]], dtype=float)
    Z, meta = build_context_features([row], X, names)
    out = {m["name"]: Z[0, i] for i, m in enumerate(meta)}
    assert out["context.rank.absGap"] == 5
    assert out["context.schedule.home.restDays"] == 5
    assert out["context.schedule.home.known7Days"] == 1
    assert out["context.form.home.pointsPerGame"] == 1.6
    assert math.isclose(out["context.goals.attackDefenseIndex.sum"], 2)
    mismatch = dict(row, analysisInputs={"homeRank": "League3", "guestRank": "Other8"})
    M, _ = build_context_features([mismatch], X, names)
    assert np.isnan(M[0, :3]).all()
    empty = dict(row, preMatchHistory={"homeRecent": {"matches": []}, "awayRecent": {"matches": []}})
    E, _ = build_context_features([empty], X, names)
    assert np.isnan(E[0, 3:10]).all()
    low_n = X.copy(); low_n[0, 0] = 4
    N, _ = build_context_features([row], low_n, names)
    assert np.isnan(N[0, 10:]).all()
    assert parse_rank("League3-3") is None and parse_rank("0") is None
    return {"passed": True, "checks": ["same-prefix ranks", "reject cross-prefix/compound ranks", "safe target/time exclusion", "deduplication", "empty history is NaN", "minimum five-match guard", "arithmetic formulas"]}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", default="reports/draw-full-feature-study")
    parser.add_argument("--output", default="reports/draw-historical-value-study")
    args = parser.parse_args()
    src, dst = Path(args.input), Path(args.output)
    dst.mkdir(parents=True, exist_ok=True)
    rows = json.loads((src / "dataset.json").read_text(encoding="utf-8"))["rows"]
    with (src / "features.csv").open(encoding="utf-8-sig", newline="") as handle:
        reader = csv.DictReader(handle)
        names = [n for n in reader.fieldnames if n not in ("matchId", "resultLabel")]
        source_rows = list(reader)
    if [str(r["matchId"]) for r in rows] != [r["matchId"] for r in source_rows]:
        raise ValueError("Source rows are not aligned")
    X = np.asarray([[float(r[n]) if r[n] else np.nan for n in names] for r in source_rows])
    Z, meta = build_context_features(rows, X, names)
    mutated = [dict(r, result="mutated", homeScore=-999, awayScore=999, status="mutated") for r in rows]
    changed, _ = build_context_features(mutated, X, names)
    mutation_ok = np.array_equal(Z, changed, equal_nan=True)
    if not mutation_ok:
        raise AssertionError("Target label/score mutation altered context inputs")
    for j, item in enumerate(meta):
        n = int(np.isfinite(Z[:, j]).sum())
        item.update({"availableRows": n, "missingRows": len(rows) - n, "missingRate": (len(rows) - n) / len(rows)})
    np.savez_compressed(dst / "context-features.npz", X=Z, values=Z,
                        names=np.asarray([m["name"] for m in meta]),
                        matchIds=np.asarray([str(r["matchId"]) for r in rows]))
    (dst / "context-feature-manifest.json").write_text(json.dumps(meta, ensure_ascii=False, indent=2), encoding="utf-8")
    review = {"rows": len(rows), "features": len(meta), "targetLabelAndScoreMutationAllRows": mutation_ok,
              "syntheticTests": self_test(), "sourceSHA256": {str(src / n): sha(src / n) for n in ("dataset.json", "features.csv", "feature-manifest.json")},
              "adapterSHA256": sha(__file__), "serviceStarted": False, "sourceWrites": False,
              "limitations": ["Rank snapshots do not establish common match-time standings, season or motivation; no points table is available.",
                              "Observed history density is incomplete and has at most ten recorded recent matches; it does not establish fixture congestion or lineup rotation.",
                              "Form points are a synthetic 3-1-0 historical result score, not competition standings points.",
                              "Arithmetic goal indices are not calibrated expected goals or xG.",
                              "Schedule features use matchTime only; source date differs from matchTime's Beijing calendar date in some records."]}
    (dst / "context-adapter-review.json").write_text(json.dumps(review, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"rows": len(rows), "features": len(meta), "mutationPassed": mutation_ok, "available": {m["name"]: m["availableRows"] for m in meta}}, ensure_ascii=False))


if __name__ == "__main__":
    main()
