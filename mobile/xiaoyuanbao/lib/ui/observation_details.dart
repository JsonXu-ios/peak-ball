import 'package:flutter/material.dart';
import '../models/observation_view.dart';

const _ink = Color(0xFF142B4B);
const _muted = Color(0xFF697B92);

class ObservationDetails extends StatefulWidget {
  const ObservationDetails({super.key, required this.row});
  final Json row;
  @override
  State<ObservationDetails> createState() => _ObservationDetailsState();
}

class _ObservationDetailsState extends State<ObservationDetails> {
  int _tab = 0;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(12, 0, 12, 16),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SegmentedButton<int>(
          segments: const [
            ButtonSegment(value: 0, label: Text('战绩')),
            ButtonSegment(value: 1, label: Text('指数')),
            ButtonSegment(value: 2, label: Text('盈亏')),
          ],
          selected: {_tab},
          showSelectedIcon: false,
          onSelectionChanged: (s) => setState(() => _tab = s.first),
        ),
        const SizedBox(height: 14),
        if (_tab == 0)
          ..._history()
        else if (_tab == 1)
          ..._quotes()
        else
          ..._profits(),
      ],
    ),
  );

  Widget _note(String content) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: Text(
      content,
      style: const TextStyle(fontSize: 13, color: _muted, height: 1.6),
    ),
  );
  Widget _block(String title, List<Widget> children) => Container(
    margin: const EdgeInsets.only(bottom: 14),
    padding: const EdgeInsets.all(12),
    decoration: BoxDecoration(
      border: Border.all(color: const Color(0xFFE6EBF3)),
      borderRadius: BorderRadius.circular(12),
    ),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          title,
          style: const TextStyle(
            fontSize: 16,
            fontWeight: FontWeight.w700,
            color: _ink,
          ),
        ),
        const SizedBox(height: 12),
        ...children,
      ],
    ),
  );

  Widget _table(List<List<String>> rows, {bool signed = false}) => Table(
    defaultVerticalAlignment: TableCellVerticalAlignment.middle,
    columnWidths: const {0: FlexColumnWidth(1.15)},
    children: rows
        .asMap()
        .entries
        .map(
          (entry) => TableRow(
            decoration: BoxDecoration(
              color: entry.key == 0 ? const Color(0xFFF3F6FB) : Colors.white,
              border: const Border(
                bottom: BorderSide(color: Color(0xFFEEF1F6)),
              ),
            ),
            children: entry.value.asMap().entries.map((cell) {
              final tone =
                  signed &&
                      entry.key > 1 &&
                      cell.key > 0 &&
                      cell.value.startsWith('+')
                  ? const Color(0xFFC64250)
                  : signed &&
                        entry.key > 1 &&
                        cell.key > 0 &&
                        (cell.value.startsWith('−') ||
                            cell.value.startsWith('-'))
                  ? const Color(0xFF168263)
                  : cell.key == 0 || entry.key == 0
                  ? _muted
                  : _ink;
              return Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 3,
                  vertical: 10,
                ),
                child: Text(
                  cell.value,
                  textAlign: cell.key == 0 ? TextAlign.left : TextAlign.center,
                  style: TextStyle(
                    fontSize: 13,
                    color: tone,
                    height: 1.4,
                    fontWeight: cell.key > 0 && entry.key > 0
                        ? FontWeight.w600
                        : FontWeight.w400,
                  ),
                ),
              );
            }).toList(),
          ),
        )
        .toList(),
  );

  List<Widget> _history() {
    final row = widget.row, view = ObservationView(row);
    return [
      _note('仅使用该场开赛前已完赛的战绩；日期按北京时间显示。'),
      _block('联赛排名与赛程', [
        _table([
          ['球队', '总榜排名', '已赛', '积分'],
          for (final home in [true, false])
            [
              home ? '主队' : '客队',
              text(view.standing(home)['rank']),
              text(view.standing(home)['game']),
              text(view.standingPoints(home)),
            ],
        ]),
        const SizedBox(height: 10),
        _note('取本场保存的联赛总榜。缺少榜单数据时显示「—」。'),
      ]),
      for (final section in [
        ('homeRecent', '${text(row['home'])} · 近10场'),
        ('awayRecent', '${text(row['guest'])} · 近10场'),
        ('headToHead', '双方历史交锋'),
      ])
        _historySection(
          section.$2,
          object(row[section.$1]),
          text(row[section.$1 == 'awayRecent' ? 'guest' : 'home'], ''),
          headToHead: section.$1 == 'headToHead',
        ),
    ];
  }

  Widget _historySection(
    String title,
    Json history,
    String team, {
    required bool headToHead,
  }) {
    final matches = array(history['matches']).map(object).toList();
    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      decoration: BoxDecoration(
        border: Border.all(color: const Color(0xFFE6EBF3)),
        borderRadius: BorderRadius.circular(12),
      ),
      child: ExpansionTile(
        key: PageStorageKey('${widget.row["matchId"]}/$title'),
        tilePadding: const EdgeInsets.symmetric(horizontal: 12),
        childrenPadding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
        shape: const Border(),
        collapsedShape: const Border(),
        title: Text(
          title,
          style: const TextStyle(
            fontSize: 15,
            color: _ink,
            fontWeight: FontWeight.w600,
          ),
        ),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              (number(history['sample']) ?? 0) > 0
                  ? '${history['sample']}场 · ${history['draws']}平'
                  : '暂无有效记录',
              style: const TextStyle(fontSize: 13, color: _muted),
            ),
            if (matches.isNotEmpty) ...[
              const SizedBox(height: 5),
              Text(
                headToHead ? '近 → 远 · 以$team为准' : '近 → 远',
                style: const TextStyle(fontSize: 12, color: _muted),
              ),
              const SizedBox(height: 4),
              SingleChildScrollView(
                // The parent tile stores a bool; this child stores a scroll offset.
                key: const PageStorageKey<String>('form-strip-offset'),
                scrollDirection: Axis.horizontal,
                child: Row(
                  children: [
                    for (final match in matches)
                      Padding(
                        padding: const EdgeInsets.only(right: 3),
                        child: _resultBadge(historyResult(match, team)),
                      ),
                  ],
                ),
              ),
            ],
          ],
        ),
        children: matches.isEmpty
            ? [_note('暂无有效历史明细。')]
            : matches
                  .map(
                    (m) => Container(
                      padding: const EdgeInsets.symmetric(vertical: 11),
                      decoration: const BoxDecoration(
                        border: Border(
                          top: BorderSide(color: Color(0xFFEEF1F6)),
                        ),
                      ),
                      child: Column(
                        children: [
                          Row(
                            children: [
                              Text(
                                text(m['date']),
                                style: const TextStyle(
                                  fontSize: 12,
                                  color: _muted,
                                ),
                              ),
                              const SizedBox(width: 8),
                              Expanded(
                                child: Text(
                                  text(m['league']),
                                  style: const TextStyle(
                                    fontSize: 12,
                                    color: _muted,
                                  ),
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 7),
                          Row(
                            children: [
                              Expanded(
                                child: Text(
                                  text(m['home']),
                                  style: const TextStyle(
                                    fontSize: 14,
                                    color: _ink,
                                  ),
                                ),
                              ),
                              Padding(
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 12,
                                ),
                                child: Text(
                                  '${text(m['homeScore'])} : ${text(m['awayScore'])}',
                                  style: TextStyle(
                                    fontSize: 16,
                                    fontWeight: FontWeight.w700,
                                    color: m['homeScore'] == m['awayScore']
                                        ? const Color(0xFF168263)
                                        : _ink,
                                  ),
                                ),
                              ),
                              Expanded(
                                child: Text(
                                  text(m['guest']),
                                  textAlign: TextAlign.right,
                                  style: const TextStyle(
                                    fontSize: 14,
                                    color: _ink,
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                  )
                  .toList(),
      ),
    );
  }

  Widget _resultBadge(String label) {
    final (background, foreground) = switch (label) {
      '胜' => (const Color(0xFFDCF3E9), const Color(0xFF126747)),
      '平' => (const Color(0xFFFFF0CC), const Color(0xFF81560B)),
      '负' => (const Color(0xFFFCE3E5), const Color(0xFF9F3441)),
      _ => (const Color(0xFFEDF0F5), _muted),
    };
    return DecoratedBox(
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(5),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
        child: Text(
          label,
          style: TextStyle(
            fontSize: 13,
            fontWeight: FontWeight.w700,
            color: foreground,
          ),
        ),
      ),
    );
  }

  List<String> _triple(dynamic value) {
    final values = array(value);
    return List.generate(3, (i) => i < values.length ? text(values[i]) : '—');
  }

  List<Widget> _quotes() {
    final row = widget.row,
        view = ObservationView(row),
        analysis = object(row['frontendAnalysis']);
    final stages = array(analysis['comparisonStages']).map(object).toList();
    Json stage(String key) =>
        stages.where((s) => s['key'] == key).firstOrNull ?? {};
    final companies = array(row['companyQuotes']).map(object).toList();
    if (companies.isEmpty) {
      companies.add({
        'name': text(object(row['euroOdds'])['company'], '已保存公司指数'),
        'euroOdds': row['euroOdds'],
        'asianHandicap': row['asianHandicap'],
        'goalsLine': row['goalsLine'],
      });
    }
    return [
      _note('即时盘为最近一次同步的最后已保存报价，缺报价显示“—”。'),
      _block('平均欧赔', [
        _table([
          ['阶段', '主胜', '平局', '客胜'],
          ['初盘', ..._triple(stage('opening')['average'])],
          [
            '即时',
            ..._triple(
              stage('latest')['average'] ??
                  view.average.map((v) => v?.toStringAsFixed(2)).toList(),
            ),
          ],
        ]),
      ]),
      _block('页面盘口', [
        ..._line('亚洲让球', object(row['asianHandicap']), false),
        ..._line('大小球', object(row['goalsLine']), true),
      ]),
      for (final company in companies)
        _block(text(company['name'], '公司指数'), [
          _table([
            ['欧赔', '主胜', '平局', '客胜'],
            ['初盘', ..._triple(object(company['euroOdds'])['opening'])],
            ['即时', ..._triple(object(company['euroOdds'])['latest'])],
          ]),
          const SizedBox(height: 12),
          ..._line('亚洲让球', object(company['asianHandicap']), false),
          ..._line('大小球', object(company['goalsLine']), true),
        ]),
      for (final item in stages)
        _block('${text(item['label'])} · 指数比较', [
          for (final metric in [
            ('kelly', '凯利指数'),
            ('ticai', '威廉−体彩'),
            ('williamAverage', '威廉−均赔'),
          ]) ...[
            Text(
              metric.$2,
              style: const TextStyle(
                fontSize: 14,
                color: _ink,
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(height: 8),
            if (object(item[metric.$1])['available'] == true)
              _table([
                ['方向', '胜', '平', '负'],
                [
                  '差值 / 指数',
                  ..._triple(
                    array(
                      object(item[metric.$1])['values'],
                    ).map((v) => decimal(v)).toList(),
                  ),
                ],
              ])
            else
              _note(text(object(item[metric.$1])['reason'], '暂无有效指数')),
            const SizedBox(height: 12),
          ],
        ]),
    ];
  }

  List<Widget> _line(String title, Json line, bool goals) {
    List<String> row(String phase, String label) {
      final water = array(line['${phase}Odds']);
      return [
        label,
        water.isNotEmpty ? text(water[0]) : '—',
        text(line[phase]),
        water.length > 1 ? text(water[1]) : '—',
      ];
    }

    return [
      Text(
        title,
        style: const TextStyle(
          fontSize: 14,
          fontWeight: FontWeight.w600,
          color: _ink,
        ),
      ),
      const SizedBox(height: 7),
      if (line.isEmpty || line['source'] == 'missing')
        _note('暂无已采集盘口')
      else if (line['source'] == 'history_moneys')
        _note('历史盘口 ${text(line['historical'])}，不作即时盘')
      else ...[
        _table([
          ['阶段', goals ? '大水' : '主水', '盘口', goals ? '小水' : '客水'],
          row('opening', '初盘'),
          row('latest', '即时'),
        ]),
        const SizedBox(height: 7),
        _note(text(line['company'], '已保存盘口')),
      ],
    ];
  }

  List<Widget> _profits() {
    final simulation = object(
      object(widget.row['frontendAnalysis'])['roiSimulation'],
    );
    final markets = array(simulation['markets']).map(object).where((market) {
      if (market['key'] == 'sportteryRqspfSim' ||
          market['key'] == 'sportteryRqspf' && market['simulated'] == true) {
        return false;
      }
      return market['simulated'] == true ||
          ['sporttery', 'sportteryRqspf'].contains(market['key']);
    }).toList();
    return [
      _note('正数为庄家盈利，负数为亏损；金额按测算本金与支持率计算。源站支持率与模拟支持率分开显示。'),
      if (markets.isEmpty) _note('暂无可展示的胜平负或真实竞彩让球盈亏数据。'),
      ...markets.map((market) {
        final outcomes = array(
          market['bookmakerByOutcome'],
        ).map(object).toList();
        final hasSupport = outcomes.any(
          (o) =>
              (number(o['retailShare']) ?? 0) > 0 &&
              number(o['retailShare'])! <= 100,
        );
        final mapped = ['home', 'draw', 'away']
            .map(
              (direction) =>
                  outcomes
                      .where((o) => o['outcome'] == direction)
                      .firstOrNull ??
                  <String, dynamic>{},
            )
            .toList();
        bool priced(Json o) =>
            market['oddsAvailable'] == true && (number(o['odds']) ?? 0) > 0;
        final simulated = market['simulated'] == true;
        final stakes = outcomes.map((o) => number(o['totalStake'])).toList();
        final stake =
            stakes.isNotEmpty &&
                stakes.every((n) => n != null && n > 0 && n == stakes.first)
            ? stakes.first
            : number(simulation['totalStake']);
        return _block(text(market['name'], text(market['key'])), [
          _note(
            '${simulated
                ? '模拟胜平负'
                : market['key'] == 'sportteryRqspf'
                ? '真实竞彩让球'
                : '真实竞彩胜平负'}${text(market['goal'], '').isNotEmpty ? ' · 让球 ${market['goal']}' : ''}\n测算本金 ${stake == null ? '—' : amount(stake).replaceFirst('+', '')}',
          ),
          _table([
            [
              '项目',
              ...[
                '胜',
                '平',
                '负',
              ].map((s) => market['key'] == 'sportteryRqspf' ? '让$s' : s),
            ],
            [
              '支持率',
              ...mapped.map(
                (o) =>
                    hasSupport &&
                        number(o['retailShare']) != null &&
                        number(o['retailShare'])! >= 0 &&
                        number(o['retailShare'])! <= 100
                    ? percent(o['retailShare'])
                    : '—',
              ),
            ],
            [
              '盈亏额',
              ...mapped.map(
                (o) => priced(o) && o['available'] == true
                    ? amount(o['bookmakerProfit'])
                    : '—',
              ),
            ],
            [
              simulated ? '测算ROI' : '源站率',
              ...mapped.map(
                (o) => priced(o) && (!simulated || o['available'] == true)
                    ? percent(
                        o[simulated ? 'bookmakerRoi' : 'officialProfitRate'],
                        signed: true,
                      )
                    : '—',
              ),
            ],
          ], signed: true),
          const SizedBox(height: 10),
          if (!simulated) _note('源站率与本地测算金额可能采用不同口径。'),
          if (text(market['simulationNote'], '').isNotEmpty)
            _note(text(market['simulationNote'])),
        ]);
      }),
    ];
  }
}
