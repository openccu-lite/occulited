package radio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// occulited B-175: after the switch from an OpenCCU without a radio module, HmIP is set to none once,
// so hmipserver runs the virtual devices with HMServer.jar instead of failing for want of a module.
func TestRadiolessSwitch(t *testing.T) {
	type setup struct {
		regadom, identity, device, marker bool
		conf                              string
	}
	cases := []struct {
		name string
		s    setup
		want bool
	}{
		{"a radio-less OpenCCU switched", setup{regadom: true}, true},
		{"its hmip_user.conf has other keys, kept", setup{regadom: true, conf: "Adapter.1.Foo=bar\n"}, true},
		{"a fresh installation without its stick: no ReGa database", setup{}, false},
		{"a CCU3 whose module is not found: its access point identity", setup{regadom: true, identity: true}, false},
		{"paired HmIP devices", setup{regadom: true, device: true}, false},
		{"the user chose a module", setup{regadom: true, conf: HmIPChoiceKey + "=0000000FFF\n"}, false},
		{"none already", setup{regadom: true, conf: HmIPChoiceKey + "=none\n"}, false},
		{"done once, the user set automatic again", setup{regadom: true, marker: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			put := func(rel, content string) {
				t.Helper()
				p := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if c.s.regadom {
				put("etc/config/homematic.regadom", "regadom")
			}
			if c.s.identity {
				put("etc/config/crRFD/data/3014F711A0001F0000000A03.ap", "")
			}
			if c.s.device {
				put("etc/config/crRFD/data/3014F711A0001F0000000C06.dev", "")
			}
			if c.s.marker {
				put("usr/local/etc/occulite/radio-radioless-switch", "hmip=none\n")
			}
			put("etc/config/crRFD/data/.keep", "") // the directory the prep step makes is there after a first boot
			in := inputs()
			in.HmIPUserConf = c.s.conf
			var lines []string
			out, ok := RadiolessSwitch(Detector{Root: root}, in, func(f string, a ...any) { lines = append(lines, f) })
			if ok != c.want {
				t.Fatalf("changed %v, want %v (%v)", ok, c.want, lines)
			}
			file := readFile(filepath.Join(root, "etc/config/crRFD/hmip_user.conf"))
			if !c.want {
				if file != "" || out.HmIPUserConf != c.s.conf {
					t.Fatalf("nothing should change: file %q, inputs %q", file, out.HmIPUserConf)
				}
				return
			}
			if ReadChoices(file, "").HmIP != HmIPNone || out.HmIPUserConf != file || !strings.HasPrefix(file, c.s.conf) {
				t.Fatalf("hmip_user.conf %q, inputs %q", file, out.HmIPUserConf)
			}
			if p := MakePlan(out); !p.HmIPOff() || p.NoHmIPModule() || p.HmIPServerJar != JarHMServer {
				t.Fatalf("the plan: %+v", p)
			}
			if !exists(filepath.Join(root, "usr/local/etc/occulite/radio-radioless-switch")) || len(lines) != 1 || !strings.Contains(lines[0], "HmIP-RF set to none") {
				t.Fatalf("marker or log: %v", lines)
			}
			// once: a second run changes nothing, also when the user set automatic again
			in.HmIPUserConf = ""
			if _, again := RadiolessSwitch(Detector{Root: root}, in, func(string, ...any) {}); again {
				t.Fatal("the second run changed it again")
			}
		})
	}
	// a ReGa database that is not a regular file is no switch
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc/config/homematic.regadom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := RadiolessSwitch(Detector{Root: root}, inputs(), func(string, ...any) {}); ok {
		t.Fatal("a directory at the database's path")
	}
	// the file cannot be written: nothing changes, the reason is logged
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc/config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/config/homematic.regadom"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/config/crRFD"), []byte("a file, not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	var lines []string
	if _, ok := RadiolessSwitch(Detector{Root: root}, inputs(), func(f string, a ...any) { lines = append(lines, f) }); ok || len(lines) != 1 {
		t.Fatalf("unwritable: %v %v", ok, lines)
	}
}
