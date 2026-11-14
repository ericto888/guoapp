import 'dart:async';

import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../football_models.dart';
import '../app_scope.dart';

enum _FootballMatchFilter { all, live, upcoming, finished }

/// 足球赛事中心首页。数据通过 Go 核心获取，不依赖影视剧库。
class FootballHomePage extends StatefulWidget {
  const FootballHomePage({super.key});

  @override
  State<FootballHomePage> createState() => _FootballHomePageState();
}

class _FootballHomePageState extends State<FootballHomePage> {
  late Future<List<FootballMatch>> _matches;
  var _loaded = false;
  _FootballMatchFilter _filter = _FootballMatchFilter.all;
  final Map<String, Future<List<FootballLiveChannel>>> _channelLoads = {};
  Timer? _refreshTimer;

  @override
  void initState() {
    super.initState();
    _refreshTimer = Timer.periodic(const Duration(seconds: 20), (_) {
      if (!mounted) return;
      setState(() => _matches = _load(force: true));
    });
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    super.dispose();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (!_loaded) {
      _loaded = true;
      _matches = _load();
    }
  }

  Future<List<FootballMatch>> _load({bool force = false}) =>
      AppScope.of(context).repository.footballMatches(force: force);

  Future<void> _refresh() async {
    final next = _load(force: true);
    setState(() => _matches = next);
    await next;
  }

  void _loadChannels(String matchId, {bool retry = false}) {
    if (!retry && _channelLoads.containsKey(matchId)) return;
    setState(() {
      _channelLoads[matchId] = AppScope.of(
        context,
      ).repository.footballMatchChannels(matchId);
    });
  }

  Widget _filterChip(String label, _FootballMatchFilter filter, int count) {
    return ChoiceChip(
      label: Text('$label $count'),
      selected: _filter == filter,
      onSelected: (_) => setState(() => _filter = filter),
      showCheckmark: false,
    );
  }

  String _matchStatusLabel(String status) => switch (status) {
    'live' => '直播中',
    'halftime' => '中场',
    'finished' => '已结束',
    'postponed' => '延期',
    'cancelled' => '已取消',
    'unknown' => '状态待确认',
    _ => '未开始',
  };

  Widget _channelTile(BuildContext context, FootballLiveChannel channel) {
    return ListTile(
      dense: true,
      leading: const Icon(Icons.play_circle_outline),
      title: Text(channel.name),
      subtitle: const Text('打开第三方直播入口'),
      onTap: () async {
        final uri = Uri.tryParse(channel.url);
        if (uri == null || !uri.isScheme('https') || !await canLaunchUrl(uri)) {
          if (context.mounted) {
            ScaffoldMessenger.of(
              context,
            ).showSnackBar(const SnackBar(content: Text('直播入口暂时无法打开')));
          }
          return;
        }
        await launchUrl(uri, mode: LaunchMode.externalApplication);
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('足球赛事'),
        actions: [
          IconButton(
            tooltip: '刷新赛事',
            onPressed: _refresh,
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _refresh,
        child: FutureBuilder<List<FootballMatch>>(
          future: _matches,
          builder: (context, snapshot) {
            if (snapshot.connectionState == ConnectionState.waiting &&
                !snapshot.hasData) {
              return const Center(child: CircularProgressIndicator());
            }
            if (snapshot.hasError) {
              return ListView(
                children: [
                  const SizedBox(height: 140),
                  Icon(
                    Icons.cloud_off_outlined,
                    size: 42,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(height: 12),
                  Center(
                    child: Text(
                      snapshot.error.toString(),
                      textAlign: TextAlign.center,
                    ),
                  ),
                  Center(
                    child: TextButton(
                      onPressed: _refresh,
                      child: const Text('重试'),
                    ),
                  ),
                ],
              );
            }
            final matches = snapshot.data ?? const <FootballMatch>[];
            if (matches.isEmpty) {
              return ListView(
                physics: const AlwaysScrollableScrollPhysics(),
                children: [
                  const SizedBox(height: 100),
                  Icon(
                    Icons.sports_soccer,
                    size: 56,
                    color: theme.colorScheme.primary,
                  ),
                  const SizedBox(height: 18),
                  const Center(child: Text('足球赛事中心已就绪')),
                  const SizedBox(height: 8),
                  Center(
                    child: Text(
                      '当前暂无可展示赛事，请稍后刷新',
                      style: theme.textTheme.bodyMedium?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                ],
              );
            }
            final liveCount = matches.where((m) => m.isLive).length;
            final upcomingCount = matches
                .where((m) => m.status == 'scheduled')
                .length;
            final finishedCount = matches
                .where((m) => m.status == 'finished')
                .length;
            final visibleMatches = matches.where((match) {
              return switch (_filter) {
                _FootballMatchFilter.all => true,
                _FootballMatchFilter.live => match.isLive,
                _FootballMatchFilter.upcoming => match.status == 'scheduled',
                _FootballMatchFilter.finished => match.status == 'finished',
              };
            }).toList();

            return ListView.separated(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.all(16),
              itemCount: visibleMatches.isEmpty ? 2 : visibleMatches.length + 1,
              separatorBuilder: (_, _) => const SizedBox(height: 10),
              itemBuilder: (context, index) {
                if (index == 0) {
                  return SingleChildScrollView(
                    scrollDirection: Axis.horizontal,
                    child: Row(
                      children: [
                        _filterChip(
                          '全部',
                          _FootballMatchFilter.all,
                          matches.length,
                        ),
                        const SizedBox(width: 8),
                        _filterChip(
                          '直播中',
                          _FootballMatchFilter.live,
                          liveCount,
                        ),
                        const SizedBox(width: 8),
                        _filterChip(
                          '未开始',
                          _FootballMatchFilter.upcoming,
                          upcomingCount,
                        ),
                        const SizedBox(width: 8),
                        _filterChip(
                          '已结束',
                          _FootballMatchFilter.finished,
                          finishedCount,
                        ),
                      ],
                    ),
                  );
                }
                if (visibleMatches.isEmpty) {
                  return Padding(
                    padding: const EdgeInsets.symmetric(vertical: 56),
                    child: Center(
                      child: Text(
                        '当前分类暂无赛事',
                        style: theme.textTheme.bodyMedium?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                  );
                }
                final match = visibleMatches[index - 1];
                return Card(
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Row(
                          children: [
                            Expanded(
                              child: Text(
                                match.league.isEmpty ? '其他赛事' : match.league,
                                style: theme.textTheme.labelMedium,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                            const SizedBox(width: 8),
                            Text(
                              _matchStatusLabel(match.status),
                              style: theme.textTheme.labelSmall?.copyWith(
                                color: match.isLive
                                    ? theme.colorScheme.error
                                    : theme.colorScheme.onSurfaceVariant,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                          ],
                        ),
                        const SizedBox(height: 12),
                        Row(
                          children: [
                            Expanded(
                              child: Text(
                                match.homeTeam,
                                textAlign: TextAlign.center,
                              ),
                            ),
                            Text(
                              match.homeScore == null || match.awayScore == null
                                  ? 'VS'
                                  : '${match.homeScore} : ${match.awayScore}',
                              style: theme.textTheme.titleLarge,
                            ),
                            Expanded(
                              child: Text(
                                match.awayTeam,
                                textAlign: TextAlign.center,
                              ),
                            ),
                          ],
                        ),
                        const SizedBox(height: 8),
                        Text(
                          match.kickoff?.toLocal().toString() ?? '开赛时间待定',
                          textAlign: TextAlign.center,
                          style: theme.textTheme.bodySmall,
                        ),
                        if (match.matchTime.isNotEmpty)
                          Text(match.matchTime, textAlign: TextAlign.center,
                            style: theme.textTheme.labelMedium?.copyWith(color: theme.colorScheme.error)),
                        if (match.halfTimeHomeScore != null && match.halfTimeAwayScore != null)
                          Text('半场 ${match.halfTimeHomeScore}:${match.halfTimeAwayScore}', textAlign: TextAlign.center, style: theme.textTheme.bodySmall),
                        if (match.cornerHome != null && match.cornerAway != null)
                          Text('角球 ${match.cornerHome}:${match.cornerAway}', textAlign: TextAlign.center, style: theme.textTheme.bodySmall),
                        if (match.detailUrl.isNotEmpty ||
                            match.channels.isNotEmpty) ...[
                          const SizedBox(height: 8),
                          ExpansionTile(
                            tilePadding: EdgeInsets.zero,
                            dense: true,
                            title: Text(
                              match.channels.isNotEmpty
                                  ? '直播频道（${match.channels.length}）'
                                  : '查看直播频道',
                            ),
                            onExpansionChanged: (expanded) {
                              if (expanded && match.channels.isEmpty) {
                                _loadChannels(match.id);
                              }
                            },
                            children: [
                              if (match.channels.isNotEmpty)
                                for (final channel in match.channels)
                                  _channelTile(context, channel)
                              else
                                FutureBuilder<List<FootballLiveChannel>>(
                                  future: _channelLoads[match.id],
                                  builder: (context, channelSnapshot) {
                                    if (channelSnapshot.connectionState ==
                                        ConnectionState.waiting) {
                                      return const Padding(
                                        padding: EdgeInsets.all(16),
                                        child: Center(
                                          child: CircularProgressIndicator(),
                                        ),
                                      );
                                    }
                                    if (channelSnapshot.hasError) {
                                      return TextButton.icon(
                                        onPressed: () => _loadChannels(
                                          match.id,
                                          retry: true,
                                        ),
                                        icon: const Icon(Icons.refresh),
                                        label: const Text('频道加载失败，点击重试'),
                                      );
                                    }
                                    final channels =
                                        channelSnapshot.data ??
                                        const <FootballLiveChannel>[];
                                    if (channels.isEmpty) {
                                      return TextButton.icon(
                                        onPressed: () => _loadChannels(
                                          match.id,
                                          retry: true,
                                        ),
                                        icon: const Icon(Icons.refresh),
                                        label: const Text('暂未发现直播频道，重新加载'),
                                      );
                                    }
                                    return Column(
                                      children: [
                                        for (final channel in channels)
                                          _channelTile(context, channel),
                                      ],
                                    );
                                  },
                                ),
                            ],
                          ),
                        ],
                      ],
                    ),
                  ),
                );
              },
            );
          },
        ),
      ),
    );
  }
}

/// 发现首页使用的赛事预览，不嵌套完整页面 Scaffold。
class FootballMatchesSection extends StatefulWidget {
  const FootballMatchesSection({super.key, required this.onOpen});
  final VoidCallback onOpen;

  @override
  State<FootballMatchesSection> createState() => _FootballMatchesSectionState();
}

class _FootballMatchesSectionState extends State<FootballMatchesSection> {
  late Future<List<FootballMatch>> _matches;
  var _loaded = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (!_loaded) {
      _loaded = true;
      _matches = AppScope.of(context).repository.footballMatches();
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(
              child: Text(
                '足球赛事',
                style: theme.textTheme.titleMedium?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
            ),
            TextButton(onPressed: widget.onOpen, child: const Text('更多')),
          ],
        ),
        FutureBuilder<List<FootballMatch>>(
          future: _matches,
          builder: (context, snapshot) {
            if (snapshot.connectionState == ConnectionState.waiting) {
              return const SizedBox(
                height: 72,
                child: Center(child: CircularProgressIndicator()),
              );
            }
            if (snapshot.hasError) {
              return const Padding(
                padding: EdgeInsets.symmetric(vertical: 18),
                child: Text('赛事暂时无法加载，进入赛事中心可重试'),
              );
            }
            final matches = (snapshot.data ?? const <FootballMatch>[])
                .take(3)
                .toList();
            if (matches.isEmpty) {
              return Padding(
                padding: const EdgeInsets.symmetric(vertical: 18),
                child: Text(
                  '暂无赛事数据，点击“更多”进入赛事中心',
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              );
            }
            return Column(
              children: [
                for (final match in matches)
                  Card(
                    margin: const EdgeInsets.only(bottom: 8),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 14,
                        vertical: 12,
                      ),
                      child: Row(
                        children: [
                          Expanded(
                            child: Text(
                              match.homeTeam,
                              textAlign: TextAlign.end,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                          Padding(
                            padding: const EdgeInsets.symmetric(horizontal: 12),
                            child: Text(
                              match.homeScore == null || match.awayScore == null
                                  ? 'VS'
                                  : '${match.homeScore} : ${match.awayScore}',
                              style: theme.textTheme.titleMedium?.copyWith(
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                          ),
                          Expanded(
                            child: Text(
                              match.awayTeam,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
              ],
            );
          },
        ),
      ],
    );
  }
}
