import 'dart:async';

import 'package:duanju_app/drama_details.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/local_store.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/fakes.dart';

class _GatedSeasons extends FakeRepository {
  _GatedSeasons(this.gate);
  final Future<List<DramaSeason>> gate;

  @override
  Future<List<DramaSeason>> seasons(Drama drama) => gate;
}

class _GatedDetail extends FakeRepository {
  _GatedDetail(this.gate);
  final Future<DramaDetail> gate;

  @override
  Future<DramaDetail> detail(Drama drama) {
    detailRequests++;
    return gate;
  }
}

DramaDetails _open(FakeRepository repository, LocalStore store, Drama drama) {
  final details = DramaDetails(repository, store, drama);
  addTearDown(details.dispose);
  return details;
}

void main() {
  final first = drama('a', title: '剧甲');

  test('loads episodes and opens the group of the resume episode', () async {
    final repository = FakeRepository()
      ..details[first.id] = dramaDetail(
        Drama(id: first.id, source: first.source, title: '剧甲', tags: ['甜宠']),
      );
    final store = await openStore();
    await store.saveWatch(watching(first, episode: 35));
    final details = _open(repository, store, first);
    await pumpEventQueue();

    expect(details.loading, isFalse);
    expect(details.drama.tags, ['甜宠']);
    expect(details.episodes, hasLength(80));
    expect(details.groupCount, 3);
    expect(details.group, 1);
    expect(details.groupLabel(2), '61-80');
    expect(details.groupEpisodes.first.number, 31);
    expect(details.currentIndex, 34);
    expect(details.resumeIndex, 34);

    details.selectGroup(2);
    expect(details.groupEpisodes.map((episode) => episode.number).last, 80);
  });

  test('a finished episode resumes from the next one', () async {
    final repository = FakeRepository()
      ..details[first.id] = dramaDetail(first, count: 10);
    final store = await openStore();
    await store.saveWatch(watching(first, episode: 4, position: 100));
    final details = _open(repository, store, first);
    await pumpEventQueue();

    expect(details.currentIndex, 3);
    expect(details.resumeIndex, 4);
    expect(details.groupCount, 1);
  });

  test('failures keep the drama and can be retried', () async {
    final repository = FakeRepository()..detailFailure = '站源超时';
    final details = _open(repository, await openStore(), first);
    await pumpEventQueue();

    expect(details.error, contains('站源超时'));
    expect(details.drama.title, '剧甲');
    expect(details.episodes, isEmpty);

    repository
      ..detailFailure = ''
      ..details[first.id] = dramaDetail(first, count: 3);
    await details.load();
    expect(details.error, isNull);
    expect(details.episodes, hasLength(3));
  });

  test('sources outside the profile are never requested', () async {
    final repository = FakeRepository();
    final store = await openStore(viewerPreferences);
    final details = _open(repository, store, drama('x', source: 'dsd'));
    await pumpEventQueue();

    expect(details.allowed, isFalse);
    expect(repository.detailRequests, 0);
  });

  test('switching season refetches and fills earlier seasons', () async {
    final repository = FakeRepository();
    final store = await openStore();
    final second = drama('s2', title: '剧甲第二季');
    final third = drama('s3', title: '剧甲第三季');
    final firstSeason = drama('s1', title: '剧甲第一季');
    repository.details[first.id] = dramaDetail(first, count: 5);
    repository.details[second.id] = dramaDetail(second, count: 5);
    DramaSeason season(Drama drama, int number) => DramaSeason(
      number: number,
      title: drama.title,
      drama: drama,
      episodes: 5,
    );
    // 从第二季视角一开始只拿到 [2,3]（首季在另一个系列组）。
    repository.seasonLists[first.id] = [season(second, 2), season(third, 3)];
    // 切到第二季后重新请求，能把第 1 季补回来。
    repository.seasonLists[second.id] = [
      season(firstSeason, 1),
      DramaSeason(
        number: 2,
        title: second.title,
        drama: second,
        episodes: 9,
      ),
      season(third, 3),
    ];

    final details = _open(repository, store, first);
    await pumpEventQueue();
    expect(details.seasons.map((item) => item.number), [2, 3]);

    await details.selectSeason(details.seasons.first);
    await pumpEventQueue();

    expect(repository.seasonRequests, [first.id, second.id]);
    expect(details.seasons.map((item) => item.number), [1, 2, 3]);
    expect(
      details.seasons[1].episodes,
      9,
      reason: '同一季刷新后应采用最新官方集数，不能保留待上线时的旧数据',
    );
  });

  test('季列表加载完成（即使只有一季）才标记 seasonsReady', () async {
    final gate = Completer<List<DramaSeason>>();
    final repository = _GatedSeasons(gate.future)
      ..details[first.id] = dramaDetail(first, count: 3);
    final details = _open(repository, await openStore(), first);
    await pumpEventQueue();

    expect(details.seasonsReady, isFalse, reason: '加载中不能被当成「确实没有下一季」');

    gate.complete(const []);
    await pumpEventQueue();
    expect(details.seasonsReady, isTrue);
  });

  test('季列表与剧集详情同时请求，不必等详情返回', () async {
    final gate = Completer<DramaDetail>();
    final second = drama('s2', title: '剧甲第二季');
    final third = drama('s3', title: '剧甲第三季');
    final repository = _GatedDetail(gate.future)
      ..seasonLists[first.id] = [
        DramaSeason(number: 2, title: second.title, drama: second, episodes: 5),
        DramaSeason(number: 3, title: third.title, drama: third, episodes: 5),
      ];
    final details = _open(repository, await openStore(), first);
    await pumpEventQueue();

    expect(repository.detailRequests, 1);
    expect(repository.seasonRequests, [first.id], reason: '详情尚未返回时季列表已在请求');
    expect(details.loading, isTrue);
    expect(details.seasons.map((item) => item.number), [
      2,
      3,
    ], reason: '季列表先到，不必等详情');

    gate.complete(dramaDetail(first, count: 3));
    await pumpEventQueue();
    expect(details.loading, isFalse);
    expect(details.seasonsReady, isTrue);
  });
}
