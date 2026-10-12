# DumpingGround

[中文](README.zh-CN.md)

A Windows-native maintenance utility in Go + [MyGo](https://github.com/egoist/mygo)
native UI — process inspection, disk-usage analysis, and *safe* cleanup
(Rust build artifacts, Windows junk, full uninstall with leftover review).
Aims at the everyday Process Explorer / WizTree workflows; remaining gaps are
listed below.

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

## Running

Download `DumpingGround.exe` from Releases, or build below. Double-click —
no installer, no UAC prompt at launch. Relaunching focuses the
existing window.

### Privileges

The app runs unelevated and stays that way. The first operation that
actually needs administrator rights — reading the NTFS journal or MFT,
deleting an HKLM leftover or a protected system directory, terminating a
process owned by another user — prompts UAC **once** and starts a
short-lived elevated worker (`DumpingGround.exe --elevated-worker`, no
window). It talks to the parent over a named pipe that accepts only the
parent's PID, answers a closed set of operations (handle donation,
whitelisted file/registry deletes, protected-directory listings), is
logged to the same audit log, and exits when the app exits or after ten
idle minutes. Declining the prompt just keeps the original "access
denied" error — nothing else changes. A `⛨ admin` badge in the status
bar shows whenever that worker is alive.

The worker never enables `SeDebugPrivilege`: kernel-protected processes
(csrss, lsass, antivirus) stay out of reach — killing one would blue-
screen the machine, and an `lsass` memory-read handle is what
credential-dumping tools look like. The kernel's boundary is the
feature, not a limitation.

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
