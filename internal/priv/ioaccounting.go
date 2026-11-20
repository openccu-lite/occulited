package priv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Per-service disk write accounting (occulited task 159). The kernel counts what each cgroup
// reads and writes only while the io controller is enabled for it, and systemd enables it for
// the units whose IOAccounting= is on; DefaultIOAccounting= in the manager's configuration sets
// that for every unit. / is read-only, so the setting is a drop-in in /run, which systemd reads
// at a daemon-reload - no reboot, no unit restarted; the daemon keeps the switch on the userfs
// and has it written again at each start (system.IOAccounting).
//
// One typed operation with a boolean and nothing else: the file's path and its two lines are the
// helper's, so the daemon cannot write anything else into the manager's configuration.

// opIOAccounting is the operation's name on the wire.
const opIOAccounting = "io-accounting"

// IOAccountingDropIn is the manager drop-in; a variable so a test can point it elsewhere.
var IOAccountingDropIn = "/run/systemd/system.conf.d/50-occulite-io-accounting.conf"

const ioAccountingConf = "# occulited: disk writes are recorded per service (System > Services)\n[Manager]\nDefaultIOAccounting=yes\n"

// SetIOAccounting writes the drop-in, or removes it. It reports nothing when the file already is
// what was asked for; the caller reloads systemd.
func (Local) SetIOAccounting(_ context.Context, on bool) error {
	path := IOAccountingDropIn
	if !on {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("io-accounting: %w", err)
		}
		return nil
	}
	if b, err := os.ReadFile(path); err == nil && bytes.Equal(b, []byte(ioAccountingConf)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("io-accounting: %w", err)
	}
	// a link put there is replaced, not followed: the temporary file is renamed over it
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("io-accounting: %w", err)
	}
	_, werr := f.WriteString(ioAccountingConf)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("io-accounting: %w", werr)
	}
	return nil
}

// SetIOAccounting asks the helper to write or remove the drop-in.
func (c Client) SetIOAccounting(ctx context.Context, on bool) error {
	_, err := c.call(ctx, request{Op: opIOAccounting, Enabled: on})
	return err
}
