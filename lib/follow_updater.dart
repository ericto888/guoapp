import 'dart:async';
import 'dart:math';

import 'package:flutter/widgets.dart';

import 'core_bridge.dart';
import 'follow_state.dart';
import 'local_store.dart';
import 'models.dart';

class FollowUpdater with WidgetsBindingObserver {
  FollowUpdater(
    this.repository,
    this.store, {
    this.startDelay = const Duration(seconds: 30),
    this.interval = const Duration(minutes: 30),
    this.recheck = const Duration(hours: 6),
    this.limit = 10,
    DateTime Function()? clock,
    this.onReservationOnline,
  }) : _epoch = store.profileEpoch,
       _clock = clock ?? DateTime.now;

  static const _maxBackoffShift = 3;

  final AppRepository repository;
  final LocalStore store;
  final Duration startDelay;
  final Duration interval;
  final Duration recheck;
  final int limit;
  final DateTime Function() _clock;
  final Future<void> Function(
    SeasonReservation reservation,
    DramaSeason season,
  )?
  onReservationOnline;
  final int _epoch;
  Timer? _timer;
  Timer? _reservationTimer;
  int _reservationOffset = 0;
  bool _running = false;
  bool _disposed = false;
  bool _checkingReservations = false;
  static const _reservationBatchSize = 20;

  bool get _valid =>
      !_disposed && !store.locked && _epoch == store.profileEpoch;

  void start() {
    if (_timer != null || !repository.supportsSourceManagement) return;
    if (!_valid) {
      // 启动时可能仍处于锁定态，之前直接返回会导致整个会话都不再检查，
      // 这里改为等解锁后再启动。
      store.addListener(_startWhenReady);
      return;
    }
    store.removeListener(_startWhenReady);
    WidgetsBinding.instance.addObserver(this);
    unawaited(_checkReservations());
    // 预约检查独立于普通追剧刷新，App 使用期间每 10 分钟核对一次。
    _reservationTimer = Timer.periodic(
      const Duration(minutes: 10),
      (_) => unawaited(_checkReservations()),
    );
    _timer = Timer(startDelay, () {
      _timer = Timer.periodic(interval, (_) => _tick());
      _tick();
    });
  }

  void _tick() => unawaited(run().catchError((Object _) {}));

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _tick();
      unawaited(_checkReservations());
    }
  }

  /// 分批核对预约对应的官方季数据。只按精确 season/book ID 匹配，
  /// 避免同名作品或其他季上线时误报；必须检测到实际集数才发上线提醒。
  Future<void> _checkReservations() async {
    // 已提醒的预约也要继续同步官方元数据，以便修正历史错误日期和集数。
    // 已通知预约只需纠正一次历史快照；尚未通知的预约继续定期核对上线状态。
    final reservations = store.seasonReservations
        .where((item) => item.notifiedAt == 0 || item.metadataRefreshedAt == 0)
        .toList();
    if (reservations.isEmpty || !_valid || _checkingReservations) return;
    _checkingReservations = true;
    // 预约超过单批上限时轮转检查，避免后面的预约长期得不到刷新。
    final start = _reservationOffset % reservations.length;
    final count = min(_reservationBatchSize, reservations.length);
    final first = reservations.skip(start).take(count).toList();
    final batch = [...first, ...reservations.take(count - first.length)];
    _reservationOffset = (start + batch.length) % reservations.length;
    try {
      for (final reservation in batch) {
        if (!_valid) return;
        try {
          final seasons = await repository
              .seasons(reservation.drama)
              .timeout(const Duration(seconds: 25));
          final current = reservation.matchSeason(seasons);
          if (current == null) continue;

          // 先刷新本地快照：覆盖旧日期，并把官方实际集数写回预约。
          await store.refreshSeasonReservation(reservation.id, current);
          if (current.episodes <= 0 && current.drama.episodes <= 0) continue;
          if (reservation.notifiedAt > 0) continue;

          final notify = onReservationOnline;
          if (notify == null) continue;
          await notify(reservation, current);
          await store.markSeasonReservationNotified(reservation.id);
        } catch (_) {
          // 网络请求或弹窗失败时保留未提醒状态，下次打开 App 再检查。
        }
      }
    } finally {
      _checkingReservations = false;
    }
  }

  List<Drama> _due(Map<String, FollowCheck> checks) {
    final now = _clock();
    final never = DateTime.fromMillisecondsSinceEpoch(0);
    final items = store.favorites.where((drama) {
      if (const {'finished', 'completed'}.contains(drama.releaseStatus)) {
        return false;
      }
      final check = checks[drama.id];
      return check == null ||
          !now.isBefore(
            check.checkedAt.add(
              recheck * (1 << min(check.failures, _maxBackoffShift)),
            ),
          );
    }).toList();
    items.sort(
      (a, b) => (checks[a.id]?.checkedAt ?? never).compareTo(
        checks[b.id]?.checkedAt ?? never,
      ),
    );
    return items.take(limit).toList();
  }

  Future<void> run() async {
    if (_running || !_valid) return;
    _running = true;
    try {
      final checks = store.followChecks;
      final updates = <Drama>[];
      for (final drama in _due(checks)) {
        if (!_valid) return;
        try {
          updates.add(await repository.followCheck(drama));
          checks[drama.id] = FollowCheck(_clock());
        } catch (_) {
          checks[drama.id] = FollowCheck(
            _clock(),
            failures: (checks[drama.id]?.failures ?? 0) + 1,
          );
        }
      }
      if (!_valid) return;
      if (updates.isNotEmpty) await store.refreshDramas(updates);
      await store.saveFollowChecks(checks, epoch: _epoch);
    } finally {
      _running = false;
    }
  }

  void _startWhenReady() {
    if (_disposed || _timer != null || !_valid) return;
    start();
  }

  void dispose() {
    _disposed = true;
    store.removeListener(_startWhenReady);
    WidgetsBinding.instance.removeObserver(this);
    _timer?.cancel();
    _reservationTimer?.cancel();
  }
}
