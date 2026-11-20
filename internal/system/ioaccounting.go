package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// IOAccounting is the Services page's switch "record disk writes per service" (occulited task
// 159). On: systemd counts every unit's reads and writes (DefaultIOAccounting=yes), which enables
// the kernel's io controller for the services' cgroups; the figures are the Storage page's
// (internal/iowatch). Off, the default: only the disks' totals are known.
//
// / is read-only, so the setting is a manager drop-in in /run (priv.IOAccountingDropIn), applied
// with a daemon-reload: no reboot and no restart of any unit, in either direction. /run is empty
// after a boot, so the switch itself is a file in the state directory on the userfs - written
// when the switch is used, never otherwise - and Replay applies it again at each start, as the
// unit switch and the unit overrides are (B-26, task 27.4). What the services wrote in a boot
// before occulited started is therefore not attributed to them.
type IOAccounting struct {
	// File is the stored switch: it exists while the recording is on.
	File string
	// Reload is systemctl daemon-reload.
	Reload func(ctx context.Context) error
	// Set writes or removes the drop-in; nil = the helper's (Priv.SetIOAccounting).
	Set func(ctx context.Context, on bool) error
	// DropIn is where the drop-in is looked for; "" = priv.IOAccountingDropIn.
	DropIn string
}

// Enabled reports the stored switch.
func (a *IOAccounting) Enabled() bool {
	if a == nil || a.File == "" {
		return false
	}
	_, err := os.Stat(a.File)
	return err == nil
}

func (a *IOAccounting) dropIn() string {
	if a.DropIn != "" {
		return a.DropIn
	}
	return priv.IOAccountingDropIn
}

func (a *IOAccounting) applied() bool {
	_, err := os.Stat(a.dropIn())
	return err == nil
}

func (a *IOAccounting) apply(ctx context.Context, on bool) error {
	set := a.Set
	if set == nil {
		if Priv == nil {
			return errors.New("no privilege helper")
		}
		set = Priv.SetIOAccounting
	}
	if err := set(ctx, on); err != nil {
		return err
	}
	if a.Reload != nil {
		if err := a.Reload(ctx); err != nil {
			return fmt.Errorf("daemon-reload: %w", err)
		}
	}
	return nil
}

// Switch stores the switch and applies it.
func (a *IOAccounting) Switch(ctx context.Context, on bool) error {
	if a == nil || a.File == "" {
		return errors.New("no state directory: the switch cannot be kept")
	}
	if on {
		if err := os.MkdirAll(filepath.Dir(a.File), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(a.File, []byte("on\n"), 0o640); err != nil {
			return err
		}
	} else if err := os.Remove(a.File); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := a.apply(ctx, on); err != nil {
		if on {
			// not recorded, so not stored as on: the page shows what is
			_ = os.Remove(a.File)
		}
		return err
	}
	return nil
}

// Replay brings /run in line with the stored switch at start. It does nothing - no helper call,
// no reload - when the two already agree, which is every start but the first after a boot.
func (a *IOAccounting) Replay(ctx context.Context) (changed bool, err error) {
	if a == nil || a.File == "" {
		return false, nil
	}
	on := a.Enabled()
	if on == a.applied() {
		return false, nil
	}
	return true, a.apply(ctx, on)
}

// DaemonReload runs systemctl daemon-reload through s, so its unit cache learns of it.
func (s SystemdServices) DaemonReload(ctx context.Context) error {
	if out, err := s.run(ctx, "daemon-reload"); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
