import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';

import '../../app_device.dart';
import '../../local_store.dart';
import '../../picture_in_picture.dart';
import '../../playback_session.dart';
import '../app_scope.dart';
import 'landscape_player_page.dart';
import 'mini_player.dart';
import 'playback_handle.dart';
import 'player_page.dart';
import 'portrait_player_page.dart';
import 'television_player_page.dart';
import 'window_fullscreen.dart';

class PlaybackPage extends StatefulWidget {
  const PlaybackPage({super.key, required this.store, required this.handle});
  final LocalStore store;
  final PlaybackHandle handle;

  @override
  State<PlaybackPage> createState() => _PlaybackPageState();
}

/// 页面布局是否按横屏渲染。
///
/// 与新视频首帧尺寸确认状态分离：切集时会话会作废上一集的 aspect 证据，
/// 此时不能把「尺寸未知」误判成「竖屏」——否则横屏切下一集会先退回竖屏、
/// 等新首帧到达再切回横屏。尺寸未知期间沿用当前布局；新尺寸到达
/// （videoParams）后再按实际宽高比判断是否真的需要换布局。
@visibleForTesting
bool playbackLayoutIsWide({
  required bool aspectKnown,
  required double aspectRatio,
  required bool currentWide,
}) => aspectKnown ? aspectRatio >= 1 : currentWide;

class _PlaybackPageState extends State<PlaybackPage> {
  final _padFullscreen = ValueNotifier(false);
  final _pageScope = FocusScopeNode(debugLabel: 'playbackPage');
  final _pip = PictureInPicture.instance;
  bool _wideVideo = false;

  /// 进入本页时定格的布局偏好：播放中改设置不重建当前页面，下次进入生效。
  late final bool _landscapePlayerForWide;
  MediaQueryData? _media;
  bool _shown = false;
  Player? _bound;
  StreamSubscription<bool>? _playing;

  PlaybackSession get _session => widget.handle.session;

  /// 当前页面布局是否按横屏渲染。
  bool _measure() => playbackLayoutIsWide(
    aspectKnown: _session.aspectKnown,
    aspectRatio: _session.aspectRatio,
    currentWide: _wideVideo,
  );

  bool get _television =>
      AppScope.formFactorOf(context) == FormFactor.television;

  @override
  void initState() {
    super.initState();
    _wideVideo = _session.aspectKnown && _session.aspectRatio >= 1;
    _landscapePlayerForWide =
        widget.store.playbackPreferences.landscapePlayerForWide;
    _session.addListener(_changed);
    _padFullscreen.addListener(_padFullscreenChanged);
    _pip.addListener(_pipChanged);
    _pip.onAction = _action;
    _bind();
    WidgetsBinding.instance.addPostFrameCallback((_) => _sync());
  }

  @override
  void dispose() {
    _session.removeListener(_changed);
    _pip.removeListener(_pipChanged);
    if (_pip.onAction == _action) _pip.onAction = null;
    _pip.clear();
    if (_pip.window) unawaited(_restore());
    unawaited(_playing?.cancel());
    _pageScope.dispose();
    if (_padFullscreen.value) _applyPadFullscreen(false);
    _padFullscreen.dispose();
    unawaited(AppDevice.resetBrightness());
    widget.handle.dispose();
    super.dispose();
  }

  void _changed() {
    final previous = _bound;
    _bind();
    _sync();
    final wide = _measure();
    final swapped = !identical(previous, _bound);
    if (wide != _wideVideo || swapped) setState(() => _wideVideo = wide);
  }

  void _bind() {
    final player = _session.player;
    if (identical(player, _bound)) return;
    _bound = player;
    unawaited(_playing?.cancel());
    _playing = player.stream.playing.listen((_) => _sync());
  }

  void _sync() {
    if (!mounted) return;
    final session = _session;
    final playing = session.player.state.playing;
    _pip.update(
      aspectRatio: session.aspectRatio,
      playing: playing,
      hasPrevious: session.hasPrevious,
      hasNext: session.hasNext,
      autoEnter:
          playing &&
          !_television &&
          widget.store.playbackPreferences.autoPictureInPicture,
    );
    _pip.resize(session.aspectRatio);
  }

  void _pipChanged() {
    if (!mounted) return;
    final shown = _pip.shown;
    if (shown && !_shown) _closeRoutesAbove();
    if (!shown && _shown) _pageScope.requestFocus();
    _shown = shown;
    if (!_television) {
      // PiP 状态交给 session 统一持有；observer 只上报原始生命周期，
      // 不再由页面各自拼装「是否前台」的布尔口径。
      _session.setPictureInPicture(shown);
    }
    _sync();
    setState(() {});
  }

  void _closeRoutesAbove() {
    final route = ModalRoute.of(context);
    if (route == null || route.isCurrent) return;
    Navigator.of(context).popUntil((other) => other == route);
  }

  void _action(String action) {
    switch (action) {
      case 'toggle':
        _session.togglePlayback();
      case 'previous':
        unawaited(_session.previous());
      case 'next':
        unawaited(_session.next());
    }
  }

  Future<void> _enterMini() async {
    _closeRoutesAbove();
    await _pip.enter(_session.aspectRatio, frame: widget.store.miniWindow);
  }

  Future<void> _restore() async {
    final frame = await _pip.exit();
    if (frame != null) {
      unawaited(widget.store.setMiniWindow(frame).catchError((Object _) {}));
    }
  }

  Future<void> _close() async {
    await _restore();
    if (mounted) Navigator.pop(context);
  }

  void _padFullscreenChanged() {
    _applyPadFullscreen(_padFullscreen.value);
    setState(() {});
  }

  void _applyPadFullscreen(bool full) {
    if (desktopPlatform) {
      unawaited(setWindowFullScreen(full));
      return;
    }
    unawaited(
      AppDevice.setPlayerOrientation(
        full ? PlayerOrientation.lockedLandscape : PlayerOrientation.auto,
      ),
    );
    unawaited(
      SystemChrome.setEnabledSystemUIMode(
        full ? SystemUiMode.immersiveSticky : SystemUiMode.edgeToEdge,
      ).catchError((Object _) {}),
    );
  }

  @override
  Widget build(BuildContext context) {
    final live = MediaQuery.of(context);
    final shown = _pip.shown || _pip.leaving && live.size != _media?.size;
    if (!shown || _media == null) _media = live;
    return Stack(
      fit: StackFit.expand,
      children: [
        FocusScope(
          node: _pageScope,
          child: Offstage(
            offstage: shown,
            child: MediaQuery(
              data: _media!,
              child: Builder(builder: _page),
            ),
          ),
        ),
        if (shown)
          _pip.window
              ? MiniWindowView(
                  handle: widget.handle,
                  onRestore: () => unawaited(_restore()),
                  onClose: () => unawaited(_close()),
                )
              : PictureInPictureView(handle: widget.handle),
      ],
    );
  }

  Widget _page(BuildContext context) {
    final form = AppScope.formFactorOf(context);
    final mini = _pip.available ? () => unawaited(_enterMini()) : null;
    if (desktopPlatform) {
      final size = MediaQuery.sizeOf(context);
      return switch (form) {
        FormFactor.phone when _wideVideo => LandscapePlayerPage(
          store: widget.store,
          handle: widget.handle,
          onMiniWindow: mini,
        ),
        FormFactor.phone => PortraitPlayerPage(
          store: widget.store,
          handle: widget.handle,
          onMiniWindow: mini,
        ),
        FormFactor.pad => _padPage(context, mini),
        FormFactor.window
            when _padFullscreen.value || size.width <= size.height =>
          _padPage(context, mini),
        FormFactor.window => PlayerPage(
          store: widget.store,
          handle: widget.handle,
          onMiniWindow: mini,
        ),
        FormFactor.television => TelevisionPlayerPage(handle: widget.handle),
      };
    }
    return switch (form) {
      FormFactor.pad || FormFactor.window => _padPage(context, mini),
      FormFactor.phone
          when _wideVideo &&
              (_landscapePlayerForWide ||
                  MediaQuery.orientationOf(context) == Orientation.landscape) =>
        LandscapePlayerPage(
          store: widget.store,
          handle: widget.handle,
          onMiniWindow: mini,
        ),
      FormFactor.phone => PortraitPlayerPage(
        store: widget.store,
        handle: widget.handle,
        onMiniWindow: mini,
      ),
      FormFactor.television => TelevisionPlayerPage(handle: widget.handle),
    };
  }

  Widget _padPage(BuildContext context, VoidCallback? mini) {
    final size = MediaQuery.sizeOf(context);
    if (_padFullscreen.value || size.width > size.height) {
      return PlayerPage(
        store: widget.store,
        handle: widget.handle,
        padFullscreen: _padFullscreen,
        onMiniWindow: mini,
      );
    }
    return _wideVideo
        ? LandscapePlayerPage(
            store: widget.store,
            handle: widget.handle,
            onFullscreen: () => _padFullscreen.value = true,
            onMiniWindow: mini,
          )
        : PortraitPlayerPage(
            store: widget.store,
            handle: widget.handle,
            onMiniWindow: mini,
          );
  }
}
