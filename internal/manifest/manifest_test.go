package manifest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const good = `{
  "format": 1,
  "id": "mosquitto",
  "version": "2.1.2+3",
  "name": "Mosquitto",
  "description": {"de": "Der MQTT-Broker.", "en": "The MQTT broker."},
  "homepage": "https://github.com/homematic-community/ccu-addon-mosquitto",
  "changelog": "https://github.com/homematic-community/ccu-addon-mosquitto/blob/master/CHANGELOG.md",
  "release": {"github": "homematic-community/ccu-addon-mosquitto", "asset": "mosquitto-{arch}-{version}.tar.gz", "fallback_asset": "mosquitto-{version}.tar.gz"},
  "requires": {"architectures": ["armv7l", "aarch64", "x86_64"]},
  "ui": {"session_header": true, "fullscreen": true},
  "runtime": {
    "needs": [],
    "ports": [1883, 8883],
    "port_info": {"8883": {"proto": "tcp", "tls": true, "label": {"de": "MQTT über TLS", "en": "MQTT over TLS"}}},
    "note": {"de": "Beide Ports sind geschlossen.", "en": "Both ports stay closed."}
  }
}`

func TestParse(t *testing.T) {
	m, err := Parse([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "mosquitto" || m.Name.In("de") != "Mosquitto" || m.Description.In("de") != "Der MQTT-Broker." || m.Description.In("fr") != "The MQTT broker." {
		t.Fatalf("%+v", m)
	}
	if m.Release == nil || m.Release.GitHub != "homematic-community/ccu-addon-mosquitto" || !m.UI.SessionHeader || !m.UI.Fullscreen {
		t.Fatalf("%+v %+v", m.Release, m.UI)
	}
	// occulited task 24: ui.fullscreen is false unless declared
	if m2, err := Parse([]byte(`{"format": 1, "id": "x", "name": "X", "ui": {"session_header": true}}`)); err != nil || m2.UI.Fullscreen {
		t.Fatalf("fullscreen undeclared: %v %+v", err, m2.UI)
	}
	if m.Runtime == nil || m.Runtime.Needs == nil || len(*m.Runtime.Needs) != 0 || !m.Runtime.PortInfo["8883"].TLS || m.Runtime.PortInfo["8883"].Label.In("en") != "MQTT over TLS" {
		t.Fatalf("%+v", m.Runtime)
	}
	if !m.SupportsArch("x86_64") || m.SupportsArch("mips") || m.NeedsRega() {
		t.Fatal("arch or rega")
	}
	// a key this binary does not know is a later format's: ignored, not refused
	if _, err := Parse([]byte(`{"format": 1, "id": "x", "name": "X", "later": {"k": 1}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestParseRefuses(t *testing.T) {
	cases := map[string]string{
		"format":        `{"format": 2, "id": "x", "name": "X"}`,
		"id upper case": `{"format": 1, "id": "Mosquitto", "name": "X"}`,
		"id empty":      `{"format": 1, "name": "X"}`,
		"name empty":    `{"format": 1, "id": "x", "name": {"de": " "}}`,
		"homepage":      `{"format": 1, "id": "x", "name": "X", "homepage": "javascript:alert(1)"}`,
		"changelog":     `{"format": 1, "id": "x", "name": "X", "changelog": "javascript:alert(1)"}`,
		"release":       `{"format": 1, "id": "x", "name": "X", "release": {"github": "nobody"}}`,
		"no asset":      `{"format": 1, "id": "x", "name": "X", "release": {"github": "a/b"}}`,
		"settings_url":  `{"format": 1, "id": "x", "name": "X", "ui": {"settings_url": "/system/users"}}`,
		"icon path":     `{"format": 1, "id": "x", "name": "X", "ui": {"icon": "../etc/passwd"}}`,
		"capability":    `{"format": 1, "id": "x", "name": "X", "runtime": {"capabilities": ["sys_admin"]}}`,
		"group":         `{"format": 1, "id": "x", "name": "X", "runtime": {"groups": ["Root"]}}`,
		"path":          `{"format": 1, "id": "x", "name": "X", "runtime": {"paths": ["etc"]}}`,
		"data_dir":      `{"format": 1, "id": "x", "name": "X", "runtime": {"data_dirs": ["/etc/config"]}}`,
		"port":          `{"format": 1, "id": "x", "name": "X", "runtime": {"ports": [0]}}`,
		"port_info":     `{"format": 1, "id": "x", "name": "X", "runtime": {"ports": [1883], "port_info": {"8883": {}}}}`,
		// openccu-lite task 105: a range's "to" lies above the port and at most at 65535
		"port_info to":  `{"format": 1, "id": "x", "name": "X", "runtime": {"ports": [51800], "port_info": {"51800": {"to": 51800}}}}`,
		"port_info to2": `{"format": 1, "id": "x", "name": "X", "runtime": {"ports": [51800], "port_info": {"51800": {"to": 70000}}}}`,
		"start":         `{"format": 1, "id": "x", "name": "X", "runtime": {"start": "late"}}`,
		"scope":         `{"format": 1, "id": "x", "name": "X", "runtime": {"api_scopes": ["*"]}}`,
		"not json":      `{`,
		// B-251: a confined addon (no "root": true) may not declare a root-equivalent capability
		// or the occulite/root group.
		"confined cap sys_admin":  `{"format": 1, "id": "x", "name": "X", "runtime": {"capabilities": ["CAP_SYS_ADMIN"]}}`,
		"confined cap dac read":   `{"format": 1, "id": "x", "name": "X", "runtime": {"capabilities": ["CAP_DAC_READ_SEARCH"]}}`,
		"confined cap net admin":  `{"format": 1, "id": "x", "name": "X", "runtime": {"capabilities": ["CAP_NET_ADMIN"]}}`,
		"confined group occulite": `{"format": 1, "id": "x", "name": "X", "runtime": {"groups": ["occulite"]}}`,
		"confined group root":     `{"format": 1, "id": "x", "name": "X", "runtime": {"groups": ["root"]}}`,
	}
	for name, body := range cases {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: accepted %s", name, body)
		}
	}
	if _, err := Parse(bytes.Repeat([]byte(" "), MaxSize+1)); err == nil {
		t.Error("an oversized manifest was accepted")
	}
}

// TestConfinedDenylist checks the B-251 denylist: a confined addon is refused a root-equivalent
// capability or group, while the same declaration is accepted for a root addon (the user's *root
// (unsafe)* choice), and a harmless capability or group is accepted either way.
func TestConfinedDenylist(t *testing.T) {
	accepted := []string{
		// harmless for a confined addon
		`{"format": 1, "id": "x", "name": "X", "runtime": {"capabilities": ["CAP_NET_BIND_SERVICE", "CAP_NET_RAW"]}}`,
		`{"format": 1, "id": "x", "name": "X", "runtime": {"groups": ["dialout", "video"]}}`,
		// a root addon may declare CAP_SYS_ADMIN (D-66) and any group - it runs as root
		`{"format": 1, "id": "x", "name": "X", "runtime": {"root": true, "capabilities": ["CAP_SYS_ADMIN"]}}`,
		`{"format": 1, "id": "x", "name": "X", "runtime": {"root": true, "groups": ["occulite"]}}`,
	}
	for _, body := range accepted {
		if _, err := Parse([]byte(body)); err != nil {
			t.Errorf("refused %s: %v", body, err)
		}
	}
	// every capability on the denylist is refused for a confined addon
	for _, c := range DeniedConfinedCaps() {
		body := fmt.Sprintf(`{"format": 1, "id": "x", "name": "X", "runtime": {"capabilities": [%q]}}`, c)
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("confined addon accepted denied capability %s", c)
		}
	}
	for _, g := range DeniedConfinedGroups() {
		body := fmt.Sprintf(`{"format": 1, "id": "x", "name": "X", "runtime": {"groups": [%q]}}`, g)
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("confined addon accepted denied group %s", g)
		}
	}
	if len(DeniedConfinedCaps()) < 14 {
		t.Errorf("the capability denylist shrank to %d; the maintainer's floor is 14", len(DeniedConfinedCaps()))
	}
}

func TestTextForms(t *testing.T) {
	var m Manifest
	if err := (&m).unmarshal(`{"format": 1, "id": "x", "name": {"en": "X", "de": "Y"}}`); err != nil {
		t.Fatal(err)
	}
	if m.Name.In("de") != "Y" || m.Name.In("en") != "X" || m.Name.In("") != "X" {
		t.Fatalf("%v", m.Name)
	}
	var none Text
	if none.In("de") != "" {
		t.Fatal("nil text")
	}
}

func (m *Manifest) unmarshal(s string) error {
	p, err := Parse([]byte(s))
	if err != nil {
		return err
	}
	*m = *p
	return nil
}

// archive builds a package: the entries in order, gzipped unless plain.
func archive(t *testing.T, plain bool, entries map[string]string, order ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	var w interface {
		Write([]byte) (int, error)
		Close() error
	}
	if plain {
		w = nopCloser{&buf}
	} else {
		w = gzip.NewWriter(&buf)
	}
	tw := tar.NewWriter(w)
	for _, name := range order {
		body := entries[name]
		typ := byte(tar.TypeReg)
		if strings.HasSuffix(name, "/") {
			typ = tar.TypeDir
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: typ}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

func TestFromArchive(t *testing.T) {
	files := map[string]string{"update_script": "#!/bin/sh\n", "mosquitto/": "", "mosquitto/bin/x": "bin", FileName: good, "mosquitto/" + FileName: `{"format": 1, "id": "wrong", "name": "not at the root"}`}
	cases := []struct {
		name  string
		plain bool
		order []string
		want  string
		err   error
	}{
		{"the manifest first", false, []string{FileName, "update_script"}, "mosquitto", nil},
		{"the manifest last, behind the tree", false, []string{"update_script", "mosquitto/", "mosquitto/bin/x", "mosquitto/" + FileName, FileName}, "mosquitto", nil},
		{"a plain tar", true, []string{"update_script", FileName}, "mosquitto", nil},
		{"./ prefix", false, []string{"./update_script", "./" + FileName}, "mosquitto", nil},
		{"none at the root: the copy inside the tree does not count", false, []string{"update_script", "mosquitto/" + FileName}, "", ErrNoManifest},
		{"empty archive", false, nil, "", ErrNoManifest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entries := map[string]string{}
			for k, v := range files {
				entries[k] = v
				entries["./"+k] = v
			}
			m, err := FromArchive(bytes.NewReader(archive(t, c.plain, entries, c.order...)))
			if !errors.Is(err, c.err) {
				t.Fatalf("err %v, want %v", err, c.err)
			}
			if c.want != "" && (m == nil || m.ID != c.want) {
				t.Fatalf("%+v", m)
			}
		})
	}
	// a broken manifest is an error of its own, not "none"
	if _, err := FromArchive(bytes.NewReader(archive(t, false, map[string]string{FileName: `{"format": 1}`}, FileName))); err == nil || errors.Is(err, ErrNoManifest) {
		t.Fatalf("a broken manifest: %v", err)
	}
	// not an archive at all: no manifest (the installer says what is wrong with the package)
	if _, err := FromArchive(strings.NewReader("hello")); !errors.Is(err, ErrNoManifest) {
		t.Fatalf("a non-archive: %v", err)
	}
	// the file variant
	path := filepath.Join(t.TempDir(), "p.tar.gz")
	if err := os.WriteFile(path, archive(t, false, map[string]string{FileName: good}, FileName), 0o644); err != nil {
		t.Fatal(err)
	}
	if m, err := FromArchiveFile(path); err != nil || m.ID != "mosquitto" {
		t.Fatalf("%v %+v", err, m)
	}
}

// The repository's own catalogue and adapter manifests parse: what CI checks on every pull request
// against catalog/.
func TestRepositoryCatalogueParses(t *testing.T) {
	root := filepath.Join("..", "..", "catalog")
	entries, err := filepath.Glob(filepath.Join(root, "manifests", "*.json"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no adapter manifests: %v", err)
	}
	for _, p := range entries {
		m, err := ParseFile(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if want := strings.TrimSuffix(filepath.Base(p), ".json"); m.ID != want {
			t.Errorf("%s: id %q, want the file name", p, m.ID)
		}
		if m.Release == nil {
			t.Errorf("%s: an adapter manifest names the author's release source", p)
		}
	}
}

// openccu-lite task 406: runtime.web_ingress - the ranges an ingress addon may claim.
func TestWebIngress(t *testing.T) {
	with := func(ranges string) string {
		return strings.Replace(good, `"needs": [],`, `"needs": [], "web_ingress": {"ranges": `+ranges+`},`, 1)
	}
	m, err := Parse([]byte(with(`["10.77.0.0/24", "fd7a:115c:a1e0::/48"]`)))
	if err != nil {
		t.Fatal(err)
	}
	ps, err := m.Runtime.WebIngress.Prefixes()
	if err != nil || len(ps) != 2 || ps[0].String() != "10.77.0.0/24" || ps[1].String() != "fd7a:115c:a1e0::/48" {
		t.Fatalf("prefixes %v %v", ps, err)
	}
	if ps, err := (*WebIngress)(nil).Prefixes(); ps != nil || err != nil {
		t.Fatal("nil ingress")
	}
	for _, c := range []struct{ ranges, want string }{
		{`[]`, "1 to 8"},
		{`["10.77.0.1/24"]`, "not a network address"},
		{`["10.0.0.0/7"]`, "too wide"},
		{`["0.0.0.0/0"]`, "too wide"},
		{`["::/0"]`, "too wide"},
		{`["fd00::/16"]`, "too wide"},
		{`["127.0.0.0/24"]`, "never claim"},
		{`["127.0.0.1/32"]`, "never claim"},
		{`["::1/128"]`, "never claim"},
		{`["169.254.0.0/16"]`, "never claim"},
		{`["fe80::/64"]`, "never claim"},
		{`["224.0.0.0/8"]`, "never claim"},
		{`["::ffff:10.77.0.0/120"]`, "IPv4 range as IPv4"},
		{`["10.77.0.0"]`, "not a CIDR"},
		{`["x/24"]`, "not a CIDR"},
		{`["10.1.0.0/24","10.2.0.0/24","10.3.0.0/24","10.4.0.0/24","10.5.0.0/24","10.6.0.0/24","10.7.0.0/24","10.8.0.0/24","10.9.0.0/24"]`, "1 to 8"},
	} {
		_, err := Parse([]byte(with(c.ranges)))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.ranges, err, c.want)
		}
	}
	// 100.64.0.0/10 (Tailscale) and the private ranges a VPN hands out are allowed
	for _, ok := range []string{`["100.64.0.0/10"]`, `["192.168.77.0/24"]`, `["172.16.0.0/12"]`, `["fd7a:115c:a1e0::/48"]`} {
		if _, err := Parse([]byte(with(ok))); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	// a manifest without it: nil
	if m, _ := Parse([]byte(good)); m.Runtime.WebIngress != nil {
		t.Fatal("web_ingress set without a declaration")
	}
}

// openccu-lite task 399: the backup block - hook and staging inside the tree, the hook never inside
// the staging (emptied after every backup), the timeout within its bounds.
func TestBackupBlock(t *testing.T) {
	with := func(block string) string {
		return strings.Replace(good, `"runtime": {`, `"backup": `+block+`, "runtime": {`, 1)
	}
	for _, c := range []struct {
		block string
		ok    bool
		key   string
	}{
		{`{"hook": "bin/backup-snapshot", "staging": "var/backup"}`, true, ""},
		{`{"hook": "bin/backup-snapshot", "staging": "var/backup", "timeout": 600, "size_hint_mb": 50, "restore": "bin/backup-restore"}`, true, ""},
		{`{"hook": "bin/backup-snapshot", "staging": "var/backup", "timeout": 10}`, true, ""},
		{`{"hook": "bin/backup-snapshot", "staging": "var/backup", "timeout": 1800}`, true, ""},
		{`{"hook": "bin/backup-snapshot", "staging": "var/backup", "size_hint_mb": 0}`, true, ""},
		{`{"staging": "var/backup"}`, false, "backup needs hook and staging"},
		{`{"hook": "bin/backup-snapshot"}`, false, "backup needs hook and staging"},
		{`{"hook": "/usr/local/addons/x/bin/snap", "staging": "var/backup"}`, false, "backup.hook"},
		{`{"hook": "bin/../../snap", "staging": "var/backup"}`, false, "backup.hook"},
		{`{"hook": "bin/snap", "staging": "var/backup/"}`, false, "backup.staging"},
		{`{"hook": "bin/snap", "staging": "../var"}`, false, "backup.staging"},
		{`{"hook": "var/backup/snap", "staging": "var/backup"}`, false, "lies inside backup.staging"},
		{`{"hook": "var/backup", "staging": "var/backup"}`, false, "lies inside backup.staging"},
		{`{"hook": "var/backups/snap", "staging": "var/backup"}`, true, ""}, // a sibling, not inside
		{`{"hook": "bin/snap", "staging": "var/backup", "restore": "var/backup/restore"}`, false, "backup.restore"},
		{`{"hook": "bin/snap", "staging": "var/backup", "restore": "./restore"}`, false, "backup.restore"},
		{`{"hook": "bin/snap", "staging": "var/backup", "timeout": 5}`, false, "backup.timeout"},
		{`{"hook": "bin/snap", "staging": "var/backup", "timeout": 1801}`, false, "backup.timeout"},
		{`{"hook": "bin/snap", "staging": "var/backup", "timeout": -1}`, false, "backup.timeout"},
		{`{"hook": "bin/snap", "staging": "var/backup", "size_hint_mb": -1}`, false, "backup.size_hint_mb"},
	} {
		m, err := Parse([]byte(with(c.block)))
		if c.ok != (err == nil) {
			t.Errorf("%s: %v", c.block, err)
			continue
		}
		if err != nil && !strings.Contains(err.Error(), c.key) {
			t.Errorf("%s: the error does not say %q: %v", c.block, c.key, err)
		}
		if c.ok && (m.Backup == nil || m.Backup.Hook == "" || m.Backup.Staging == "") {
			t.Errorf("%s: read as %+v", c.block, m.Backup)
		}
	}
	m, _ := Parse([]byte(good))
	if m.Backup != nil || m.Backup.TimeoutSeconds() != BackupHookTimeout || m.Backup.Large() {
		t.Fatal("a manifest without the block")
	}
	m, _ = Parse([]byte(with(`{"hook": "bin/snap", "staging": "var/backup", "timeout": 30, "size_hint_mb": 101}`)))
	if m.Backup.TimeoutSeconds() != 30 || !m.Backup.Large() {
		t.Fatalf("timeout and large: %+v", m.Backup)
	}
	if !ValidRelPath("bin/snap") || ValidRelPath("/bin") || ValidRelPath("a/../b") || ValidRelPath("") {
		t.Fatal("ValidRelPath")
	}
}

// occulited task 136: runtime.bin - a relative directory inside the addon's tree, of characters
// that need no quoting in the profile snippet.
func TestRuntimeBin(t *testing.T) {
	with := func(bin string) string {
		return strings.Replace(good, `"needs": [],`, `"needs": [], "bin": `+bin+`,`, 1)
	}
	for _, c := range []struct {
		bin string
		ok  bool
	}{
		{`"bin"`, true},
		{`"usr/bin"`, true},
		{`"opt/tools-2.1/bin"`, true},
		{`"_bin"`, true},
		{`"bin/.hidden"`, true},
		{`""`, true}, // the same as none
		{`"/usr/bin"`, false},
		{`"../other/bin"`, false},
		{`"bin/../../etc"`, false},
		{`"bin/.."`, false},
		{`"a..b"`, false},
		{`"."`, false},
		{`"./bin"`, false},
		{`"bin/./x"`, false},
		{`"bin/"`, false},
		{`"bin//x"`, false},
		{`"-bin"`, false},
		{`".bin"`, false},
		{`"b in"`, false},
		{`"bin\"x"`, false},
		{`"$(id)"`, false},
		{`"bin;id"`, false},
		{`"bin\nx"`, false},
		{`"` + strings.Repeat("a", 202) + `"`, false},
		{`["bin"]`, false},
	} {
		m, err := Parse([]byte(with(c.bin)))
		if c.ok != (err == nil) {
			t.Errorf("%s: %v", c.bin, err)
			continue
		}
		if c.ok && c.bin != `""` && `"`+m.Runtime.Bin+`"` != c.bin {
			t.Errorf("%s: read as %q", c.bin, m.Runtime.Bin)
		}
		if err != nil && c.bin != `["bin"]` && !strings.Contains(err.Error(), "runtime.bin") {
			t.Errorf("%s: the error does not name the key: %v", c.bin, err)
		}
	}
	if m, _ := Parse([]byte(good)); m.Runtime.Bin != "" {
		t.Fatal("bin set without a declaration")
	}
}

// occulited task 154: runtime.timers names the program an addon's own timers run - a plain file
// name in the root-owned bin directory and a few plain arguments, nothing a unit file expands.
func TestRuntimeTimers(t *testing.T) {
	with := func(extra string) string {
		return strings.Replace(good, `"needs": [],`, `"needs": [], `+extra+`,`, 1)
	}
	for _, c := range []struct {
		extra string
		ok    bool
	}{
		{`"bin": "bin", "timers": {"exec": "lite-sync", "args": ["run"]}`, true},
		{`"bin": "bin", "timers": {"exec": "job"}`, true},
		{`"bin": "bin", "timers": {"exec": "job", "args": ["--mode=run", "a.b", "c_d", "x"]}, "read_addon_data": true`, true},
		{`"bin": "bin", "read_addon_data": true`, true},
		{`"timers": {"exec": "job"}`, false}, // no bin: nothing root-owned to run
		{`"bin": "bin", "timers": {}`, false},
		{`"bin": "bin", "timers": {"exec": ""}`, false},
		{`"bin": "bin", "timers": {"exec": "sub/job"}`, false},
		{`"bin": "bin", "timers": {"exec": "../job"}`, false},
		{`"bin": "bin", "timers": {"exec": ".."}`, false},
		{`"bin": "bin", "timers": {"exec": "-job"}`, false},
		{`"bin": "bin", "timers": {"exec": "job x"}`, false},
		{`"bin": "bin", "timers": {"exec": "job", "args": ["a b"]}`, false},
		{`"bin": "bin", "timers": {"exec": "job", "args": ["%i"]}`, false},
		{`"bin": "bin", "timers": {"exec": "job", "args": ["$HOME"]}`, false},
		{`"bin": "bin", "timers": {"exec": "job", "args": ["a;b"]}`, false},
		{`"bin": "bin", "timers": {"exec": "job", "args": ["a\nb"]}`, false},
		{`"bin": "bin", "timers": {"exec": "job", "args": [""]}`, false},
		{`"bin": "bin", "timers": {"exec": "job", "args": ["1", "2", "3", "4", "5"]}`, false},
	} {
		m, err := Parse([]byte(with(c.extra)))
		if c.ok != (err == nil) {
			t.Errorf("%s: %v", c.extra, err)
			continue
		}
		if err != nil && !strings.Contains(err.Error(), "runtime.timers") {
			t.Errorf("%s: the error does not name the key: %v", c.extra, err)
		}
		if c.ok && strings.Contains(c.extra, "read_addon_data") != m.Runtime.ReadAddonData {
			t.Errorf("%s: read_addon_data read as %v", c.extra, m.Runtime.ReadAddonData)
		}
	}
	if m, _ := Parse([]byte(good)); m.Runtime.Timers != nil || m.Runtime.ReadAddonData {
		t.Fatal("timers set without a declaration")
	}
}

// occulited task 150: backup.targets declares that the addon provides backup targets; such a block
// needs neither hook nor staging, and may stand beside them.
func TestBackupTargets(t *testing.T) {
	base := `{"format":1,"id":"sync","name":"Sync","backup":%s}`
	ok := []string{
		`{"targets":{"protocol":1,"label":{"de":"Cloud-Speicher","en":"Cloud storage"},"setup":"/addon-settings/sync#accounts"}}`,
		`{"targets":{"protocol":1,"label":"Cloud storage","setup":"/addons/sync/"}}`,
		`{"hook":"bin/snap","staging":"var/backup","targets":{"protocol":1,"label":{"en":"Cloud storage"},"setup":"/addons/sync/?tab=accounts"}}`,
	}
	for _, b := range ok {
		m, err := Parse([]byte(fmt.Sprintf(base, b)))
		if err != nil || m.Backup == nil || m.Backup.Targets == nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	if m, _ := Parse([]byte(fmt.Sprintf(base, ok[0]))); m.Backup.HasHook() {
		t.Error("targets alone count as a snapshot hook")
	}
	if m, _ := Parse([]byte(fmt.Sprintf(base, ok[2]))); !m.Backup.HasHook() {
		t.Error("a hook beside targets is lost")
	}
	bad := map[string]string{
		"protocol":       `{"targets":{"protocol":2,"label":{"en":"x"},"setup":"/addons/sync/"}}`,
		"no label":       `{"targets":{"protocol":1,"setup":"/addons/sync/"}}`,
		"long label":     `{"targets":{"protocol":1,"label":{"en":"` + strings.Repeat("x", 41) + `"},"setup":"/addons/sync/"}}`,
		"other site":     `{"targets":{"protocol":1,"label":{"en":"x"},"setup":"https://example.org/"}}`,
		"scheme-less":    `{"targets":{"protocol":1,"label":{"en":"x"},"setup":"//example.org/addons/x"}}`,
		"system page":    `{"targets":{"protocol":1,"label":{"en":"x"},"setup":"/system/backup"}}`,
		"dots":           `{"targets":{"protocol":1,"label":{"en":"x"},"setup":"/addons/../api/x"}}`,
		"hook, no stage": `{"hook":"bin/snap","targets":{"protocol":1,"label":{"en":"x"},"setup":"/addons/sync/"}}`,
		"empty":          `{}`,
	}
	for name, b := range bad {
		if _, err := Parse([]byte(fmt.Sprintf(base, b))); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// occulited task 169: runtime.rpc_interface - a name of its own, a loopback port, an optional path.
func TestRuntimeRPCInterface(t *testing.T) {
	with := func(extra string) string {
		return strings.Replace(good, `"needs": [],`, `"needs": [], `+extra+`,`, 1)
	}
	for _, c := range []struct {
		extra, url string
		ok         bool
	}{
		{`"rpc_interface": {"name": "Logic", "port": 39310}`, "xmlrpc://127.0.0.1:39310", true},
		{`"rpc_interface": {"name": "My_Logic-2", "port": 40000, "path": "/rpc", "protocol": "xmlrpc"}`, "xmlrpc://127.0.0.1:40000/rpc", true},
		{`"rpc_interface": {"name": "Logic", "port": 39310, "path": "/"}`, "xmlrpc://127.0.0.1:39310", true},
		{`"rpc_interface": {"name": "HmIP-RF", "port": 39310}`, "", false},
		{`"rpc_interface": {"name": "virtualdevices", "port": 39310}`, "", false},
		{`"rpc_interface": {"name": "1Logic", "port": 39310}`, "", false},
		{`"rpc_interface": {"name": "Lo gic", "port": 39310}`, "", false},
		{`"rpc_interface": {"name": "L", "port": 39310}`, "", false},
		{`"rpc_interface": {"name": "Logic", "port": 80}`, "", false},
		{`"rpc_interface": {"name": "Logic", "port": 70000}`, "", false},
		{`"rpc_interface": {"name": "Logic"}`, "", false},
		{`"rpc_interface": {"name": "Logic", "port": 39310, "path": "rpc"}`, "", false},
		{`"rpc_interface": {"name": "Logic", "port": 39310, "path": "/a b"}`, "", false},
		{`"rpc_interface": {"name": "Logic", "port": 39310, "path": "/../x"}`, "", false},
		{`"rpc_interface": {"name": "Logic", "port": 39310, "protocol": "binrpc"}`, "", false},
	} {
		m, err := Parse([]byte(with(c.extra)))
		if c.ok != (err == nil) {
			t.Errorf("%s: %v", c.extra, err)
			continue
		}
		if err != nil {
			if !strings.Contains(err.Error(), "runtime.rpc_interface") {
				t.Errorf("%s: %v", c.extra, err)
			}
			continue
		}
		if got := m.Runtime.RPCInterface.URL(); got != c.url {
			t.Errorf("%s: %s", c.extra, got)
		}
	}
}
