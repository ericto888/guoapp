import 'package:duanju_app/follow_list.dart';
import 'package:duanju_app/follow_updater.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/fakes.dart';

class _ReservationRepository extends FakeRepository {
  @override
  bool get supportsSourceManagement => true;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('sources list only those with follows, in site order', () {
    final follows = [drama('a'), drama('b'), drama('c', source: 'huangdou')];
    expect(followedSources(follows).map((site) => site.id), [
      'hongguo',
      if (SourceSite.isAvailable('huangdou')) 'huangdou',
    ]);
    expect(followedSources(const []), isEmpty);
  });

  test('titles match ignoring case, width and punctuation', () {
    final item = drama('a', title: '霸道总裁 · Love');
    expect(matchesFollowTitle(item, ''), isTrue);
    expect(matchesFollowTitle(item, '总裁love'), isTrue);
    expect(matchesFollowTitle(item, 'ＬＯＶＥ'), isTrue);
    expect(matchesFollowTitle(item, '王爷'), isFalse);
  });

  test('titles match pinyin initials', () {
    final item = drama('a', title: '霸道总裁 · Love');
    expect(matchesFollowTitle(item, 'BDZC'), isTrue);
    expect(matchesFollowTitle(item, 'zclove'), isTrue);
    expect(matchesFollowTitle(item, 'wy'), isFalse);
  });

  test('refreshes an old reservation and notifies when real episodes appear', () async {
    final store = await openStore();
    final repository = _ReservationRepository();
    final reservedDrama = Drama(
      id: 'hongguo:reserved-season',
      source: 'hongguo',
      title: '预约剧第二季',
      onlineDate: '2026-11-15',
    );
    final reservation = SeasonReservation(
      drama: reservedDrama,
      season: 2,
      onlineDate: '2026-11-15',
      reservedAt: 1,
    );
    await store.toggleSeasonReservation(reservation);

    final liveDrama = Drama(
      id: reservedDrama.id,
      source: 'hongguo',
      title: reservedDrama.title,
      episodes: 12,
    );
    repository.seasonLists[reservedDrama.id] = [
      DramaSeason(
        number: 2,
        title: liveDrama.title,
        drama: liveDrama,
        episodes: 12,
      ),
    ];

    var notified = false;
    final updater = FollowUpdater(
      repository,
      store,
      startDelay: const Duration(days: 1),
      onReservationOnline: (_, _) async {
        notified = true;
      },
    );
    addTearDown(updater.dispose);
    updater.start();
    await pumpEventQueue();

    final refreshed = store.seasonReservations.single;
    expect(refreshed.onlineDate, isEmpty, reason: '官方已不再提供旧日期时应清除错误日期');
    expect(refreshed.drama.episodes, 12);
    expect(refreshed.isLive, isTrue);
    expect(refreshed.notifiedAt, greaterThan(0));
    expect(refreshed.metadataRefreshedAt, greaterThan(0));
    expect(notified, isTrue);
  });

  test('the update check covers every unfinished follow', () async {
    final store = await openStore();
    final repository = FakeRepository();
    final ongoing = drama('a');
    final finished = Drama(
      id: 'hongguo:b',
      source: 'hongguo',
      title: 'b',
      episodes: 80,
      releaseStatus: 'finished',
    );
    for (final item in [ongoing, finished]) {
      await store.toggleFavorite(item);
      repository.details[item.id] = dramaDetail(
        Drama(
          id: item.id,
          source: 'hongguo',
          title: item.title,
          episodes: 90,
          releaseStatus: item.releaseStatus,
        ),
      );
    }

    final updater = FollowUpdater(repository, store);
    addTearDown(updater.dispose);
    await updater.run();
    expect(repository.detailRequests, 1);
    expect(store.following(ongoing.id)?.newEpisodes, 10);
    expect(store.following(finished.id)?.hasUpdates, isFalse);
  });
}
