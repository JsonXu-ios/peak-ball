#!/usr/bin/env python3
"""Independently audit the fixed local sporttery cohort; no service access."""
from __future__ import annotations

import hashlib
import json
import math
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "reports/draw-full-feature-study/dataset.json"
OUT = ROOT / "reports/draw-sporttery-study/cohort-audit.json"


def valid_spf(values):
    if not isinstance(values, list) or len(values) != 3:
        return False
    try:
        return all(math.isfinite(float(v)) and float(v) > 1 for v in values)
    except (TypeError, ValueError):
        return False


def sporttery_quote(row):
    return next((q for q in row.get("companyQuotes", [])
                 if q.get("key") == "sporttery"), {}).get("euroOdds", {})


def main():
    rows = json.loads(SOURCE.read_text(encoding="utf-8"))["rows"]
    ids = [str(r["matchId"]) for r in rows]
    flags = {
        "jingcaiIdPresent": [bool(str(r.get("jingcaiId") or "").strip()) for r in rows],
        "sportteryOpeningComplete": [valid_spf(sporttery_quote(r).get("opening")) for r in rows],
        "sportteryLatestComplete": [valid_spf(sporttery_quote(r).get("latest")) for r in rows],
    }
    J, O, L = (flags[k] for k in ("jingcaiIdPresent", "sportteryOpeningComplete", "sportteryLatestComplete"))
    flags.update({
        "sportteryOpeningAndLatestComplete": [o and l for o, l in zip(O, L)],
        "jingcaiIdAndOpeningComplete": [j and o for j, o in zip(J, O)],
        "jingcaiIdAndLatestComplete": [j and l for j, l in zip(J, L)],
        "jingcaiIdAndBothQuotesComplete": [j and o and l for j, o, l in zip(J, O, L)],
        "jingcaiIdPresentButLatestMissing": [j and not l for j, l in zip(J, L)],
        "jingcaiIdPresentButOpeningMissing": [j and not o for j, o in zip(J, O)],
        "latestPresentButJingcaiIdMissing": [l and not j for j, l in zip(J, L)],
        "openingPresentButJingcaiIdMissing": [o and not j for j, o in zip(J, O)],
        "openingMissingButLatestPresent": [l and not o for o, l in zip(O, L)],
        "quoteOrIdEvidence": [j or o or l for j, o, l in zip(J, O, L)],
    })

    def summary(flag):
        chosen = [r for r, keep in zip(rows, flag) if keep]
        outcomes = Counter(r["result"] for r in chosen)
        n, draws = len(chosen), outcomes[1]
        target = math.ceil(draws * 900 / 970)
        # Avoid rounding a floating point 30% threshold up or down incorrectly.
        allowed_selected = target * 10 // 3
        return {"n": n, "home": outcomes[0], "draw": draws, "away": outcomes[2],
                "drawRate": draws/n if n else None,
                "coverageAnalogueOf900Over970": {
                    "targetRecall": 900/970, "targetDraws": target,
                    "minimumPrecision": .30,
                    "maximumSelectionsAtExactTargetDraws": allowed_selected,
                    "maximumFalseSelectionsAtExactTargetDraws": allowed_selected-target,
                    "allowedLostDraws": draws-target},
                "matchIds": [str(r["matchId"]) for r in chosen]}

    score_errors = []
    for r in rows:
        h, a = r.get("homeScore"), r.get("awayScore")
        ok = isinstance(h, (int, float)) and isinstance(a, (int, float)) and h >= 0 and a >= 0
        observed = 0 if ok and h > a else 1 if ok and h == a else 2 if ok else None
        if observed != r["result"]:
            score_errors.append(str(r["matchId"]))
    flat_open = [valid_spf(r.get("opening", {}).get("sporttery")) for r in rows]
    flat_latest = [valid_spf(r.get("latest", {}).get("sporttery")) for r in rows]
    opening_mismatch = [ids[i] for i in range(len(rows)) if O[i] != flat_open[i]]
    latest_mismatch = [ids[i] for i in range(len(rows)) if L[i] != flat_latest[i]]
    duplicate_ids = [k for k, n in Counter(ids).items() if n > 1]
    issues = []
    if len(rows) != 4161 or sum(r["result"] == 1 for r in rows) != 970:
        issues.append("frozen input universe changed")
    if score_errors:
        issues.append("score/result mismatch")
    if duplicate_ids:
        issues.append("duplicate matchId")
    if opening_mismatch or latest_mismatch:
        issues.append("nested and company quote validity disagree")
    data = {
        "purpose": "Independent read-only audit of sporttery cohort in fixed historical snapshot",
        "source": str(SOURCE.relative_to(ROOT)),
        "sourceSha256": hashlib.sha256(SOURCE.read_bytes()).hexdigest(),
        "auditScriptSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "scopeDefinitions": {
            "primary": "Nonblank jingcaiId, matching existing application lottery scope; missing odds DO NOT remove the match from this cohort",
            "quoteValidity": "Exactly 3 finite decimal SPF odds > 1 from companyQuotes[key=sporttery].euroOdds; simulated sporttery market is NOT evidence of actual quote availability",
            "quoteEvidenceWithoutId": "Keep separately: absent jingcaiId is an unknown marker, not proof that a match is non-lottery",
            "cohortVsInputs": "Restricting matches to jingcaiId does not restrict feature inputs to sporttery: William/average/Bet365 quotes, heat/Kelly and simulations must be explicitly labelled if used",
            "strictSportteryInputs": "Only raw sporttery SPF quotes and deterministic transforms of those same quotes; cross-company differences and average-odds/simulation fallback are excluded",
        },
        "allConfirmedFinished": summary([True]*len(rows)),
        "cohorts": {name: summary(flag) for name, flag in flags.items()},
        "checks": {"scoreResultMismatchIds": score_errors,
                   "duplicateMatchIds": duplicate_ids,
                   "flatVsQuoteOpeningMismatchIds": opening_mismatch,
                   "flatVsQuoteLatestMismatchIds": latest_mismatch,
                   "quoteSourcesAmongLatestComplete": dict(Counter(sporttery_quote(r).get("source") for r,l in zip(rows,L) if l)),
                   "allCaptureTimestampsKnown": all(r.get("timing",{}).get("sportteryCaptureKnown") is True for r,l in zip(rows,L) if l)},
        "interpretation": {
            "absolute900DrawTarget": "Not applicable to this subset: primary marked cohort contains only 124 observed draws, complete quote cohort only 119",
            "proportionalTarget": "Keep recall >=900/970 and precision >=30%; requires 116/124 draws among marked matches, 111/119 among complete-quote matches",
            "finiteHistoricalFit": "Outcome-selected threshold/feature searches on this same snapshot are historical fits, not independent prediction validation",
        },
        "issues": issues,
    }
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(json.dumps(data, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")
    print(json.dumps({"output": str(OUT.relative_to(ROOT)), "n": len(rows),
                      "marked": data["cohorts"]["jingcaiIdPresent"]["n"],
                      "markedDraws": data["cohorts"]["jingcaiIdPresent"]["draw"],
                      "issues": issues}, ensure_ascii=False))
    if issues:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
