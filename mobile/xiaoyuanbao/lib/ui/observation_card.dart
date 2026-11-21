import 'package:flutter/material.dart';
import '../models/observation_view.dart';
import 'observation_details.dart';

const _ink = Color(0xFF142B4B);
const _blue = Color(0xFF2762E9);
const _muted = Color(0xFF697B92);
const _green = Color(0xFF168263);
const _red = Color(0xFFC64250);

class ObservationCard extends StatefulWidget {
  const ObservationCard({
    super.key,
    required this.row,
    this.showDate = false,
    this.loadDetails,
  });
  final Json row;
  final bool showDate;
  final Future<Json?> Function()? loadDetails;
  @override
  State<ObservationCard> createState() => _ObservationCardState();
}

class _ObservationCardState extends State<ObservationCard> {
  bool _expanded = false;
  bool _loading = false;
  Json? _details;
  String? _error;
  int _loadSequence = 0;

  @override
  void didUpdateWidget(covariant ObservationCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.row, widget.row)) {
      _loadSequence++;
      _expanded = false;
      _loading = false;
      _details = null;
      _error = null;
    }
  }

  Future<void> _loadDetails() async {
    final sequence = ++_loadSequence;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final details = widget.loadDetails == null
          ? widget.row
          : await widget.loadDetails!();
      if (details == null) throw StateError('本场缓存明细缺失，请重新同步');
      if (mounted && sequence == _loadSequence) {
        setState(() {
          _details = details;
          _loading = false;
        });
      }
    } catch (error) {
      if (mounted && sequence == _loadSequence) {
        setState(() {
          _error = '$error';
          _loading = false;
        });
      }
    }
  }

  void _toggle() {
    setState(() {
      _expanded = !_expanded;
      if (!_expanded) {
        _loadSequence++;
        _details = null;
        _loading = false;
        _error = null;
      }
    });
    if (_expanded) _loadDetails();
  }

  @override
  Widget build(BuildContext context) {
    final row = widget.row, view = ObservationView(widget.row);
    final probs = view.probabilities;
    return Container(
      clipBehavior: Clip.antiAlias,
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: view.selected
              ? const Color(0xFFB6CBFF)
              : const Color(0xFFE4E9F1),
        ),
        boxShadow: const [
          BoxShadow(
            color: Color(0x050A2348),
            offset: Offset(0, 3),
            blurRadius: 10,
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Container(
            color: const Color(0xFFF7F9FD),
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Wrap(
                    spacing: 8,
                    runSpacing: 5,
                    children: [
                      Text(
                        text(row['league']),
                        style: const TextStyle(
                          fontSize: 14,
                          color: _ink,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      if (view.sporttery)
                        Text(
                          text(row['jingcaiId']),
                          style: const TextStyle(
                            fontSize: 13,
                            color: _blue,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      Text(
                        '${widget.showDate ? '${text(row['date'])} ' : ''}${kickoff(row['matchTime'])}',
                        style: const TextStyle(fontSize: 13, color: _muted),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                Text(
                  view.confirmed ? '完赛' : text(row['displayState'], '待赛果'),
                  style: const TextStyle(fontSize: 13, color: _muted),
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(10, 18, 10, 18),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: _team(
                    text(row['home']),
                    view.standingText(true),
                    true,
                  ),
                ),
                Padding(
                  padding: const EdgeInsets.fromLTRB(8, 9, 8, 0),
                  child: Column(
                    children: [
                      Text(
                        view.confirmed
                            ? '${row['homeScore']} : ${row['awayScore']}'
                            : 'VS',
                        style: TextStyle(
                          fontSize: view.confirmed ? 28 : 24,
                          fontWeight: FontWeight.w800,
                          color: _ink,
                          letterSpacing: 1,
                        ),
                      ),
                      const SizedBox(height: 8),
                      if (view.selected)
                        Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 9,
                            vertical: 4,
                          ),
                          decoration: BoxDecoration(
                            color: const Color(0xFFEDF3FF),
                            borderRadius: BorderRadius.circular(6),
                          ),
                          child: const Text(
                            '入选平',
                            style: TextStyle(
                              fontSize: 13,
                              color: _blue,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                      if (view.confirmed)
                        const Padding(
                          padding: EdgeInsets.only(top: 7),
                          child: Text(
                            '常规时间',
                            style: TextStyle(fontSize: 12, color: _muted),
                          ),
                        ),
                    ],
                  ),
                ),
                Expanded(
                  child: _team(
                    text(row['guest']),
                    view.standingText(false),
                    false,
                  ),
                ),
              ],
            ),
          ),
          Container(
            margin: const EdgeInsets.symmetric(horizontal: 12),
            padding: const EdgeInsets.symmetric(vertical: 12),
            decoration: BoxDecoration(
              color: const Color(0xFFF5F8FF),
              borderRadius: BorderRadius.circular(12),
            ),
            child: IntrinsicHeight(
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Expanded(
                    child: _decision(
                      view.sporttery ? '模型 · 平局' : '非竞彩 · 平局',
                      view.primaryLabel,
                      view.selected
                          ? (view.draw
                                ? _green
                                : view.confirmed
                                ? const Color(0xFFB67712)
                                : _blue)
                          : _muted,
                      '',
                    ),
                  ),
                  const VerticalDivider(width: 1, color: Color(0xFFE1E8F6)),
                  Expanded(
                    child: _decision(
                      '胜负次选',
                      view.alternativeLabel,
                      _blue,
                      view.alternativeOutcome,
                    ),
                  ),
                  const VerticalDivider(width: 1, color: Color(0xFFE1E8F6)),
                  Expanded(
                    child: _decision(
                      '大小球',
                      view.totalsLabel,
                      _ink,
                      view.totalsOutcome,
                    ),
                  ),
                ],
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 14, 14, 10),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Icon(Icons.scoreboard_outlined, size: 17, color: _muted),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(
                    view.scoreLabel,
                    style: const TextStyle(
                      fontSize: 14,
                      color: _muted,
                      height: 1.5,
                    ),
                  ),
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14),
            child: Row(
              children: List.generate(
                3,
                (i) => Expanded(
                  child: Padding(
                    padding: EdgeInsets.only(right: i == 2 ? 0 : 9),
                    child: Column(
                      children: [
                        Row(
                          children: [
                            Text(
                              ['主胜', '平局', '客胜'][i],
                              style: const TextStyle(
                                fontSize: 13,
                                color: _muted,
                              ),
                            ),
                            const Spacer(),
                            Text(
                              percent(probs[i]),
                              style: const TextStyle(
                                fontSize: 13,
                                fontWeight: FontWeight.w700,
                                color: _ink,
                              ),
                            ),
                          ],
                        ),
                        const SizedBox(height: 6),
                        ClipRRect(
                          borderRadius: BorderRadius.circular(4),
                          child: LinearProgressIndicator(
                            value: (probs[i] ?? 0) / 100,
                            color: [
                              const Color(0xFF19AE8C),
                              const Color(0xFFE8A935),
                              const Color(0xFF4F8DF2),
                            ][i],
                            backgroundColor: const Color(0xFFE8EDF5),
                            minHeight: 4,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 14, 14, 12),
            child: Wrap(
              spacing: 15,
              runSpacing: 5,
              children: [
                Text(
                  '近10场平局  ${view.ratio('homeRecent')} / ${view.ratio('awayRecent')}',
                  style: const TextStyle(fontSize: 13, color: _muted),
                ),
                Text(
                  '交锋平局  ${view.ratio('headToHead')}',
                  style: const TextStyle(fontSize: 13, color: _muted),
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 0, 14, 12),
            child: Semantics(
              label: '即时赔率差值：威廉减体彩，依次为胜、平、负',
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    '威廉−体彩',
                    style: TextStyle(
                      fontSize: 13,
                      color: _muted,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  ...List.generate(3, (i) {
                    final value = view.differences[i];
                    return Expanded(
                      child: Wrap(
                        alignment: WrapAlignment.end,
                        spacing: 3,
                        children: [
                          Text(
                            ['胜', '平', '负'][i],
                            style: const TextStyle(fontSize: 13, color: _muted),
                          ),
                          Text(
                            value == null
                                ? '—'
                                : '${value > 0 ? '+' : ''}${value.toStringAsFixed(2)}',
                            style: TextStyle(
                              fontSize: 14,
                              fontWeight: FontWeight.w700,
                              color: value == null || value == 0
                                  ? _muted
                                  : value > 0
                                  ? _red
                                  : _green,
                            ),
                          ),
                        ],
                      ),
                    );
                  }),
                ],
              ),
            ),
          ),
          Container(
            decoration: const BoxDecoration(
              border: Border(top: BorderSide(color: Color(0xFFEDF0F6))),
            ),
            padding: const EdgeInsets.only(left: 14, right: 4),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    view.secondaryLabel,
                    style: const TextStyle(fontSize: 12, color: _muted),
                  ),
                ),
                TextButton(
                  onPressed: _toggle,
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        _expanded ? '收起详情' : '战绩 / 指数 / 盈亏',
                        style: const TextStyle(fontSize: 14),
                      ),
                      Icon(
                        _expanded ? Icons.expand_less : Icons.expand_more,
                        size: 20,
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
          if (_expanded && _loading)
            const Padding(
              padding: EdgeInsets.all(22),
              child: Center(child: CircularProgressIndicator()),
            ),
          if (_expanded && _error != null)
            Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                children: [
                  Text(
                    '本地明细读取失败：$_error',
                    style: const TextStyle(fontSize: 14, color: _red),
                  ),
                  TextButton(
                    onPressed: _loadDetails,
                    child: const Text('重新读取'),
                  ),
                ],
              ),
            ),
          if (_expanded && _details != null) ObservationDetails(row: _details!),
        ],
      ),
    );
  }

  Widget _team(String name, String standing, bool home) => Column(
    children: [
      Container(
        width: 44,
        height: 44,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          color: home ? const Color(0xFFEEF3FF) : const Color(0xFFFFF6E5),
          border: Border.all(
            color: home ? const Color(0xFFDCE6FE) : const Color(0xFFFFEAC1),
          ),
        ),
        child: Text(
          name.characters.take(2).toString(),
          style: TextStyle(
            fontSize: 17,
            fontWeight: FontWeight.w700,
            color: home ? _blue : const Color(0xFFB67E17),
          ),
        ),
      ),
      const SizedBox(height: 10),
      Text(
        name,
        textAlign: TextAlign.center,
        style: const TextStyle(
          fontSize: 17,
          fontWeight: FontWeight.w700,
          color: _ink,
          height: 1.4,
        ),
      ),
      const SizedBox(height: 7),
      Text(
        standing,
        textAlign: TextAlign.center,
        style: const TextStyle(fontSize: 12, color: _muted, height: 1.5),
      ),
    ],
  );

  Widget _decision(String title, String value, Color color, String outcome) =>
      Padding(
        padding: const EdgeInsets.symmetric(horizontal: 9),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: const TextStyle(fontSize: 13, color: _muted)),
            const SizedBox(height: 7),
            Text(
              value,
              style: TextStyle(
                fontSize: 17,
                fontWeight: FontWeight.w700,
                color: color,
                height: 1.35,
              ),
            ),
            if (outcome.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  outcome,
                  style: const TextStyle(
                    fontSize: 12,
                    color: _muted,
                    height: 1.4,
                  ),
                ),
              ),
          ],
        ),
      );
}
