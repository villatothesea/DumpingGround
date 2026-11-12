//go:build windows

package startup

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPackageFamilyName(t *testing.T) {
	for in, want := range map[string]string{
		"OpenAI.Codex_26.1103.7773.0_x64__2p2nqsd0c76g0":  "OpenAI.Codex_2p2nqsd0c76g0",
		"Microsoft.Todos_2.178.8571.0_x64__8wekyb3d8bbwe": "Microsoft.Todos_8wekyb3d8bbwe",
		"broken": "",
		"_bad":   "",
	} {
		if got := packageFamilyName(in); got != want {
			t.Errorf("packageFamilyName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePackagedTasks(t *testing.T) {
	const manifest = `<?xml version="1.0"?>
<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10" xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10" xmlns:uap5="http://schemas.microsoft.com/appx/manifest/uap/windows10/5" xmlns:desktop="http://schemas.microsoft.com/appx/manifest/desktop/windows10">
  <Applications>
    <Application Id="App" Executable="app/ChatGPT.exe" EntryPoint="Windows.FullTrustApplication">
      <uap:VisualElements DisplayName="ChatGPT" />
      <Extensions>
        <uap5:Extension Category="windows.startupTask" Executable="app/ChatGPT.exe" EntryPoint="Windows.FullTrustApplication">
          <uap5:StartupTask TaskId="App" Enabled="true" DisplayName="ChatGPT" />
        </uap5:Extension>
      </Extensions>
    </Application>
  </Applications>
</Package>`
	pkg := packagedApp{fullName: "OpenAI.Codex_26.1103.7773.0_x64__2p2nqsd0c76g0", family: "OpenAI.Codex_2p2nqsd0c76g0", root: `C:\Pkg`}
	tasks := parsePackagedTasks(pkg, []byte(manifest))
	if len(tasks) != 1 {
		t.Fatalf("tasks=%v", tasks)
	}
	if tasks[0].taskID != "App" || tasks[0].display != "ChatGPT" || !tasks[0].manifestEnabled {
		t.Fatalf("task=%+v", tasks[0])
	}
}

func regDword(path, name string) (uint32, bool) {
	h, err := openKey(syscall.HKEY_CURRENT_USER, path, syscall.KEY_QUERY_VALUE)
	if err != nil {
		return 0, false
	}
	defer syscall.RegCloseKey(h)
	b := queryValue(h, name)
	if len(b) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b), true
}

// TestPackagedTaskRoundTrip exercises the same HKCU state switch used by
// Windows Settings → Apps → Startup, under a synthetic package-family key.
func TestPackagedTaskRoundTrip(t *testing.T) {
	path := packagedTaskStatePath("DumpingGround.Test_abcdefghijk", "Startup")
	h, err := createKey(syscall.HKEY_CURRENT_USER, path)
	if err != nil {
		t.Skip(err)
	}
	syscall.RegCloseKey(h)
	defer exec.Command("reg", "delete", `HKCU\`+appxStateKey+`\DumpingGround.Test_abcdefghijk`, "/f").Run()

	it := Item{Kind: KindPackaged, Key: "packaged|DumpingGround.Test_abcdefghijk|Startup", Name: "Synthetic", value: path}
	if err := SetEnabled(it, false); err != nil {
		t.Fatal(err)
	}
	if state, ok := regDword(path, "State"); !ok || state != startupTaskDisabledByUser {
		t.Fatalf("disabled state=%d ok=%v", state, ok)
	}
	if _, ok := regDword(path, "LastDisabledTime"); !ok {
		t.Fatal("LastDisabledTime was not recorded")
	}
	if err := SetEnabled(it, true); err != nil {
		t.Fatal(err)
	}
	if state, ok := regDword(path, "State"); !ok || state != startupTaskEnabled {
		t.Fatalf("enabled state=%d ok=%v", state, ok)
	}
	if once, ok := regDword(path, "UserEnabledStartupOnce"); !ok || once != 1 {
		t.Fatalf("UserEnabledStartupOnce=%d ok=%v", once, ok)
	}
	bad := it
	bad.value = `Software\Elsewhere\Nope`
	if err := SetEnabled(bad, false); err == nil || !strings.Contains(err.Error(), "packaged startup") {
		t.Fatalf("foreign path accepted: %v", err)
	}
}

func TestPackagedTaskItemStates(t *testing.T) {
	family := "DumpingGround.States_abcdefghijk"
	defer exec.Command("reg", "delete", `HKCU\`+appxStateKey+`\`+family, "/f").Run()
	for _, tc := range []struct {
		state    uint32
		enabled  bool
		locked   bool
		taskName string
	}{
		{startupTaskDisabled, false, false, "Off"},
		{startupTaskDisabledByUser, false, false, "OffByUser"},
		{startupTaskEnabled, true, false, "On"},
		{startupTaskDisabledByPolicy, false, true, "OffPolicy"},
		{startupTaskEnabledByPolicy, true, true, "OnPolicy"},
		{99, false, false, "Missing"},
	} {
		if tc.state != 99 {
			path := packagedTaskStatePath(family, tc.taskName)
			h, err := createKey(syscall.HKEY_CURRENT_USER, path)
			if err != nil {
				t.Skip(err)
			}
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], tc.state)
			if err := regSet(h, "State", syscall.REG_DWORD, b[:]); err != nil {
				syscall.RegCloseKey(h)
				t.Fatal(err)
			}
			syscall.RegCloseKey(h)
		}
		it := packagedTaskItem(packagedTask{
			pkg:        packagedApp{family: family, root: `C:\Pkg`},
			display:    tc.taskName,
			executable: "app.exe",
			taskID:     tc.taskName,
		}, nil)
		if it.Enabled != tc.enabled || (it.Locked != "") != tc.locked {
			t.Errorf("state %d: enabled=%v locked=%q", tc.state, it.Enabled, it.Locked)
		}
		if tc.locked {
			if err := SetEnabled(it, !tc.enabled); err == nil || !strings.Contains(err.Error(), "administrator policy") {
				t.Errorf("state %d accepted a policy-controlled switch: %v", tc.state, err)
			}
			if got, ok := regDword(packagedTaskStatePath(family, tc.taskName), "State"); !ok || got != tc.state {
				t.Errorf("state %d changed behind policy: got=%d ok=%v", tc.state, got, ok)
			}
		}
	}
}

func TestPackagedTaskItemsDedupesPackageVersions(t *testing.T) {
	const manifest = `<?xml version="1.0"?>
<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10" xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10" xmlns:uap5="http://schemas.microsoft.com/appx/manifest/uap/windows10/5">
  <Applications><Application Executable="app.exe"><uap:VisualElements DisplayName="Dup" />
    <Extensions><uap5:Extension Category="windows.startupTask"><uap5:StartupTask TaskId="App" Enabled="true" DisplayName="Dup" /></uap5:Extension></Extensions>
  </Application></Applications>
</Package>`
	family := "DumpingGround.Dedupe_abcdefghijk"
	var pkgs []packagedApp
	for _, version := range []string{"1.0.0.0", "2.0.0.0"} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "AppxManifest.xml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, packagedApp{
			fullName: fmt.Sprintf("DumpingGround.Dedupe_%s_x64__abcdefghijk", version),
			family:   family,
			root:     root,
		})
	}
	items := packagedTaskItems(pkgs, nil)
	if len(items) != 1 || items[0].Key != "packaged|"+family+"|App" {
		t.Fatalf("deduped packaged items = %+v", items)
	}
}

// TestLivePackagedItems documents this machine's ChatGPT package entry when
// present; other machines simply have no such package and skip.
func TestLivePackagedItems(t *testing.T) {
	var app, svc *Item
	items := List()
	for i := range items {
		if items[i].Kind == KindPackaged && items[i].Package == "OpenAI.Codex_2p2nqsd0c76g0" {
			app = &items[i]
		}
		if items[i].Kind == KindService && items[i].value == "CodexSandboxService.OpenAI.Codex" {
			svc = &items[i]
		}
	}
	if app == nil {
		t.Skip("OpenAI.Codex packaged startup task not installed")
	}
	if !app.Enabled || !app.App() || app.Locked != "" {
		t.Fatalf("packaged startup item: %+v", app)
	}
	if svc == nil || svc.Package != app.Package {
		t.Fatalf("packaged service: %+v, app: %+v", svc, app)
	}
}
