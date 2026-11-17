package sshkeys

import (
	"strings"
	"testing"
	"time"
)

const (
	addonBlob = "AAAAC3NzaC1lZDI1NTE5AAAAIEFSx3g/oLwaC84xZMOZIv9tk8m/eImdz0UxBwEjrnMj"
	addonLine = `restrict,pty,from="127.0.0.1,::1",expiry-time="20261117213000Z" ssh-ed25519 ` + addonBlob + ` openccu-lite-addon:shell`
)

// occulited task 152: an addon's key is written with the system's options and nothing of the
// pasted line but the key
func TestForAddonWritesTheSystemsLine(t *testing.T) {
	k, err := Parse("ssh-ed25519 " + addonBlob + ` evil",command="sh`)
	if err != nil {
		t.Fatal(err)
	}
	// a local time: the line is UTC
	until := time.Date(2026, 11, 17, 22, 30, 0, 999, time.FixedZone("CET", 3600))
	a, err := ForAddon(k, "shell", until)
	if err != nil {
		t.Fatal(err)
	}
	if a.Line() != addonLine {
		t.Errorf("line:\n%s\nwant:\n%s", a.Line(), addonLine)
	}
	if a.Addon != "shell" || a.ArmedUntil != "2026-11-17T21:30:00Z" || a.Comment != "openccu-lite-addon:shell" || a.Fingerprint != k.Fingerprint ||
		a.Options != `restrict,pty,from="127.0.0.1,::1",expiry-time="20261117213000Z"` {
		t.Errorf("%+v", a)
	}
	for name, id := range map[string]string{"empty": "", "a blank": "a b", "a quote": `a"b`, "a slash": "a/b", "a leading dot": ".a", "too long": strings.Repeat("a", 33), "a line break": "a\nb"} {
		if _, err := ForAddon(k, id, until); err == nil {
			t.Errorf("%s: taken as an addon id", name)
		}
	}
	rsa, err := Parse(rsa3072Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ForAddon(rsa, "shell", until); err == nil {
		t.Error("an RSA key became an addon's key")
	}
}

// the section's reader takes exactly the system's line as an addon's key; anything else with
// options is no line of the section
func TestParseSectionAddonLine(t *testing.T) {
	k, err := ParseSection(addonLine)
	if err != nil {
		t.Fatal(err)
	}
	if k.Addon != "shell" || k.ArmedUntil != "2026-11-17T21:30:00Z" || k.Line() != addonLine {
		t.Errorf("%+v\n%s", k, k.Line())
	}
	// a key without options stays what it was, whatever its comment says
	plain, err := ParseSection("ssh-ed25519 " + addonBlob + " openccu-lite-addon:shell")
	if err != nil || plain.Addon != "" || plain.Options != "" || plain.ArmedUntil != "" {
		t.Errorf("a plain key with the comment: %v %+v", err, plain)
	}
	for name, line := range map[string]string{
		"no expiry":           `restrict,pty,from="127.0.0.1,::1" ssh-ed25519 ` + addonBlob + ` openccu-lite-addon:shell`,
		"another from":        strings.Replace(addonLine, `127.0.0.1,::1`, `127.0.0.1,::1,192.0.2.1`, 1),
		"a wildcard from":     strings.Replace(addonLine, `127.0.0.1,::1`, `*`, 1),
		"no restrict":         strings.TrimPrefix(addonLine, "restrict,"),
		"one more option":     strings.Replace(addonLine, `restrict,pty,`, `restrict,pty,port-forwarding,`, 1),
		"an option after":     strings.Replace(addonLine, `Z" ssh`, `Z",agent-forwarding ssh`, 1),
		"a command":           strings.Replace(addonLine, `restrict,`, `command="sh",restrict,`, 1),
		"no Z":                strings.Replace(addonLine, `3000Z"`, `3000"`, 1),
		"a short time":        strings.Replace(addonLine, `20261117213000Z`, `202611172130Z`, 1),
		"no time at all":      strings.Replace(addonLine, `20261117213000Z`, `20261340213000Z`, 1),
		"another key type":    strings.Replace(addonLine, "ssh-ed25519 "+addonBlob, strings.Join(strings.Fields(rsa3072Key)[:2], " "), 1),
		"a broken key":        strings.Replace(addonLine, addonBlob, "AAAA", 1),
		"another comment":     strings.Replace(addonLine, "openccu-lite-addon:shell", "laptop", 1),
		"words after the id":  addonLine + " more",
		"a bad id":            strings.Replace(addonLine, "addon:shell", "addon:.shell", 1),
		"two blanks":          strings.Replace(addonLine, `Z" ssh`, `Z"  ssh`, 1),
		"upper case":          strings.Replace(addonLine, "restrict", "Restrict", 1),
		"a quote in the id":   strings.Replace(addonLine, "addon:shell", `addon:sh"ell`, 1),
		"a second line":       addonLine + "\n" + addonLine,
		"options, plain key":  `from="127.0.0.1" ssh-ed25519 ` + addonBlob + ` laptop`,
		"expiry on any key":   `expiry-time="20261117213000Z" ssh-ed25519 ` + addonBlob + ` openccu-lite-addon:shell`,
		"a tab for the blank": strings.Replace(addonLine, `Z" ssh`, "Z\"\tssh", 1),
	} {
		if k, err := ParseSection(line); err == nil {
			t.Errorf("%s: taken: %+v", name, k)
		}
	}
}

// Read lists the addon's line as the addon's, and an edited one not at all (it is no line of the
// section and goes at the next write); WithSection writes it back as it was
func TestReadAndWriteAnAddonLine(t *testing.T) {
	edited := strings.Replace(addonLine, `restrict,pty,`, `pty,`, 1)
	file := Begin + "\n" + rsa3072Key + "\n" + addonLine + "\n" + edited + "\n" + End + "\n" + addonLine + "\n"
	f := Read([]byte(file))
	if len(f.Managed) != 2 || f.Managed[0].Addon != "" || f.Managed[1].Addon != "shell" || f.Managed[1].ArmedUntil != "2026-11-17T21:30:00Z" {
		t.Fatalf("managed: %+v", f.Managed)
	}
	// outside the section the same line is an ordinary key with options: not the system's
	if len(f.Other) != 1 || f.Other[0].Addon != "" || f.Other[0].ArmedUntil != "" || !strings.HasPrefix(f.Other[0].Options, "restrict,pty,") {
		t.Fatalf("other: %+v", f.Other)
	}
	out := string(WithSection([]byte(file), f.Managed))
	if out != addonLine+"\n\n"+Begin+"\n"+rsa3072Key+"\n"+addonLine+"\n"+End+"\n" {
		t.Errorf("written:\n%s", out)
	}
}
