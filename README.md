# CloudMirror

A small desktop app that mirrors chosen folders from one main cloud account to
other cloud accounts, using [rclone](https://rclone.org) underneath.

- Pick a main account (for example Google Drive) and tick which folders go to which other account.
- **Check for changes** lists what would be added, updated and removed, without changing anything.
- **Sync now** makes each copy match the main folder. Removed or replaced files are moved to
  `_SyncTrash/<date>/` on the copy by default.
- Shows size and file count for every folder and every copy.

## Use

Download `CloudMirror.exe` from the [latest release](https://github.com/Jackpison/Cloud-Mirror/releases/latest) and double-click it. It opens in your browser at `http://127.0.0.1:47800` and downloads
rclone on first run. Use **Quit** in the top right to close it. Settings are stored in
`%AppData%\CloudMirror`; accounts are stored in rclone's normal config file.

## Build

```
GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui -s -w" -o dist/CloudMirror.exe .
```
