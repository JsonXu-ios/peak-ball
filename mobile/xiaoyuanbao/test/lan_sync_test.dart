import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:xiaoyuanbao/data/offline_repository.dart';

void main() {
  test(
    'downloads the LAN manifest, reopens cache and checks deltas',
    () async {
      sqfliteFfiInit();
      final folder = await Directory.systemTemp.createTemp('yuanbao-lan-');
      final path = '${folder.path}/cache.db';
      var repo = await OfflineRepository.open(
        databasePath: path,
        factory: databaseFactoryFfi,
      );
      try {
        await repo.sync();
        expect(repo.lastError, isNull);
        final count = repo.matches.length;
        expect(count, greaterThan(0));
        final id = repo.matches.first['matchId'] as String;
        expect((await repo.matchDetails(id))!['matchId'], id);
        final logoSource = repo.matches
            .map((r) => r['homeLogo'])
            .whereType<String>()
            .firstWhere((s) => s.isNotEmpty);
        await repo.close();
        final db = await databaseFactoryFfi.openDatabase(path);
        final totals = await db.rawQuery(
          'SELECT COUNT(*) AS n FROM team_logos',
        );
        final logos = totals.single['n'] as int;
        expect(logos, greaterThan(0));
        await db.close();
        repo = await OfflineRepository.open(
          databasePath: path,
          factory: databaseFactoryFfi,
        );
        expect(repo.matches.length, count);
        expect(await repo.teamLogo(logoSource), isNotEmpty);
        await repo.sync();
        expect(repo.lastError, isNull);
        printOnFailure('initial=$count delta=${repo.completed}');
        stdout.writeln(
          'LAN: initial=$count; cached logos=$logos; subsequent match downloads=${repo.completed}',
        );
      } finally {
        await repo.close();
        await folder.delete(recursive: true);
      }
    },
    skip: Platform.environment['TEST_YUANBAO_LAN'] != '1',
    timeout: const Timeout(Duration(minutes: 25)),
  );
}
