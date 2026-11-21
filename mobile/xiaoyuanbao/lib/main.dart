import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'data/offline_repository.dart';
import 'ui/observation_page.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const XiaoyuanbaoApp());
}

ThemeData yuanbaoTheme() => ThemeData(
  useMaterial3: true,
  colorScheme: ColorScheme.fromSeed(
    seedColor: const Color(0xff245cdd),
    brightness: Brightness.light,
    surface: Colors.white,
  ),
  scaffoldBackgroundColor: const Color(0xfff3f6fb),
  textTheme: const TextTheme(
    bodyLarge: TextStyle(fontSize: 16, height: 1.45),
    bodyMedium: TextStyle(fontSize: 15, height: 1.4),
    bodySmall: TextStyle(fontSize: 13, height: 1.4),
    titleLarge: TextStyle(fontSize: 23, fontWeight: FontWeight.w700),
    titleMedium: TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
    labelLarge: TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
  ),
  appBarTheme: const AppBarTheme(
    backgroundColor: Colors.white,
    foregroundColor: Color(0xff122345),
    elevation: 0,
    scrolledUnderElevation: 0,
  ),
  inputDecorationTheme: InputDecorationTheme(
    filled: true,
    fillColor: const Color(0xfff3f6fb),
    border: OutlineInputBorder(borderRadius: BorderRadius.circular(14)),
  ),
);

class XiaoyuanbaoApp extends StatefulWidget {
  const XiaoyuanbaoApp({super.key});

  @override
  State<XiaoyuanbaoApp> createState() => _XiaoyuanbaoAppState();
}

class _XiaoyuanbaoAppState extends State<XiaoyuanbaoApp> {
  late Future<OfflineRepository> _opening;
  OfflineRepository? _repository;

  @override
  void initState() {
    super.initState();
    _opening = _open();
  }

  Future<OfflineRepository> _open() async {
    final repository = await OfflineRepository.open();
    if (!mounted) {
      await repository.close();
      throw StateError('页面已关闭');
    }
    _repository = repository;
    return repository;
  }

  @override
  void dispose() {
    final repository = _repository;
    if (repository != null) unawaited(repository.close());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: '小元宝',
    debugShowCheckedModeBanner: false,
    theme: yuanbaoTheme(),
    locale: const Locale('zh', 'CN'),
    supportedLocales: const [Locale('zh', 'CN')],
    localizationsDelegates: GlobalMaterialLocalizations.delegates,
    home: FutureBuilder<OfflineRepository>(
      future: _opening,
      builder: (context, snapshot) {
        if (snapshot.hasData) {
          return ObservationPage(repository: snapshot.requireData);
        }
        return Scaffold(
          body: SafeArea(
            child: Center(
              child: Padding(
                padding: const EdgeInsets.all(28),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Image.asset(
                      'assets/images/logo.png',
                      width: 96,
                      height: 96,
                    ),
                    const SizedBox(height: 20),
                    Text('小元宝', style: Theme.of(context).textTheme.titleLarge),
                    const SizedBox(height: 12),
                    if (snapshot.hasError) ...[
                      const Text(
                        '暂时无法打开本地比赛缓存，请重试。已有数据不会被自动清除。',
                        textAlign: TextAlign.center,
                      ),
                      const SizedBox(height: 16),
                      FilledButton(
                        onPressed: () => setState(() => _opening = _open()),
                        child: const Text('重新打开'),
                      ),
                    ] else ...[
                      const Text('正在读取本地比赛'),
                      const SizedBox(height: 18),
                      const CircularProgressIndicator(),
                    ],
                  ],
                ),
              ),
            ),
          ),
        );
      },
    ),
  );
}
