# Changelog

Versions follow `MAJOR.MINOR.PATCH`. While the app is below 1.0, small changes (bug fixes, UI tweaks, small features such as
a button or a setting) bump the patch number (0.1.1, 0.1.2 …). The minor number (0.2.0, 0.3.0 …) is only for a substantial new
capability. When unsure, use patch.

## 0.1.2

- Google Docs, Sheets and Slides: new option (Options panel, shown when your main account is Google Drive). They are now skipped
  by default because they aren't real files and can make every run slow or look "changed". Switch to "Copy them as Office files"
  to keep the old behaviour. **Existing users:** Docs are skipped from this version on unless you change the option.
- Skip some files: per-sync patterns such as `*.tmp` or `Thumbs.db`, with one-click suggestions. Sizes and counts respect them.
- Clean up trash: find and permanently delete old `_SyncTrash` snapshots on your destinations (7 / 14 / 30 / 90 days or all),
  with a preview of what it frees.
- History: every check and sync is remembered (last 100) with what changed and any errors.
- Sign-in help for the awkward steps: Google Drive (own client ID) and Filen (API key).
- "Update available" notice. It asks GitHub for the newest version at most every six hours, anonymously; it can be switched off.
- Automated tests, including an end-to-end test that runs real syncs between local folders; they run on every push.

## 0.1.1

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
