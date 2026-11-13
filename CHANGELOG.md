# Changelog

Versions follow `MAJOR.MINOR.PATCH`. While the app is below 1.0, a new set of features bumps the minor
number (0.2.0, 0.3.0 …) and bug fixes bump the patch number (0.1.1, 0.1.2 …).

## 0.2.0

- Light / dark mode button in the top bar. It starts with your system setting, remembers your choice, and follows
  the system again only until you press it.

## 0.1.0

First release.

- Connect cloud accounts (Google Drive, pCloud, Koofr, Filen, Dropbox, OneDrive, MEGA, Box, Proton Drive and
  everything else rclone supports) and pick one as **Main**.
- Mirror any folder or sub-folder of the main account to one or more other accounts.
- Choose the destination folder on each account, browse it, or create a new one.
- Size and file count for every source and destination, with a Matches / Differs badge.
- **Check for changes** previews exactly what would be added, updated or removed; **Sync now** applies it.
- Removed or replaced files go to a `_SyncTrash` folder on the destination (or are deleted, or left alone — your choice).
- Windows exe with icon, version information and a build-provenance attestation.
