#!/usr/bin/env python3
"""Label-blind adapters for the draw conflict/anti-favourite study.

Only prior-match records and the supplied frozen market-feature matrix are
read. No fitting, dataset/file reads, network, database, or service operations
occur in ``build_conflict_features``. Missing numeric observations remain NaN;
zero is an observation. The smallest-two difference pairs are explicitly a
secondary sensitivity view, never an alias for the largest-two definition.
"""
from __future__ import annotations

import argparse
import copy
import hashlib
import json
from pathlib import Path

import numpy as np

from draw_full_features import EPS, safe_records

OUTCOMES = ("home", "draw", "away")
PAIR_OUTCOMES = (("home_draw", (0, 1), "胜平"),
                 ("draw_away", (1, 2), "平负"),
                 ("home_away", (0, 2), "胜负"))
PHASE_LABELS = {"opening": "初盘", "latest": "即时盘"}
COMPARISON_LABELS = {"sporttery": "竞彩", "average": "平均欧赔"}


def _history_row(row):
    """Expose only target identity/time used by the frozen safety filter.

    A present canonical container, including an explicit null, takes priority
    over an alias. Never repair an explicitly missing canonical history by
    silently borrowing another source.
    """
    if not isinstance(row, dict):
        return {}
    container = row.get("preMatchHistory") if "preMatchHistory" in row else row.get("prematch_history")
    return {"matchId": row.get("matchId"), "matchTime": row.get("matchTime"),
            "preMatchHistory": container}


def _recent_values(row, key):
    records = safe_records(row, key)
    if records is None:
        return [np.nan] * 5
    # The observed page window is at most ten matches. If every observed
    # match satisfies the condition, the streak is a lower bound, not proof
    # that the preceding eleventh (or unobserved) match broke the sequence.
    records = records[:10]
    selected = records[:3]
    n = len(selected)
    if not n:
        return [np.nan, np.nan, 0.0, np.nan, np.nan]
    win_streak = unbeaten_streak = 0
    for _, _, goals_for, goals_against in records:
        if goals_for <= goals_against:
            break
        win_streak += 1
    for _, _, goals_for, goals_against in records:
        if goals_for < goals_against:
            break
        unbeaten_streak += 1
    wins = sum(goals_for > goals_against for _, _, goals_for, goals_against in selected)
    return [float(win_streak), float(unbeaten_streak), float(n), float(wins), wins / n]


def _pair_values(raw, largest):
    """Exact pair indicators plus a boundary-tie/three-member indicator.

    Ties use the same absolute EPS=1e-9 as the frozen extractor. A three-way
    membership is represented by ambiguous=1 and all exact pairs=0. Missing
    input is represented by four NaNs, never by a negative pair observation.
    """
    if not np.isfinite(raw).all():
        return [np.nan] * 4
    absolute = np.abs(raw)
    boundary = np.sort(absolute)[1]
    selected = absolute >= boundary - EPS if largest else absolute <= boundary + EPS
    members = tuple(np.flatnonzero(selected))
    return [float(members == pair) for _, pair, _ in PAIR_OUTCOMES] + [float(len(members) != 2)]


def build_conflict_features(rows, X, names, manifest):
    """Return ``(Z, meta)``; Z contains only the newly adapted features.

    ``rows`` and ``X`` must have identical order. ``names`` identifies X's
    columns; ``manifest`` is column provenance, not a source of thresholds or
    labels. Original 470 columns remain the caller's responsibility and are
    never modified or duplicated here. ``meta`` is a column-aligned list.

    Streaks count the current team's result regardless of its historical home
    or away venue. Safe records use the frozen ``safe_records`` function:
    target ID excluded, valid integer prior scores, unique records, and prior
    kickoff + two hours strictly earlier than target kickoff. Target IDs and
    time serve this exclusion only; target scores/status/results are not read.
    """
    rows, names = list(rows), list(names)
    matrix = np.asarray(X, dtype=np.float64)
    if matrix.ndim != 2 or matrix.shape != (len(rows), len(names)):
        raise ValueError("X must be a rows-by-names matrix aligned with rows")
    if not all(isinstance(name, str) for name in names) or len(set(names)) != len(names):
        raise ValueError("names must be unique feature strings")
    if not isinstance(manifest, (list, tuple)) or any(not isinstance(item, dict) or not isinstance(item.get("name"), str) for item in manifest):
        raise ValueError("manifest must be a sequence of named metadata mappings")
    source_meta = {item["name"]: item for item in manifest}
    if len(source_meta) != len(manifest):
        raise ValueError("manifest feature names must be unique")
    indices = {name: i for i, name in enumerate(names)}
    history_rows = [_history_row(row) for row in rows]
    values, metadata = [], []

    def emit(name, label, column, family, source, view, unit, **extra):
        values.append(np.asarray(column, dtype=np.float64))
        metadata.append({"name": name, "label": label, "family": family,
                         "source": source, "view": view, "unit": unit, **extra})

    for side, key, title in (("home", "homeRecent", "主队"), ("away", "awayRecent", "客队")):
        block = np.asarray([_recent_values(row, key) for row in history_rows], dtype=np.float64).reshape((len(rows), 5))
        prefix = "conflict.recent." + side
        source = "preMatchHistory." + key + ".matches; prematch_history alias only when canonical key absent; draw_full_features.safe_records"
        history_guard = [{"feature": f"safeRecent.{side}.10.n", "op": "ge", "value": 1}]
        streak_note = "Observed current streak in safe latest <=10 records; all-observed satisfying is a lower bound, not a known break"
        for index, metric, label, unit in ((0, "winStreak", "最近连续胜场", "count"),
                                           (1, "unbeatenStreak", "最近连续不败场", "count"),
                                           (2, "3.n", "赛前近3场实际样本数（仅分母）", "count"),
                                           (3, "3.wins", "赛前近3场胜场", "count"),
                                           (4, "3.winRate", "赛前近3场胜率", "probability")):
            guards = [] if index == 2 else history_guard if index < 2 else [{"feature": prefix + ".3.n", "op": "ge", "value": 3}]
            emit(prefix + "." + metric, title + label, block[:, index], "conflictSafeRecent", source,
                 "safeHistory", unit, guards=guards, denominatorOnly=index == 2,
                 definition=streak_note if index < 2 else "Last min(3,n) safe matches; wins / actual n; require n>=3 for complete-window candidate use",
                 safety="target ID excluded; prior kickoff + 2h < target; venue-relative outcome; duplicate IDs removed")

    for phase in ("opening", "latest"):
        for comparison in ("sporttery", "average"):
            raw_names = [f"difference.{phase}.{comparison}.{outcome}" for outcome in OUTCOMES]
            has_sources = all(name in indices and name in source_meta for name in raw_names)
            raw = matrix[:, [indices[name] for name in raw_names]] if has_sources else np.full((len(rows), 3), np.nan)
            views = {source_meta[name].get("view") for name in raw_names if name in source_meta}
            view = next(iter(views)) if len(views) == 1 and next(iter(views)) in ("market", "safeHistory", "snapshotDerived") else "snapshotDerived"
            source = "; ".join(dict.fromkeys(str(source_meta[name].get("source", name)) for name in raw_names if name in source_meta)) or "missing raw same-phase difference triple"
            for largest, rank_name, label in ((True, "absLargestTwo", "差值绝对值最大两项"),
                                              (False, "absSmallestTwo", "差值绝对值最小两项（次要灵敏度）")):
                block = np.asarray([_pair_values(row, largest) for row in raw], dtype=np.float64).reshape((len(rows), 4))
                prefix = f"conflict.difference.{phase}.{comparison}.{rank_name}"
                labels = [(pair_name, pair_title) for pair_name, _, pair_title in PAIR_OUTCOMES] + [("ambiguous", "边界并列纳入三项")]
                for index, (pair_name, pair_title) in enumerate(labels):
                    emit(prefix + "." + pair_name, PHASE_LABELS[phase] + "威廉减" + COMPARISON_LABELS[comparison] + label + ":" + pair_title,
                         block[:, index], "conflictDifferencePair", source, view, "indicator", guards=[],
                         requires=raw_names, missingSourceFeatures=[name for name in raw_names if name not in indices or name not in source_meta],
                         transform="abs", rankDirection="largest" if largest else "smallest", secondary=not largest,
                         definition="All three raw differences required finite; include every item tied at second rank within absolute EPS=1e-9; exact pairs only when two members; three members sets ambiguous=1 and every exact pair=0")
    result = np.column_stack(values) if values else np.empty((len(rows), 0), dtype=np.float64)
    assert result.shape == (len(rows), len(metadata))
    assert len({item["name"] for item in metadata}) == len(metadata)
    assert np.isfinite(result[~np.isnan(result)]).all()
    return result, metadata


def self_test():
    """Synthetic inputs only: never read a study dataset or outcomes."""
    from draw_full_features import build_features

    target = {"matchId": "target", "matchTime": "2026-10-24T12:00:00+08:00",
              "preMatchHistory": {"homeRecent": {"matches": []}, "awayRecent": {"matches": []}}}

    def record(match_id, day, gf, ga, venue="home"):
        hs, aws = (gf, ga) if venue == "home" else (ga, gf)
        return {"matchId": match_id, "matchTime": f"2026-10-{day:02d}T12:00:00+08:00",
                "homeScore": hs, "awayScore": aws, "venue": venue}

    target["preMatchHistory"]["homeRecent"]["matches"] = [
        record("h1", 23, 2, 0, "away"), record("h2", 22, 3, 0), record("h3", 21, 1, 1, "away"),
        record("h4", 20, 1, 0), record("h5", 19, 0, 2, "away"),
        record("target", 18, 9, 0), record("future", 25, 9, 0),
        {**record("boundary", 24, 9, 0), "matchTime": "2026-10-24T10:00:00+08:00"},
        {**record("tooLate", 24, 9, 0), "matchTime": "2026-10-24T02:00:01Z"},
        {**record("badTime", 23, 9, 0), "matchTime": "2026-10-23T12:00:00"},
        {**record("badScore", 23, 9, 0), "homeScore": -1},
    ]
    target["preMatchHistory"]["homeRecent"]["matches"].append(copy.deepcopy(target["preMatchHistory"]["homeRecent"]["matches"][0]))
    target["preMatchHistory"]["awayRecent"]["matches"] = [record("a1", 23, 0, 1, "away"), record("a2", 22, 3, 0, "away"), record("a3", 21, 2, 1)]
    changed = copy.deepcopy(target)
    changed.update(result=0, isDraw=False, homeScore=99, guestScore=0, awayScore=0, status=99,
                   score="99:0", predictionHit=True, subtype="not a feature")
    alias = {"matchId": target["matchId"], "matchTime": target["matchTime"],
             "prematch_history": copy.deepcopy(target["preMatchHistory"])}
    empty = {"matchId": "empty", "matchTime": target["matchTime"],
             "preMatchHistory": {"homeRecent": {"matches": []}, "awayRecent": {"matches": []}}}
    zero = {"matchId": "zero", "matchTime": target["matchTime"],
            "preMatchHistory": {"homeRecent": {"matches": [record("z0", 23, 0, 0)]}}}
    rows = [target, changed, alias, empty, {}, zero]
    x, manifest = build_features(rows)
    names = [item["name"] for item in manifest]
    # Supply exact original difference values, independently of target rows.
    difference_names = [f"difference.opening.sporttery.{outcome}" for outcome in OUTCOMES]
    for i, values in enumerate(([1, 2, 3], [1, 2, 3], [0, 0, 0], [5, 2, 2], [np.nan, 2, 3], [0, -4, 5])):
        for feature, value in zip(difference_names, values):
            x[i, names.index(feature)] = value
    original_x, original_rows = x.copy(), copy.deepcopy(rows)
    z, meta = build_conflict_features(rows, x, names, manifest)
    index = {item["name"]: i for i, item in enumerate(meta)}
    get = lambda row, name: z[row, index[name]]
    hp = "conflict.recent.home."
    ap = "conflict.recent.away."
    lp = "conflict.difference.opening.sporttery.absLargestTwo."
    sp = "conflict.difference.opening.sporttery.absSmallestTwo."
    assert get(0, hp + "winStreak") == 2 and get(0, hp + "unbeatenStreak") == 4
    assert get(0, hp + "3.wins") == 2 and get(0, hp + "3.winRate") == 2 / 3
    assert get(0, ap + "winStreak") == get(0, ap + "unbeatenStreak") == 0
    assert get(0, ap + "3.wins") == 2
    assert np.array_equal(z[0], z[1], equal_nan=True)
    assert all(get(0, hp + metric) == get(2, hp + metric) for metric in ("winStreak", "unbeatenStreak", "3.n", "3.wins", "3.winRate"))
    assert get(3, hp + "3.n") == 0 and np.isnan(get(3, hp + "winStreak")) and np.isnan(get(4, hp + "3.n"))
    assert get(5, hp + "winStreak") == 0 and get(5, hp + "unbeatenStreak") == 1 and get(5, hp + "3.winRate") == 0
    assert get(0, lp + "draw_away") == get(0, sp + "home_draw") == 1
    assert get(2, lp + "ambiguous") == get(2, sp + "ambiguous") == 1
    assert all(get(2, lp + pair) == 0 for pair, _, _ in PAIR_OUTCOMES)
    assert get(3, lp + "ambiguous") == 1 and get(3, sp + "draw_away") == 1
    assert all(np.isnan(get(4, lp + pair)) for pair, _, _ in PAIR_OUTCOMES)
    assert np.isnan(get(4, lp + "ambiguous")) and get(5, lp + "draw_away") == 1
    assert np.array_equal(x, original_x, equal_nan=True) and rows == original_rows
    # Reordering input columns and manifest cannot change definitions/results.
    order = list(reversed(range(len(names))))
    reordered, reordered_meta = build_conflict_features(rows, x[:, order], [names[i] for i in order], list(reversed(manifest)))
    assert np.array_equal(z, reordered, equal_nan=True) and meta == reordered_meta
    blank, blank_meta = build_conflict_features([], np.empty((0, len(names))), names, manifest)
    assert blank.shape == (0, len(meta)) and blank_meta == meta
    # Explicit missing canonical history cannot borrow a populated alias.
    canonical_null = {**alias, "preMatchHistory": None}
    c, _ = build_conflict_features([canonical_null], x[:1], names, manifest)
    assert np.isnan(c[0, index[hp + "winStreak"]])
    # Exact two-hour boundary rejected, one second earlier valid (aware UTC).
    boundary_row = copy.deepcopy(empty)
    boundary_row["preMatchHistory"]["homeRecent"]["matches"] = [
        {**record("before", 24, 1, 0), "matchTime": "2026-10-24T01:59:59Z"}]
    b, _ = build_conflict_features([boundary_row], x[:1], names, manifest)
    assert b[0, index[hp + "winStreak"]] == 1
    # A capped streak of ten is legal; the eleventh match is not consulted.
    ten = copy.deepcopy(empty)
    ten["preMatchHistory"]["homeRecent"]["matches"] = [record("ten" + str(i), 23 - i, 1, 0) for i in range(11)]
    t, _ = build_conflict_features([ten], x[:1], names, manifest)
    assert t[0, index[hp + "winStreak"]] == t[0, index[hp + "unbeatenStreak"]] == 10
    assert _pair_values(np.array([np.inf, 1, 2]), True) != [0, 0, 0, 0]
    assert np.isnan(_pair_values(np.array([np.inf, 1, 2]), True)).all()
    assert _pair_values(np.array([1, 2, 2 + EPS / 2]), False)[3] == 1
    assert _pair_values(np.array([1, 2, 2 + EPS * 2]), False)[0] == 1
    # Access sentinel raises if the adapter tries to inspect a target label.
    class LabelReadSentinel(dict):
        forbidden = {"result", "isDraw", "homeScore", "guestScore", "awayScore", "status", "score", "predictionHit", "subtype"}
        def get(self, key, default=None):
            if key in self.forbidden:
                raise AssertionError("target label read: " + key)
            return super().get(key, default)
        def __getitem__(self, key):
            if key in self.forbidden:
                raise AssertionError("target label read: " + key)
            return super().__getitem__(key)
    s, _ = build_conflict_features([LabelReadSentinel(changed)], x[:1], names, manifest)
    assert np.array_equal(s[0], z[1], equal_nan=True)
    return {"passed": True, "syntheticOnly": True, "rows": len(rows), "addedFeatures": len(meta),
            "historyFeatures": 10, "differencePairFeatures": 32,
            "targetLabelMutationInvariant": True, "targetLabelAccessSentinel": True,
            "targetIdExcluded": True, "futureAndTwoHourBoundaryExcluded": True,
            "strictEarlierThanBoundaryAccepted": True, "historicalVenueRelativeOutcome": True,
            "duplicateAndMalformedHistoryExcluded": True, "zeroPreserved": True,
            "missingAndInfinityRemainNaN": True, "boundaryTiesNotForcedIntoPair": True,
            "smallestTwoExplicitlySecondary": True, "canonicalNullNotBackfilledFromAlias": True,
            "streakWindowCappedAtTen": True, "columnAndManifestOrderInvariant": True,
            "emptyRowsSupported": True, "inputsNotMutated": True,
            "columns": meta,
            "noDatasetLabelsFilesNetworkDatabaseOrServicesReadByAdapter": True}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--self-test-out", type=Path)
    args = parser.parse_args()
    if not args.self_test:
        parser.error("only --self-test is supported; study data are supplied through the API")
    result = self_test()
    result["scriptSHA256"] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    if args.self_test_out:
        args.self_test_out.parent.mkdir(parents=True, exist_ok=True)
        args.self_test_out.write_text(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")
    print(json.dumps({key: value for key, value in result.items() if key != "columns"}, ensure_ascii=False))
