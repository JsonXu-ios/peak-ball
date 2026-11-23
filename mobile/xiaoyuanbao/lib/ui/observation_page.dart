import 'dart:async';
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

class _ObservationPageState extends State<ObservationPage>
    with WidgetsBindingObserver {
  int _tab = 0;
  DateTime? _lastAttempt;
  Timer? _timer;
  bool _active = true;
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
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _autoCheck();
    });
    _timer = Timer.periodic(const Duration(minutes: 5), (_) => _autoCheck());
  }

  void _autoCheck() {
    if (!_active || repo.isSyncing || repo.serverUrl.isEmpty) return;
    final now = DateTime.now();
    if (_lastAttempt != null &&
        now.difference(_lastAttempt!) < const Duration(minutes: 1)) {
      return;
    }
    unawaited(_sync());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _active = state == AppLifecycleState.resumed;
    if (_active) _autoCheck();
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
    _timer?.cancel();
    WidgetsBinding.instance.removeObserver(this);
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
    _lastAttempt = DateTime.now();
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
    var address = repo.serverUrl;
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
                TextFormField(
                  initialValue: address,
                  onChanged: (value) => address = value,
                  enabled: !saving && !repo.isSyncing,
                  keyboardType: TextInputType.url,
                  autocorrect: false,
                  textCapitalization: TextCapitalization.none,
                  decoration: InputDecoration(
                    labelText: '后台地址',
                    hintText: ObservationRepository.defaultServer,
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
                              await repo.setServer(address);
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
                    label: Text(saving ? '正在保存…' : '保存并检查更新'),
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
    return Scaffold(
      backgroundColor: const Color(0xfff3f5f4),
      appBar: AppBar(
        toolbarHeight: 54,
        title: Row(
          children: [
            Image.asset('assets/images/logo.png', width: 32, height: 32),
            const SizedBox(width: 9),
            const Text(
              '小元宝',
              style: TextStyle(fontWeight: FontWeight.w800, fontSize: 19),
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: '连接后台与缓存信息',
            onPressed: _settings,
            icon: const Icon(Icons.settings_outlined),
          ),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _tab,
        onDestinationSelected: (value) => setState(() => _tab = value),
        height: 66,
        backgroundColor: Colors.white,
        indicatorColor: const Color(0xffdcefe8),
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.stadium_outlined),
            selectedIcon: Icon(Icons.stadium),
            label: '赛程',
          ),
          NavigationDestination(
            icon: Icon(Icons.bar_chart_outlined),
            selectedIcon: Icon(Icons.bar_chart),
            label: '复盘',
          ),
          NavigationDestination(
            icon: Icon(Icons.storage_outlined),
            selectedIcon: Icon(Icons.storage),
            label: '数据',
          ),
        ],
      ),
      body: SafeArea(
        top: false,
        child: RefreshIndicator(
          onRefresh: _sync,
          child: CustomScrollView(
            key: PageStorageKey('yuanbao-tab-$_tab'),
            physics: const AlwaysScrollableScrollPhysics(),
            slivers: [
              if (_tab != 2) ...[
                SliverToBoxAdapter(child: _masthead(matches.length)),
                SliverToBoxAdapter(
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 16),
                    child: InkWell(
                      onTap: () => setState(() => _tab = 2),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(vertical: 5),
                        child: Row(
                          children: [
                            Icon(
                              repo.lastError != null
                                  ? Icons.cloud_off_outlined
                                  : repo.isSyncing
                                  ? Icons.sync
                                  : Icons.offline_pin_outlined,
                              size: 16,
                              color: muted,
                            ),
                            const SizedBox(width: 6),
                            Expanded(
                              child: Text(
                                repo.isSyncing
                                    ? (repo.total == 0
                                          ? '正在核对比赛清单…'
                                          : '正在同步 ${repo.completed}/${repo.total}')
                                    : repo.lastError != null
                                    ? '更新未完成 · 查看原因'
                                    : '本地已缓存 ${repo.matches.length} 场',
                                style: const TextStyle(
                                  color: muted,
                                  fontSize: 13,
                                ),
                              ),
                            ),
                            const Icon(
                              Icons.chevron_right,
                              size: 16,
                              color: muted,
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
                SliverToBoxAdapter(child: _toolbar()),
                if (_tab == 0) ...[
                  SliverToBoxAdapter(
                    child: Padding(
                      padding: const EdgeInsets.fromLTRB(18, 14, 18, 8),
                      child: Row(
                        children: [
                          const Expanded(
                            child: Text(
                              '比赛列表',
                              style: TextStyle(
                                fontSize: 17,
                                fontWeight: FontWeight.w800,
                              ),
                            ),
                          ),
                          Text(
                            '${matches.length} 场',
                            style: const TextStyle(color: muted),
                          ),
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
                        itemBuilder: (context, index) => Padding(
                          padding: const EdgeInsets.only(bottom: 12),
                          child: ObservationCard(
                            key: ValueKey(matches[index]['matchId']),
                            row: matches[index],
                            repository: repo,
                            showDate: _allDates,
                            loadDetails: () => repo.matchDetails(
                              text(matches[index]['matchId'], ''),
                            ),
                          ),
                        ),
                      ),
                    ),
                ] else
                  SliverToBoxAdapter(child: _review(matches)),
              ] else
                SliverToBoxAdapter(child: _dataPanel()),
              const SliverToBoxAdapter(child: SizedBox(height: 20)),
            ],
          ),
        ),
      ),
    );
  }

  Widget _masthead(int count) => Container(
    margin: const EdgeInsets.fromLTRB(12, 8, 12, 8),
    decoration: BoxDecoration(
      borderRadius: BorderRadius.circular(20),
      gradient: const LinearGradient(
        colors: [Color(0xff112f36), Color(0xff20524f)],
      ),
    ),
    clipBehavior: Clip.antiAlias,
    child: Stack(
      children: [
        const Positioned.fill(
          child: IgnorePointer(child: CustomPaint(painter: _PitchLines())),
        ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                _tab == 1 ? 'FORM REPORT   /   02' : 'MATCHDAY   /   01',
                style: const TextStyle(
                  color: Color(0xffffd484),
                  fontWeight: FontWeight.w700,
                  fontSize: 12,
                  letterSpacing: 1.8,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                _tab == 1 ? '赛果复盘' : '平局观察',
                style: const TextStyle(
                  fontSize: 29,
                  height: 1.22,
                  letterSpacing: 1,
                  fontWeight: FontWeight.w800,
                  color: Colors.white,
                ),
              ),
              const SizedBox(height: 5),
              Text(
                _tab == 1
                    ? '已结算 ${_cachedSettled.length} 场入选比赛'
                    : '${_sporttery ? "竞彩" : "全部赛事"}  ·  $count 场比赛  /  ${_cachedSelected.length} 场入选',
                style: const TextStyle(color: Color(0xffd0e4dd), fontSize: 14),
              ),
            ],
          ),
        ),
      ],
    ),
  );

  Widget _toolbar() => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 14),
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
                onSelectionChanged: (s) => setState(() => _sporttery = s.first),
                style: const ButtonStyle(visualDensity: VisualDensity.compact),
              ),
            ),
            PopupMenuButton<String>(
              tooltip: '入选筛选',
              initialValue: _selection,
              onSelected: (value) => setState(() => _selection = value),
              itemBuilder: (_) => const [
                PopupMenuItem(value: 'all', child: Text('全部入选状态')),
                PopupMenuItem(value: 'selected', child: Text('只看入选平')),
                PopupMenuItem(value: 'not_selected', child: Text('只看未入选')),
              ],
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 8,
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
                    ),
                    const Icon(Icons.expand_more, size: 17),
                  ],
                ),
              ),
            ),
            IconButton(
              tooltip: '筛选比赛',
              onPressed: _filters,
              icon: Badge(
                isLabelVisible:
                    _league.isNotEmpty ||
                    _search.isNotEmpty ||
                    _status != 'all',
                child: const Icon(Icons.tune_rounded),
              ),
            ),
          ],
        ),
        DecoratedBox(
          decoration: BoxDecoration(
            color: Colors.white,
            borderRadius: BorderRadius.circular(14),
          ),
          child: Row(
            children: [
              IconButton(
                tooltip: '前一天',
                onPressed: () => _step(-1),
                icon: const Icon(Icons.chevron_left),
              ),
              Expanded(
                child: TextButton(
                  onPressed: _chooseDate,
                  child: Text(
                    _allDates ? '全部缓存日期' : _date.replaceAll('-', '.'),
                    style: const TextStyle(
                      fontWeight: FontWeight.w700,
                      fontSize: 16,
                    ),
                  ),
                ),
              ),
              IconButton(
                tooltip: '后一天',
                onPressed: () => _step(1),
                icon: const Icon(Icons.chevron_right),
              ),
              PopupMenuButton<String>(
                tooltip: '今天与全部日期',
                icon: const Icon(Icons.calendar_month_outlined),
                itemBuilder: (_) => const [
                  PopupMenuItem(value: 'today', child: Text('今天（北京时间）')),
                  PopupMenuItem(value: 'all', child: Text('全部缓存日期')),
                ],
                onSelected: (value) => setState(() {
                  _allDates = value == 'all';
                  if (!_allDates) _date = day(beijingNow());
                }),
              ),
            ],
          ),
        ),
        if (_league.isNotEmpty || _search.isNotEmpty || _status != 'all')
          Align(
            alignment: Alignment.centerLeft,
            child: Text(
              [
                _league,
                _search,
                {'settled': '已完赛', 'pending': '待赛果', 'draw': '已打平'}[_status] ??
                    '',
              ].where((item) => item.isNotEmpty).join(' · '),
            ),
          ),
      ],
    ),
  );
  void _step(int delta) => setState(() {
    _date = day(DateTime.parse(_date).add(Duration(days: delta)));
    _allDates = false;
  });

  Widget _review(List<Json> matches) {
    final settled = _cachedSettled;
    final alternatives = _cachedAlternatives;
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Text(
            '当前筛选 · 命中表现',
            style: TextStyle(fontWeight: FontWeight.w800, fontSize: 18),
          ),
          const SizedBox(height: 10),
          _measure(
            '入选平命中率',
            settled.where((v) => v.draw).length,
            settled.length,
            '平局命中 / 已结算入选场次',
            const Color(0xff237961),
          ),
          _measure(
            '次选命中率',
            alternatives.where((v) => v.alternativeHit).length,
            alternatives.length,
            '次选命中 / 有次选的已结算场次',
            const Color(0xffa86619),
          ),
          _measure(
            '入选占比',
            _cachedSelected.length,
            matches.length,
            '入选场次 / 当前列表场次',
            const Color(0xff496ba1),
          ),
          const Text('结算按常规时间赛果；待确认的比赛不计入命中率。', style: TextStyle(color: muted)),
        ],
      ),
    );
  }

  Widget _measure(
    String label,
    int hits,
    int count,
    String note,
    Color color,
  ) => Card(
    margin: const EdgeInsets.only(bottom: 12),
    elevation: 0,
    color: Colors.white,
    child: Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  label,
                  style: const TextStyle(
                    fontWeight: FontWeight.w700,
                    fontSize: 16,
                  ),
                ),
              ),
              Text(
                count == 0 ? '—' : percent(100 * hits / count),
                style: TextStyle(
                  color: color,
                  fontWeight: FontWeight.w800,
                  fontSize: 23,
                ),
              ),
            ],
          ),
          const SizedBox(height: 9),
          LinearProgressIndicator(
            value: count == 0 ? 0 : hits / count,
            color: color,
            backgroundColor: const Color(0xffedf1ef),
            borderRadius: BorderRadius.circular(4),
          ),
          const SizedBox(height: 8),
          Text(
            '$hits / $count  ·  $note',
            style: const TextStyle(color: muted, fontSize: 13),
          ),
        ],
      ),
    ),
  );
  Widget _dataPanel() => Padding(
    padding: const EdgeInsets.all(16),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text(
          '数据中心',
          style: TextStyle(fontWeight: FontWeight.w800, fontSize: 28),
        ),
        const SizedBox(height: 14),
        Container(
          padding: const EdgeInsets.all(20),
          decoration: BoxDecoration(
            color: const Color(0xff163d3d),
            borderRadius: BorderRadius.circular(18),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text(
                '离线比赛库',
                style: TextStyle(
                  color: Colors.white,
                  fontWeight: FontWeight.w600,
                ),
              ),
              Text(
                '${repo.matches.length} 场',
                style: const TextStyle(
                  fontWeight: FontWeight.w800,
                  color: Colors.white,
                  fontSize: 34,
                ),
              ),
              Text(
                '最近同步  ${_lastSync()}',
                style: const TextStyle(color: Color(0xffcee1dc)),
              ),
            ],
          ),
        ),
        const SizedBox(height: 16),
        ListTile(
          tileColor: Colors.white,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(14),
          ),
          leading: const Icon(Icons.router_outlined),
          title: const Text('局域网后台'),
          subtitle: Text(repo.serverUrl),
          trailing: const Icon(Icons.chevron_right),
          onTap: _settings,
        ),
        const SizedBox(height: 16),
        if (repo.isSyncing) ...[
          LinearProgressIndicator(
            value: repo.logoProgress != null || repo.total == 0
                ? null
                : repo.completed / repo.total,
          ),
          const SizedBox(height: 8),
          Text(
            repo.logoProgress ??
                (repo.total == 0
                    ? '正在核对比赛清单…'
                    : '已校验 ${repo.completed} / ${repo.total} 场变更'),
          ),
        ],
        if (repo.lastError != null)
          Text(repo.lastError!, style: const TextStyle(color: red)),
        FilledButton.icon(
          onPressed: repo.isSyncing ? repo.cancelSync : _sync,
          icon: Icon(repo.isSyncing ? Icons.pause : Icons.sync),
          label: Text(repo.isSyncing ? '暂停同步' : '检查数据更新'),
        ),
        const SizedBox(height: 12),
        const Text(
          '首次下载全部比赛。之后只下载新增或变动；中途退出，下次可从已校验的比赛继续。覆盖安装保留缓存。',
          style: TextStyle(color: muted, height: 1.65),
        ),
        if (repo.modelVersion != null)
          Padding(
            padding: const EdgeInsets.only(top: 16),
            child: Text(
              '当前模型\n${repo.modelVersion}',
              style: const TextStyle(color: muted, fontSize: 13),
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
              ? () => setState(() => _tab = 2)
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
          label: Text(repo.matches.isEmpty ? '查看同步状态' : '查看全部缓存日期'),
        ),
      ],
    ),
  );
}

class _PitchLines extends CustomPainter {
  const _PitchLines();
  @override
  void paint(Canvas canvas, Size size) {
    canvas.save();
    canvas.translate(size.width * .91, size.height * .50);
    canvas.rotate(-.24);
    final pen = Paint()
      ..color = const Color(0x3384bba1)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.5;
    canvas.drawRRect(
      RRect.fromLTRBR(-74, -77, 72, 77, const Radius.circular(3)),
      pen,
    );
    canvas.drawLine(const Offset(-74, 0), const Offset(72, 0), pen);
    canvas.drawCircle(Offset.zero, 21, pen);
    canvas.drawRect(const Rect.fromLTWH(-35, 49, 70, 28), pen);
    canvas.restore();
  }

  @override
  bool shouldRepaint(_PitchLines oldDelegate) => false;
}
