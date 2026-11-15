# GuoApp upstream refactor audit

## Branch and immutable baselines

- Working branch: `refactor/upstream-sync-2026-11-15`
- Local baseline (`ericto888/guoapp` main): `de72caf6cad428735eb63bdaf99425d13a2a8844`
- Upstream baseline (`Xanerix/guoapp` main observed during audit): `fd3a417cf1bf7cbfb4ff9945adfe5f7480d930c8`
- The production `main` branch is not modified by this work.

## First-pass file audit

### Preserve local implementation; do not overwrite with upstream

| File / area | Reason | Migration treatment |
| --- | --- | --- |
| `lib/playback_session.dart` | Local implementation has substantially expanded lifecycle/recovery behavior absent from upstream: serialized player operations, standby-player state machine, raw-open tracking, bounded stop, lifecycle stages, foreground resume validation, single-flight rebuild and stream epoch handling. | Keep local implementation as the starting point; reconcile constructor/API differences only after upstream UI and player ownership are mapped. |
| `ios/Runner/AppDelegate.swift` | Local version adds playback audio-session activation at launch and an `activateAudio` method that reports activation failures. Upstream version currently contains device settings/proxy methods but not these playback-specific additions. | Preserve local additions while checking native channel callers and iOS background audio configuration. |
| `lib/hongguo_series.dart`, `lib/follow_state.dart` | Local series title/season parsing and per-season read/notice state are application-specific. | Preserve; map to upstream's generic provider `seasons` capability rather than deleting the local feature. |
| `lib/follow_state.dart`, `lib/follow_updater.dart`, `lib/ui/follow/follow_page.dart`, `lib/ui/shell/app_shell.dart` | Local follow/reservation workflow and update detection are integrated across state, navigation and UI. | Preserve feature behavior and migrate UI integration against upstream shell/page APIs. |
| `lib/ui/widgets/season_notice_dialog.dart`, `lib/ui/widgets/reservation_online_dialog.dart` | App-local in-app new-season and reservation-online notices. | Preserve and reconnect to upstream navigation/state lifecycle. |
| `native/core/app_seasons.go`, `native/core/provider_hongguo_album.go` and tests | Local red-fruit season discovery, official album batch metadata and publish-time handling. Upstream's JS provider architecture does not provide this app-specific workflow by default. | Preserve behavior; later move provider-specific HTTP/response parsing into the private Hongguo JS module where appropriate, retaining app-level season orchestration in the core. |

### Architectural migration required before bulk file replacement

- Local Go core currently registers hard-coded Go providers (`provider_*.go`); upstream has a runtime JS module system built around `module_loader.go`, `module_script.go`, module manifests/capabilities, install/store logic and a host API.
- Upstream's documented JS runtime is Goja, not Node.js or a browser. Module code must use the upstream `host` API; browser/Node globals such as `fetch`, `URL`, `Buffer`, `atob` and `TextEncoder` cannot be assumed.
- The migration therefore cannot be a blind overwrite of `native/core`: provider registration, host/network policy, module storage, module install/update, bundled modules, app dispatch and tests must move as one compatible unit.
- User-facing drama IDs and local snapshots are persisted. Existing history, follows, reservations, downloads, provider states and playback progress must remain readable; unknown snapshot keys can make old snapshots fail to load.


### Player UI / orientation audit

| File | Observed divergence | Treatment |
| --- | --- | --- |
| `lib/ui/player/playback_page.dart` | Local version protects the previous layout while the next video's aspect ratio is unknown and routes lifecycle events through `onLifecycleEvent`; upstream has a `portraitStyleForWide` preference path and additional desktop fullscreen handling. | Merge: keep local no-flicker aspect transition and lifecycle forwarding; port upstream's selectable portrait-style-for-wide behavior and desktop/fullscreen handling. |
| `lib/ui/player/portrait_player_page.dart` | Local adds lifecycle forwarding, season-aware swipe-to-next-season, quiet auto-switch behavior and its own video surface/zoom calculation. Upstream includes mini-player integration and different fit/zoom handling. | Merge local season/lifecycle behavior with upstream UI capabilities; preserve the portrait container's ability to host landscape media. |
| `lib/ui/player/landscape_player_page.dart` | Local has lifecycle forwarding, season-aware paging, orientation re-arm after exiting fullscreen and more extensive UI code; upstream has cast integration, mini-player integration, fit panel and sensor-follow behavior. | Merge both; do not replace local orientation exit sequencing or season paging. |
| `lib/playback_preferences.dart` | Upstream has extra settings including `portraitStyleForWide`, swipe episode behavior, boost speed and danmaku speed. Local has a smaller preference schema and additional video-fit documentation. | Adopt upstream preference model, retain local `VideoFit` behavior, and add compatibility defaults for existing settings. |
| `lib/playback_session.dart` | Local has deep playback lifecycle and recovery changes; upstream is materially simpler. | Local file remains protected; adapt upstream UI to the preserved session API rather than downgrading the session. |

### Shared state / bridge audit

- `lib/local_store.dart`: local adds season reservation persistence and operations; upstream has additional storage hardening, missing-source record reporting and newer settings. Merge both. Do not replace the local file wholesale.
- `lib/models.dart`: local has a hard-coded source catalogue; upstream has dynamic source descriptors and capability flags for installed JS modules. Upstream model architecture is the base; migrate local-only drama metadata and reservation/season model types into it.
- `lib/core_bridge.dart`: upstream exposes module install/store APIs and dynamic source management; local exposes playback stream health/epoch and Hongguo-specific methods. Merge the bridge contract deliberately, retaining stream epoch health and moving provider-specific operations behind the module/provider boundary where possible.
- `lib/follow_updater.dart`: local adds lifecycle-triggered, rotating batch reservation checks and in-app online callbacks; upstream provides generic periodic follow refresh. Merge local reservation checks into upstream updater.
- `ios/Runner/AppDelegate.swift`: preserve local `activateAudio` and launch audio-session activation in addition to upstream's existing device/proxy channels.

## Next audit / migration order

1. Complete the file map for player UI/orientation, playback ownership, series/reservation state and local persistence.
2. Map upstream module runtime files and all Go/Dart dispatch boundaries; identify which local Go provider logic becomes private JS modules.
3. Migrate the upstream baseline in coherent subsystems, not one-file-at-a-time where files have compile-time/API dependencies.
4. Reconnect the preserved local feature set and add compatibility tests before any removal of legacy providers.
5. Run Dart format/analyze/tests and Go race tests, then build and exercise iOS on macOS. GitHub file operations alone cannot establish that an iOS build or device lifecycle test passes.

## Explicit non-goals

- Do not modify `main` during refactor.
- Do not replace the local playback lifecycle with upstream's simpler implementation.
- Do not move player lifecycle, audio-session ownership or display-orientation policy into station JS modules.
- Do not change persisted drama/provider IDs without an explicit compatibility mapping.
