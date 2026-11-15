import 'package:duanju_app/follow_state.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/ui/widgets/season_notice_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('新季提醒保留封面并可在标记已读后保留', () {
    const notice = SeriesSeasonNotice(
      id: 'hongguo:123',
      title: '家有五女，穿越来的他连夜搞事业啦',
      season: 3,
      unit: '季',
      cover: 'https://example.com/cover.jpg',
    );
    final parsed = SeriesSeasonNotice.fromJson(notice.toJson());
    expect(parsed.cover, 'https://example.com/cover.jpg');
    expect(parsed.read, isFalse);
    final read = parsed.markRead();
    expect(read.read, isTrue);
    expect(read.cover, 'https://example.com/cover.jpg');
  });

  test('旧版提醒数据缺少封面时按空封面解析', () {
    final parsed = SeriesSeasonNotice.fromJson({
      'id': 'hongguo:123',
      'title': '家有五女',
      'season': 2,
      'unit': '季',
      'read': false,
    });
    expect(parsed.cover, isEmpty);
  });

  testWidgets('新季提醒弹窗显示封面位、剧名与季号，可选择稍后再看', (tester) async {
    bool? details;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: FilledButton(
                onPressed: () async {
                  details = await showDialog<bool>(
                    context: context,
                    barrierDismissible: false,
                    builder: (_) => const SeasonNoticeDialog(
                      drama: Drama(
                        id: '',
                        source: 'hongguo',
                        title: '家有五女，穿越来的他连夜搞事业啦',
                        cover: 'https://example.com/cover.jpg',
                      ),
                      season: 3,
                      unit: '季',
                    ),
                  );
                },
                child: const Text('打开提醒'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开提醒'));
    await tester.pumpAndSettle();
    expect(find.text('发现新季'), findsOneWidget);
    expect(find.text('家有五女，穿越来的他连夜搞事业啦'), findsOneWidget);
    expect(find.text('第3季已上线'), findsOneWidget);
    expect(find.text('你追的剧更新了新季，点击查看'), findsOneWidget);

    await tester.tap(find.text('稍后再看'));
    await tester.pumpAndSettle();
    expect(details, isFalse);
    expect(find.text('发现新季'), findsNothing);
  });

  testWidgets('新季提醒弹窗点击查看详情返回 true', (tester) async {
    bool? details;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: FilledButton(
                onPressed: () async {
                  details = await showDialog<bool>(
                    context: context,
                    barrierDismissible: false,
                    builder: (_) => const SeasonNoticeDialog(
                      drama: Drama(
                        id: '',
                        source: 'hongguo',
                        title: '家有五女',
                      ),
                      season: 4,
                      unit: '季',
                    ),
                  );
                },
                child: const Text('打开提醒'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开提醒'));
    await tester.pumpAndSettle();
    expect(find.text('第4季已上线'), findsOneWidget);

    await tester.tap(find.text('查看详情'));
    await tester.pumpAndSettle();
    expect(details, isTrue);
  });
}
