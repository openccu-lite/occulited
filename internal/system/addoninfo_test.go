package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// occulited task 181: an addon's `info` runs once while nothing about the addon changes - not at every
// warning evaluation and page request - and again when its script or directory changes, when told
// (an install, an update, enable or disable), and after AddonInfoTTL.
func TestAddonInfoKept(t *testing.T) {
	r := rootWith(t, map[string]string{"usr/local/addons/demo/VERSION": "1.0\n"})
	rcd := filepath.Join(string(r), "usr/local/etc/config/rc.d")
	_ = os.MkdirAll(rcd, 0o755)
	count := filepath.Join(string(r), "runs")
	script := filepath.Join(rcd, "demo")
	write := func(version string) {
		t.Helper()
		body := "#!/bin/sh\necho x >> " + count + "\ncase $1 in info) printf 'Name: Demo\\nVersion: " + version + "\\n';; esac\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runs := func() int {
		b, _ := os.ReadFile(count)
		return strings.Count(string(b), "x")
	}
	write("1.0")
	ForgetAddonInfo()
	now := time.Now()
	addonInfoNow = func() time.Time { return now }
	t.Cleanup(func() { addonInfoNow = time.Now })
	b := AddonScripts{Root: r}
	version := func() string {
		t.Helper()
		list, err := b.ListAddons(context.Background())
		if err != nil || len(list) != 1 {
			t.Fatalf("%v %v", list, err)
		}
		return list[0].Version
	}
	if v := version(); v != "1.0" || runs() != 1 {
		t.Fatalf("first: %q, %d runs", v, runs())
	}
	for i := 0; i < 5; i++ {
		version()
	}
	if runs() != 1 {
		t.Errorf("kept: %d runs", runs())
	}
	// an update rewrites the script: asked again, the new version
	write("1.1.0")
	if v := version(); v != "1.1.0" || runs() != 2 {
		t.Errorf("after the script changed: %q, %d runs", v, runs())
	}
	// the addon's VERSION file changed (an update that leaves the script alone)
	_ = os.WriteFile(filepath.Join(string(r), "usr/local/addons/demo/VERSION"), []byte("1.2.0-beta\n"), 0o644)
	version()
	if runs() != 3 {
		t.Errorf("after VERSION changed: %d runs", runs())
	}
	// told: an install, an update, enable or disable
	ForgetAddonInfo()
	version()
	if runs() != 4 {
		t.Errorf("after ForgetAddonInfo: %d runs", runs())
	}
	// and after the TTL
	now = now.Add(AddonInfoTTL + time.Second)
	version()
	if runs() != 5 {
		t.Errorf("after the TTL: %d runs", runs())
	}
	version()
	if runs() != 5 {
		t.Errorf("kept again: %d runs", runs())
	}
}
