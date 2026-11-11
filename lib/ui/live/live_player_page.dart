import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit_video/media_kit_video.dart';

import '../../live/live_models.dart';
import '../../live/live_playback.dart';
import '../../live/live_repository.dart';
import '../../live/live_sources.dart';
import '../../live/live_store.dart';

/// 直播播放页：内嵌播放器 + 换台 / 切线路 / 收藏 / 横屏。
class LivePlayerPage extends StatefulWidget {
  const LivePlayerPage({
    super.key,
    required this.repository,
    required this.store,
    required this.source,
    required this.channels,
    required this.index,
  });
  final LiveRepository repository;
  final LiveStore store;
  final LiveSource source;
  final List<LiveChannel> channels;
  final int index;

  @override
  State<LivePlayerPage> createState() => _LivePlayerPageState();
}

class _LivePlayerPageState extends State<LivePlayerPage> {
  late final LivePlaybackController _playback;
  bool _overlay = true;
  bool _landscape = false;

  @override
  void initState() {
    super.initState();
    _playback = LivePlaybackController(repository: widget.repository)
      ..addListener(_changed);
    unawaited(
      _playback.load(widget.source, widget.channels, index: widget.index),
    );
    unawaited(widget.store.remember(widget.channels[widget.index]));
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    _playback
      ..removeListener(_changed)
      ..dispose();
    if (_landscape) {
      unawaited(_restoreOrientation());
    }
    super.dispose();
  }

  Future<void> _restoreOrientation() => SystemChrome.setPreferredOrientations(
    const [DeviceOrientation.portraitUp],
  );

  Future<void> _toggleOrientation() async {
    final next = !_landscape;
    setState(() => _landscape = next);
    if (next) {
      await SystemChrome.setPreferredOrientations(const [
        DeviceOrientation.landscapeLeft,
        DeviceOrientation.landscapeRight,
      ]);
    } else {
      await _restoreOrientation();
    }
  }

  void _switchBy(int delta) {
    _playback.switchBy(delta);
    final channel = _playback.channel;
    if (channel != null) unawaited(widget.store.remember(channel));
  }

  Future<void> _toggleFavourite() async {
    final channel = _playback.channel;
    if (channel == null) return;
    await widget.store.toggleFavourite(channel);
    if (mounted) setState(() {});
  }

  Future<void> _showRoutes() async {
    final channel = _playback.channel;
    if (channel == null || channel.urls.length < 2) return;
    final selected = await showModalBottomSheet<int>(
      context: context,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Padding(padding: EdgeInsets.all(16), child: Text('切换线路')),
            for (var index = 0; index < channel.urls.length; index++)
              ListTile(
                leading: Icon(
                  index == _playback.route
                      ? Icons.radio_button_checked
                      : Icons.radio_button_unchecked,
                ),
                title: Text('线路 ${index + 1}'),
                subtitle: Text(
                  channel.urls[index],
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                onTap: () => Navigator.pop(context, index),
              ),
          ],
        ),
      ),
    );
    if (selected != null) await _playback.playRoute(selected);
  }

  @override
  Widget build(BuildContext context) {
    final controller = _playback.controller;
    final channel = _playback.channel;
    final favourite = channel != null && widget.store.isFavourite(channel.key);
    return Scaffold(
      backgroundColor: Colors.black,
      body: Stack(
        fit: StackFit.expand,
        children: [
          if (controller == null)
            const Center(child: CircularProgressIndicator())
          else
            Center(
              child: AspectRatio(
                aspectRatio: _playback.aspectRatio,
                child: Video(
                  controller: controller,
                  controls: NoVideoControls,
                  fit: BoxFit.contain,
                ),
              ),
            ),
          GestureDetector(
            behavior: HitTestBehavior.opaque,
            onTap: () => setState(() => _overlay = !_overlay),
          ),
          Positioned.fill(
            child: IgnorePointer(
              ignoring: !_overlay,
              child: AnimatedOpacity(
                opacity: _overlay ? 1 : 0,
                duration: const Duration(milliseconds: 180),
                child: Container(
                  decoration: const BoxDecoration(
                    gradient: LinearGradient(
                      begin: Alignment.topCenter,
                      end: Alignment.bottomCenter,
                      colors: [
                        Colors.black54,
                        Colors.transparent,
                        Colors.black87,
                      ],
                      stops: [0, 0.35, 1],
                    ),
                  ),
                  child: SafeArea(
                    child: Column(
                      children: [
                        _topBar(channel, favourite),
                        const Spacer(),
                        if (_playback.error != null)
                          _errorPanel(_playback.error!)
                        else if (_playback.loading || _playback.buffering)
                          const Padding(
                            padding: EdgeInsets.only(bottom: 24),
                            child: CircularProgressIndicator(
                              color: Colors.white,
                            ),
                          ),
                        _bottomBar(channel),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _topBar(LiveChannel? channel, bool favourite) => Row(
    children: [
      IconButton(
        tooltip: '返回',
        color: Colors.white,
        onPressed: () => Navigator.of(context).maybePop(),
        icon: const Icon(Icons.arrow_back),
      ),
      Expanded(
        child: Text(
          channel?.name ?? '直播',
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: const TextStyle(
            color: Colors.white,
            fontSize: 16,
            fontWeight: FontWeight.w600,
          ),
        ),
      ),
      if (channel != null)
        IconButton(
          tooltip: favourite ? '取消收藏' : '收藏',
          onPressed: _toggleFavourite,
          icon: Icon(
            favourite ? Icons.star_rounded : Icons.star_border_rounded,
            color: favourite ? Colors.amber : Colors.white,
          ),
        ),
      IconButton(
        tooltip: _landscape ? '退出横屏' : '横屏',
        color: Colors.white,
        onPressed: _toggleOrientation,
        icon: Icon(_landscape ? Icons.crop_portrait : Icons.screen_rotation),
      ),
    ],
  );

  Widget _errorPanel(String message) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          message,
          textAlign: TextAlign.center,
          style: const TextStyle(color: Colors.white),
        ),
        const SizedBox(height: 12),
        FilledButton(onPressed: _playback.retry, child: const Text('重试')),
      ],
    ),
  );

  Widget _bottomBar(LiveChannel? channel) => Padding(
    padding: const EdgeInsets.fromLTRB(8, 0, 8, 8),
    child: Row(
      children: [
        IconButton(
          tooltip: '上一台',
          color: Colors.white,
          onPressed: () => _switchBy(-1),
          icon: const Icon(Icons.skip_previous),
        ),
        IconButton(
          tooltip: _playback.player?.state.playing == true ? '暂停' : '播放',
          color: Colors.white,
          onPressed: () async {
            final player = _playback.player;
            if (player == null) return;
            if (player.state.playing) {
              await player.pause();
            } else {
              await player.play();
            }
            if (mounted) setState(() {});
          },
          icon: Icon(
            _playback.player?.state.playing == true
                ? Icons.pause
                : Icons.play_arrow,
          ),
        ),
        IconButton(
          tooltip: '下一台',
          color: Colors.white,
          onPressed: () => _switchBy(1),
          icon: const Icon(Icons.skip_next),
        ),
        if (channel != null)
          Padding(
            padding: const EdgeInsets.only(left: 8),
            child: Text(
              '${_playback.index + 1}/${_playback.count}',
              style: const TextStyle(color: Colors.white70),
            ),
          ),
        const Spacer(),
        if ((channel?.urls.length ?? 0) > 1)
          TextButton.icon(
            onPressed: _showRoutes,
            icon: const Icon(Icons.alt_route, color: Colors.white),
            label: Text(
              '线路 ${_playback.route + 1}',
              style: const TextStyle(color: Colors.white),
            ),
          ),
      ],
    ),
  );
}
