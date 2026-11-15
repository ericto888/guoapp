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
  final Future<void> Function(SeasonReservation reservation, DramaSeason season)? onReservationOnline;
  final int _epoch;
  Timer? _timer;
  bool _running = false;
  bool _disposed = false;
  bool _checkingReservations = false;
  static const _reservationBatchSize = 20;

  bool get _valid =>
      !_disposed && !store.locked && _epoch == store.profileEpoch;

  void start() {
    if (_timer != null || !repository.supportsSourceManagement || !_valid) {
      return;
    }
    WidgetsBinding.instance.addObserver(this);
    unawaited(_checkReservations());
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
    final pending = store.seasonReservations
        .where((item) => item.notifiedAt == 0)
        .toList();
    if (pending.isEmpty || !_valid || _checkingReservations) return;
    _checkingReservations = true;
    final batch = pending.take(_reservationBatchSize).toList();
    try {
    for (final reservation in batch) {
      if (!_valid) return;
      try {
        final seasons = await repository
            .seasons(reservation.drama)
            .timeout(const Duration(seconds: 25));
        final current = seasons.where(
          (season) => season.drama.id == reservation.id,
        ).firstOrNull;
        if (current == null ||
            (current.episodes <= 0 && current.drama.episodes <= 0)) {
          continue;
        }
        final notify = onReservationOnline;
        if (notify == null) continue;
        await notify(reservation, current);
        await store.markSeasonReservationNotified(reservation.id);
      } catch (_) {
        // 网络或系统通知失败时保留未提醒状态，下次启动/轮询继续重试。
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

  void dispose() {
    _disposed = true;
    WidgetsBinding.instance.removeObserver(this);
    _timer?.cancel();
  }
}
