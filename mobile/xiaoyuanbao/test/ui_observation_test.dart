import 'dart:io';
import 'dart:convert';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:xiaoyuanbao/data/offline_repository.dart';
import 'package:xiaoyuanbao/main.dart';
import 'package:xiaoyuanbao/models/observation_view.dart';
import 'package:xiaoyuanbao/ui/observation_page.dart';
import 'package:xiaoyuanbao/ui/observation_details.dart';

Json fixture({bool confirmed = true, int home = 1, int away = 2}) => {
  'matchId': 'ui-fixture',
  'jingcaiId': '周六 004',
  'date': day(beijingNow()),
  'matchTime': '${day(beijingNow())}T09:00:00Z',
  'league': '澳超',
  'home': '布里斯班狮吼',
  'guest': '悉尼FC',
  'homeLogo': '/footballimg/661f8dc1f91b940012592482',
  'guestLogo': '/footballimg/61c590f1b3c0c30012ed7153',
  'settled': confirmed,
  'regulationSettled': confirmed,
  'homeScore': home,
  'awayScore': away,
  'displayState': confirmed ? '完赛' : '未开赛',
  'frozenDrawSignal': {
    'state': 'selected',
    'selectionVersion': 'jc-h2h-required-v1',
    'minimumHeadToHead': 1,
    'headToHeadSample': 4,
    'modelVersion': 'test-v1',
  },
  'secondaryDecision': {
    'reason': '有效交锋与赔率满足二次筛选',
    'version': 'secondary-v4-h2h-required',
    'threshold': .225,
    'minimumHeadToHead': 1,
    'headToHeadSample': 4,
    'decision': 'retained',
  },
  'scoreRecommendation': {
    'version': 'draw-score-dynamic-v2',
    'state': 'recommended',
    'scores': [
      {'home': 1, 'away': 1},
      {'home': 0, 'away': 0},
    ],
    'reason': '基于历史进球与盘口',
  },
  'frontendAnalysis': {
    'averageEuroOdds': [2.05, 3.4, 3.25],
    'winProbability': 46.2,
    'drawProbability': 28.1,
    'loseProbability': 25.7,
    'roiSimulation': {
      'totalStake': 50000000,
      'markets': [
        {
          'key': 'sporttery',
          'name': '竞彩胜平负',
          'simulated': false,
          'oddsAvailable': true,
          'bookmakerByOutcome': [
            {
              'outcome': 'home',
              'retailShare': 55.1,
              'odds': 1.9,
              'available': true,
              'bookmakerProfit': -2345000,
              'officialProfitRate': -4.7,
            },
            {
              'outcome': 'draw',
              'retailShare': 22.2,
              'odds': 3.45,
              'available': true,
              'bookmakerProfit': 11705000,
              'officialProfitRate': 23.4,
            },
            {
              'outcome': 'away',
              'retailShare': 22.7,
              'odds': 3.1,
              'available': true,
              'bookmakerProfit': 14815000,
              'officialProfitRate': 29.6,
            },
          ],
        },
      ],
    },
  },
  'totalsPrediction': {
    'modelVersion': 'totals-test',
    'state': 'recommended',
    'direction': 'over',
    'line': 2.75,
    'overWater': .9,
    'underWater': .9,
  },
  'companyQuotes': [
    {
      'key': 'william',
      'name': '威廉',
      'euroOdds': {
        'latest': ['2.05', '3.40', '3.25'],
        'opening': ['2.10', '3.30', '3.25'],
      },
    },
    {
      'key': 'sporttery',
      'name': '竞彩',
      'euroOdds': {
        'latest': ['1.90', '3.45', '3.10'],
      },
    },
  ],
  'homeRecent': {
    'sample': 10,
    'draws': 3,
    'matches': [
      {
        'date': '2026-11-14',
        'league': '澳超',
        'home': '布里斯班狮吼',
        'guest': '西悉尼流浪者',
        'homeScore': 1,
        'awayScore': 1,
      },
    ],
  },
  'awayRecent': {'sample': 10, 'draws': 2},
  'headToHead': {'sample': 4, 'draws': 2},
  'history': {
    'leagueSummary': {
      'home': {
        'all': {'rank': 4, 'game': 5, 'point': '15'},
      },
      'guest': {
        'all': {'rank': 3, 'game': 5, 'point': 0},
      },
    },
  },
};

class FakeRepository extends ObservationRepository {
  FakeRepository(this.matches);
  int detailReads = 0;
  bool failDetails = false;
  @override
  String? logoProgress;
  @override
  Future<Uint8List?> teamLogo(String source) async => _testLogos.putIfAbsent(
    source,
    () => File('../../public$source').readAsBytesSync(),
  );
  final _testLogos = <String, Uint8List>{};
  @override
  List<Json> matches;
  @override
  String serverUrl = 'http://192.168.1.10:28080';
  @override
  bool isSyncing = false;
  @override
  int completed = 0;
  @override
  int total = 0;
  @override
  String? lastError;
  @override
  DateTime? lastSync = DateTime.utc(2026, 11, 21, 9, 30);
  @override
  String? modelVersion = 'test-v1';
  @override
  Future<void> setServer(String input) async {
    serverUrl = input;
    notifyListeners();
  }

  @override
  Future<void> sync() async {
    notifyListeners();
  }

  @override
  void cancelSync() {
    isSyncing = false;
    notifyListeners();
  }

  @override
  Future<Json?> matchDetails(String id) async {
    detailReads++;
    if (failDetails) throw StateError('缓存读取测试失败');
    return matches.where((row) => row['matchId'] == id).firstOrNull;
  }
}

Future<void> capture(
  WidgetTester tester,
  GlobalKey boundary,
  String name,
) async {
  await tester.runAsync(() async {
    final render =
        boundary.currentContext!.findRenderObject() as RenderRepaintBoundary;
    final image = await render.toImage(pixelRatio: 2);
    final bytes = (await image.toByteData(format: ui.ImageByteFormat.png))!;
    final target = File('../../reports/app-refresh/$name.png');
    await target.parent.create(recursive: true);
    await target.writeAsBytes(bytes.buffer.asUint8List());
    image.dispose();
  });
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'standings and form keep the team perspective and preserve zero and deductions',
    () {
      final match = {
        'home': '甲',
        'guest': '乙',
        'history': {
          'leagueSummary': {
            'home': {
              'all': {'point': '-2', 'score': 12},
            },
            'guest': {
              'all': {'rank': 3},
            },
          },
          'rankData': {
            'total': [
              {'name': '乙', 'rank': 3, 'totalScore': 0},
            ],
            'guest': [
              {'name': '乙', 'rank': 2, 'totalScore': 7},
            ],
          },
        },
      };
      final view = ObservationView(match);
      expect(view.standingPoints(true), -2);
      expect(view.standingPoints(false), 0);
      expect(
        ObservationView({
          'guest': '丙',
          'history': match['history'],
        }).standingPoints(false),
        isNull,
      );
      final awayWin = {
        'home': '甲',
        'guest': '乙',
        'homeScore': 0,
        'awayScore': 1,
      };
      expect(historyResult(awayWin, '乙'), '胜');
      expect(historyResult(awayWin, '甲'), '负');
      expect(historyResult({...awayWin, 'awayScore': 0}, '乙'), '平');
      expect(historyResult({'result': 'win'}, '乙'), '胜');
      expect(historyResult({'venue': 'away'}, '乙'), '—');
    },
  );
  setUpAll(() async {
    final font = File('C:/Windows/Fonts/msyh.ttc');
    if (font.existsSync()) {
      final loader = FontLoader('UiTestChinese')
        ..addFont(Future.value(ByteData.sublistView(font.readAsBytesSync())));
      await loader.load();
    }
    final icons = File(
      'D:/flutter/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf',
    );
    if (icons.existsSync()) {
      final loader = FontLoader('MaterialIcons')
        ..addFont(Future.value(ByteData.sublistView(icons.readAsBytesSync())));
      await loader.load();
    }
  });

  Future<void> mount(
    WidgetTester tester,
    FakeRepository repo,
    double width,
    GlobalKey boundary, {
    double scale = 1,
  }) async {
    tester.view.physicalSize = Size(width, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(const SizedBox());
    await tester.runAsync(() async {
      for (final source
          in repo.matches
              .expand((m) => [m['homeLogo'], m['guestLogo']])
              .whereType<String>()
              .toSet()) {
        final bytes = await repo.teamLogo(source);
        if (bytes == null) continue;
        await precacheImage(
          ResizeImage.resizeIfNeeded(128, null, MemoryImage(bytes)),
          tester.element(find.byType(SizedBox).last),
        );
      }
    });
    await tester.pumpWidget(
      RepaintBoundary(
        key: boundary,
        child: MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: yuanbaoTheme().copyWith(
            textTheme: yuanbaoTheme().textTheme.apply(
              fontFamily: 'UiTestChinese',
            ),
          ),
          locale: const Locale('zh', 'CN'),
          supportedLocales: const [Locale('zh', 'CN')],
          localizationsDelegates: GlobalMaterialLocalizations.delegates,
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: TextScaler.linear(scale)),
            child: child!,
          ),
          home: ObservationPage(repository: repo),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.runAsync(() async {
      await precacheImage(
        const AssetImage('assets/images/logo.png'),
        tester.element(find.byType(ObservationPage)),
      );
    });
    await tester.pumpAndSettle();
  }

  for (final width in [320.0, 390.0]) {
    testWidgets('offline page, card and details fit $width', (tester) async {
      final boundary = GlobalKey();
      final repo = FakeRepository([fixture()]);
      await mount(tester, repo, width, boundary);
      expect(tester.takeException(), isNull);
      expect(find.text('小元宝'), findsOneWidget);
      expect(find.text('本地已缓存 1 场'), findsOneWidget);
      expect(find.bySemanticsLabel('布里斯班狮吼队徽'), findsOneWidget);
      expect(find.bySemanticsLabel('悉尼FC队徽'), findsOneWidget);
      for (final name in ['布里斯班狮吼', '悉尼FC']) {
        final badge = find.byWidgetPredicate(
          (w) => w is Image && w.semanticLabel == '$name队徽',
        );
        expect(
          tester
              .widget<RawImage>(
                find.descendant(of: badge, matching: find.byType(RawImage)),
              )
              .image,
          isNotNull,
        );
      }
      expect(
        repo.detailReads,
        0,
        reason: 'Do not decode full cached rows for cards',
      );
      await capture(tester, boundary, 'home-${width.toInt()}');
      await tester.drag(find.byType(CustomScrollView), const Offset(0, -510));
      await tester.pumpAndSettle();
      expect(find.text('+0.15'), findsNWidgets(2));
      expect(find.text('-0.05'), findsOneWidget);
      expect(find.text('主次均未命中'), findsOneWidget);
      expect(find.text('赢半'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await capture(tester, boundary, 'card-${width.toInt()}');
      await tester.tap(find.text('战绩 / 指数 / 盈亏'));
      await tester.pumpAndSettle();
      expect(repo.detailReads, 1);
      expect(find.text('15'), findsOneWidget);
      expect(find.text('0'), findsOneWidget);
      expect(find.text('胜'), findsOneWidget);
      await tester.ensureVisible(find.text('盈亏').last);
      await tester.tap(find.text('盈亏').last);
      await tester.pumpAndSettle();
      await tester.drag(find.byType(CustomScrollView), const Offset(0, -380));
      await tester.pumpAndSettle();
      expect(find.text('55.1%'), findsOneWidget);
      expect(find.text('−234.5万'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await capture(tester, boundary, 'profit-${width.toInt()}');
    });
  }

  testWidgets(
    'form strip scroll and expanded history survive tab changes independently',
    (tester) async {
      final row = fixture();
      final results = [
        'win',
        'win',
        'win',
        'win',
        'loss',
        'win',
        'win',
        'draw',
        'win',
        'win',
      ];
      row['home'] = '利物浦';
      row['homeRecent'] = {
        'sample': results.length,
        'draws': 1,
        'matches': [
          for (final result in results) {'result': result, 'league': '战绩明细测试'},
        ],
      };
      await mount(tester, FakeRepository([row]), 320, GlobalKey(), scale: 1.4);
      Future<void> reveal(Finder finder) async {
        await Scrollable.ensureVisible(tester.element(finder), alignment: .4);
        await tester.pumpAndSettle();
      }

      Future<void> tapLabel(String label) async {
        final target = find.text(label);
        await reveal(target);
        await tester.tap(target);
        await tester.pumpAndSettle();
      }

      await tapLabel('战绩 / 指数 / 盈亏');
      final section = find.byKey(
        PageStorageKey("${row['matchId']}/利物浦 · 近10场"),
      );
      final strip = find.descendant(
        of: section,
        matching: find.byType(SingleChildScrollView),
      );
      final scroller = find.descendant(
        of: strip,
        matching: find.byType(Scrollable),
      );
      ScrollPosition position() =>
          tester.state<ScrollableState>(scroller).position;
      await tapLabel('利物浦 · 近10场');
      await tapLabel('指数');
      await tapLabel('战绩');
      expect(tester.takeException(), isNull);
      expect(find.text('战绩明细测试'), findsNWidgets(10));
      await reveal(strip);
      await tester.drag(strip, const Offset(-180, 0));
      await tester.pumpAndSettle();
      final offset = position().pixels;
      expect(offset, greaterThan(0));
      await tapLabel('盈亏');
      await tapLabel('战绩');
      expect(tester.takeException(), isNull);
      expect(position().pixels, closeTo(offset, 0.01));
      expect(find.text('战绩明细测试'), findsNWidgets(10));
      await tapLabel('利物浦 · 近10场');
      expect(find.text('战绩明细测试'), findsNothing);
    },
  );

  final previewMatch = Platform.environment['YUANBAO_HISTORY_SAMPLE'];
  testWidgets(
    'renders a saved match with the full team form strips',
    (tester) async {
      final row = object(jsonDecode(File(previewMatch!).readAsStringSync()));
      final boundary = GlobalKey();
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(
        RepaintBoundary(
          key: boundary,
          child: MaterialApp(
            debugShowCheckedModeBanner: false,
            theme: yuanbaoTheme().copyWith(
              textTheme: yuanbaoTheme().textTheme.apply(
                fontFamily: 'UiTestChinese',
              ),
            ),
            home: Scaffold(
              appBar: AppBar(title: Text("${row['home']} · ${row['guest']}")),
              body: SingleChildScrollView(child: ObservationDetails(row: row)),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final homeMatches = array(object(row['homeRecent'])['matches']);
      final homeSection = find.byKey(
        PageStorageKey("${row['matchId']}/${row['home']} · 近10场"),
      );
      final badges = find.descendant(
        of: homeSection,
        matching: find.byWidgetPredicate(
          (w) => w is Text && ['胜', '平', '负', '—'].contains(w.data),
        ),
      );
      expect(
        badges.evaluate().map((e) => (e.widget as Text).data),
        homeMatches.map((m) => historyResult(object(m), row['home'])),
      );
      expect(tester.takeException(), isNull);
      await capture(tester, boundary, 'history-${row['matchId']}');
    },
    skip: previewMatch == null,
  );

  testWidgets('missing data and enlarged text stay usable', (tester) async {
    final boundary = GlobalKey(),
        row = fixture(confirmed: false, home: 0, away: 0);
    row.remove('frontendAnalysis');
    row.remove('history');
    row.remove('companyQuotes');
    final repo = FakeRepository([row]);
    await mount(tester, repo, 320, boundary, scale: 1.3);
    await tester.drag(find.byType(CustomScrollView), const Offset(0, -550));
    await tester.pumpAndSettle();
    expect(find.text('命中平局'), findsNothing);
    expect(find.text('VS'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('settings shows manual LAN address without losing local cache', (
    tester,
  ) async {
    final repo = FakeRepository([fixture()]);
    await mount(tester, repo, 320, GlobalKey());
    await tester.tap(find.byTooltip('连接后台与缓存信息'));
    await tester.pumpAndSettle();
    expect(find.text('连接你的后台'), findsOneWidget);
    expect(find.text('保存并检查更新'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('all dates and selected filters operate on cached matches', (
    tester,
  ) async {
    final second = fixture();
    second['matchId'] = 'ui-other';
    second['jingcaiId'] = '';
    second['date'] = day(beijingNow().subtract(const Duration(days: 1)));
    second['nonSportterySignal'] = {'state': 'not_selected'};
    final repo = FakeRepository([fixture(), second]);
    await mount(tester, repo, 320, GlobalKey());
    await tester.tap(find.text('全部'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('今天与全部日期'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('全部缓存日期').last);
    await tester.pumpAndSettle();
    expect(find.text('全部缓存日期'), findsOneWidget);
    await tester.tap(find.byTooltip('入选筛选'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('只看入选平'));
    await tester.pumpAndSettle();
    expect(find.text('入选平').first, findsOneWidget);
    expect(tester.takeException(), isNull);
    expect(repo.detailReads, 0);
  });

  testWidgets(
    'navigation keeps filters and moves data controls out of match cards',
    (tester) async {
      final repo = FakeRepository([fixture()]);
      final boundary = GlobalKey();
      await mount(tester, repo, 390, boundary);
      await tester.tap(find.text('复盘'));
      await tester.pumpAndSettle();
      expect(find.text('次选命中率'), findsOneWidget);
      await capture(tester, boundary, 'review');
      await tester.tap(find.text('数据'));
      await tester.pumpAndSettle();
      expect(find.text(repo.serverUrl), findsOneWidget);
      expect(find.text('检查数据更新'), findsOneWidget);
      await capture(tester, boundary, 'data');
      await tester.tap(find.text('赛程'));
      await tester.pumpAndSettle();
      expect(find.text('比赛列表'), findsOneWidget);
    },
  );

  testWidgets('detail failure can retry locally without another sync', (
    tester,
  ) async {
    final repo = FakeRepository([fixture()])..failDetails = true;
    await mount(tester, repo, 320, GlobalKey());
    await tester.drag(find.byType(CustomScrollView), const Offset(0, -550));
    await tester.pumpAndSettle();
    await tester.tap(find.text('战绩 / 指数 / 盈亏'));
    await tester.pumpAndSettle();
    expect(find.text('重新读取'), findsOneWidget);
    repo.failDetails = false;
    await tester.ensureVisible(find.text('重新读取'));
    await tester.drag(find.byType(CustomScrollView), const Offset(0, -220));
    await tester.pumpAndSettle();
    await tester.tap(find.text('重新读取'));
    await tester.pumpAndSettle();
    expect(find.text('联赛排名与赛程'), findsOneWidget);
    expect(repo.detailReads, 2);
    expect(tester.takeException(), isNull);
  });

  test(
    'prediction follows selected metadata and confirmed regulation score',
    () {
      final pending = ObservationView(
        fixture(confirmed: false, home: 0, away: 0),
      );
      expect(pending.primaryLabel, '入选平 · 待赛果');
      expect(pending.totalsOutcome, '待赛果核对');
      expect(pending.alternative, 'home');
      final zero = ObservationView(fixture(home: 0, away: 0));
      expect(zero.draw, true);
      expect(zero.primaryLabel, '命中平局');
      expect(zero.alternativeOutcome, '主选平命中');
      final stale = fixture();
      object(stale['frozenDrawSignal']);
      (stale['frozenDrawSignal'] as Map).remove('selectionVersion');
      expect(ObservationView(stale).selected, false);
      expect(ObservationView(stale).alternative, null);
    },
  );

  test('quarter lines split correctly; missing prices are not zero', () {
    for (final entry in {
      2.0: '全赢',
      2.25: '全赢',
      2.5: '全赢',
      2.75: '赢半',
      3.0: '走盘',
      3.25: '输半',
      3.5: '全输',
    }.entries) {
      final row = fixture();
      (row['totalsPrediction'] as Map)['line'] = entry.key;
      expect(ObservationView(row).totalsOutcome, entry.value);
    }
    final row = fixture();
    row['companyQuotes'] = [];
    expect(ObservationView(row).differences, [null, null, null]);
    (row['frontendAnalysis'] as Map)['averageEuroOdds'] = [2.0, 3.4, 2.0];
    expect(ObservationView(row).alternative, null);
  });
}
