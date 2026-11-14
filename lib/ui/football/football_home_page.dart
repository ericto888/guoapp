import 'package:flutter/material.dart';

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

  @override
  void initState() {
    super.initState();
    _matches = _load();
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
                      '赛事数据源将在下一阶段接入',
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
