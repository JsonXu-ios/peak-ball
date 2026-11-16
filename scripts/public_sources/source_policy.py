"""Read eligibility for the current independent source allocation.

Old evidence stays in SQLite for inspection. Neither a URL nor a successful
historical import can implicitly opt that evidence into the current policy.
"""
from __future__ import annotations

import json
import re
from datetime import datetime, timedelta, timezone
from urllib.parse import urlsplit


SOURCE_POLICY = "sporttery_votes_zgzcw_v1"
VOTE_KINDS = frozenset({"support_had", "support_hhad"})
_CHINA = timezone(timedelta(hours=8))
_KICKOFF = re.compile(r"\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:\d{2})?")


def parse_acquisition(value):
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except ValueError:
            return {}
    return value if isinstance(value, dict) else {}


def _uses_domain(evidence, domain):
    if isinstance(evidence, dict):
        return any(_uses_domain(value, domain) for value in evidence.values())
    if isinstance(evidence, (list, tuple)):
        return any(_uses_domain(value, domain) for value in evidence)
    if isinstance(evidence, str):
        try:
            host = (urlsplit(evidence).hostname or "").lower().rstrip(".")
        except ValueError:
            return False
        return host == domain or host.endswith("." + domain)
    return False


def allows_snapshot(kind, source, acquisition):
    metadata = parse_acquisition(acquisition)
    is_vote = kind in VOTE_KINDS
    expected_source = "sporttery" if is_vote else "zgzcw"
    if source != expected_source or metadata.get("sourcePolicy") != SOURCE_POLICY:
        return False
    # Re-normalizing old evidence must not launder its original provider by
    # stamping a new policy on the top-level record. Empty fixture receipts
    # remain valid; this checks cross-provider data rather than a URL allowlist.
    excluded_domain = "zgzcw.com" if is_vote else "sporttery.cn"
    return not any(_uses_domain(metadata.get(key), excluded_domain) for key in ("interfaces", "pageFields"))


def effective_match(match, head):
    """Only an accepted ZGCW match head supplies the current kickoff.

    The caller must select ``head`` with ``allows_snapshot`` first. A missing
    or malformed head cannot borrow the old value from ``ps_matches``.
    """
    result = dict(match)
    result["kickoff"] = None
    stamp = head.get("kickoff") if isinstance(head, dict) else None
    if isinstance(stamp, str) and _KICKOFF.fullmatch(stamp):
        try:
            kickoff = datetime.fromisoformat(stamp)
            if kickoff.tzinfo is not None:
                kickoff = kickoff.astimezone(_CHINA).replace(tzinfo=None)
            result["kickoff"] = kickoff.isoformat(sep=" ", timespec="seconds")
        except ValueError:
            pass
    return result
