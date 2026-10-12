# DumpingGround

[中文](README.zh-CN.md)

A Windows-native maintenance utility in Go + [MyGo](https://github.com/egoist/mygo)
native UI — a live hardware overview, process inspection, disk-usage
analysis, startup management, and *safe* cleanup (Rust build artifacts,
Windows junk, full uninstall with leftover review). Aims at the everyday
Task Manager / Process Explorer / WizTree / Autoruns workflows.

## Safety model

This app deletes things, so the rules come first:

- **Reversible by default.** Everything that can go to the Recycle Bin does.
  If Windows would silently bypass the bin (over-capacity item, disabled bin,
  non-recyclable drive), the item is left in place with an error — never
  downgraded to permanent delete.
- **Explicit confirmation.** Clean asks once, then asks again. Registry
  leftovers are exported to a `.reg` backup (UTF-16, type-faithful) before
  removal; no valid backup, no delete.
- **No silent scope.** Guards are re-checked at delete time: running
  processes, reparse points, paths claimed by another installed app, the
  active Rust toolchain, Windows servicing in progress.
- **Visible reasoning.** Every leftover shows the path and *why* it was
  flagged; guesses are never pre-selected.
- **Switches, not deletions.** Turning a startup item off never removes its
  entry, and Windows' own services and tasks are read-only.

## Running

Download `DumpingGround.exe` from Releases, or build below. Double-click,
approve the UAC prompt — the app runs elevated, like WizTree or
BCUninstall. Relaunching focuses the existing window.

### Privileges

Administrator rights are required once at launch — the NTFS journal,
MFT scan, HKLM leftover deletion and system-directory cleanup all
depend on them, so the app asks up front instead of interrupting every
later operation. A `⛨ admin` badge in the status bar shows the state.

Even elevated, the app never enables `SeDebugPrivilege`: kernel-
protected processes (csrss, lsass, antivirus) stay out of reach —
killing one would blue-screen the machine, and an `lsass` memory-read
handle is what credential-dumping tools look like. The kernel's
boundary is the feature, not a limitation.

### Command line

- `--page=computer|processes|disk|startup` — the page to open on
  (default `computer`).
- `--scan=<drive or folder>` — open the disk page and start scanning.
- `--renderer=cpu|gpu` — software rendering (default) or D3D11.

Pages are also on Ctrl+1 Computer, Ctrl+2 Processes, Ctrl+3 Disk Usage,
Ctrl+4 Uninstall, Ctrl+5 Startup.

### Computer

The page the app opens on (Ctrl+1; Ctrl+2–5 are the other tabs).

- A header with the machine's identity — name, make and model, Windows
  edition and build, processor, memory, graphics, storage, network,
  motherboard and BIOS — read from SMBIOS, the registry and DXGI.
- One live card per piece of hardware: CPU, memory, each GPU, each disk,
  each connected network adapter, and the battery on a laptop. Each shows
  the current figure and a rolling 60-second chart.
- Click a card for its detail page: a large chart, live figures and the
  full specification — per-core CPU charts and cache sizes, memory modules
  (slot, size, type, speed, part number), GPU engines and VRAM, disk
  active time, throughput and volumes, adapter addresses and link speed.
  Back, Esc, Backspace or the mouse's back button returns; so does
  clicking the Computer tab again.
- No driver is installed and nothing is written: sources are SMBIOS,
  `NtQuerySystemInformation`, performance counters (PDH), the disk
  performance IOCTL and the IP helper API. Temperatures and fan speeds
  are not shown — Windows only exposes them through vendor drivers.

### Processes

- Flat list by default, Tree checkbox for parent/child view.
- Search by name, PID or path; click headers to sort.
- CPU, working set, private commit, I/O rates, threads; inaccessible
  processes show as unavailable.
- Pause sampling (1/2/5 s intervals), F5 manual refresh.
- Details / right-click Properties: command line, user, elevation, handles,
  modules, threads.
- Open file location, copy path; kill confirms in-app and checks creation
  time to guard against PID reuse.

### Disk usage

1. Pick a drive and Scan drive, or Browse… / type a path and Scan.
2. Live counters during scan; Cancel keeps partial results.
3. Tree/file table plus extension stats on top, a per-directory treemap
   below, draggable splitter.
4. Enter/double-click drills down, Up/Root goes back; clicking a treemap
   file locates it in the table.
5. Allocated / Logical toggle; Export CSV; Clear index frees the index.

Whole-drive NTFS scans try the read-only MFT path and fall back to plain
traversal with the reason shown. Strict verification uses a separate
`DumpingGround-verify.exe` and reports `INCONCLUSIVE` rather than
pretending to pass.

### Cleanup

- Rust build artifacts (per-preset: target dirs, cargo caches; the active
  toolchain and projects referenced by settings/overrides are protected).
- Windows junk (temp files, caches, Downloads on request) with age and
  in-use checks; browser caches skipped while the browser runs.
- The Recycle Bin itself is never part of a preset.

### Uninstall

- Launches the app's own uninstaller (elevated when required), then watches
  the registry entry — leftover review opens only when the entry is truly
  gone, so cancelling the uninstaller is a no-op.
- Leftover scan covers files, startup entries, and registry keys; each item
  is Confirmed (evidence) or Likely (name match, never pre-checked).
- Registry removals are backed up; filesystem items go to the Recycle Bin.

### Startup

Everything that starts by itself, in one list with an on/off switch each:

- **Apps** — the `Run` keys (current user, all users, 32-bit) and both
  Startup folders.
- **Services** — automatic and delayed-start services, and ones switched off.
- **Scheduled tasks** — tasks that fire at sign-in or at boot.

Each row shows the publisher (from the program's own version resource),
whether it is running right now, and the exact command. Selecting a row
says where it is configured and what switching it does.

Nothing is deleted, and every switch can be undone:

- Apps use the `StartupApproved` flags — the mechanism Task Manager uses —
  so the `Run` value or shortcut stays exactly where it is.
- Tasks are enabled or disabled through Task Scheduler itself.
- Services have their start type changed; the original (automatic, and
  whether delayed) is remembered and restored on switching back on.
  Turning a service off asks first, and a running service keeps running
  until the next boot.
- Windows' own services and tasks are hidden by default and read-only when
  shown, decided by who made the program rather than where it sits —
  vendor driver services under `System32\DriverStore` stay switchable.
- Every change is written to the app log.

## Building

Go 1.27+, MyGo pinned to v0.3.5. No Bun, WebView2, or C toolchain.

```powershell
go test ./...
$env:CGO_ENABLED = "0"
go build -tags mygo_noinspector -trimpath -ldflags "-s -w -H=windowsgui" -o build/DumpingGround.exe ./cmd/dumpingground
```

CPU rendering by default; `--renderer=gpu` switches to D3D11.

## Docs

- [Development notes](docs/development.md)
- [Acceptance checklist](docs/acceptance.md)
- [Diagnostics: MFT validation, handle search, stacks](docs/diagnostics.md)
- [MFT validation log](docs/mft-validation.md)

## License

MIT — see [LICENSE](LICENSE). The license may move to Apache-2.0 in the
future; contributions are accepted on those terms, see
[CONTRIBUTING.md](CONTRIBUTING.md).
