import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:path/path.dart' as p;
import 'package:sqflite/sqflite.dart';

abstract class ObservationRepository extends ChangeNotifier {
  String get serverUrl;
  List<Map<String, dynamic>> get matches;
  bool get isSyncing;
  int get completed;
  int get total;
  String? get lastError;
  DateTime? get lastSync;
  String? get modelVersion;
  Future<void> setServer(String input);
  Future<void> sync();
  void cancelSync();
  Future<Map<String, dynamic>?> matchDetails(String id);
}

/// Downloaded records are staged in SQLite and verified against the immutable
/// server manifest. No visible cache changes until every record is complete.
class OfflineRepository extends ObservationRepository {
  OfflineRepository._(this._db, this._clientFactory);
  final Database _db;
  final http.Client Function() _clientFactory;
  http.Client? _activeClient;
  Completer<void>? _cancel;
  int _generation = 0;
  bool _closed = false;
  bool _committing = false;

  @override
  String serverUrl = '';
  @override
  List<Map<String, dynamic>> matches = const [];
  @override
  bool isSyncing = false;
  @override
  int completed = 0;
  @override
  int total = 0;
  @override
  String? lastError;
  @override
  DateTime? lastSync;
  @override
  String? modelVersion;

  static Future<OfflineRepository> open({
    String? databasePath,
    DatabaseFactory? factory,
    http.Client Function()? clientFactory,
  }) async {
    final dbFactory = factory ?? databaseFactory;
    final path =
        databasePath ??
        p.join(await dbFactory.getDatabasesPath(), 'xiaoyuanbao.db');
    final db = await dbFactory.openDatabase(
      path,
      options: OpenDatabaseOptions(
        version: 1,
        onCreate: (db, _) async {
          await db.execute(
            'CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)',
          );
          await db.execute(
            'CREATE TABLE servers (server TEXT PRIMARY KEY, snapshot TEXT NOT NULL, model TEXT NOT NULL, synced TEXT NOT NULL)',
          );
          await db.execute(
            'CREATE TABLE records (server TEXT NOT NULL, id TEXT NOT NULL, revision TEXT NOT NULL, payload TEXT NOT NULL, view TEXT NOT NULL, PRIMARY KEY(server,id))',
          );
          await db.execute(
            'CREATE TABLE staged (token TEXT NOT NULL, id TEXT NOT NULL, revision TEXT NOT NULL, payload TEXT NOT NULL, view TEXT NOT NULL, PRIMARY KEY(token,id))',
          );
          await db.execute(
            'CREATE TABLE manifest_entries (token TEXT NOT NULL, id TEXT NOT NULL, revision TEXT NOT NULL, PRIMARY KEY(token,id))',
          );
        },
      ),
    );
    final repo = OfflineRepository._(db, clientFactory ?? http.Client.new);
    // An interrupted download was never committed, so it is safe to discard.
    await db.delete('staged');
    await db.delete('manifest_entries');
    final settings = await db.query(
      'settings',
      where: 'key = ?',
      whereArgs: ['server'],
    );
    if (settings.isNotEmpty) {
      repo.serverUrl = settings.single['value'] as String;
    }
    await repo._load();
    return repo;
  }

  static String normalizeServer(String input) {
    var value = input.trim();
    if (value.isEmpty) throw const FormatException('请输入局域网后台地址');
    if (!value.contains('://')) value = 'http://$value';
    final uri = Uri.tryParse(value);
    if (uri == null ||
        !['http', 'https'].contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment ||
        !['', '/', '/api', '/api/'].contains(uri.path)) {
      throw const FormatException('请输入后台地址，例如 http://192.168.1.10:28080');
    }
    return uri
        .replace(path: '', query: null, fragment: null)
        .toString()
        .replaceFirst(RegExp(r'/$'), '');
  }

  @override
  Future<void> setServer(String input) async {
    final normalized = normalizeServer(input);
    if (normalized == serverUrl) return;
    cancelSync();
    serverUrl = normalized;
    lastError = null;
    await _db.insert('settings', {
      'key': 'server',
      'value': serverUrl,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
    await _load();
    _notify();
  }

  Future<void> _load() async {
    final server = serverUrl;
    final meta = await _db.query(
      'servers',
      where: 'server = ?',
      whereArgs: [server],
    );
    final rows = await _db.query(
      'records',
      columns: ['view'],
      where: 'server = ?',
      whereArgs: [server],
    );
    if (server != serverUrl || _closed) return;
    lastSync = meta.isEmpty
        ? null
        : DateTime.tryParse(meta.single['synced'] as String);
    modelVersion = meta.isEmpty ? null : meta.single['model'] as String;
    matches = rows.map((r) => _object(jsonDecode(r['view'] as String))).toList()
      ..sort(
        (a, b) => '${a['matchTime'] ?? ''}|${a['matchId']}'.compareTo(
          '${b['matchTime'] ?? ''}|${b['matchId']}',
        ),
      );
  }

  @override
  Future<Map<String, dynamic>?> matchDetails(String id) async {
    final rows = await _db.query(
      'records',
      columns: ['payload'],
      where: 'server = ? AND id = ?',
      whereArgs: [serverUrl, id],
      limit: 1,
    );
    return rows.isEmpty
        ? null
        : _object(jsonDecode(rows.single['payload'] as String));
  }

  @override
  Future<void> sync() async {
    if (isSyncing || _closed) return;
    if (serverUrl.isEmpty) {
      lastError = '请先设置局域网后台地址';
      _notify();
      return;
    }
    final generation = ++_generation;
    final server = serverUrl;
    final token = '${DateTime.now().microsecondsSinceEpoch}-$generation';
    final client = _clientFactory();
    final cancel = Completer<void>();
    _activeClient = client;
    _cancel = cancel;
    isSyncing = true;
    completed = 0;
    total = 0;
    lastError = null;
    _notify();
    void check() {
      if (_closed ||
          generation != _generation ||
          cancel.isCompleted ||
          server != serverUrl) {
        throw const _SyncCancelled();
      }
    }

    var committed = false;
    try {
      final manifest = await _post(
        client,
        server,
        '/api/mobile/sync/manifest',
        {},
        cancel,
        const Duration(minutes: 11),
        24 << 20,
      );
      check();
      if (manifest['schema'] != 1 ||
          manifest['scheduleTimezone'] != 'Asia/Shanghai' ||
          !_hash(manifest['snapshot']) ||
          manifest['modelVersion'] is! String ||
          manifest['entries'] is! List ||
          manifest['total'] != (manifest['entries'] as List).length) {
        throw const FormatException('后台离线数据格式不兼容，请更新后台');
      }
      final entries = <String, String>{};
      for (final raw in manifest['entries'] as List) {
        final item = _object(raw);
        final id = item['matchId'];
        final revision = item['revision'];
        if (id is! String ||
            id.isEmpty ||
            id.length > 128 ||
            !_hash(revision) ||
            entries.containsKey(id)) {
          throw const FormatException('离线清单包含无效或重复比赛');
        }
        entries[id] = revision as String;
      }
      final oldRows = await _db.query(
        'records',
        columns: ['id', 'revision'],
        where: 'server = ?',
        whereArgs: [server],
      );
      final old = {
        for (final r in oldRows) r['id'] as String: r['revision'] as String,
      };
      final changed = entries.keys
          .where((id) => old[id] != entries[id])
          .toList();
      total = changed.length;
      _notify();
      final manifestBatch = _db.batch();
      for (final e in entries.entries) {
        manifestBatch.insert('manifest_entries', {
          'token': token,
          'id': e.key,
          'revision': e.value,
        });
      }
      await manifestBatch.commit(noResult: true);
      for (var offset = 0; offset < changed.length; offset += 8) {
        check();
        final ids = changed.skip(offset).take(8).toList();
        final response = await _post(
          client,
          server,
          '/api/mobile/sync/batch',
          {'snapshot': manifest['snapshot'], 'ids': ids},
          cancel,
          const Duration(minutes: 2),
          64 << 20,
        );
        check();
        if (response['snapshot'] != manifest['snapshot'] ||
            response['rows'] is! List ||
            (response['rows'] as List).length != ids.length) {
          throw const FormatException('下载的数据不属于当前快照');
        }
        final pending = ids.toSet();
        final batch = _db.batch();
        for (final raw in response['rows'] as List) {
          final row = _object(raw);
          final id = row['matchId'];
          final payload = row['payload'];
          if (id is! String ||
              !pending.remove(id) ||
              payload is! String ||
              row['revision'] != entries[id] ||
              sha256.convert(utf8.encode(payload)).toString() != entries[id]) {
            throw const FormatException('比赛数据校验失败，原缓存已保留');
          }
          final decoded = _object(jsonDecode(payload));
          if (decoded['matchId'] != id) {
            throw const FormatException('比赛编号与下载清单不一致');
          }
          batch.insert('staged', {
            'token': token,
            'id': id,
            'revision': entries[id],
            'payload': payload,
            'view': jsonEncode(_compactView(decoded)),
          });
        }
        if (pending.isNotEmpty) throw const FormatException('部分比赛尚未下载完成');
        await batch.commit(noResult: true);
        completed += ids.length;
        _notify();
      }
      check();
      final synced = DateTime.now().toUtc().toIso8601String();
      // The final SQLite commit is indivisible. Cancellation is available while
      // downloading, but cannot report "cancelled" after that commit succeeds.
      _committing = true;
      await _db.transaction((txn) async {
        check();
        final count = Sqflite.firstIntValue(
          await txn.rawQuery('SELECT COUNT(*) FROM staged WHERE token = ?', [
            token,
          ]),
        );
        if (count != changed.length) {
          throw const FormatException('下载未完成，原缓存已保留');
        }
        await txn.rawInsert(
          'INSERT OR REPLACE INTO records(server,id,revision,payload,view) SELECT ?,id,revision,payload,view FROM staged WHERE token = ?',
          [server, token],
        );
        await txn.rawDelete(
          'DELETE FROM records WHERE server = ? AND NOT EXISTS (SELECT 1 FROM manifest_entries WHERE token = ? AND manifest_entries.id = records.id)',
          [server, token],
        );
        final actual = Sqflite.firstIntValue(
          await txn.rawQuery('SELECT COUNT(*) FROM records WHERE server = ?', [
            server,
          ]),
        );
        if (actual != entries.length) throw const FormatException('缓存数量校验失败');
        await txn.insert('servers', {
          'server': server,
          'snapshot': manifest['snapshot'],
          'model': manifest['modelVersion'],
          'synced': synced,
        }, conflictAlgorithm: ConflictAlgorithm.replace);
        check();
      });
      committed = true;
      await _load();
    } on _SyncCancelled {
      if (generation == _generation) lastError = '同步已取消，原缓存已保留';
    } catch (error) {
      if (generation == _generation) {
        lastError = committed
            ? '数据已保存，但页面载入失败，请重新打开 App'
            : error is FormatException
            ? error.message.toString()
            : error is TimeoutException
            ? '连接超时，请检查同一局域网、后台地址和防火墙；原缓存已保留'
            : '同步失败：$error；原缓存已保留';
      }
    } finally {
      _committing = false;
      client.close();
      if (!_closed) {
        await _db.delete('staged', where: 'token = ?', whereArgs: [token]);
        await _db.delete(
          'manifest_entries',
          where: 'token = ?',
          whereArgs: [token],
        );
      }
      if (generation == _generation) {
        isSyncing = false;
        _activeClient = null;
        _cancel = null;
        _notify();
      }
    }
  }

  @override
  void cancelSync() {
    if (_committing) return;
    if (_cancel?.isCompleted == false) _cancel!.complete();
    _activeClient?.close();
    _activeClient = null;
    if (isSyncing) {
      ++_generation;
      isSyncing = false;
      lastError = '同步已取消，原缓存已保留';
      _notify();
    }
  }

  Future<void> close() async {
    if (_closed) return;
    cancelSync();
    _closed = true;
    await _db.close();
    super.dispose();
  }

  void _notify() {
    if (!_closed) notifyListeners();
  }

  static bool _hash(Object? value) =>
      value is String && RegExp(r'^[0-9a-f]{64}$').hasMatch(value);
  static Map<String, dynamic> _object(Object? value) {
    if (value is! Map) throw const FormatException('后台返回的数据不是对象');
    return Map<String, dynamic>.from(value);
  }

  static Map<String, dynamic> _compactView(Map<String, dynamic> source) {
    // Only fields needed by collapsed cards are materialized in memory. Details
    // (including full historical arrays) are read from payload on expansion.
    final result = _pick(source, [
      'matchId',
      'jingcaiId',
      'date',
      'matchTime',
      'league',
      'home',
      'guest',
      'homeLogo',
      'guestLogo',
      'homeScore',
      'awayScore',
      'status',
      'displayState',
      'settled',
      'regulationSettled',
      'frozenDrawSignal',
      'nonSportterySignal',
      'scoreRecommendation',
    ]);
    for (final key in ['homeRecent', 'awayRecent', 'headToHead']) {
      result[key] = _pick(source[key], ['sample', 'draws']);
    }
    result['secondaryDecision'] = _pick(source['secondaryDecision'], [
      'version',
      'threshold',
      'minimumHeadToHead',
      'headToHeadSample',
      'decision',
    ]);
    result['totalsPrediction'] = _pick(source['totalsPrediction'], [
      'state',
      'modelVersion',
      'direction',
      'line',
      'overWater',
      'underWater',
    ]);
    final analysis = _map(source['frontendAnalysis']);
    result['frontendAnalysis'] = {
      ..._pick(analysis, [
        'winProbability',
        'drawProbability',
        'loseProbability',
        'averageEuroOdds',
      ]),
      'localMarket': _pick(analysis['localMarket'], [
        'key',
        'odds',
        'oddsAvailable',
      ]),
    };
    result['companyQuotes'] = source['companyQuotes'] is List
        ? (source['companyQuotes'] as List).map((raw) {
            final quote = _map(raw);
            return {
              'key': quote['key'],
              'euroOdds': _pick(quote['euroOdds'], ['latest']),
            };
          }).toList()
        : [];
    final summary = _map(_map(source['history'])['leagueSummary']);
    result['history'] = {
      'leagueSummary': {
        for (final side in ['home', 'guest'])
          side: {
            'all': _pick(_map(summary[side])['all'], [
              'rank',
              'game',
              'score',
              'points',
              'integral',
            ]),
          },
      },
    };
    return result;
  }

  static Map<String, dynamic> _map(Object? value) {
    if (value is String) {
      try {
        value = jsonDecode(value);
      } catch (_) {
        return {};
      }
    }
    return value is Map ? Map<String, dynamic>.from(value) : {};
  }

  static Map<String, dynamic> _pick(Object? value, List<String> keys) {
    final source = _map(value);
    return {
      for (final key in keys)
        if (source.containsKey(key)) key: source[key],
    };
  }

  static Future<Map<String, dynamic>> _post(
    http.Client client,
    String server,
    String path,
    Map<String, dynamic> body,
    Completer<void> cancel,
    Duration timeout,
    int maxBytes,
  ) async {
    Future<Map<String, dynamic>> request() async {
      final message = http.Request('POST', Uri.parse('$server$path'))
        ..headers['Content-Type'] = 'application/json'
        ..body = jsonEncode(body);
      final response = await client.send(message);
      final bytes = <int>[];
      await for (final chunk in response.stream) {
        if (bytes.length + chunk.length > maxBytes) {
          throw const FormatException('单批数据过大，原缓存已保留');
        }
        bytes.addAll(chunk);
      }
      if (response.statusCode == 410 || response.statusCode == 409) {
        throw const FormatException('后台快照已更新，请再次点击同步；原缓存已保留');
      }
      if (response.statusCode != 200) {
        throw FormatException('后台返回 ${response.statusCode}，请确认地址是支持离线同步的新后台');
      }
      return _object(jsonDecode(utf8.decode(bytes)));
    }

    return Future.any([
      request().timeout(timeout),
      cancel.future.then<Map<String, dynamic>>(
        (_) => throw const _SyncCancelled(),
      ),
    ]);
  }
}

class _SyncCancelled implements Exception {
  const _SyncCancelled();
}
