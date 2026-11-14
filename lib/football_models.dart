/// 足球赛事领域模型。独立于影视 Drama / Episode 模型。
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
  });

  final String id;
  final String league;
  final String homeTeam;
  final String awayTeam;
  final DateTime? kickoff;
  final String status;
  final int? homeScore;
  final int? awayScore;

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
  );
}
