import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core_bridge.dart';
import '../../football_models.dart';
import '../app_scope.dart';

/// 足球赛事中心首页。数据通过 Go 核心获取，不依赖影视剧库。
class FootballHomePage extends StatefulWidget {
  const FootballHomePage({super.key});

  @override
  State<FootballHomePage> createState() => _FootballHomePageState();
}

class _FootballHomePageState extends State<FootballHomePage> {
  late Future<List<FootballMatch>> _matches;
  var _loaded = false;

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
            if (snapshot.connectionState == ConnectionState.waiting) {
              return const Center(child: CircularProgressIndicator());
            }
            if (snapshot.hasError) {
              return ListView(
                children: [
                  const SizedBox(height: 140),
                  Icon(Icons.cloud_off_outlined,
                      size: 42, color: theme.colorScheme.onSurfaceVariant),
                  const SizedBox(height: 12),
                  const Center(child: Text('赛事暂时无法加载')),
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
                  Icon(Icons.sports_soccer,
                      size: 56, color: theme.colorScheme.primary),
                  const SizedBox(height: 18),
                  const Center(
                    child: Text('足球赛事中心已就绪'),
                  ),
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
            return ListView.separated(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.all(16),
              itemCount: matches.length,
              separatorBuilder: (_, _) => const SizedBox(height: 10),
              itemBuilder: (context, index) {
                final match = matches[index];
                return Card(
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(match.league,
                            style: theme.textTheme.labelMedium),
                        const SizedBox(height: 12),
                        Row(
                          children: [
                            Expanded(
                              child: Text(match.homeTeam,
                                  textAlign: TextAlign.center),
                            ),
                            Text(
                              match.homeScore == null || match.awayScore == null
                                  ? 'VS'
                                  : '${match.homeScore} : ${match.awayScore}',
                              style: theme.textTheme.titleLarge,
                            ),
                            Expanded(
                              child: Text(match.awayTeam,
                                  textAlign: TextAlign.center),
                            ),
                          ],
                        ),
                        const SizedBox(height: 8),
                        Text(
                          match.kickoff?.toLocal().toString() ?? '开赛时间待定',
                          textAlign: TextAlign.center,
                          style: theme.textTheme.bodySmall,
                        ),
                        if (match.channels.isNotEmpty) ...[
                          const SizedBox(height: 8),
                          ExpansionTile(
                            tilePadding: EdgeInsets.zero,
                            dense: true,
                            title: Text('直播频道（${match.channels.length}）'),
                            children: [
                              for (final channel in match.channels)
                                ListTile(
                                  dense: true,
                                  leading: const Icon(Icons.play_circle_outline),
                                  title: Text(channel.name),
                                  subtitle: const Text('打开第三方直播入口'),
                                  onTap: () async {
                                    final uri = Uri.tryParse(channel.url);
                                    if (uri == null ||
                                        !uri.isScheme('https') ||
                                        !await canLaunchUrl(uri)) {
                                      if (context.mounted) {
                                        ScaffoldMessenger.of(context).showSnackBar(
                                          const SnackBar(content: Text('直播入口暂时无法打开')),
                                        );
                                      }
                                      return;
                                    }
                                    await launchUrl(
                                      uri,
                                      mode: LaunchMode.externalApplication,
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
