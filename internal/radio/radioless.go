package radio

import (
	"os"
	"path/filepath"
	"regexp"
)

// occulited B-175 (the maintainer, 2026-11-24: fix for 2.0.0): an OpenCCU without a radio module -
// a VM without a stick - ran its virtual devices (heating groups) with HMServer.jar, which upstream
// chooses whenever no HmIP module is found. Here a missing module never changes the server
// (task 147, deviation 18), so after the switch from such a system hmipserver failed for want of a
// module and the virtual devices were gone until the user set HmIP-RF to none.
//
// The run therefore sets HmIP to none itself, once, where everything says the old system had no
// HmIP radio: the choice is still automatic, the plan finds no module for HMIPServer.jar, the
// userfs holds the CCU's ReGa database (a system that came by the switch; task 167 keeps it), and
// there is no HmIP identity of a module (no access point files) and no paired HmIP device in
// crRFD/data - HMIPServer.jar writes the access point's identity at its first start with a module,
// and a radio-less OpenCCU never had that directory. A fresh installation without its stick yet is
// not a switch and keeps automatic; a CCU3 whose module is not found at the first start has its
// identity files and keeps automatic too (the pages say the module is missing). The marker makes
// it once per system: a user who sets HmIP back to automatic keeps that.

// RadiolessSwitchMarker records that the run made the choice: it is made once per system.
const RadiolessSwitchMarker = "/usr/local/etc/occulite/radio-radioless-switch"

// keptRegadomPath is the CCU's ReGa database as the switch leaves it (system.KeptRegadomPath).
const keptRegadomPath = "/etc/config/homematic.regadom"

var sgtinFileRe = regexp.MustCompile(`^[0-9A-Fa-f]{24}\.`)

// hmipIdentity: crRFD/data holds an access point's identity or a paired device (files named by an
// SGTIN: <SGTIN>.ap, .apkx, .bbkx, .dev).
func hmipIdentity(d Detector) bool {
	entries, err := os.ReadDir(d.path("/etc/config/crRFD/data"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if sgtinFileRe.MatchString(e.Name()) {
			return true
		}
	}
	return false
}

// RadiolessSwitch sets HmIP to none in hmip_user.conf where a switched system had no HmIP radio (see
// above) and returns the inputs with the new file; ok is false where nothing was changed.
func RadiolessSwitch(d Detector, in Inputs, logf func(string, ...any)) (Inputs, bool) {
	if exists(d.path(RadiolessSwitchMarker)) {
		return in, false
	}
	st, err := os.Lstat(d.path(keptRegadomPath))
	if err != nil || !st.Mode().IsRegular() {
		return in, false // not a switched system
	}
	if ReadChoices(in.HmIPUserConf, "").HmIP != ChoiceAuto || !MakePlan(in).NoHmIPModule() || hmipIdentity(d) {
		return in, false
	}
	conf := SetHmIPChoice(in.HmIPUserConf, HmIPNone)
	file := d.path("/etc/config/crRFD/hmip_user.conf")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		logf("radio: HmIP could not be set to none for a switched system without an HmIP radio: %v", err)
		return in, false
	}
	// the prep step gives crRFD and its files to the server before every start
	if err := os.WriteFile(file, []byte(conf), 0o640); err != nil {
		logf("radio: HmIP could not be set to none for a switched system without an HmIP radio: %v", err)
		return in, false
	}
	if err := os.MkdirAll(filepath.Dir(d.path(RadiolessSwitchMarker)), 0o755); err == nil {
		_ = os.WriteFile(d.path(RadiolessSwitchMarker), []byte("hmip=none\n"), 0o644)
	}
	logf("radio: this system came from a CCU or OpenCCU without an HmIP radio module (no module now, no HmIP identity, no paired HmIP device): HmIP-RF set to none - hmipserver runs the virtual devices alone (HMServer.jar), as the old system did; set HmIP-RF back on the Interfaces page once a module is connected")
	in.HmIPUserConf = conf
	return in, true
}
