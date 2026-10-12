//go:build windows

package startup

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
)

// taskXML is the part of a Task Scheduler definition that matters here.
// Element names are matched without their namespace.
type taskXML struct {
	Info struct {
		Author string `xml:"Author"`
		Desc   string `xml:"Description"`
	} `xml:"RegistrationInfo"`
	Triggers struct {
		Logon []struct {
			Enabled *bool `xml:"Enabled"`
		} `xml:"LogonTrigger"`
		Boot []struct {
			Enabled *bool `xml:"Enabled"`
		} `xml:"BootTrigger"`
	} `xml:"Triggers"`
	Settings struct {
		Enabled *bool `xml:"Enabled"`
	} `xml:"Settings"`
	Actions struct {
		Exec []struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
		} `xml:"Exec"`
		COM []struct {
			ClassId string `xml:"ClassId"`
		} `xml:"ComHandler"`
	} `xml:"Actions"`
}

// decodeTaskFile turns a Task Scheduler file into UTF-8 text. Windows
// writes them as UTF-16 with a BOM; older or hand-made ones may be UTF-8.
func decodeTaskFile(b []byte) []byte {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
		}
		b = []byte(string(utf16.Decode(u)))
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	// encoding/xml rejects a UTF-16 declaration on text that is now UTF-8.
	if i := bytes.Index(b, []byte("?>")); bytes.HasPrefix(b, []byte("<?xml")) && i > 0 {
		b = b[i+2:]
	}
	return b
}

func enabledOr(b *bool) bool { return b == nil || *b }

// parseTask reads one definition and reports its logon/boot behaviour;
// ok is false for tasks that never fire at logon or boot.
func parseTask(raw []byte) (t taskXML, trigger string, ok bool) {
	if err := xml.Unmarshal(decodeTaskFile(raw), &t); err != nil {
		return t, "", false
	}
	var logon, boot bool
	for _, l := range t.Triggers.Logon {
		logon = logon || enabledOr(l.Enabled)
	}
	for _, b := range t.Triggers.Boot {
		boot = boot || enabledOr(b.Enabled)
	}
	switch {
	case logon && boot:
		return t, "At startup and sign-in", true
	case logon:
		return t, "At sign-in", true
	case boot:
		return t, "At startup", true
	}
	return t, "", false
}

// listTasks reads the Task Scheduler store directly: every task is an XML
// file under System32\Tasks. Only tasks that fire at logon or boot are
// startup items. Needs administrator rights to read.
func listTasks(running map[string]bool) []Item {
	root := filepath.Join(os.Getenv("SystemRoot"), `System32\Tasks`)
	var items []Item
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		t, trigger, ok := parseTask(raw)
		if !ok {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		key := `\` + rel
		it := Item{
			Kind: KindTask, Key: "task|" + key, Name: d.Name(), Location: "Task Scheduler " + key,
			Scope: "System", Trigger: trigger, Enabled: enabledOr(t.Settings.Enabled),
			Windows: strings.HasPrefix(strings.ToLower(key), `\microsoft\windows\`),
			value:   key, Publisher: cleanAuthor(t.Info.Author), Desc: strings.TrimSpace(t.Info.Desc),
		}
		switch {
		case len(t.Actions.Exec) > 0:
			a := t.Actions.Exec[0]
			cmd := strings.TrimSpace(a.Command)
			it.Exe, _ = splitCommand(`"` + strings.Trim(cmd, `"`) + `"`)
			it.Command = strings.TrimSpace(cmd + " " + strings.TrimSpace(a.Arguments))
		case len(t.Actions.COM) > 0:
			it.Command = "COM handler " + strings.TrimSpace(t.Actions.COM[0].ClassId)
		}
		it.Running = it.Exe != "" && running[strings.ToLower(it.Exe)]
		items = append(items, it)
		return nil
	})
	return items
}

// cleanAuthor drops resource references like $(@%SystemRoot%\...,-100)
// that Task Scheduler stores instead of a name.
func cleanAuthor(a string) string {
	a = strings.TrimSpace(a)
	// A resource reference, or a MACHINE\user account — neither is a maker.
	if strings.HasPrefix(a, "$(@") || strings.HasPrefix(a, "@") || strings.Contains(a, `\`) {
		return ""
	}
	return a
}

// setTaskEnabled asks Task Scheduler itself to flip the task — the same
// call as Task Scheduler's own Disable/Enable.
func setTaskEnabled(it Item, on bool) error {
	if !strings.HasPrefix(it.value, `\`) || strings.ContainsAny(it.value, "\x00\r\n") {
		return fmt.Errorf("refusing task name %q", it.value)
	}
	flag := "/DISABLE"
	if on {
		flag = "/ENABLE"
	}
	cmd := exec.Command("schtasks", "/Change", "/TN", it.value, flag)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		// schtasks answers in the console's OEM code page; unreadable text
		// is worse than the exit status.
		if msg == "" || !utf8.ValidString(msg) {
			msg = err.Error()
		}
		return errors.New("schtasks: " + msg)
	}
	return nil
}
