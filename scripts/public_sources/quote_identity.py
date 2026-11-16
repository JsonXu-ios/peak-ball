"""Exact quote identity checks before joining API prices and page metadata.

Asian lines intentionally require Chinese source labels: the two providers use
different signs for numeric handicaps. A caller must not silently compare those
numeric representations as though they shared one convention.
"""
from __future__ import annotations

import re
import unicodedata
from collections.abc import Sequence
from decimal import Decimal, InvalidOperation
from fractions import Fraction


_DECIMAL = re.compile(r"[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?\Z")
_HALF = Decimal("0.5")
_ASIAN_WORDS = {"平": Decimal(0), "平手": Decimal(0), "半": _HALF,
                "半球": _HALF, "球半": Decimal("1.5")}
for _word, _integer in (("一", 1), ("二", 2), ("两", 2), ("三", 3),
                        ("四", 4), ("五", 5), ("六", 6), ("七", 7),
                        ("八", 8), ("九", 9), ("十", 10)):
    _ASIAN_WORDS[_word] = _ASIAN_WORDS[_word + "球"] = Decimal(_integer)
    _ASIAN_WORDS[_word + "半"] = _ASIAN_WORDS[_word + "球半"] = Decimal(_integer) + _HALF


def _text(value):
    if value is None or isinstance(value, bool):
        return None
    if not isinstance(value, (str, int, float, Decimal)):
        return None
    return unicodedata.normalize("NFKC", str(value)).strip()


def _number(value):
    text = _text(value)
    if text is None or not _DECIMAL.fullmatch(text):
        return None
    try:
        result = Decimal(text)
        return result if result.is_finite() else None
    except InvalidOperation:
        return None


def _price(value):
    text = _text(value)
    if text is not None:
        # The source displays a direction arrow next to the same numeric price.
        text = re.sub(r"[↑↓]$", "", text).strip()
    result = _number(text)
    return result if result is not None and result > 0 else None


def _asian(value):
    if not isinstance(value, str):
        return None
    text = re.sub(r"\s+", "", unicodedata.normalize("NFKC", value))
    direction = 1
    if text.startswith("受让"):
        direction, text = -1, text[2:]
    elif text.startswith("受"):
        direction, text = -1, text[1:]
    elif text.startswith("让"):
        text = text[1:]
    if "/" in text:
        pieces = text.split("/")
        if len(pieces) != 2:
            return None
        left, right = (_ASIAN_WORDS.get(piece) for piece in pieces)
        if left is None or right is None or right - left != _HALF:
            return None
        line = (left + right) / 2
    else:
        line = _ASIAN_WORDS.get(text)
    # Keep direction explicit even for a zero handicap. Do not erase an
    # inconsistent receive/give label just because -0 numerically equals 0.
    return (direction, line) if line is not None else None


def _totals(value):
    text = _text(value)
    if text is None:
        return None
    pieces = text.split("/")
    if len(pieces) not in (1, 2):
        return None
    values = []
    for piece in pieces:
        piece = piece.strip()
        if piece.endswith("球"):
            piece = piece[:-1].strip()
        result = _number(piece)
        if result is None or result < 0:
            return None
        values.append(Fraction(result))
    if len(values) == 2:
        # A split goal line means stakes are shared between adjacent half-goal
        # endpoints. Averaging arbitrary ranges would change settlement rules.
        if (values[1] - values[0] != Fraction(1, 2)
                or any(value.denominator not in (1, 2) for value in values)):
            return None
    # Decimal arithmetic inherits the process-wide precision. Fraction keeps
    # long source decimals exact when averaging a split line.
    return sum(values) / len(values)


def same_prices(first, second, kind):
    """Return whether both complete triples describe exactly the same quote.

    No numeric tolerance is applied. Missing/invalid values and unknown markets
    return False. For asian/totals the middle entry is the handicap/goal line;
    the outer entries must retain their order and have identical decimal prices.
    """
    if kind not in {"euro", "asian", "totals"}:
        return False
    if not all(isinstance(row, Sequence) and not isinstance(row, (str, bytes, bytearray))
               and len(row) == 3 for row in (first, second)):
        return False
    parsers = (_price, _price, _price) if kind == "euro" else (
        _price, _asian if kind == "asian" else _totals, _price)
    left = [parse(value) for parse, value in zip(parsers, first)]
    right = [parse(value) for parse, value in zip(parsers, second)]
    return all(value is not None for value in left + right) and left == right
