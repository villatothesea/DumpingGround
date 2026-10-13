# Security & Safety Audit

DumpingGround deletes files and edits the registry. This document states
the invariants the code enforces, where they are enforced, and what a
failure does. The principle throughout: **when the tool cannot prove an
operation is safe, it refuses** — an unreadable process table, an
incomplete inventory, a moved path or a failed backup all end in "kept",
never in "deleted anyway".

## Layers, in order

### 1. A closed catalog

Cleanup scans enumerate a fixed list of known locations (`internal/disk/junk.go`).
Nothing is matched heuristically against arbitrary paths: every row in a
report is one of a bounded set of category members — user/Windows temp,
browser caches, shader caches, WER archives, dev caches, and the REVIEW-tier
rows (Windows.old, chkdsk fragments, WinSxS, orphaned app data, downloaded
installers). There is no code path that walks a user folder and guesses.

### 2. Three-state process guards

Live caches are protected by a process probe (`JunkOpts.Running`). The
probe is three-valued: running, not-running, or **unknown** — a failed
process snapshot denies cleanup exactly like a running application does.
The guard is checked once at scan time (a skip reason on the row) and
again inside `JunkClean` immediately before deletion, so an app launched
between scan and clean still protects its cache.

### 3. Ancestor-chain path identity (TOCTOU)

Filesystem rows are bound at discovery time to the volume:file-index
identity of every ancestor directory (`internal/disk/pathid_windows.go`,
`BindPath`). Before any delete, `PathID.Verify` re-reads the chain:

- any ancestor replaced, removed, or turned into a junction/reparse
  point → refuse;
- the leaf itself may be legitimately recreated (apps rebuild caches),
  but a leaf that was a plain directory at scan time and is now a
  reparse point → refuse.

This closes the scan→delete window where an attacker (or just an
installer) swaps a path component for a junction aimed somewhere else.

### 4. Reversibility first

- Filesystem leftovers go to the **Recycle Bin** (`SHFileOperation` with
  `FOF_ALLOWUNDO`, sized per item). Junk rows that carry user content —
  Downloads, orphaned app data, downloaded installers — are recycled, not
  erased. Nothing else is touched.
- Registry items are **exported to a `.reg` backup** in the log folder
  before deletion (`exportRegKey`/`exportRegValue` in
  `internal/uninstall/remove_windows.go`). If the backup cannot be
  written, the item is kept.
- Startup items are **disabled, never deleted**: scheduled tasks via
  `schtasks /Change /Disable`, services via `sc config ... start=
  disabled` semantics, Run keys by value rename. Windows-owned services
  and tasks are read-only by policy.
- The Recycle Bin itself, Windows.old, upgrade remnants and WinSxS are
  permanent by nature — they sit in REVIEW tier, never in a preset, and
  carry an explicit consequence note on the row.

### 5. Inventory completeness gates

Uninstall cleanup depends on knowing what is still installed.
`ListChecked` returns the entry list **plus a completeness flag**: any
registry/AppX enumeration failure marks the inventory incomplete. An
incomplete inventory means leftover candidates can be *shown* but never
removed — "could not enumerate" is never interpreted as "no owner".
The same rule drives the orphaned-app-data category: with no proven
installed-app list it flags nothing at all.

### 6. What "belongs to the app" means

Leftover detection (`internal/uninstall/footprint.go`) matches only:

- the registry-declared `InstallLocation`;
- directories whose normalized name equals a token derived from the
  app's own display name — punctuation-free, with version tails and
  channel words (Nightly/Beta/Insiders/Developer Edition…) stripped;
- a small explicit alias table for products whose data folder shares
  nothing with the display name (e.g. Visual Studio Code → `Code`);
- the app's own registry records: its Uninstall key, vendor trees under
  `Software\`, Run/RunOnce values that point into the install folder;
- Start Menu/Desktop shortcuts carrying the name.

There is no substring or fuzzy matching. A name variant another
still-installed app also claims (`sharedTokens`) is dropped unless it is
the exact full name or the install folder's basename. Shared runtimes —
VC++ redistributables, .NET, WebView2, Java — are never reported as
another app's leftover.

Duplicates registered in both registry views (32- and 64-bit hives, an
MSI beside its bootstrapper) merge into one row — but only when install
folders match, the uninstall command is identical, or name+version+
publisher coincide. Different install folders never merge.

### 7. User data is opt-in, always

Every leftover item carries a data-nature judgment (`Item.Personal`,
`Item.MachineWide`):

- caches, logs, temp and dump folders are disposable;
- settings, documents, profiles, saves and credentials are **personal**
  and start unticked;
- HKLM registry trees and `ProgramData` folders are **machine-wide** and
  also start unticked — the blast radius covers every account;
- REVIEW categories (orphaned data, setup files, Windows.old) are never
  part of any preset.

Two-clicks confirm on the removal button; nothing destructive is
single-click.

### 8. The user whitelist

Settings → Safety holds a protected-path list persisted in
`settings.json`. It is enforced twice: at discovery (`junkAdd`,
`Footprint`, `Orphans` drop covered paths) and again at the deletion
sink (`JunkClean`, `RemoveItems`) — a path protected after the scan still
refuses removal.

### 9. Uninstaller launch planning

`internal/uninstall/plan_windows.go` plans how the vendor uninstaller is
invoked: prefer the registry's `QuietUninstallString`; for MSI extract
the product code and call `msiexec /x {code} /passive`; add Inno's
`/SILENT /SUPPRESSMSGBOXES /NORESTART` only to a verified `uninsNNN.exe`
with its adjacent `.dat`; add NSIS `/S` only after the `Nullsoft` marker
is found in the executable's first megabyte — never by filename alone.
Steam's client uninstaller is deliberately excluded: its warning dialog
about the game library must be seen. Launch goes through ShellExecuteEx
with a UAC-elevation fallback, and the registry entry itself is the only
verdict — a surviving entry after the process exits is treated as
cancelled, and no cleanup is offered.

### 10. Logging and history

Every destructive action appends to a dated log under `<state dir>\logs`
(`internal/applog`), retained 30 days and pruned automatically. Filenames
and app names pass through control-character escaping so a hostile name
cannot forge extra log lines. Settings → Safety shows a read-only tail.

### 11. Elevation

The app manifest asks for administrator; privileged fallbacks (deleting
a protected leftover, reading a locked tree) go through a one-shot
elevated worker spawned after a UAC prompt, which exits with the app or
after an idle timeout. Declined consent degrades gracefully — the
operation reports its failure, nothing is retried silently.

## What this does not protect against

- A vendor uninstaller is vendor code; the app launches it and verifies
  the registry record afterwards, but cannot sandbox what it does.
- The Recycle Bin is the undo boundary — emptying it is permanent, which
  is why emptying is never bundled into a preset.
- The orphan-app-data heuristic is deliberately conservative (exact or
  prefix name match, 60-day quiet window), but a folder can still be
  wanted — that is why the category is review-only and recycles.
