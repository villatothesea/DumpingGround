# DumpingGround

DumpingGround is a low footprint Windows process viewer and disk space analyzer built with Go and [MyGo](https://github.com/egoist/mygo) native UI.

- Process tree with CPU, memory and I/O snapshots.
- On demand cancellable directory scanning.
- Treemap view for disk usage.
- No WebView2, Electron or helper process in the native UI build.

The current implementation is an early development build. See [docs/development.md](docs/development.md) for product boundaries, architecture, development order, testing and commit rules.

## Development

The project targets Go 1.27.1 and MyGo 0.3.5. Native UI builds do not need Bun.

```powershell
go test ./...
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w -H=windowsgui" -o build/DumpingGround.exe ./cmd/dumpingground
```

The process collector is Windows-specific. Platform-independent treemap and disk scanner tests can run on any host.
