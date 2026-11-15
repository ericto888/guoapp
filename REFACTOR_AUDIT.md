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
