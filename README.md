<p align="center"><img src="assets/icon.svg" width="96" alt="CloudMirror"></p>

# CloudMirror

A small desktop app that mirrors chosen folders from one main cloud account to
other cloud accounts, using [rclone](https://rclone.org) underneath.

- Pick a main account (for example Google Drive) and connect your other accounts (pCloud, Koofr, Filen, Dropbox, OneDrive, MEGA …).
- Choose **any folder or sub-folder** of the main account, then choose **which accounts** get a copy and **which folder** on each.
- **Check for changes** lists what would be added, updated and removed, without changing anything. **Sync now** applies it.
- Removed or replaced files are moved to `_SyncTrash/<date>/` on the copy by default (or deleted, or left alone).
- Shows size and file count for every source and destination, with a Matches / Differs badge.
- Skip files by pattern (`*.tmp`, `Thumbs.db` …), keep a history of every run, and clean up old trash in one click.
- Google Docs, Sheets and Slides are skipped by default (they aren't real files); you can switch them to be copied as Office files.

<p align="center"><img src="mockups/plans.png" width="900" alt="Sync plans"></p>

## Use

Download `CloudMirror.exe` from the [latest release](https://github.com/Jackpison/Cloud-Mirror/releases/latest) and double-click it. It opens in your browser at `http://127.0.0.1:47800` and downloads
rclone on first run. Closing the browser tab closes CloudMirror too (after about 20 seconds; a sync in progress finishes first), or use **Quit** in the top right to close it at once. Settings are stored in
`%AppData%\CloudMirror`; accounts are stored in rclone's normal config file.

### "Windows protected your PC"

The exe is not code-signed yet, so Windows SmartScreen may warn the first time: click **More info → Run anyway**.
Every release lists a SHA-256 checksum (`SHA256SUMS.txt`) and carries a GitHub build-provenance attestation, so you
can verify the file came from this repository:

```
gh attestation verify CloudMirror.exe --repo Jackpison/Cloud-Mirror
```

## Versions

See [CHANGELOG.md](CHANGELOG.md). A release is published automatically when a tag such as `v0.1.0` is pushed.

## Build

```
go install github.com/tc-hib/go-winres@v0.3.3
go-winres make --in winres/winres.json --out rsrc --arch amd64
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w -X main.version=0.1.0" -o dist/CloudMirror.exe .
```

## Code signing policy

Releases are built from this repository by the [Release workflow](.github/workflows/release.yml) on GitHub Actions, and each
one carries a SHA-256 checksum and a build-provenance attestation. Signing of the Windows exe is being set up; until then the
exe is unsigned (see "Windows protected your PC" above).

- **Authors, reviewers and approvers:** [@Jackpison](https://github.com/Jackpison) (owner and sole maintainer). Every release is
  approved manually before it is published.
- **Only this project's own binaries are signed.** The rclone engine that CloudMirror downloads on first start is a separate
  program, signed and published by its own maintainers; CloudMirror does not sign or redistribute it.

## Privacy

CloudMirror has no telemetry and no account of its own. It does not send information anywhere except as you ask it to:

- On first start, if rclone is not installed, it downloads rclone from `downloads.rclone.org` when you press the download button.
- When you add accounts, check or sync, it talks to the cloud services you chose, through rclone, using the credentials you
  entered. Those credentials are stored on your computer in rclone's configuration file and are only sent to the service they belong to.
- At most every six hours it asks GitHub (`api.github.com`) for the newest CloudMirror version number, to show an "Update available"
  notice. The request is anonymous and sends nothing about you or your files. Untick "Tell me when a new version is out" to stop it.
- Its web interface listens only on `127.0.0.1` (your own computer).
- A history of your runs (folder names and file names) is kept in `%AppData%\CloudMirror\history.json` on your computer only.

## License

[MIT](LICENSE). CloudMirror runs the separate [rclone](https://rclone.org) program (also MIT licensed), which it downloads on first start.
