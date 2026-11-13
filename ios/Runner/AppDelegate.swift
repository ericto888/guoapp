import Flutter
import UIKit
import DuanjuCore
import CFNetwork
import AVFoundation
import MediaPlayer
import CoreMotion

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  private var originalBrightness: CGFloat?
  private let volumeView = MPVolumeView(frame: CGRect(x: -100, y: -100, width: 1, height: 1))

  /// 锁定方向后用来判断设备是否真的已经转到该方向（与 Android 的
  /// OrientationEventListener 对齐），稳定后才让 Dart 侧恢复自动旋转。
  private let motionManager = CMMotionManager()
  private var settleMode = ""
  private var settleReachedAt: CFTimeInterval = 0
  private static let settleSeconds: CFTimeInterval = 0.8

  private var keyWindow: UIWindow? {
    UIApplication.shared.connectedScenes
      .compactMap { $0 as? UIWindowScene }
      .flatMap { $0.windows }
      .first { $0.isKeyWindow }
  }

  private var screen: UIScreen? { keyWindow?.screen }

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    ZgjEnsureCoreLinked()
    // 启动即激活 playback 类音频会话（配合 Info.plist 的 UIBackgroundModes=audio）：
    // 锁屏/切出后即使播放器已暂停，进程也不会被系统立即挂起，
    // mpv 与外部纹理保持存活，回前台无需纹理重建，不产生花屏帧。
    // 长时间锁屏后系统仍会挂起进程，那种情况由 Dart 侧的重建恢复链路兜底。
    do {
      let session = AVAudioSession.sharedInstance()
      try session.setCategory(.playback, mode: .moviePlayback, options: [])
      try session.setActive(true)
    } catch {}
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
    if let registrar = engineBridge.pluginRegistry.registrar(forPlugin: "DeviceSettings") {
      let channel = FlutterMethodChannel(name: "duanju/device", binaryMessenger: registrar.messenger())
      channel.setMethodCallHandler { call, result in
        if call.method == "battery" {
          let device = UIDevice.current
          device.isBatteryMonitoringEnabled = true
          guard device.batteryLevel >= 0 else {
            result(nil)
            return
          }
          result(["level": Int((device.batteryLevel * 100).rounded()),
                  "charging": device.batteryState == .charging || device.batteryState == .full])
          return
        }
        let arguments = call.arguments as? [String: Any]
        switch call.method {
        case "activateAudio":
          // iOS 从后台/锁屏恢复时音频会话可能已被系统停用。
          // 失败必须让 Dart 侧知道，不能一律回报成功。
          do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playback, mode: .moviePlayback, options: [])
            try session.setActive(true)
            result(true)
          } catch {
            result(FlutterError(
              code: "audio_session_activation_failed",
              message: error.localizedDescription,
              details: nil))
          }
          return
        case "getBrightness":
          result(self.screen.map { Double($0.brightness) })
          return
        case "setBrightness":
          guard let screen = self.screen, let value = arguments?["brightness"] as? Double else {
            result(nil)
            return
          }
          if self.originalBrightness == nil { self.originalBrightness = screen.brightness }
          screen.brightness = CGFloat(min(max(value, 0.01), 1))
          result(true)
          return
        case "resetBrightness":
          if let original = self.originalBrightness { self.screen?.brightness = original }
          self.originalBrightness = nil
          result(true)
          return
        case "watchOrientationSettle":
          // 锁定成竖屏/横屏后开始监听重力，设备真正转过去并稳定一会儿
          // 再回调 Dart 恢复 auto，避免还横持时立刻弹回全屏。
          self.watchOrientationSettle(
            mode: arguments?["mode"] as? String ?? "portrait",
            channel: channel)
          result(nil)
          return
        case "cancelOrientationSettle":
          self.cancelOrientationSettle()
          result(nil)
          return
        case "volume":
          result(Double(AVAudioSession.sharedInstance().outputVolume))
          return
        case "setVolume":
          if let value = arguments?["volume"] as? Double {
            if self.volumeView.superview == nil { self.keyWindow?.addSubview(self.volumeView) }
            let slider = self.volumeView.subviews.first { $0 is UISlider } as? UISlider
            slider?.value = Float(min(max(value, 0), 1))
          }
          result(nil)
          return
        default:
          break
        }
        guard call.method == "systemProxy" else {
          result(FlutterMethodNotImplemented)
          return
        }
        let settings = CFNetworkCopySystemProxySettings()?.takeRetainedValue() as? [String: Any] ?? [:]
        func address(_ prefix: String) -> String {
          guard (settings["\(prefix)Enable"] as? NSNumber)?.boolValue == true,
                let host = settings["\(prefix)Proxy"] as? String,
                let port = settings["\(prefix)Port"] as? NSNumber,
                !host.isEmpty, port.intValue > 0 else { return "" }
          let name = host.contains(":") ? "[\(host)]" : host
          return "http://\(name):\(port.intValue)"
        }
        let http = address("HTTP")
        let https = address("HTTPS")
        result(["http": http, "https": https.isEmpty ? http : https,
                "bypass": settings["ExceptionsList"] as? [String] ?? [],
                "pac": (settings["ProxyAutoConfigEnable"] as? NSNumber)?.boolValue == true])
      }
    }
  }

  private func watchOrientationSettle(mode: String, channel: FlutterMethodChannel) {
    cancelOrientationSettle()
    guard motionManager.isDeviceMotionAvailable else { return }
    settleMode = mode
    settleReachedAt = 0
    motionManager.deviceMotionUpdateInterval = 0.1
    motionManager.startDeviceMotionUpdates(to: .main) { [weak self] motion, _ in
      guard let self, let gravity = motion?.gravity else { return }
      // 竖持：重力大致朝屏幕下方；横持：重力大致朝左右两侧。
      // 阈值 0.86 约等于偏离目标方向 30° 以内。
      let matched = self.settleMode == "landscape"
        ? abs(gravity.x) >= 0.86
        : gravity.y <= -0.86
      guard matched else {
        self.settleReachedAt = 0
        return
      }
      let now = CACurrentMediaTime()
      if self.settleReachedAt == 0 {
        self.settleReachedAt = now
        return
      }
      guard now - self.settleReachedAt >= AppDelegate.settleSeconds else { return }
      self.cancelOrientationSettle()
      channel.invokeMethod("orientationSettled", arguments: nil)
    }
  }

  private func cancelOrientationSettle() {
    motionManager.stopDeviceMotionUpdates()
    settleMode = ""
    settleReachedAt = 0
  }
}
