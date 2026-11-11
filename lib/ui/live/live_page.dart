import 'dart:async';

import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../live/live_models.dart';
import '../../live/live_repository.dart';
import '../../live/live_sources.dart';
import '../../live/live_store.dart';
import '../../local_store.dart';
import 'live_player_page.dart';

/// 直播：按分类浏览频道，播放页支持换台、切线路与收藏。
class LivePage extends StatefulWidget {
  const LivePage({super.key, required this.store, this.greenMode = false});
  final LocalStore store;

  /// 绿色模式只显示公开电视直播分类；直播面板以秀场为主，默认关闭。
  final bool greenMode;

  @override
  State<LivePage> createState() => _LivePageState();
}

class _LivePageState extends State<LivePage> {
  static const favouritesId = '__favourites';
  static const recentId = '__recent';

  final LiveRepository _repository = LiveRepository();
  LiveStore? _liveStore;
  LiveSource? _source;
  List<LivePlatform> _platforms = const [];
  List<LiveChannel> _channels = const [];
  String _selected = '';
  bool _loading = true;
  String _error = '';

  @override
  void initState() {
    super.initState();
    unawaited(_bootstrap());
  }

  @override
  void dispose() {
    _repository.dispose();
    _liveStore?.dispose();
    super.dispose();
  }

  Future<void> _bootstrap() async {
    final preferences = await SharedPreferences.getInstance();
    final store = LiveStore(preferences, widget.store.profile.id)..load();
    if (!mounted) {
      store.dispose();
      return;
    }
    setState(() => _liveStore = store);
    final visible = LiveSource.visible(widget.greenMode);
    if (visible.isEmpty) {
      setState(() {
        _loading = false;
        _error = '当前模式没有可用的直播源';
      });
      return;
    }
    _source = visible.first;
    await _loadPlatforms();
  }

  Future<void> _loadPlatforms() async {
    final source = _source;
    if (source == null) return;
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final platforms = await _repository.platforms(source, force: true);
      if (!mounted) return;
      final visible = [
        for (final platform in platforms)
          if (!LiveSource.hidesCategory(
            platform.name,
            greenMode: widget.greenMode,
          ))
            platform,
      ];
      setState(() {
        _platforms = visible;
        _loading = false;
      });
      if (visible.isEmpty) {
        setState(() => _error = '该直播源没有可用分类');
        return;
      }
      final first = _platforms.any((item) => item.id == _selected)
          ? _selected
          : visible.first.id;
      await _loadChannels(first);
    } catch (failure) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = failure.toString();
      });
    }
  }

  Future<void> _loadChannels(String id) async {
    setState(() {
      _selected = id;
      _loading = true;
      _error = '';
    });
    try {
      final List<LiveChannel> channels;
      if (id == favouritesId) {
        channels = _liveStore?.favourites ?? const [];
      } else if (id == recentId) {
        channels = _liveStore?.recent ?? const [];
      } else {
        final source = _source;
        final platform = _platforms.firstWhere(
          (item) => item.id == id,
          orElse: () => _platforms.first,
        );
        channels = source == null
            ? const []
            : await _repository.channels(source, platform);
      }
      if (!mounted) return;
      setState(() {
        _channels = channels;
        _loading = false;
        if (channels.isEmpty) {
          _error = id == favouritesId ? '还没有收藏频道，播放时点星标加入收藏' : '该分类暂时没有频道';
        }
      });
    } catch (failure) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _channels = const [];
        _error = failure.toString();
      });
    }
  }

  void _openChannel(int index) {
    final source = _source;
    if (source == null || _channels.isEmpty) return;
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => LivePlayerPage(
          repository: _repository,
          store: _liveStore!,
          source: source,
          channels: _channels,
          index: index,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('直播'),
        actions: [
          IconButton(
            tooltip: '刷新',
            onPressed: _loading ? null : _loadPlatforms,
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: Column(
        children: [
          SizedBox(
            height: 56,
            child: ListView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(horizontal: 12),
              children: [
                _chip('收藏', favouritesId),
                _chip('最近', recentId),
                for (final platform in _platforms)
                  _chip(
                    platform.count > 0
                        ? '${platform.name}（${platform.count}）'
                        : platform.name,
                    platform.id,
                  ),
              ],
            ),
          ),
          Expanded(child: _body(theme)),
        ],
      ),
    );
  }

  Widget _chip(String label, String id) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 8),
    child: ChoiceChip(
      label: Text(label),
      selected: _selected == id,
      onSelected: (_) => _loadChannels(id),
    ),
  );

  Widget _body(ThemeData theme) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_channels.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                Icons.live_tv_outlined,
                size: 48,
                color: theme.colorScheme.onSurfaceVariant,
              ),
              const SizedBox(height: 12),
              Text(
                _error.isEmpty ? '没有可用频道' : _error,
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 16),
              FilledButton(onPressed: _loadPlatforms, child: const Text('重试')),
            ],
          ),
        ),
      );
    }
    return LayoutBuilder(
      builder: (context, constraints) {
        final columns = (constraints.maxWidth / 220).floor().clamp(2, 8);
        return GridView.builder(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
          gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: columns,
            mainAxisExtent: 64,
            crossAxisSpacing: 10,
            mainAxisSpacing: 10,
          ),
          itemCount: _channels.length,
          itemBuilder: (context, index) => _tile(_channels[index], index),
        );
      },
    );
  }

  Widget _tile(LiveChannel channel, int index) {
    final store = _liveStore;
    final favourite = store?.isFavourite(channel.key) ?? false;
    return Material(
      color: Theme.of(context).colorScheme.surfaceContainerHighest,
      borderRadius: BorderRadius.circular(12),
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: () => _openChannel(index),
        child: Padding(
          padding: const EdgeInsets.only(left: 12, right: 4),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.center,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      channel.label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontWeight: FontWeight.w600),
                    ),
                    if (channel.group.isNotEmpty)
                      Text(
                        channel.group,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                  ],
                ),
              ),
              if (store != null)
                IconButton(
                  tooltip: favourite ? '取消收藏' : '收藏',
                  onPressed: () async {
                    await store.toggleFavourite(channel);
                    if (mounted) setState(() {});
                  },
                  icon: Icon(
                    favourite ? Icons.star_rounded : Icons.star_border_rounded,
                    color: favourite
                        ? Theme.of(context).colorScheme.primary
                        : null,
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
