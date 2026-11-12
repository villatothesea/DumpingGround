package app

import (
	"os"
	"testing"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/dumpingground/dumpingground/internal/uninstall"
	"github.com/egoist/mygo/ui"
)

// README screenshots: rendered headlessly from fabricated data so nothing
// from a real machine leaks into the repo. Run with DG_SHOTS=<dir>.
func TestReadmeScreenshots(t *testing.T) {
	dest := os.Getenv("DG_SHOTS")
	if dest == "" {
		t.Skip("DG_SHOTS not set")
	}
	os.MkdirAll(dest, 0o755)
	const W, H = 1536, 924
	newShot := func(m *Model) *ui.Tester {
		tt := ui.NewTester(m.view, W, H)
		tt.SetScale(2)
		tt.SetDark(true)
		tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
		return tt
	}
	save := func(tt *ui.Tester, name string) {
		tt.Frame()
		tt.Frame()
		dump(t, tt, dest, name)
	}

	// 1. Computer — the synthetic laptop inventory.
	m := testModel(t)
	m.page = computerPage
	fakeMachine(m)
	save(newShot(m), "screenshot-computer.png")

	// 2. Processes — a believable desktop process list, tree mode.
	mp := testModel(t)
	fakeMachine(mp)
	mp.page = processPage
	mp.processTreeMode = true
	dev := `DESKTOP-TEST\dev`
	mp.processRows = []process.Info{
		{PID: 4, Name: "System", User: `NT AUTHORITY\SYSTEM`, CPU: 1.2, Memory: 160 << 20, Private: 14 << 20, Threads: 210, Path: `C:\Windows\System32`},
		{PID: 512, PPID: 4, Name: "svchost.exe", User: `NT AUTHORITY\SYSTEM`, CPU: 0.4, Memory: 96 << 20, Private: 24 << 20, Threads: 18, Path: `C:\Windows\System32`},
		{PID: 620, PPID: 4, Name: "svchost.exe", User: `NT AUTHORITY\NETWORK SERVICE`, Memory: 64 << 20, Private: 18 << 20, Threads: 12, Path: `C:\Windows\System32`},
		{PID: 884, Name: "csrss.exe", User: `NT AUTHORITY\SYSTEM`, Memory: 6 << 20, Private: 3 << 20, Threads: 14},
		{PID: 1024, Name: "explorer.exe", User: dev, CPU: 0.8, Memory: 310 << 20, Private: 240 << 20, ReadRate: 40 << 10, Threads: 46, Path: `C:\Windows`},
		{PID: 1300, PPID: 1024, Name: "msedge.exe", User: dev, CPU: 3.4, Memory: 2100 << 20, Private: 1900 << 20, ReadRate: 300 << 10, WriteRate: 120 << 10, Threads: 54, Path: `C:\Program Files (x86)\Microsoft\Edge\Application`},
		{PID: 1312, PPID: 1300, Name: "msedge.exe", User: dev, CPU: 1.1, Memory: 420 << 20, Private: 300 << 20, Threads: 22, Path: `C:\Program Files (x86)\Microsoft\Edge\Application`},
		{PID: 1325, PPID: 1300, Name: "msedge.exe", User: dev, CPU: 0.6, Memory: 260 << 20, Private: 190 << 20, Threads: 15, Path: `C:\Program Files (x86)\Microsoft\Edge\Application`},
		{PID: 1400, PPID: 1024, Name: "Code.exe", User: dev, CPU: 2.2, Memory: 1500 << 20, Private: 1100 << 20, WriteRate: 60 << 10, Threads: 40, Path: `C:\Users\dev\AppData\Local\Programs\Microsoft VS Code`},
		{PID: 1444, PPID: 1400, Name: "Code.exe", User: dev, CPU: 0.9, Memory: 620 << 20, Private: 480 << 20, Threads: 19, Path: `C:\Users\dev\AppData\Local\Programs\Microsoft VS Code`},
		{PID: 1500, PPID: 1024, Name: "DumpingGround.exe", User: dev, CPU: 1.6, Memory: 120 << 20, Private: 88 << 20, Threads: 32, Path: `D:\Tools\DumpingGround`},
		{PID: 1601, PPID: 1024, Name: "Docker Desktop.exe", User: dev, CPU: 0.3, Memory: 340 << 20, Private: 260 << 20, Threads: 28, Path: `C:\Program Files\Docker\Docker\frontend`},
		{PID: 1620, Name: "com.docker.backend.exe", User: dev, CPU: 0.7, Memory: 480 << 20, Private: 390 << 20, Threads: 30, Path: `C:\Program Files\Docker\Docker\resources`},
		{PID: 1701, PPID: 1024, Name: "Snipaste.exe", User: dev, Memory: 90 << 20, Private: 60 << 20, Threads: 12, Path: `D:\Tools\Snipaste`},
		{PID: 1802, PPID: 1024, Name: "OneDrive.exe", User: dev, CPU: 0.2, Memory: 220 << 20, Private: 180 << 20, Threads: 24, Path: `C:\Program Files\Microsoft OneDrive`},
		{PID: 1903, PPID: 1024, Name: "clash-verge.exe", User: dev, CPU: 0.5, Memory: 210 << 20, Private: 160 << 20, Threads: 26, Path: `D:\Tools\Clash Verge`},
		{PID: 1910, PPID: 1903, Name: "verge-mihomo.exe", User: dev, CPU: 0.1, Memory: 130 << 20, Private: 110 << 20, ReadRate: 200 << 10, WriteRate: 180 << 10, Threads: 20, Path: `D:\Tools\Clash Verge\resources`},
		{PID: 2004, PPID: 1024, Name: "tailscale-ipn.exe", User: `NT AUTHORITY\SYSTEM`, Memory: 60 << 20, Private: 40 << 20, Threads: 15, Path: `C:\Program Files\Tailscale`},
		{PID: 2100, PPID: 1024, Name: "WindowsTerminal.exe", User: dev, CPU: 0.4, Memory: 260 << 20, Private: 200 << 20, Threads: 18, Path: `C:\Program Files\WindowsApps\Microsoft.WindowsTerminal_1.21_x64__8wekyb3d8bbwe`},
		{PID: 2150, PPID: 2100, Name: "pwsh.exe", User: dev, CPU: 0.2, Memory: 110 << 20, Private: 90 << 20, Threads: 9, Path: `C:\Program Files\PowerShell\7`},
		{PID: 2200, Name: "SecurityHealthSystray.exe", User: dev, Memory: 12 << 20, Private: 8 << 20, Threads: 3, Path: `C:\Windows\System32`},
		{PID: 2300, Name: "RtkAudUService64.exe", User: `NT AUTHORITY\SYSTEM`, Memory: 14 << 20, Private: 9 << 20, Threads: 8, Path: `C:\Windows\System32\DriverStore\FileRepository\realtekservice.inf_amd64_ab12cd34`},
		{PID: 2400, PPID: 1024, Name: "SearchApp.exe", User: dev, Memory: 180 << 20, Private: 140 << 20, Threads: 20, Path: `C:\Windows\SystemApps\Microsoft.Windows.Search_cw5n1h2txyewy`},
		{PID: 2500, PPID: 1024, Name: "StartMenuExperienceHost.exe", User: dev, Memory: 90 << 20, Private: 70 << 20, Threads: 16, Path: `C:\Windows\SystemApps\Microsoft.Windows.StartMenuExperienceHost_cw5n1h2txyewy`},
		{PID: 2600, PPID: 1024, Name: "TextInputHost.exe", User: dev, Memory: 70 << 20, Private: 50 << 20, Threads: 11, Path: `C:\Windows\SystemApps\MicrosoftWindows.Client.CBS_cw5n1h2txyewy`},
	}
	mp.rebuildProcessTree()
	save(newShot(mp), "screenshot-processes.png")

	// 3. Disk usage — a scanned C:\ root with a treemap.
	md := testModel(t)
	fakeMachine(md)
	md.page = diskPage
	md.diskVolumes = []disk.Volume{
		{Path: "C:\\", Label: "", Total: 952 << 30, Free: 48 << 30},
		{Path: "D:\\", Label: "", Total: 1907 << 30, Free: 372 << 30},
		{Path: "E:\\", Label: "", Total: 953 << 30, Free: 102 << 30},
	}
	md.diskDrive, md.diskPath = `C:\`, `C:\`
	nodes := []disk.Node{
		{ID: 0, Parent: -1, Name: `C:\`, Dir: true, Children: []int{1, 2, 3, 4, 5, 6, 7}},
		{ID: 1, Parent: 0, Name: "Users", Dir: true, Bytes: 142 << 30, Allocated: 139 << 30},
		{ID: 2, Parent: 0, Name: "Program Files", Dir: true, Bytes: 14 << 30, Allocated: 13500 << 20},
		{ID: 3, Parent: 0, Name: "Windows", Dir: true, Bytes: 14 << 30, Allocated: 12 << 30},
		{ID: 4, Parent: 0, Name: "Program Files (x86)", Dir: true, Bytes: 5800 << 20, Allocated: 5600 << 20},
		{ID: 5, Parent: 0, Name: "ProgramData", Dir: true, Bytes: 4600 << 20, Allocated: 4300 << 20},
		{ID: 6, Parent: 0, Name: "Recovery", Dir: true, Bytes: 220 << 20, Allocated: 220 << 20},
		{ID: 7, Parent: 0, Name: "$Recycle.Bin", Dir: true, Bytes: 190 << 20, Allocated: 193 << 20},
	}
	md.diskSnapshot = &disk.Snapshot{
		Root: 0, Nodes: nodes,
		Files: 1636000, Dirs: 320000, Bytes: 176 << 30, Allocated: 182 << 30,
		Skipped: 72400, Estimated: 8500, Method: "NTFS MFT",
	}
	md.diskRoot, md.diskNode = 0, 0
	save(newShot(md), "screenshot-disk.png")

	// 4. Uninstall — an installed-apps table.
	mu := testModel(t)
	fakeMachine(mu)
	mu.page = uninstallPage
	mu.uninstLoaded = true
	mu.uninstRows = []uninstall.Entry{
		{Name: "7-Zip 24.09", Version: "24.09", Publisher: "Igor Pavlov", SizeBytes: 20 << 20, InstallDate: "20250914", Location: `C:\Program Files\7-Zip`},
		{Name: "Blender", Version: "4.3.2", Publisher: "Blender Foundation", SizeBytes: 913 << 20, InstallDate: "20250907", Location: `D:\Programs\Blender`},
		{Name: "Clash Verge", Version: "2.4.2", Publisher: "Clash Verge Rev", SizeBytes: 212 << 20, InstallDate: "20251028", Location: `D:\Programs\Clash Verge`},
		{Name: "Docker Desktop", Version: "4.47.0", Publisher: "Docker Inc.", SizeBytes: 3300 << 20, InstallDate: "20250816", Location: `C:\Program Files\Docker`},
		{Name: "Everything 1.4.1.1028", Version: "1.4.1.1028", Publisher: "voidtools", SizeBytes: 12 << 20, InstallDate: "20250830", Location: `C:\Program Files\Everything`},
		{Name: "Git version 2.51.0", Version: "2.51.0", Publisher: "The Git Development Community", SizeBytes: 336 << 20, InstallDate: "20250811", Location: `C:\Program Files\Git`},
		{Name: "Go Programming Language amd64 go1.25.3", Version: "1.25.3", Publisher: "https://go.dev", SizeBytes: 270 << 20, InstallDate: "20251011", Location: `C:\Program Files\Go`},
		{Name: "Google Chrome", Version: "142.0.7444.60", Publisher: "Google LLC", SizeBytes: 508 << 20, InstallDate: "20251103", Location: `C:\Program Files\Google\Chrome\Application`},
		{Name: "Microsoft Edge", Version: "142.0.3595.53", Publisher: "Microsoft Corporation", SizeBytes: 2800 << 20, InstallDate: "20251107", Location: `C:\Program Files (x86)\Microsoft\Edge\Application`},
		{Name: "Microsoft Visual Studio Code", Version: "1.105.1", Publisher: "Microsoft Corporation", SizeBytes: 390 << 20, InstallDate: "20251018", Location: `C:\Users\dev\AppData\Local\Programs\Microsoft VS Code`},
		{Name: "OBS Studio", Version: "32.0.1", Publisher: "OBS Project", SizeBytes: 480 << 20, InstallDate: "20250921", Location: `D:\Programs\obs-studio`},
		{Name: "PowerShell 7.5.4.0", Version: "7.5.4.0", Publisher: "Microsoft Corporation", SizeBytes: 110 << 20, InstallDate: "20251025", Location: `C:\Program Files\PowerShell\7`},
		{Name: "Python 3.13.2 (64-bit)", Version: "3.13.2", Publisher: "Python Software Foundation", SizeBytes: 140 << 20, InstallDate: "20250905", Location: `C:\Users\dev\AppData\Local\Programs\Python\Python313`},
		{Name: "Snipaste 2.10.8", Version: "2.10.8", Publisher: "Snipaste", SizeBytes: 45 << 20, InstallDate: "20250912", Location: `D:\Tools\Snipaste`},
		{Name: "Steam", Version: "", Publisher: "Valve Corporation", SizeBytes: 3100 << 20, InstallDate: "20250820", Location: `D:\Games\Steam`},
		{Name: "Tailscale", Version: "1.88.3", Publisher: "Tailscale Inc.", SizeBytes: 120 << 20, InstallDate: "20251002", Location: `C:\Program Files\Tailscale`},
		{Name: "VLC media player", Version: "3.0.21", Publisher: "VideoLAN", SizeBytes: 200 << 20, InstallDate: "20250901", Location: `C:\Program Files\VideoLAN\VLC`},
		{Name: "Windows Terminal", Version: "1.23.12811.0", Publisher: "Microsoft Corporation", SizeBytes: 30 << 20, InstallDate: "20251030", Location: `C:\Program Files\WindowsApps\Microsoft.WindowsTerminal_1.23_x64__8wekyb3d8bbwe`},
		{Name: "WinRAR 7.11 (64-bit)", Version: "7.11.0", Publisher: "win.rar GmbH", SizeBytes: 8 << 20, InstallDate: "20250808", Location: `C:\Program Files\WinRAR`},
		{Name: "WireGuard", Version: "0.5.3", Publisher: "WireGuard LLC", SizeBytes: 60 << 20, InstallDate: "20250926", Location: `C:\Program Files\WireGuard`},
	}
	save(newShot(mu), "screenshot-uninstall.png")

	// 5. Startup — switches plus the selected row's detail card.
	ms := testModel(t)
	fakeMachine(ms)
	ms.page = startupPage
	ms.st.items = fakeStartupItems()
	ms.st.loaded = true
	ms.st.selKey = "run|HKCU|Run|Steam"
	ms.st.selected = 0
	save(newShot(ms), "screenshot-startup.png")

	// 6. Cleanup — the Rust build-artifact plans and the item table.
	mc := testModel(t)
	fakeMachine(mc)
	mc.page = diskPage
	mc.rustMode = true
	mc.rustReport = testRustReport()
	save(newShot(mc), "screenshot-cleanup.png")
}
