import 'dart:async';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:http/http.dart' as http;
import 'package:sqflite/sqflite.dart';

Uri? teamLogoUri(String server, String source) {
  final raw = source.trim();
  if (raw.isEmpty) return null;
  final uri = Uri.tryParse(raw.startsWith('//') ? 'https:$raw' : raw);
  if (uri == null) return null;
  final base = Uri.tryParse(server);
  if (uri.hasScheme) {
    if (!['http', 'https'].contains(uri.scheme) || uri.host.isEmpty) {
      return null;
    }
    if ((uri.host == 'vipc.cn' || uri.host.endsWith('.vipc.cn')) &&
        uri.path.contains('/vipc-sport/image/') &&
        uri.pathSegments.last.isNotEmpty) {
      return base?.resolve(
        '/footballimg/${Uri.encodeComponent(uri.pathSegments.last)}',
      );
    }
    return uri;
  }
  if (uri.path.startsWith('/footballimg/') || uri.path.startsWith('/images/')) {
    return base?.resolveUri(uri);
  }
  return null;
}

class TeamLogoCache {
  TeamLogoCache(this.db, this.createClient);
  final Database db;
  final http.Client Function() createClient;
  http.Client? _client;
  final _pending = <Uri, Future<Uint8List?>>{};
  final _unavailableUntil = <Uri, DateTime>{};
  bool _closed = false;

  static Future<void> createTable(Database db) => db.execute(
    'CREATE TABLE team_logos (url TEXT PRIMARY KEY, image BLOB NOT NULL)',
  );

  Future<Uint8List?> read(String server, String source) async {
    final url = teamLogoUri(server, source);
    if (url == null || _closed) return null;
    try {
      return await _load(url);
    } catch (_) {
      return null;
    }
  }

  Future<Uint8List?> _load(Uri url) => _pending.putIfAbsent(
    url,
    () => _readOrDownload(url).whenComplete(() {
      _pending.remove(url);
    }),
  );

  Future<Uint8List?> _readOrDownload(Uri url) async {
    final rows = await db.query(
      'team_logos',
      columns: ['image'],
      where: 'url=?',
      whereArgs: [url.toString()],
      limit: 1,
    );
    if (rows.isNotEmpty) return rows.single['image'] as Uint8List;
    if (_closed || _unavailableUntil[url]?.isAfter(DateTime.now()) == true) {
      return null;
    }
    final client = _client ??= createClient();
    Future<Uint8List?> download() async {
      final response = await client.send(http.Request('GET', url));
      if (response.statusCode != 200) {
        await response.stream.listen(null).cancel();
        if (response.statusCode >= 500) {
          throw http.ClientException('队徽服务暂不可用', url);
        }
        return null;
      }
      final bytes = BytesBuilder(copy: false);
      await for (final chunk in response.stream) {
        if (bytes.length + chunk.length > 2 << 20) return null;
        bytes.add(chunk);
      }
      return bytes.takeBytes();
    }

    final bytes = await download().timeout(const Duration(seconds: 12));
    if (bytes == null || bytes.isEmpty || !await _isImage(bytes)) {
      _unavailableUntil[url] = DateTime.now().add(const Duration(hours: 1));
      return null;
    }
    if (!_closed) {
      await db.insert('team_logos', {
        'url': url.toString(),
        'image': bytes,
      }, conflictAlgorithm: ConflictAlgorithm.replace);
    }
    return bytes;
  }

  static Future<bool> _isImage(Uint8List bytes) async {
    final buffer = await ui.ImmutableBuffer.fromUint8List(bytes);
    try {
      final descriptor = await ui.ImageDescriptor.encoded(buffer);
      descriptor.dispose();
      return true;
    } catch (_) {
      return false;
    } finally {
      buffer.dispose();
    }
  }

  Future<void> prefetch(
    String server,
    List<Map<String, dynamic>> matches,
    bool Function() active,
    void Function(int, int) progress,
  ) async {
    final urls = <Uri>{};
    for (final row in matches) {
      for (final field in ['homeLogo', 'guestLogo']) {
        final source = row[field];
        if (source is String) {
          final url = teamLogoUri(server, source);
          if (url != null) urls.add(url);
        }
      }
    }
    if (urls.isEmpty || !active()) return;
    final cached = await db.query('team_logos', columns: ['url']);
    for (final row in cached) {
      urls.remove(Uri.parse(row['url'] as String));
    }
    urls.removeWhere(
      (url) => _unavailableUntil[url]?.isAfter(DateTime.now()) == true,
    );
    if (urls.isEmpty || !active()) return;
    final pending = urls.iterator;
    final total = urls.length;
    var done = 0, interrupted = false;
    progress(0, total);
    Future<void> worker() async {
      while (!interrupted && active() && pending.moveNext()) {
        try {
          await _load(pending.current);
        } catch (_) {
          interrupted = true;
          return;
        }
        done++;
        if (active() && (done % 16 == 0 || done == total)) {
          progress(done, total);
        }
      }
    }

    await Future.wait(List.generate(4, (_) => worker()));
    if (interrupted && active()) throw StateError('队徽连接中断');
  }

  void close() {
    _closed = true;
    _client?.close();
  }
}
