package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// occulited task 174: an Ethernet interface held at 100 Mbit/s. Gigabit Ethernet on a Raspberry
// Pi 4 or 5 disturbs the 868 MHz radio (the PHY and its cable radiate there; 100BASE-TX does
// not), so the Network page can limit what the interface offers its link partner.
//
// The setting is a systemd .link file on the userfs, EthLinkDir/10-occulite-speed-<if>.link, with
// Advertise=100baset-full 100baset-half: autonegotiation stays on and only 100 Mbit/s is offered,
// which every switch follows (a forced speed with autonegotiation off is what breaks links). The
// image's occu-link.service hands the directory's files to udev - before the network starts at
// boot, and again when occulited restarts it after a change - and puts an interface whose file
// is gone back to everything it supports. The file is the whole state: a backup carries it, a
// factory reset removes it, and nothing of it is in occulited's own configuration.

const (
	// EthLinkDir holds the .link files occu-link.service applies.
	EthLinkDir = "/etc/config/systemd-network"
	// EthLinkUnit applies them; an image without it has no speed setting.
	EthLinkUnit = "occu-link.service"

	SpeedLimitAuto = "auto"
	SpeedLimit100  = "100"
)

// ErrSpeedLimitInvalid is a request the rules refuse; ErrSpeedLimitUnsupported an image without
// EthLinkUnit.
var (
	ErrSpeedLimitInvalid     = errors.New("invalid speed limit")
	ErrSpeedLimitUnsupported = errors.New("the Ethernet speed cannot be set on this system")
)

// The link modes that matter here (ETHTOOL_LINK_MODE_*_BIT in linux/ethtool.h).
const (
	linkMode100baseTFull   = 3
	linkMode1000baseTFull  = 5
	linkMode10000baseTFull = 12
	linkMode2500baseTFull  = 47
	linkMode5000baseTFull  = 48
)

// EthLinkModes reads an interface's supported and advertised link modes. Only the live system
// has interfaces to ask (a test tree's eth0 would name the test machine's); a test replaces it.
var EthLinkModes = func(r Root, name string) (supported, advertising []uint32, err error) {
	if s := string(r); s != "" && s != "/" {
		return nil, nil, errors.New("not the live system")
	}
	return readLinkModes(name)
}

func linkModeSet(words []uint32, bit int) bool {
	return bit/32 < len(words) && words[bit/32]&(1<<(bit%32)) != 0
}

// gigabitCapable: the interface can do 1000 Mbit/s or more over twisted pair, and 100 Mbit/s
// full duplex to fall back to. A Pi 3's USB adapter, a VM's virtio interface (no link modes at
// all) and anything that is not a wired interface are not.
func gigabitCapable(supported []uint32) bool {
	if !linkModeSet(supported, linkMode100baseTFull) {
		return false
	}
	for _, bit := range []int{linkMode1000baseTFull, linkMode2500baseTFull, linkMode5000baseTFull, linkMode10000baseTFull} {
		if linkModeSet(supported, bit) {
			return true
		}
	}
	return false
}

// ethIfaceRe is an interface name a .link file is written for: what the kernel calls a wired
// interface, nothing that could leave the file's name or its Property= line.
var ethIfaceRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,14}$`)

func ethLinkFile(iface string) string {
	return EthLinkDir + "/10-occulite-speed-" + iface + ".link"
}

// ethLinkContent is the .link file. udev applies the first file that matches an interface, so
// this one repeats what systemd's 99-default.link says and adds the one line. The match is the
// interface's name as a udev property: OriginalName= would say the same and make udev warn about
// an unpredictable name on every boot (the image keeps the kernel's names, net.ifnames=0).
func ethLinkContent(iface string) []byte {
	return []byte("# openccu-lite: " + iface + " offers its link partner 100 Mbit/s only (System > Network, Ethernet speed).\n" +
		"# Written by occulited; removed when the setting goes back to automatic.\n" +
		"[Match]\n" +
		"Property=INTERFACE=" + iface + "\n" +
		"\n" +
		"[Link]\n" +
		"NamePolicy=keep kernel database onboard slot path\n" +
		"AlternativeNamesPolicy=database onboard slot path mac\n" +
		"MACAddressPolicy=persistent\n" +
		"Advertise=100baset-full 100baset-half\n")
}

// speedLimit is the stored setting of a gigabit-capable interface, "" for any other.
func (r Root) speedLimit(i NetIface) string {
	if i.Kind != "ethernet" || !ethIfaceRe.MatchString(i.Name) {
		return ""
	}
	supported, _, err := EthLinkModes(r, i.Name)
	if err != nil || !gigabitCapable(supported) {
		return ""
	}
	if _, err := os.Stat(r.join(ethLinkFile(i.Name))); err == nil {
		return SpeedLimit100
	}
	return SpeedLimitAuto
}

// SpeedLimitWritable: the image has the unit that applies the files, and the network is this
// system's own (not a container's).
func (r Root) SpeedLimitWritable() bool {
	if r.HostManaged() {
		return false
	}
	_, err := os.Stat(r.join("/usr/lib/systemd/system/" + EthLinkUnit))
	return err == nil
}

// EthSpeed sets the limit.
type EthSpeed struct {
	Root Root
	// Run restarts EthLinkUnit; the default goes through the helper.
	Run Runner
}

// Set stores the limit of one interface and applies it at once: the link renegotiates, which
// takes a few seconds, and addresses and leases stay. changed is false when it was set already.
// When the unit fails the file goes back to what it was, so the page keeps showing what applies.
func (e EthSpeed) Set(ctx context.Context, iface, limit string) (changed bool, err error) {
	if limit != SpeedLimitAuto && limit != SpeedLimit100 {
		return false, fmt.Errorf("%w: the limit is %q or %q", ErrSpeedLimitInvalid, SpeedLimitAuto, SpeedLimit100)
	}
	if !e.Root.SpeedLimitWritable() {
		return false, ErrSpeedLimitUnsupported
	}
	current := ""
	for _, i := range e.Root.ReadNetwork().Interfaces {
		if i.Name == iface {
			current = i.SpeedLimit
		}
	}
	if current == "" {
		return false, fmt.Errorf("%w: %q is not a wired interface that can do 1000 Mbit/s", ErrSpeedLimitInvalid, iface)
	}
	if current == limit {
		return false, nil
	}
	if err := e.write(iface, limit); err != nil {
		return false, err
	}
	if err := e.apply(ctx); err != nil {
		if undo := e.write(iface, current); undo == nil {
			_ = e.apply(ctx)
		}
		return false, err
	}
	return true, nil
}

func (e EthSpeed) write(iface, limit string) error {
	file := e.Root.join(ethLinkFile(iface))
	if limit == SpeedLimitAuto {
		return Priv.Remove(file)
	}
	if err := Priv.MkdirAll(e.Root.join(EthLinkDir), 0o755); err != nil {
		return err
	}
	return Priv.WriteFile(file, ethLinkContent(iface), 0o644)
}

func (e EthSpeed) apply(ctx context.Context) error {
	r := e.Run
	if r == nil {
		r = run
	}
	if out, err := r(ctx, "systemctl", "restart", "--no-pager", "--", EthLinkUnit); err != nil {
		return fmt.Errorf("%s: %v: %s", EthLinkUnit, err, strings.TrimSpace(string(out)))
	}
	return nil
}
