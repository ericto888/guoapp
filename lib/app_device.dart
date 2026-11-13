import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:package_info_plus/package_info_plus.dart';

import 'core_bridge.dart';

class AppDevice {
  const AppDevice({this.television = false, this.version = ''});
  final bool television;
  final String version;
  static const channel = MethodChannel('duanju/device');
  static var _televisionMode = false;
  static var _phoneMode = false;

  static Future<AppDevice> detect({
    AppDevice fallback = const AppDevice(),
  }) async {
    if (defaultTargetPlatform != TargetPlatform.android) {
      try {
        final info = await PackageInfo.fromPlatform();
        return AppDevice(version: info.version);
      } catch (_) {
        return fallback;
      }
    }
    try {
      final data = await channel
          .invokeMapMethod<String, dynamic>('deviceInfo')
          .timeout(const Duration(seconds: 2));
      final television = data?['television'];
      if (television is! bool) return fallback;
      final version = data?['version'];
      return AppDevice(
        television: television,
        version: version is String && version.isNotEmpty
            ? version
            : fallback.version,
      );
    } on PlatformException {
      return fallback;
    } on MissingPluginException {
      return fallback;
    } on TimeoutException {
      return fallback;
    }
  }

  static Future<bool> compatibleRendering() async {
    if (defaultTargetPlatform != TargetPlatform.android) return false;
    try {
      return await channel.invokeMethod<bool>('compatibleRendering') ?? false;
    } catch (_) {
      return false;
    }
  }

  static Future<bool> setCompatibleRendering(bool enabled) async {
    if (defaultTargetPlatform != TargetPlatform.android) return false;
    try {
      await channel.invokeMethod('setCompatibleRendering', {
        'enabled': enabled,
      });
      return true;
    } catch (_) {
      return false;
    }
  }

  static Future<void> setTelevisionMode(bool enabled) async {
    if (defaultTargetPlatform == TargetPlatform.iOS) {
      _televisionMode = enabled;
      return setPlayerOrientation(PlayerOrientation.auto);
    }
    if (defaultTargetPlatform != TargetPlatform.android) return;
    try {
      await channel.invokeMethod<void>('setTelevisionMode', {
        'enabled': enabled,
      });
    } catch (_) {}
  }

  static Future<void> setPhoneMode(bool enabled) {
    _phoneMode = enabled;
    return releaseOrientation();
  }

  static Future<void> releaseOrientation() => setPlayerOrientation(
    _phoneMode ? PlayerOrientation.lockedPortrait : PlayerOrientation.auto,
  );

  static Future<void> setPlayerOrientation(
    PlayerOrientation orientation,
  ) async {
    if (defaultTargetPlatform == TargetPlatform.android) {
      try {
        await channel.invokeMethod<void>('setPlayerOrientation', {
          'mode': orientation.name,
        });
      } catch (_) {}
      return;
    }
    try {
      // 每次设定方向前先停掉上一次的重力监听：否则「退出全屏 → 立刻再点
      // 全屏观看」时，上一次监听仍会回调 orientationSettled 把新锁解开。
      // 需要监听的方向由 [armOrientationSettle] 重新挂上。
      if (defaultTargetPlatform == TargetPlatform.iOS) {
        await channel.invokeMethod<void>('cancelOrientationSettle');
      }
      await SystemChrome.setPreferredOrientations(switch (_televisionMode
          ? PlayerOrientation.landscape
          : orientation) {
        PlayerOrientation.landscape ||
        PlayerOrientation.lockedLandscape => const [
          DeviceOrientation.landscapeLeft,
          DeviceOrientation.landscapeRight,
        ],
        PlayerOrientation.portrait || PlayerOrientation.lockedPortrait =>
          const [DeviceOrientation.portraitUp],
        PlayerOrientation.auto => const [],
      });
    } catch (_) {}
  }

  static bool _settleHandlerInstalled = false;

  /// iOS 没有系统级方向监听：锁定方向后由重力判断设备是否真的转到了
  /// 这个方向，稳定一段时间后回调 [orientationSettled]，再恢复 auto。
  /// 这样退出全屏只转一次竖屏，不会在横持时立刻弹回全屏；
  /// 之后把手机横过来仍能自动进全屏。Android 由原生 OrientationEventListener
  /// 完成同样的事，这里直接跳过。
  static Future<void> armOrientationSettle(
    PlayerOrientation orientation,
  ) async {
    if (defaultTargetPlatform != TargetPlatform.iOS) return;
    if (!_settleHandlerInstalled) {
      _settleHandlerInstalled = true;
      channel.setMethodCallHandler((call) async {
        if (call.method == 'orientationSettled') {
          await setPlayerOrientation(PlayerOrientation.auto);
        }
      });
    }
    try {
      await channel.invokeMethod<void>('watchOrientationSettle', {
        'mode': orientation.name,
      });
    } catch (_) {}
  }

  static Future<double?> brightness() async {
    try {
      return await channel.invokeMethod<double>('getBrightness');
    } catch (_) {
      return null;
    }
  }

  static Future<void> setBrightness(double value) async {
    try {
      await channel.invokeMethod<void>('setBrightness', {'brightness': value});
    } catch (_) {}
  }

  static Future<void> resetBrightness() async {
    try {
      await channel.invokeMethod<void>('resetBrightness');
    } catch (_) {}
  }

  static Future<double?> volume() async {
    try {
      return await channel.invokeMethod<double>('volume');
    } catch (_) {
      return null;
    }
  }

  static Future<void> setVolume(double value) async {
    try {
      await channel.invokeMethod<void>('setVolume', {'volume': value});
    } catch (_) {}
  }

  static Future<String?> createDocument(String name, String mimeType) async {
    try {
      return await channel.invokeMethod<String>('createDocument', {
        'name': name,
        'mimeType': mimeType,
      });
    } on PlatformException catch (error) {
      throw AppFailure(error.message ?? '无法创建文件');
    }
  }

  static Future<bool> activateAudio() async {
    if (defaultTargetPlatform != TargetPlatform.iOS) return true;
    try {
      final activated = await channel
          .invokeMethod<bool>('activateAudio')
          .timeout(const Duration(seconds: 3));
      return activated ?? false;
    } catch (_) {
      return false;
    }
  }

  static Future<bool> requestVideoExport() async {
    try {
      return await channel.invokeMethod<bool>('requestVideoExport') ?? false;
    } on PlatformException catch (error) {
      throw AppFailure(error.message ?? '无法申请存储权限');
    }
  }

  static Future<void> exportVideo(
    String source,
    String folder,
    String name,
  ) async {
    try {
      await channel.invokeMethod<void>('exportVideo', {
        'source': source,
        'folder': folder,
        'name': name,
      });
    } on PlatformException catch (error) {
      throw AppFailure(error.message ?? '导出失败');
    }
  }

  static Future<void> deleteDocument(String uri) async {
    try {
      await channel.invokeMethod<bool>('deleteDocument', {'uri': uri});
    } catch (_) {}
  }

  static Future<({int level, bool charging})?> battery() async {
    try {
      final data = await channel.invokeMapMethod<String, dynamic>('battery');
      final level = data?['level'];
      if (level is! int) return null;
      return (level: level, charging: data?['charging'] == true);
    } catch (_) {
      return null;
    }
  }
}

enum PlayerOrientation {
  landscape,
  lockedLandscape,
  portrait,
  lockedPortrait,
  auto,
}
