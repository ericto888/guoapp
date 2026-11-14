/// 足球赛事领域模型。独立于影视 Drama / Episode 模型。
class FootballLiveChannel {
  const FootballLiveChannel({required this.name, required this.url});

  final String name;
  final String url;

  factory FootballLiveChannel.fromJson(Map<String, dynamic> json) =>
      FootballLiveChannel(
        name: json['name'] as String? ?? '直播频道',
        url: json['url'] as String? ?? '',
      );
}

class FootballMatch {
  const FootballMatch({
    required this.id,
    required this.league,
    required this.homeTeam,
    required this.awayTeam,
    required this.kickoff,
    this.status = 'scheduled',
    this.homeScore,
    this.awayScore,
    this.matchTime = '',
    this.halfTimeHomeScore,
    this.halfTimeAwayScore,
    this.cornerHome,
    this.cornerAway,
    this.detailUrl = '',
    this.channels = const [],
  });

  final String id;
  final String league;
  final String homeTeam;
  final String awayTeam;
  final DateTime? kickoff;
  final String status;
  final int? homeScore;
  final int? awayScore;
  final String matchTime;
  final int? halfTimeHomeScore;
  final int? halfTimeAwayScore;
  final int? cornerHome;
  final int? cornerAway;
  final String detailUrl;
  final List<FootballLiveChannel> channels;

  bool get isLive => status == 'live' || status == 'halftime';

  factory FootballMatch.fromJson(Map<String, dynamic> json) => FootballMatch(
    id: json['id'] as String? ?? '',
    league: json['league'] as String? ?? '',
    homeTeam: json['homeTeam'] as String? ?? '',
    awayTeam: json['awayTeam'] as String? ?? '',
    kickoff: DateTime.tryParse(json['kickoff'] as String? ?? ''),
    status: json['status'] as String? ?? 'scheduled',
    homeScore: (json['homeScore'] as num?)?.toInt(),
    awayScore: (json['awayScore'] as num?)?.toInt(),
    matchTime: json['matchTime'] as String? ?? '',
    halfTimeHomeScore: (json['halfTimeHomeScore'] as num?)?.toInt(),
    halfTimeAwayScore: (json['halfTimeAwayScore'] as num?)?.toInt(),
    cornerHome: (json['cornerHome'] as num?)?.toInt(),
    cornerAway: (json['cornerAway'] as num?)?.toInt(),
    detailUrl: json['detailUrl'] as String? ?? '',
    channels: [
      for (final item in json['channels'] as List? ?? const [])
        FootballLiveChannel.fromJson(Map<String, dynamic>.from(item as Map)),
    ],
  );
}
