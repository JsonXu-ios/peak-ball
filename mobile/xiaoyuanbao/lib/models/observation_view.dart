import 'dart:convert';

typedef Json = Map<String, dynamic>;

Json object(dynamic value) {
  if (value is String) {
    try {
      value = jsonDecode(value);
    } catch (_) {
      return {};
    }
  }
  return value is Map ? Map<String, dynamic>.from(value) : {};
}

List<dynamic> array(dynamic value) => value is List ? value : [];
String text(dynamic value, [String fallback = '—']) =>
    value == null || '$value'.trim().isEmpty ? fallback : '$value'.trim();
double? number(dynamic value) {
  if (value is bool || value == null) return null;
  final parsed = value is num ? value.toDouble() : double.tryParse('$value');
  return parsed != null && parsed.isFinite ? parsed : null;
}

bool whole(dynamic value, [int minimum = 0]) {
  final n = number(value);
  return n != null && n >= minimum && n == n.roundToDouble();
}

String decimal(dynamic value, [int precision = 2]) =>
    number(value)?.toStringAsFixed(precision) ?? '—';
String percent(dynamic value, {bool signed = false}) {
  final n = number(value);
  return n == null
      ? '—'
      : '${signed && n > 0 ? '+' : ''}${n.toStringAsFixed(1)}%';
}

String amount(dynamic value) {
  final n = number(value);
  if (n == null) return '—';
  final sign = n > 0
      ? '+'
      : n < 0
      ? '−'
      : '';
  final absolute = n.abs();
  if (absolute >= 100000000) {
    return '$sign${(absolute / 100000000).toStringAsFixed(2)}亿';
  }
  if (absolute >= 10000) {
    return '$sign${(absolute / 10000).toStringAsFixed(1)}万';
  }
  return '$sign${absolute.toStringAsFixed(0)}元';
}

String day(DateTime date) => date.toIso8601String().substring(0, 10);
DateTime beijingNow() => DateTime.now().toUtc().add(const Duration(hours: 8));
String kickoff(dynamic raw) {
  final date = DateTime.tryParse(text(raw, ''));
  return date == null
      ? '—'
      : date
            .toUtc()
            .add(const Duration(hours: 8))
            .toIso8601String()
            .substring(11, 16);
}

class ObservationView {
  ObservationView(this.row);
  final Json row;
  String get id => text(row['matchId'], '');
  bool get sporttery => text(row['jingcaiId'], '').isNotEmpty;
  Json get analysis => object(row['frontendAnalysis']);
  Json get signal =>
      object(row[sporttery ? 'frozenDrawSignal' : 'nonSportterySignal']);
  bool get validSelectionMetadata =>
      !sporttery ||
      signal['selectionVersion'] == 'jc-h2h-required-v1' &&
          signal['minimumHeadToHead'] == 1 &&
          whole(signal['headToHeadSample']) &&
          (signal['state'] != 'selected' ||
              number(signal['headToHeadSample'])! >= 1);
  bool get selected => signal['state'] == 'selected' && validSelectionMetadata;
  bool get confirmed =>
      row['settled'] == true &&
      row['regulationSettled'] == true &&
      whole(row['homeScore']) &&
      whole(row['awayScore']);
  bool get draw =>
      confirmed && number(row['homeScore']) == number(row['awayScore']);
  String get primaryLabel {
    if (!validSelectionMetadata) return '待更新';
    if (selected) {
      return !confirmed
          ? '入选平 · 待赛果'
          : draw
          ? '命中平局'
          : '入选 · 未打平';
    }
    if (signal['state'] == 'not_selected') return '未入选';
    return '数据不足';
  }

  List<double?> get average {
    final local = object(analysis['localMarket']);
    final odds = object(local['odds']);
    final source = analysis.containsKey('averageEuroOdds')
        ? array(analysis['averageEuroOdds'])
        : local['key'] == 'localAverageOdds' && local['oddsAvailable'] == true
        ? [odds['home'], odds['draw'], odds['away']]
        : [];
    return List.generate(
      3,
      (i) =>
          i < source.length && source[i] is num && (number(source[i]) ?? 0) > 0
          ? number(source[i])
          : null,
    );
  }

  String? get alternative {
    if (!selected) return null;
    final a = average;
    if (a[0] == null ||
        a[2] == null ||
        a[0]! <= 1 ||
        a[2]! <= 1 ||
        (a[0]! - a[2]!).abs() <= 1e-9) {
      return null;
    }
    return a[0]! < a[2]! ? 'home' : 'away';
  }

  String get alternativeLabel => alternative == null
      ? '暂无'
      : alternative == 'home'
      ? '主胜'
      : '客胜';
  bool get alternativeHit =>
      alternative != null &&
      confirmed &&
      !draw &&
      (number(row['homeScore'])! > number(row['awayScore'])!
              ? 'home'
              : 'away') ==
          alternative;
  String get alternativeOutcome => alternative == null
      ? (selected ? '均赔不足或同价' : '未入选')
      : !confirmed
      ? '待赛果核对'
      : draw
      ? '主选平命中'
      : alternativeHit
      ? '次选命中'
      : '主次均未命中';
  List<double?> get probabilities {
    final result = ['winProbability', 'drawProbability', 'loseProbability'].map(
      (key) {
        final raw = analysis[key];
        final n = raw is num ? number(raw) : null;
        return n != null && n >= 0 && n <= 100 ? n : null;
      },
    ).toList();
    return result.any((n) => n != null && n > 0) ? result : [null, null, null];
  }

  List<double?> get differences {
    final quotes = array(row['companyQuotes']).map(object).toList();
    List<dynamic> prices(String key) => array(
      object(
        quotes.where((q) => q['key'] == key).firstOrNull?['euroOdds'],
      )['latest'],
    );
    final w = prices('william'), s = prices('sporttery');
    return List.generate(3, (i) {
      final a = i < w.length ? number(w[i]) : null,
          b = i < s.length ? number(s[i]) : null;
      return a == null || b == null || a <= 0 || b <= 0
          ? null
          : ((a - b) * 100).round() / 100;
    });
  }

  String get totalsLabel {
    if (!sporttery) return '非竞彩';
    final t = object(row['totalsPrediction']);
    if (!_validTotals(t)) return '暂无';
    return '${t['direction'] == 'over' ? '大' : '小'} ${number(t['line'])!.toString().replaceFirst(RegExp(r'\.0$'), '')}';
  }

  bool _validTotals(Json t) {
    final line = number(t['line']);
    final direction = t['direction'];
    return sporttery &&
        t['state'] == 'recommended' &&
        text(t['modelVersion'], '').isNotEmpty &&
        ['over', 'under'].contains(direction) &&
        line != null &&
        line > 0 &&
        (line * 4 - (line * 4).round()).abs() < 1e-9 &&
        (number(t[direction == 'over' ? 'overWater' : 'underWater']) ?? 0) > 0;
  }

  String get totalsOutcome {
    final t = object(row['totalsPrediction']);
    if (!_validTotals(t)) return '';
    if (!confirmed) return '待赛果核对';
    final q = (number(t['line'])! * 4).round();
    final parts = q.isEven ? [q / 4] : [(q - 1) / 4, (q + 1) / 4];
    final goals = number(row['homeScore'])! + number(row['awayScore'])!;
    int wins = 0, losses = 0;
    for (final part in parts) {
      final delta = t['direction'] == 'over' ? goals - part : part - goals;
      if (delta > 0) wins++;
      if (delta < 0) losses++;
    }
    return wins == parts.length
        ? '全赢'
        : losses == parts.length
        ? '全输'
        : wins > 0
        ? '赢半'
        : losses > 0
        ? '输半'
        : '走盘';
  }

  Json standing(bool home) {
    final history = object(row['history']);
    final summary = object(
      history['leagueSummary'] ?? history['league_summary'],
    );
    return object(object(summary[home ? 'home' : 'guest'])['all']);
  }

  String standingText(bool home) {
    final s = standing(home);
    return '排名 ${whole(s['rank'], 1) ? s['rank'] : '—'}  已赛 ${whole(s['game']) ? s['game'] : '—'}';
  }

  String ratio(String key) {
    final history = object(row[key]);
    return (number(history['sample']) ?? 0) > 0
        ? '${history['draws']}/${history['sample']}'
        : '—';
  }

  String get secondaryLabel {
    if (!sporttery) return '非竞彩独立观察';
    final secondary = object(row['secondaryDecision']);
    final reasons = array(
      secondary['reasons'],
    ).whereType<String>().where((s) => s.trim().isNotEmpty);
    if (secondary['version'] != 'secondary-v4-h2h-required' ||
        secondary['threshold'] != .225 ||
        secondary['minimumHeadToHead'] != 1 ||
        !whole(secondary['headToHeadSample']) ||
        !validSelectionMetadata ||
        reasons.isEmpty && text(secondary['reason'], '').isEmpty) {
      return '二次筛选待更新';
    }
    if (secondary['decision'] == 'retained') {
      return selected && number(secondary['headToHeadSample'])! >= 1
          ? '二次保留'
          : '二次筛选待更新';
    }
    return secondary['decision'] == 'rejected'
        ? '二次剔除'
        : secondary['decision'] == 'not_selected'
        ? '未二次入选'
        : '二次数据不足';
  }

  String get scoreLabel {
    if (secondaryLabel != '二次保留') return '未二次入选';
    final score = object(row['scoreRecommendation']);
    final scores = array(score['scores']).map(object).toList();
    if (score['version'] != 'draw-score-dynamic-v2' ||
        score['state'] != 'recommended' ||
        scores.length != 2 ||
        scores.any(
          (s) =>
              !whole(s['home']) ||
              number(s['home'])! > 6 ||
              s['home'] != s['away'],
        ) ||
        scores[0]['home'] == scores[1]['home'] ||
        text(score['reason'], '').isEmpty) {
      return '暂无推荐';
    }
    return '首选 ${scores[0]['home']}:${scores[0]['away']} · 备选 ${scores[1]['home']}:${scores[1]['away']}';
  }
}
