package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hobbyquaker/occulited/internal/system"
)

// occulited task 174: the Ethernet speed limit - the interface's field in GET /network, the route
// that sets it, and what it refuses.
func TestSpeedLimitRoute(t *testing.T) {
	r := fakeRoot(t)
	w := func(p, c string) {
		full := filepath.Join(string(r), p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"eth0", "eth1"} {
		w("sys/class/net/"+name+"/type", "1\n")
		w("sys/class/net/"+name+"/operstate", "up\n")
		w("sys/class/net/"+name+"/device/keep", "")
	}
	gigabit := []uint32{0x3f}
	fast := []uint32{0x0f}
	old := system.EthLinkModes
	system.EthLinkModes = func(_ system.Root, name string) ([]uint32, []uint32, error) {
		switch name {
		case "eth0":
			return gigabit, gigabit, nil
		case "eth1":
			return fast, fast, nil
		}
		return nil, nil, errors.New("no such device")
	}
	t.Cleanup(func() { system.EthLinkModes = old })
	var calls []string
	var fail error
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, fail
	}
	mux := http.NewServeMux()
	(&SystemAPI{Root: r, Run: run}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	limits := func(out map[string]any) map[string]any {
		got := map[string]any{}
		for _, i := range out["network"].(map[string]any)["interfaces"].([]any) {
			m := i.(map[string]any)
			got[m["name"].(string)] = m["speed_limit"]
		}
		return got
	}
	const path = "/api/system/v1/network/speed-limit"
	st, out, _ := do(t, srv, "GET", "/api/system/v1/network", "", nil)
	if l := limits(out); st != 200 || l["eth0"] != "auto" || l["eth1"] != nil || out["speed_limit_writable"] != false {
		t.Fatalf("get: %d %v %v", st, l, out["speed_limit_writable"])
	}
	// the image has no occu-link.service
	if st, out, _ := do(t, srv, "POST", path, `{"interface":"eth0","limit":"100"}`, nil); st != 501 || out["error"] != "unsupported" {
		t.Fatalf("without the unit: %d %v", st, out)
	}
	w("usr/lib/systemd/system/occu-link.service", "")
	if _, out, _ := do(t, srv, "GET", "/api/system/v1/network", "", nil); out["speed_limit_writable"] != true {
		t.Fatalf("writable: %v", out["speed_limit_writable"])
	}
	for _, body := range []string{`{"interface":"eth0","limit":"1000"}`, `{"interface":"eth1","limit":"100"}`, `{"interface":"lo","limit":"100"}`, `{"limit":"100"}`} {
		if st, out, _ := do(t, srv, "POST", path, body, nil); st != 422 || out["error"] != "invalid" {
			t.Errorf("%s: %d %v", body, st, out)
		}
	}
	if st, _, _ := do(t, srv, "POST", path, `{`, nil); st != 422 {
		t.Errorf("a broken body: %d", st)
	}
	if len(calls) != 0 {
		t.Fatalf("refused requests ran %v", calls)
	}
	st, out, _ = do(t, srv, "POST", path, `{"interface":"eth0","limit":"100"}`, nil)
	if l := limits(out); st != 200 || out["changed"] != true || l["eth0"] != "100" {
		t.Fatalf("set: %d %v", st, out)
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "restart") || !strings.HasSuffix(calls[0], "occu-link.service") {
		t.Fatalf("calls: %v", calls)
	}
	if b, err := os.ReadFile(filepath.Join(string(r), "etc/config/systemd-network/10-occulite-speed-eth0.link")); err != nil || !strings.Contains(string(b), "Advertise=100baset-full 100baset-half") {
		t.Fatalf("the .link file: %v %s", err, b)
	}
	if st, out, _ := do(t, srv, "POST", path, `{"interface":"eth0","limit":"100"}`, nil); st != 200 || out["changed"] != false || len(calls) != 1 {
		t.Errorf("the same again: %d %v %v", st, out["changed"], calls)
	}
	// the unit fails: 500, and the setting is what it was
	fail = errors.New("exit status 1")
	if st, out, _ := do(t, srv, "POST", path, `{"interface":"eth0","limit":"auto"}`, nil); st != 500 || !strings.Contains(out["message"].(string), "occu-link.service") {
		t.Errorf("a failing unit: %d %v", st, out)
	}
	fail = nil
	if _, out, _ := do(t, srv, "GET", "/api/system/v1/network", "", nil); limits(out)["eth0"] != "100" {
		t.Errorf("after the failure: %v", limits(out))
	}
	if st, out, _ := do(t, srv, "POST", path, `{"interface":"eth0","limit":"auto"}`, nil); st != 200 || out["changed"] != true || limits(out)["eth0"] != "auto" {
		t.Errorf("auto: %d %v", st, out)
	}
	// a container: the network is the host's
	w("run/systemd/container", "lxc\n")
	if st, out, _ := do(t, srv, "POST", path, `{"interface":"eth0","limit":"100"}`, nil); st != 501 || out["error"] != "host-managed" {
		t.Errorf("in a container: %d %v", st, out)
	}
}
