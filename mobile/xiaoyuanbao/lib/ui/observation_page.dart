import 'package:flutter/material.dart';
import '../data/offline_repository.dart';
import '../models/observation_view.dart';
import 'observation_card.dart';

const ink = Color(0xFF142B4B);
const blue = Color(0xFF2762E9);
const muted = Color(0xFF697B92);
const green = Color(0xFF168263);
const red = Color(0xFFC64250);
const background = Color(0xFFF4F6FA);

class ObservationPage extends StatefulWidget {
  const ObservationPage({super.key, required this.repository});
  final ObservationRepository repository;
  @override
  State<ObservationPage> createState() => _ObservationPageState();
}

class _ObservationPageState extends State<ObservationPage> {
  String _date = day(beijingNow());
  bool _allDates = false, _sporttery = true;
  String _selection = 'all', _status = 'all', _league = '', _search = '';
  List<Json>? _filterSource;
  Object? _filterSignature;
  List<Json> _cachedRows = [];
  List<ObservationView> _cachedSelected = [],
      _cachedSettled = [],
      _cachedAlternatives = [];
  ObservationRepository get repo => widget.repository;
  @override
  void initState() {
    super.initState();
    repo.addListener(_changed);
  }

  @override
  void didUpdateWidget(covariant ObservationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.repository != repo) {
      oldWidget.repository.removeListener(_changed);
      repo.addListener(_changed);
    }
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    repo.removeListener(_changed);
    super.dispose();
  }

  List<Json> get _filtered {
    final source = repo.matches;
    final signature = (
      _date,
      _allDates,
      _sporttery,
      _selection,
      _status,
      _league,
      _search,
    );
    if (identical(source, _filterSource) && signature == _filterSignature) {
      return _cachedRows;
    }
    final q = _search.trim().toLowerCase();
    _cachedRows =
        source.where((row) {
          final view = ObservationView(row);
          if (_sporttery && !view.sporttery ||
              !_allDates && row['date'] != _date) {
            return false;
          }
          if (_selection == 'selected' && !view.selected ||
              _selection == 'not_selected' && view.selected) {
            return false;
          }
          if (_status == 'settled' && !view.confirmed ||
              _status == 'pending' && view.confirmed ||
              _status == 'draw' && !view.draw) {
            return false;
          }
          if (_league.isNotEmpty && row['league'] != _league) return false;
          return q.isEmpty ||
              [
                'home',
                'guest',
                'league',
                'jingcaiId',
              ].any((key) => text(row[key], '').toLowerCase().contains(q));
        }).toList()..sort(
          (a, b) =>
              text(a['matchTime'], '').compareTo(text(b['matchTime'], '')),
        );
    _filterSource = source;
    _filterSignature = signature;
    _cachedSelected = _cachedRows
        .map(ObservationView.new)
        .where((v) => v.selected)
        .toList();
    _cachedSettled = _cachedSelected.where((v) => v.confirmed).toList();
    _cachedAlternatives = _cachedSettled
        .where((v) => v.alternative != null)
        .toList();
    return _cachedRows;
  }

  Future<void> _sync() async {
    if (repo.serverUrl.isEmpty) {
      await _settings();
      return;
    }
    try {
      await repo.sync();
    } catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('同步失败，仍可查看本地数据：$error')));
      }
    }
  }

  Future<void> _settings() async {
    final controller = TextEditingController(text: repo.serverUrl);
    String? error;
    bool saving = false;
    final connect = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: Colors.white,
      builder: (context) => StatefulBuilder(
        builder: (context, update) => Padding(
          padding: EdgeInsets.fromLTRB(
            22,
            24,
            22,
            MediaQuery.viewInsetsOf(context).bottom + 24,
          ),
          child: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  '连接你的后台',
                  style: TextStyle(
                    fontSize: 23,
                    fontWeight: FontWeight.w700,
                    color: ink,
                  ),
                ),
                const SizedBox(height: 10),
                const Text(
                  '手机与电脑连接同一局域网，填写电脑的 IP 地址和后台端口。同步后，断网也能查看比赛、战绩、指数和盈亏。',
                  style: TextStyle(fontSize: 15, color: muted, height: 1.55),
                ),
                const SizedBox(height: 20),
                TextField(
                  controller: controller,
                  enabled: !saving && !repo.isSyncing,
                  keyboardType: TextInputType.url,
                  autocorrect: false,
                  textCapitalization: TextCapitalization.none,
                  decoration: InputDecoration(
                    labelText: '后台地址',
                    hintText: 'http://192.168.1.10:28080',
                    errorText: error,
                    border: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(14),
                    ),
                  ),
                ),
                const SizedBox(height: 14),
                Text(
                  '本地已保存 ${repo.matches.length} 场比赛\n上次同步：${_lastSync()}\n模型版本：${repo.modelVersion ?? '尚未同步'}',
                  style: const TextStyle(
                    fontSize: 13,
                    color: muted,
                    height: 1.6,
                  ),
                ),
                const SizedBox(height: 22),
                SizedBox(
                  width: double.infinity,
                  child: FilledButton.icon(
                    onPressed: saving || repo.isSyncing
                        ? null
                        : () async {
                            update(() {
                              saving = true;
                              error = null;
                            });
                            try {
                              await repo.setServer(controller.text);
                              if (context.mounted) Navigator.pop(context, true);
                            } catch (e) {
                              if (context.mounted) {
                                update(() {
                                  saving = false;
                                  error = '$e';
                                });
                              }
                            }
                          },
                    icon: const Icon(Icons.wifi_rounded),
                    label: Text(saving ? '正在保存…' : '保存并同步全部比赛'),
                  ),
                ),
                const SizedBox(height: 5),
                const Text(
                  '切换后台后使用该后台独立的本地缓存，不混合不同电脑的数据。',
                  style: TextStyle(fontSize: 13, color: muted, height: 1.5),
                ),
              ],
            ),
          ),
        ),
      ),
    );
    controller.dispose();
    if (connect == true && mounted) await _sync();
  }

  String _lastSync() {
    final time = repo.lastSync?.toLocal();
    return time == null
        ? '尚未同步'
        : '${day(time)} ${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}';
  }

  Future<void> _chooseDate() async {
    final now = beijingNow();
    final dates =
        repo.matches
            .map((row) => DateTime.tryParse(text(row['date'], '')))
            .whereType<DateTime>()
            .toList()
          ..sort();
    final first = dates.isEmpty || dates.first.year > 2020
        ? DateTime(2020)
        : dates.first;
    final last = dates.isEmpty || dates.last.isBefore(DateTime(now.year + 2))
        ? DateTime(now.year + 2)
        : dates.last;
    final selected = await showDatePicker(
      context: context,
      initialDate: DateTime.parse(_date),
      firstDate: first,
      lastDate: last,
      helpText: '选择比赛日期（北京时间）',
      cancelText: '取消',
      confirmText: '确定',
    );
    if (selected != null && mounted) {
      setState(() {
        _date = day(selected);
        _allDates = false;
      });
    }
  }

  Future<void> _filters() async {
    final controller = TextEditingController(text: _search);
    String status = _status, league = _league;
    final leagues =
        repo.matches
            .map((r) => text(r['league'], ''))
            .where((s) => s.isNotEmpty)
            .toSet()
            .toList()
          ..sort();
    if (!leagues.contains(league)) league = '';
    final apply = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: Colors.white,
      builder: (context) => StatefulBuilder(
        builder: (context, update) => Padding(
          padding: EdgeInsets.fromLTRB(
            20,
            24,
            20,
            MediaQuery.viewInsetsOf(context).bottom + 24,
          ),
          child: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  '筛选本地比赛',
                  style: TextStyle(
                    fontSize: 23,
                    fontWeight: FontWeight.w700,
                    color: ink,
                  ),
                ),
                const SizedBox(height: 20),
                TextField(
                  controller: controller,
                  decoration: const InputDecoration(
                    labelText: '球队 / 联赛 / 竞彩编号',
                    prefixIcon: Icon(Icons.search),
                    border: OutlineInputBorder(),
                  ),
                ),
                const SizedBox(height: 16),
                DropdownButtonFormField<String>(
                  initialValue: league,
                  isExpanded: true,
                  decoration: const InputDecoration(
                    labelText: '联赛',
                    border: OutlineInputBorder(),
                  ),
                  items: [
                    const DropdownMenuItem(value: '', child: Text('全部联赛')),
                    ...leagues.map(
                      (s) => DropdownMenuItem(
                        value: s,
                        child: Text(s, overflow: TextOverflow.ellipsis),
                      ),
                    ),
                  ],
                  onChanged: (s) => update(() => league = s ?? ''),
                ),
                const SizedBox(height: 18),
                Wrap(
                  spacing: 8,
                  runSpacing: 6,
                  children:
                      {
                            'all': '全部状态',
                            'settled': '已确认完赛',
                            'pending': '待确认赛果',
                            'draw': '已打平',
                          }.entries
                          .map(
                            (e) => ChoiceChip(
                              label: Text(e.value),
                              selected: status == e.key,
                              onSelected: (_) => update(() => status = e.key),
                            ),
                          )
                          .toList(),
                ),
                const SizedBox(height: 20),
                Row(
                  children: [
                    TextButton(
                      onPressed: () => update(() {
                        controller.clear();
                        league = '';
                        status = 'all';
                      }),
                      child: const Text('重置'),
                    ),
                    const Spacer(),
                    FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('查看比赛'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
    if (apply == true && mounted) {
      setState(() {
        _search = controller.text.trim();
        _league = league;
        _status = status;
      });
    }
    controller.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final matches = _filtered;
    final selected = _cachedSelected;
    final settled = _cachedSettled;
    final alternatives = _cachedAlternatives;
    final filters =
        _search.isNotEmpty || _league.isNotEmpty || _status != 'all';
    return Scaffold(
      backgroundColor: background,
      body: SafeArea(
        child: RefreshIndicator(
          onRefresh: _sync,
          child: CustomScrollView(
            slivers: [
              SliverToBoxAdapter(
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(16, 10, 12, 8),
                  child: Row(
                    children: [
                      ClipRRect(
                        borderRadius: BorderRadius.circular(14),
                        child: Image.asset(
                          'assets/images/logo.png',
                          width: 46,
                          height: 46,
                          errorBuilder: (_, error, trace) => Container(
                            width: 46,
                            height: 46,
                            decoration: BoxDecoration(
                              color: ink,
                              borderRadius: BorderRadius.circular(14),
                            ),
                            child: const Icon(
                              Icons.savings_rounded,
                              color: Color(0xFFFFCF62),
                              size: 29,
                            ),
                          ),
                        ),
                      ),
                      const SizedBox(width: 12),
                      const Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              '小元宝',
                              style: TextStyle(
                                fontSize: 25,
                                fontWeight: FontWeight.w800,
                                color: ink,
                              ),
                            ),
                            Text(
                              '平局比赛观察',
                              style: TextStyle(
                                fontSize: 14,
                                color: muted,
                                letterSpacing: 1,
                              ),
                            ),
                          ],
                        ),
                      ),
                      IconButton(
                        tooltip: '连接后台与缓存信息',
                        onPressed: _settings,
                        icon: const Icon(Icons.settings_outlined, color: ink),
                      ),
                    ],
                  ),
                ),
              ),
              SliverToBoxAdapter(child: _syncBanner()),
              SliverToBoxAdapter(
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
                  child: Column(
                    children: [
                      Row(
                        children: [
                          Expanded(
                            child: SegmentedButton<bool>(
                              segments: const [
                                ButtonSegment(value: true, label: Text('竞彩')),
                                ButtonSegment(value: false, label: Text('全部')),
                              ],
                              selected: {_sporttery},
                              showSelectedIcon: false,
                              onSelectionChanged: (s) =>
                                  setState(() => _sporttery = s.first),
                              style: const ButtonStyle(
                                visualDensity: VisualDensity.compact,
                              ),
                            ),
                          ),
                          const SizedBox(width: 8),
                          PopupMenuButton<String>(
                            tooltip: '入选筛选',
                            initialValue: _selection,
                            onSelected: (value) =>
                                setState(() => _selection = value),
                            itemBuilder: (_) => const [
                              PopupMenuItem(
                                value: 'all',
                                child: Text('全部入选状态'),
                              ),
                              PopupMenuItem(
                                value: 'selected',
                                child: Text('只看入选平'),
                              ),
                              PopupMenuItem(
                                value: 'not_selected',
                                child: Text('只看未入选'),
                              ),
                            ],
                            child: Padding(
                              padding: const EdgeInsets.symmetric(
                                horizontal: 4,
                                vertical: 12,
                              ),
                              child: Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  Text(
                                    {
                                      'all': '入选筛选',
                                      'selected': '入选平',
                                      'not_selected': '未入选',
                                    }[_selection]!,
                                    style: TextStyle(
                                      fontSize: 14,
                                      color: _selection == 'all' ? muted : blue,
                                    ),
                                  ),
                                  const Icon(
                                    Icons.expand_more,
                                    size: 18,
                                    color: muted,
                                  ),
                                ],
                              ),
                            ),
                          ),
                          IconButton.filledTonal(
                            tooltip: '筛选比赛',
                            onPressed: _filters,
                            icon: Badge(
                              isLabelVisible: filters,
                              child: const Icon(Icons.tune_rounded),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 8),
                      Container(
                        decoration: BoxDecoration(
                          color: Colors.white,
                          borderRadius: BorderRadius.circular(14),
                          border: Border.all(color: const Color(0xFFE2E8F1)),
                        ),
                        child: Row(
                          children: [
                            IconButton(
                              tooltip: '前一天',
                              onPressed: () => setState(() {
                                _date = day(
                                  DateTime.parse(
                                    _date,
                                  ).subtract(const Duration(days: 1)),
                                );
                                _allDates = false;
                              }),
                              icon: const Icon(Icons.chevron_left),
                            ),
                            Expanded(
                              child: TextButton(
                                onPressed: _chooseDate,
                                child: Text(
                                  _allDates
                                      ? '全部缓存日期'
                                      : _date.replaceAll('-', '.'),
                                  style: const TextStyle(
                                    fontSize: 17,
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                              ),
                            ),
                            IconButton(
                              tooltip: '后一天',
                              onPressed: () => setState(() {
                                _date = day(
                                  DateTime.parse(
                                    _date,
                                  ).add(const Duration(days: 1)),
                                );
                                _allDates = false;
                              }),
                              icon: const Icon(Icons.chevron_right),
                            ),
                            PopupMenuButton<String>(
                              tooltip: '今天与全部日期',
                              icon: const Icon(
                                Icons.calendar_month_outlined,
                                size: 21,
                              ),
                              onSelected: (value) => setState(() {
                                if (value == 'today') {
                                  _date = day(beijingNow());
                                  _allDates = false;
                                } else {
                                  _allDates = true;
                                }
                              }),
                              itemBuilder: (_) => const [
                                PopupMenuItem(
                                  value: 'today',
                                  child: Text('今天（北京时间）'),
                                ),
                                PopupMenuItem(
                                  value: 'all',
                                  child: Text('全部缓存日期'),
                                ),
                              ],
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 8),
                      Row(
                        children: [
                          Expanded(
                            child: _metric('比赛', '${matches.length}', '当前筛选'),
                          ),
                          const SizedBox(width: 7),
                          Expanded(
                            child: _metric(
                              '入选平',
                              '${selected.length}',
                              '${settled.length} 场已结算',
                            ),
                          ),
                          const SizedBox(width: 7),
                          Expanded(
                            child: _metric(
                              '平局命中',
                              settled.isEmpty
                                  ? '—'
                                  : percent(
                                      settled.where((v) => v.draw).length /
                                          settled.length *
                                          100,
                                    ),
                              '${settled.where((v) => v.draw).length}/${settled.length}',
                            ),
                          ),
                        ],
                      ),
                      if (alternatives.isNotEmpty)
                        Padding(
                          padding: const EdgeInsets.only(top: 8),
                          child: Row(
                            children: [
                              const Icon(
                                Icons.insights_rounded,
                                color: muted,
                                size: 17,
                              ),
                              const SizedBox(width: 5),
                              Expanded(
                                child: Text(
                                  '次选命中 ${alternatives.where((v) => v.alternativeHit).length}/${alternatives.length} · ${percent(alternatives.where((v) => v.alternativeHit).length / alternatives.length * 100)}',
                                  style: const TextStyle(
                                    fontSize: 14,
                                    color: muted,
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                      if (filters)
                        Padding(
                          padding: const EdgeInsets.only(top: 8),
                          child: Align(
                            alignment: Alignment.centerLeft,
                            child: Text(
                              [
                                _league,
                                _search,
                                _status == 'all'
                                    ? ''
                                    : {
                                        'settled': '已确认完赛',
                                        'pending': '待确认赛果',
                                        'draw': '已打平',
                                      }[_status]!,
                              ].where((s) => s.isNotEmpty).join(' · '),
                              style: const TextStyle(fontSize: 14, color: blue),
                            ),
                          ),
                        ),
                      const SizedBox(height: 12),
                    ],
                  ),
                ),
              ),
              if (matches.isEmpty)
                SliverToBoxAdapter(child: _empty())
              else
                SliverPadding(
                  padding: const EdgeInsets.symmetric(horizontal: 12),
                  sliver: SliverList.builder(
                    itemCount: matches.length,
                    itemBuilder: (context, i) => Padding(
                      padding: const EdgeInsets.only(bottom: 14),
                      child: ObservationCard(
                        key: ValueKey(matches[i]['matchId']),
                        row: matches[i],
                        showDate: _allDates,
                        loadDetails: () =>
                            repo.matchDetails(text(matches[i]['matchId'], '')),
                      ),
                    ),
                  ),
                ),
              SliverToBoxAdapter(
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(25, 12, 25, 28),
                  child: Text(
                    '显示本地已保存的数据。赛果、赔率与模型结果以最近一次完整同步为准。\n命中率仅统计已确认常规时间赛果的比赛。',
                    textAlign: TextAlign.center,
                    style: const TextStyle(
                      fontSize: 13,
                      color: muted,
                      height: 1.6,
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _metric(String label, String value, String detail) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
    decoration: BoxDecoration(
      color: Colors.white,
      borderRadius: BorderRadius.circular(13),
    ),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: const TextStyle(fontSize: 13, color: muted)),
        const SizedBox(height: 3),
        FittedBox(
          fit: BoxFit.scaleDown,
          child: Text(
            value,
            style: const TextStyle(
              fontSize: 22,
              fontWeight: FontWeight.w800,
              color: ink,
            ),
          ),
        ),
        const SizedBox(height: 2),
        Text(detail, style: const TextStyle(fontSize: 12, color: muted)),
      ],
    ),
  );

  Widget _syncBanner() => Container(
    margin: const EdgeInsets.symmetric(horizontal: 16),
    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 0),
    decoration: BoxDecoration(
      color: const Color(0xFFEAF0FF),
      borderRadius: BorderRadius.circular(16),
    ),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(
              repo.isSyncing ? Icons.sync_rounded : Icons.offline_pin_outlined,
              color: blue,
              size: 21,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                repo.isSyncing
                    ? '同步中 · ${repo.completed}/${repo.total}'
                    : repo.matches.isEmpty
                    ? '连接局域网，带走全部比赛'
                    : '本地已缓存 ${repo.matches.length} 场',
                style: const TextStyle(
                  fontSize: 15,
                  fontWeight: FontWeight.w700,
                  color: ink,
                ),
              ),
            ),
            const SizedBox(width: 4),
            TextButton(
              onPressed: repo.isSyncing ? repo.cancelSync : _sync,
              child: Text(repo.isSyncing ? '取消' : '同步'),
            ),
          ],
        ),
        if (repo.isSyncing)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: LinearProgressIndicator(
              value: repo.total > 0
                  ? (repo.completed / repo.total).clamp(0, 1)
                  : null,
              minHeight: 4,
              borderRadius: BorderRadius.circular(4),
            ),
          )
        else
          Text(
            repo.lastSync == null ? '首次同步后即可离线查看' : '最近同步 ${_lastSync()}',
            style: const TextStyle(fontSize: 13, color: muted),
          ),
        if (repo.lastError?.isNotEmpty == true)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(
              '同步未完成，已保留上次缓存。\n${repo.lastError}',
              style: const TextStyle(fontSize: 13, color: red, height: 1.5),
            ),
          ),
      ],
    ),
  );

  Widget _empty() => Padding(
    padding: const EdgeInsets.all(30),
    child: Column(
      children: [
        const Icon(
          Icons.sports_soccer_rounded,
          size: 54,
          color: Color(0xFFAABAD0),
        ),
        const SizedBox(height: 20),
        Text(
          repo.matches.isEmpty ? '让比赛数据住进手机' : '当前条件没有比赛',
          style: const TextStyle(
            fontSize: 20,
            fontWeight: FontWeight.w700,
            color: ink,
          ),
        ),
        const SizedBox(height: 9),
        Text(
          repo.matches.isEmpty
              ? '连接电脑后台，一次下载所有比赛。\n下次打开，离线也能查看。'
              : '可以切换日期或调整筛选。\n“全部日期”可查看所有已缓存比赛。',
          textAlign: TextAlign.center,
          style: const TextStyle(fontSize: 15, color: muted, height: 1.7),
        ),
        const SizedBox(height: 20),
        OutlinedButton.icon(
          onPressed: repo.matches.isEmpty
              ? _settings
              : () => setState(() {
                  _allDates = true;
                  _selection = 'all';
                  _status = 'all';
                  _league = '';
                  _search = '';
                }),
          icon: Icon(
            repo.matches.isEmpty
                ? Icons.wifi_rounded
                : Icons.calendar_month_rounded,
          ),
          label: Text(repo.matches.isEmpty ? '连接局域网后台' : '查看全部缓存日期'),
        ),
      ],
    ),
  );
}
