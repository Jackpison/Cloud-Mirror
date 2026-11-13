# CloudMirror: project rules

Small Go app (single exe) that serves a local web UI (`index.html`) and drives rclone. Read this before changing anything.

## Versioning

Format `MAJOR.MINOR.PATCH`, currently below 1.0.

- **Patch** (0.1.1, 0.1.2 …): bug fixes, UI tweaks and small features, for example a button, a setting, wording or styling.
- **Minor** (0.2.0 …): only a substantial new capability that changes what the app can do (for example scheduling or a new kind of sync).
- **When unsure, use patch.** Never bump the minor number for something small.
- Every user-visible change gets a line in `CHANGELOG.md` under the version it will ship in.
- Releases are built by the **Release** workflow (Actions tab, "Run workflow", enter the version). Claude sessions cannot push tags
  or create releases, so tell the user which version to enter.

## Working rules

- Keep the app offline-friendly: no CDN scripts, fonts or analytics. The UI is one self-contained `index.html`.
- The README privacy statement must stay true. Any new network call must be added to it first.
- Test before pushing: `go vet ./...`, `go test ./...` (put rclone on PATH so the end-to-end test runs instead of skipping), run the app
  against local `alias` remotes, and drive UI changes with Playwright. New behaviour gets a test in `main_test.go`.
  Do not say something was tested on Windows or on real cloud accounts unless it was.
- The app has no window of its own and no console on Windows: it must exit by itself when the page is gone (`lifecycle.go`).
  Never remove the live-connection logic. Set `CLOUDMIRROR_KEEP_RUNNING=1` when you need a test server that stays up.
- Brand icons are our own simplified drawings, not official assets.
- Commit straight to `main` with a short descriptive message.
