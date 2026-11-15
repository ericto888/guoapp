import 'dart:io';

import 'package:flutter/services.dart';

/// iOS 本地通知入口。通知权限由系统管理；非 iOS 平台保持静默。
class ReservationNotifier {
  ReservationNotifier._();

  static const _channel = MethodChannel('duanju/notifications');
  static bool _initialized = false;
  static bool _authorized = false;

  static Future<void> initialize() async {
    if (!Platform.isIOS || _initialized) return;
    _initialized = true;
    try {
      _authorized = await _channel.invokeMethod<bool>('requestPermission') ?? false;
    } on PlatformException {
      _initialized = false;
    } catch (_) {
      _initialized = false;
    }
  }

  static Future<void> show({required String title, required String body, required String id}) async {
    if (!Platform.isIOS) return;
    await initialize();
    if (!_authorized) throw StateError('尚未获得系统通知权限');
    await _channel.invokeMethod<void>('show', {
      'title': title,
      'body': body,
      'id': id,
    });
  }
}
