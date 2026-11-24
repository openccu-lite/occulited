package system

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// linkModes builds a link mode bitmap from bit numbers.
func linkModes(bits ...int) []uint32 {
	w := make([]uint32, 4)
	for _, b := range bits {
		w[b/32] |= 1 << (b % 32)
	}
	return w
}

// fakeLinkModes answers EthLinkModes per interface for the test.
func fakeLinkModes(t *testing.T, modes map[string][]uint32) {
	t.Helper()
	old := EthLinkModes
	EthLinkModes = func(_ Root, name string) ([]uint32, []uint32, error) {
		m, ok := modes[name]
		if !ok {
			return nil, nil, errors.New("no such device")
		}
		return m, m, nil
	}
	t.Cleanup(func() { EthLinkModes = old })
}

func TestGigabitCapable(t *testing.T) {
	for _, tc := range []struct {
		name string
		bits []int
		want bool
	}{
		{"a Pi 4: 10/100/1000", []int{0, 1, 2, 3, 4, 5}, true},
		{"a Pi 3: 10/100", []int{0, 1, 2, 3}, false},
		{"a VM's virtio: nothing", nil, false},
		{"2.5G without 1000", []int{2, 3, 47}, true},
		{"10G", []int{3, 12}, true},
		{"gigabit only, nothing to fall back to", []int{5}, false},
		{"100 half only and a gigabit", []int{2, 5}, false},
	} {
		if got := gigabitCapable(linkModes(tc.bits...)); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if gigabitCapable(nil) || linkModeSet([]uint32{1}, 40) {
		t.Error("a short bitmap has no further modes")
	}
}

// The live reader is only asked on the live system: a test tree's eth0 is not this machine's.
func TestEthLinkModesOnlyLive(t *testing.T) {
	if _, _, err := EthLinkModes(Root(t.TempDir()), "eth0"); err == nil {
		t.Error("a test root was asked")
	}
	if _, _, err := readLinkModes(""); err == nil {
		t.Error("an empty name")
	}
	if _, _, err := readLinkModes("an-interface-name-too-long"); err == nil {
		t.Error("a long name")
	}
	// lo has no link modes worth a limit, whatever the kernel answers
	if s, _, err := readLinkModes("lo"); err == nil && gigabitCapable(s) {
		t.Error("lo is gigabit capable")
	}
	if _, _, err := readLinkModes("nosuchif0"); err == nil {
		t.Error("an interface that is not there")
	}
}

func TestSpeedLimitInNetwork(t *testing.T) {
	r := networkFixture(t)
	// eth0 a gigabit NIC, eth1 a 100 Mbit USB adapter; wlan0 would answer gigabit and is not wired
	fakeLinkModes(t, map[string][]uint32{"eth0": linkModes(0, 1, 2, 3, 4, 5), "eth1": linkModes(0, 1, 2, 3), "wlan0": linkModes(3, 5)})
	limits := func() map[string]string {
		out := map[string]string{}
		for _, i := range r.ReadNetwork().Interfaces {
			out[i.Name] = i.SpeedLimit
		}
		return out
	}
	if l := limits(); l["eth0"] != "auto" || l["eth1"] != "" || l["wlan0"] != "" || l["br0"] != "" {
		t.Errorf("limits: %v", l)
	}
	if err := os.MkdirAll(r.join(EthLinkDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.join(ethLinkFile("eth0")), ethLinkContent("eth0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if l := limits(); l["eth0"] != "100" {
		t.Errorf("with the file: %v", l)
	}
}

func TestEthLinkContent(t *testing.T) {
	c := string(ethLinkContent("eth0"))
	for _, want := range []string{"[Match]\nProperty=INTERFACE=eth0\n", "[Link]\n", "NamePolicy=keep kernel database onboard slot path\n", "MACAddressPolicy=persistent\n", "Advertise=100baset-full 100baset-half\n"} {
		if !strings.Contains(c, want) {
			t.Errorf("the .link file lacks %q:\n%s", want, c)
		}
	}
	// autonegotiation stays on: no forced speed
	for _, not := range []string{"AutoNegotiation=", "BitsPerSecond=", "Duplex=", "OriginalName="} {
		if strings.Contains(c, not) {
			t.Errorf("the .link file has %q", not)
		}
	}
	if ethLinkFile("eth0") != "/etc/config/systemd-network/10-occulite-speed-eth0.link" {
		t.Error(ethLinkFile("eth0"))
	}
}

func TestEthSpeedSet(t *testing.T) {
	r := networkFixture(t)
	fakeLinkModes(t, map[string][]uint32{"eth0": linkModes(0, 1, 2, 3, 4, 5), "eth1": linkModes(0, 1, 2, 3)})
	var calls []string
	var fail error
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte("out"), fail
	}
	e := EthSpeed{Root: r, Run: run}
	ctx := context.Background()
	file := r.join(ethLinkFile("eth0"))
	exists := func() bool { _, err := os.Stat(file); return err == nil }

	// an image without the unit
	if _, err := e.Set(ctx, "eth0", "100"); !errors.Is(err, ErrSpeedLimitUnsupported) || exists() {
		t.Fatalf("without the unit: %v", err)
	}
	if r.SpeedLimitWritable() {
		t.Error("writable without the unit")
	}
	if err := os.MkdirAll(r.join("/usr/lib/systemd/system"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.join("/usr/lib/systemd/system/"+EthLinkUnit), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !r.SpeedLimitWritable() {
		t.Fatal("not writable with the unit")
	}

	for _, bad := range [][2]string{{"eth0", "1000"}, {"eth0", ""}, {"eth1", "100"}, {"wlan0", "100"}, {"nope", "100"}, {"../x", "100"}, {"", "auto"}} {
		if _, err := e.Set(ctx, bad[0], bad[1]); !errors.Is(err, ErrSpeedLimitInvalid) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	if len(calls) != 0 || exists() {
		t.Fatalf("a refused request did something: %v", calls)
	}

	// automatic already
	if changed, err := e.Set(ctx, "eth0", "auto"); err != nil || changed || len(calls) != 0 {
		t.Fatalf("auto on auto: %v %v %v", changed, err, calls)
	}
	changed, err := e.Set(ctx, "eth0", "100")
	if err != nil || !changed || !exists() {
		t.Fatalf("100: %v %v", changed, err)
	}
	if len(calls) != 1 || calls[0] != "systemctl restart --no-pager -- occu-link.service" {
		t.Fatalf("calls: %v", calls)
	}
	if b, _ := os.ReadFile(file); string(b) != string(ethLinkContent("eth0")) {
		t.Errorf("file: %s", b)
	}
	if changed, err := e.Set(ctx, "eth0", "100"); err != nil || changed || len(calls) != 1 {
		t.Fatalf("100 on 100: %v %v", changed, err)
	}

	// the unit fails: the file goes back, and the unit is asked again for the old state
	fail = errors.New("exit status 1")
	if _, err := e.Set(ctx, "eth0", "auto"); err == nil || !strings.Contains(err.Error(), "occu-link.service") || !strings.Contains(err.Error(), "out") {
		t.Fatalf("a failing unit: %v", err)
	}
	if !exists() || len(calls) != 3 {
		t.Fatalf("after the failure: file %v, calls %v", exists(), calls)
	}
	fail = nil
	if changed, err := e.Set(ctx, "eth0", "auto"); err != nil || !changed || exists() {
		t.Fatalf("back to auto: %v %v", changed, err)
	}

	// a container's network is the host's
	if err := os.MkdirAll(r.join("/run/systemd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.join("/run/systemd/container"), []byte("lxc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Set(ctx, "eth0", "100"); !errors.Is(err, ErrSpeedLimitUnsupported) || r.SpeedLimitWritable() {
		t.Errorf("in a container: %v", err)
	}
}

// The default runner goes through Priv.
func TestEthSpeedDefaultRunner(t *testing.T) {
	r := networkFixture(t)
	fakeLinkModes(t, map[string][]uint32{"eth0": linkModes(3, 5)})
	if err := os.MkdirAll(r.join("/usr/lib/systemd/system"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.join("/usr/lib/systemd/system/"+EthLinkUnit), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	old := Priv
	Priv = runPriv{calls: &calls}
	t.Cleanup(func() { Priv = old })
	if changed, err := (EthSpeed{Root: r}).Set(context.Background(), "eth0", "100"); err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if len(calls) != 1 || calls[0] != "systemctl restart --no-pager -- occu-link.service" {
		t.Errorf("calls: %v", calls)
	}
}
