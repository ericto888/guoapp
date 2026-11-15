import 'package:duanju_app/follow_updater.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/ui/widgets/reservation_online_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/fakes.dart';

class _ReservationRepository extends FakeRepository {
  @override
  bool get supportsSourceManagement => true;
}

Drama _seasonDrama({required int episodes, String onlineDate = ''}) => Drama(
  id: 'hongguo:season-2',
  source: 'hongguo',
  title: '预约剧第二季',
  episodes: episodes,
  onlineDate: onlineDate,
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('预约的待上线季上线后弹出一次 App 内提醒', () async {
    final store = await openStore();
    final repository = _ReservationRepository();
    final reservation = SeasonReservation(
      drama: _seasonDrama(episodes: 0, onlineDate: '2026-11-20'),
      season: 2,
      onlineDate: '2026-11-20',
      reservedAt: 1,
    );
    await store.toggleSeasonReservation(reservation);

    var notices = 0;
    final updater = FollowUpdater(
      repository,
      store,
      startDelay: const Duration(days: 1),
      onReservationOnline: (_reservation, _season) async => notices++,
    );
    addTearDown(updater.dispose);

    // 官方季列表仍为待上线（episodes=0）时只同步快照，不提醒。
    repository.seasonLists[reservation.id] = [
      DramaSeason(
        number: 2,
        title: '预约剧第二季',
        drama: _seasonDrama(episodes: 0, onlineDate: '2026-11-20'),
      ),
    ];
    updater.start();
    await pumpEventQueue();
    expect(notices, 0, reason: '待上线季不应触发上线提醒');
    expect(store.seasonReservations.single.notifiedAt, 0);
    expect(store.seasonReservations.single.metadataRefreshedAt, greaterThan(0));

    // 官方返回实际集数：视为已上线，应弹出提醒并记录已提醒时间。
    repository.seasonLists[reservation.id] = [
      DramaSeason(
        number: 2,
        title: '预约剧第二季',
        episodes: 12,
        drama: _seasonDrama(episodes: 12),
      ),
    ];
    updater.didChangeAppLifecycleState(AppLifecycleState.resumed);
    await pumpEventQueue();
    expect(notices, 1);
    expect(store.seasonReservations.single.isLive, isTrue);
    expect(store.seasonReservations.single.notifiedAt, greaterThan(0));

    // 再次回到 App 不会重复提醒。
    updater.didChangeAppLifecycleState(AppLifecycleState.resumed);
    await pumpEventQueue();
    expect(notices, 1, reason: '同一条预约只提醒一次');
  });

  testWidgets('上线提醒弹窗显示预约季信息，稍后再看不跳转', (tester) async {
    final reservation = const SeasonReservation(
      drama: Drama(
        id: '',
        source: 'hongguo',
        title: '预约剧第二季',
        episodes: 0,
      ),
      season: 2,
      reservedAt: 1,
    );
    final season = const DramaSeason(
      number: 2,
      title: '预约剧第二季',
      episodes: 12,
      drama: Drama(
        id: '',
        source: 'hongguo',
        title: '预约剧第二季',
        episodes: 12,
      ),
    );
    bool? watch;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: FilledButton(
                onPressed: () async {
                  watch = await showDialog<bool>(
                    context: context,
                    barrierDismissible: false,
                    builder: (_) => ReservationOnlineDialog(
                      reservation: reservation,
                      season: season,
                    ),
                  );
                },
                child: const Text('打开提醒'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开提醒'));
    await tester.pumpAndSettle();
    expect(find.text('预约作品已上线'), findsOneWidget);
    expect(find.text('预约剧第二季'), findsOneWidget);
    expect(find.text('第2季已上线'), findsOneWidget);
    expect(find.text('你预约的剧集现在可以观看了'), findsOneWidget);

    await tester.tap(find.text('稍后再看'));
    await tester.pumpAndSettle();
    expect(watch, isFalse);
    expect(find.text('预约作品已上线'), findsNothing);
  });

  testWidgets('上线提醒弹窗点击立即观看返回 true', (tester) async {
    final reservation = const SeasonReservation(
      drama: Drama(
        id: '',
        source: 'hongguo',
        title: '预约剧第三季',
        episodes: 0,
      ),
      season: 3,
      unit: '季',
      reservedAt: 1,
    );
    final season = const DramaSeason(
      number: 3,
      title: '预约剧第三季',
      episodes: 8,
      drama: Drama(
        id: '',
        source: 'hongguo',
        title: '预约剧第三季',
        episodes: 8,
      ),
    );
    bool? watch;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: FilledButton(
                onPressed: () async {
                  watch = await showDialog<bool>(
                    context: context,
                    barrierDismissible: false,
                    builder: (_) => ReservationOnlineDialog(
                      reservation: reservation,
                      season: season,
                    ),
                  );
                },
                child: const Text('打开提醒'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开提醒'));
    await tester.pumpAndSettle();
    expect(find.text('第3季已上线'), findsOneWidget);

    await tester.tap(find.text('立即观看'));
    await tester.pumpAndSettle();
    expect(watch, isTrue);
    expect(find.text('预约作品已上线'), findsNothing);
  });
}
