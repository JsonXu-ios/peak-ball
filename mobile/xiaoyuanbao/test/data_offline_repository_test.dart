import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:xiaoyuanbao/data/offline_repository.dart';
import 'package:xiaoyuanbao/data/team_logos.dart';

class FakeSource {
  Map<String, Map<String, dynamic>> rows = {};
  int batches = 0;
  int manifests = 0;
  int failBatch = -1;
  bool corrupt = false;
  List<String> lastBatchIds = [];
  final String snapshot = 'a' * 64;
  Map<String, String> get payloads =>
      rows.map((key, value) => MapEntry(key, jsonEncode(value)));
  String digest(String raw) => sha256.convert(utf8.encode(raw)).toString();
  Future<http.Response> handle(http.Request request) async {
    if (request.url.path.endsWith('/manifest')) {
      manifests++;
      return http.Response(
        jsonEncode({
          'schema': 1,
          'snapshot': snapshot,
          'modelVersion': 'november-test',
          'scheduleTimezone': 'Asia/Shanghai',
          'total': rows.length,
          'entries': payloads.entries
              .map((e) => {'matchId': e.key, 'revision': digest(e.value)})
              .toList(),
        }),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    }
    batches++;
    if (batches == failBatch) return http.Response('network failure', 503);
    final requestBody = jsonDecode(request.body) as Map;
    lastBatchIds = List<String>.from(requestBody['ids'] as List);
    return http.Response(
      jsonEncode({
        'snapshot': snapshot,
        'rows': (requestBody['ids'] as List).map((id) {
          final payload = payloads[id]!;
          return {
            'matchId': id,
            'revision': digest(payload),
            'payload': corrupt ? '$payload ' : payload,
          };
        }).toList(),
      }),
      200,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  late FakeSource source;
  late OfflineRepository repo;
  setUp(() async {
    source = FakeSource();
    repo = await OfflineRepository.open(
      databasePath: inMemoryDatabasePath,
      factory: databaseFactoryFfi,
      clientFactory: () => MockClient(source.handle),
    );
    await repo.setServer('192.168.1.9:28080');
  });
  tearDown(() async => repo.close());

  test('normalizes addresses and rejects credentials/paths', () {
    expect(repo.serverUrl, 'http://192.168.1.9:28080');
    expect(
      OfflineRepository.normalizeServer('http://192.168.1.9:28080/api/'),
      repo.serverUrl,
    );
    expect(
      () => OfflineRepository.normalizeServer('http://user:secret@example.com'),
      throwsFormatException,
    );
    expect(
      () => OfflineRepository.normalizeServer('http://host/other'),
      throwsFormatException,
    );
  });

  test(
    'full cache, unchanged no download, correction and deletion are incremental',
    () async {
      source.rows = {
        '1': {
          'matchId': '1',
          'date': '2026-11-21',
          'home': '甲',
          'history': {
            'leagueSummary': {
              'home': {
                'all': {'rank': 1, 'game': 10},
              },
            },
          },
          'rawEuroOdds': {
            'data': [1, 2, 3],
          },
        },
        '2': {'matchId': '2', 'date': '2026-10-01'},
      };
      await repo.sync();
      expect(repo.lastError, isNull);
      expect(repo.matches.length, 2);
      expect(source.batches, 1);
      expect(repo.completed, 2);
      expect((await repo.matchDetails('1'))!['rawEuroOdds'], {
        'data': [1, 2, 3],
      });
      expect(repo.modelVersion, 'november-test');
      expect(repo.lastSync, isNotNull);
      await repo.sync();
      expect(source.batches, 1);
      expect(repo.total, 0);
      source.rows = {
        '1': {...source.rows['1']!, 'homeScore': 2},
        '3': {'matchId': '3', 'status': 0},
      };
      await repo.sync();
      expect(repo.lastError, isNull);
      expect(source.batches, 2);
      expect(repo.matches.map((e) => e['matchId']).toSet(), {'1', '3'});
      expect((await repo.matchDetails('1'))!['homeScore'], 2);
      expect(await repo.matchDetails('2'), isNull);
    },
  );

  test(
    'interrupted download resumes after reopening and fetches only missing or corrected records',
    () async {
      final folder = await Directory.systemTemp.createTemp('yuanbao-resume-');
      final path = '${folder.path}/offline.db';
      addTearDown(() async {
        await repo.close();
        await folder.delete(recursive: true);
      });
      Future<OfflineRepository> reopen() async {
        await repo.close();
        return OfflineRepository.open(
          databasePath: path,
          factory: databaseFactoryFfi,
          clientFactory: () => MockClient(source.handle),
        );
      }

      repo = await reopen();
      source.rows = {
        'old': {'matchId': 'old', 'home': '原数据'},
      };
      await repo.sync();
      final synced = repo.lastSync;
      source.rows = {
        for (var i = 0; i < 22; i++) '$i': {'matchId': '$i'},
      };
      source.failBatch = source.batches + 2;
      await repo.sync();
      expect(repo.lastError, contains('503'));
      expect(repo.matches.single['matchId'], 'old');
      expect(repo.lastSync, synced);
      repo = await reopen();
      expect(repo.matches.single['matchId'], 'old');
      source.failBatch = -1;
      final requestsBeforeResume = source.batches;
      source.rows['0'] = {'matchId': '0', 'home': '已更正'};
      await repo.sync();
      expect(repo.lastError, isNull);
      expect(source.batches - requestsBeforeResume, 1);
      expect(source.lastBatchIds.toSet(), {'0', '20', '21'});
      expect((await repo.matchDetails('0'))!['home'], '已更正');
      expect(repo.matches.length, 22);
    },
  );

  test('wrong checksum cannot replace saved cache', () async {
    source.rows = {
      '1': {'matchId': '1', 'homeScore': 0},
    };
    await repo.sync();
    source.rows['1'] = {'matchId': '1', 'homeScore': 4};
    source.corrupt = true;
    await repo.sync();
    expect(repo.lastError, contains('校验失败'));
    expect(repo.matches.single['homeScore'], 0);
  });

  test(
    'logo paths, incremental prefetch and offline reads after reopening',
    () async {
      final folder = await Directory.systemTemp.createTemp('yuanbao-logos-');
      final path = '${folder.path}/offline.db';
      final image = File('assets/images/logo.png').readAsBytesSync();
      final requests = <Uri>[];
      Future<OfflineRepository> reopen({bool offline = false}) async {
        await repo.close();
        return OfflineRepository.open(
          databasePath: path,
          factory: databaseFactoryFfi,
          clientFactory: () => MockClient((request) async {
            if (request.method == 'POST') return source.handle(request);
            requests.add(request.url);
            if (offline) throw StateError('unexpected network read');
            return http.Response.bytes(image, 200);
          }),
        );
      }

      addTearDown(() async {
        await repo.close();
        await folder.delete(recursive: true);
      });
      repo = await reopen();
      const local = '/footballimg/alpha';
      const remote = '//img.vipc.cn/vipc-sport/image/alpha?x=1';
      expect(
        teamLogoUri(repo.serverUrl, remote),
        teamLogoUri(repo.serverUrl, local),
      );
      expect(teamLogoUri(repo.serverUrl, 'data:text/plain,no'), isNull);
      source.rows = {
        '1': {
          'matchId': '1',
          'homeLogo': local,
          'guestLogo': '/footballimg/beta',
        },
        '2': {'matchId': '2', 'homeLogo': remote},
      };
      await repo.sync();
      expect(repo.lastError, isNull);
      expect(
        requests.map((u) => u.path),
        unorderedEquals([local, '/footballimg/beta']),
      );
      expect(await repo.teamLogo(remote), image);
      repo = await reopen(offline: true);
      await repo.sync();
      expect(repo.completed, 0);
      expect(await repo.teamLogo(local), image);
      expect(requests.length, 2);
    },
  );

  test('server caches are isolated and reappear when switching back', () async {
    source.rows = {
      '1': {'matchId': '1'},
    };
    await repo.sync();
    await repo.setServer('192.168.1.10:28080');
    expect(repo.matches, isEmpty);
    expect(repo.lastSync, isNull);
    source.rows = {
      '2': {'matchId': '2'},
    };
    await repo.sync();
    expect(repo.matches.single['matchId'], '2');
    await repo.setServer('192.168.1.9:28080');
    expect(repo.matches.single['matchId'], '1');
  });

  test('server setting and complete cache survive app restart', () async {
    final folder = await Directory.systemTemp.createTemp('xiaoyuanbao-cache-');
    addTearDown(() async {
      await repo.close();
      await folder.delete(recursive: true);
    });
    await repo.close();
    final path = '${folder.path}/offline.db';
    repo = await OfflineRepository.open(
      databasePath: path,
      factory: databaseFactoryFfi,
      clientFactory: () => MockClient(source.handle),
    );
    expect(repo.serverUrl, ObservationRepository.defaultServer);
    await repo.setServer('192.168.1.9:28080');
    source.rows = {
      'persisted': {
        'matchId': 'persisted',
        'home': '离线球队',
        'rawEuroOdds': {
          'history': ['all retained'],
        },
      },
    };
    await repo.sync();
    final synced = repo.lastSync;
    await repo.close();
    repo = await OfflineRepository.open(
      databasePath: path,
      factory: databaseFactoryFfi,
      clientFactory: () => MockClient((_) async => http.Response('', 503)),
    );
    expect(repo.serverUrl, 'http://192.168.1.9:28080');
    expect(repo.lastSync, synced);
    expect(repo.matches.single['home'], '离线球队');
    expect((await repo.matchDetails('persisted'))!['rawEuroOdds'], {
      'history': ['all retained'],
    });
    await repo.sync();
    expect(repo.lastError, isNotNull);
    expect(repo.matches.single['home'], '离线球队');
  });

  test(
    'cancellation resolves a waiting network call and leaves cache intact',
    () async {
      await repo.close();
      final waiting = Completer<http.Response>();
      repo = await OfflineRepository.open(
        databasePath: inMemoryDatabasePath,
        factory: databaseFactoryFfi,
        clientFactory: () => MockClient((_) => waiting.future),
      );
      await repo.setServer('192.168.1.9:28080');
      final syncing = repo.sync();
      await Future<void>.delayed(const Duration(milliseconds: 20));
      repo.cancelSync();
      await syncing;
      expect(repo.isSyncing, false);
      expect(repo.matches, isEmpty);
      expect(repo.lastError, contains('取消'));
      waiting.complete(http.Response('{}', 500));
    },
  );
}
