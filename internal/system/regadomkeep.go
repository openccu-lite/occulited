package system

import (
	"errors"
	"os"
	"time"
)

// occulited task 167 (the maintainer, 2026-11-21): the CCU's ReGa database is kept after the switch
// from (Open)CCU. openccu-lite itself reads only the names from it (the first boot's import, D-35);
// lite-addon-logic imports the programs and system variables (openccu-lite-addons task 79, D-126).
// So the file is no longer one of the leftovers the first start removes (cleanup.go): it stays until
// the user removes it on the Backup page, and every backup carries it (createBackup.sh archives
// /usr/local whole), so a restore keeps what can be imported. Nothing on openccu-lite writes it; it
// is frozen at the day of the switch. Its .bak goes with the leftovers as before.

// KeptRegadomPath is the CCU's ReGa database as openccu-lite keeps it (/etc/config is the userfs'
// /usr/local/etc/config).
const KeptRegadomPath = "/etc/config/homematic.regadom"

// KeptRegadom is what the system says about the kept database.
type KeptRegadom struct {
	Present bool `json:"present"`
	// Bytes and Modified (RFC 3339, UTC: the ReGa's last save on the CCU) when present
	Bytes    int64  `json:"bytes,omitempty"`
	Modified string `json:"modified,omitempty"`
}

// KeptRegadom answers whether the database is there, with its size and age. A link or a directory
// at the path is not one.
func (r Root) KeptRegadom() KeptRegadom {
	st, err := os.Lstat(r.join(KeptRegadomPath))
	if err != nil || !st.Mode().IsRegular() {
		return KeptRegadom{}
	}
	return KeptRegadom{Present: true, Bytes: st.Size(), Modified: st.ModTime().UTC().Format(time.RFC3339)}
}

// ErrNoRegadom: no ReGa database is kept on this system.
var ErrNoRegadom = errors.New("no ReGa database is kept on this system")

// ReadKeptRegadom reads the database through the privilege boundary (it is root's, 0600 on a CCU).
func (r Root) ReadKeptRegadom() ([]byte, error) {
	if !r.KeptRegadom().Present {
		return nil, ErrNoRegadom
	}
	return Priv.ReadFile(r.join(KeptRegadomPath))
}

// RemoveKeptRegadom removes the database - the user's explicit choice - and its .bak if the first
// start has not removed that yet. Answers the bytes freed; ErrNoRegadom when there was nothing.
func (r Root) RemoveKeptRegadom() (int64, error) {
	k := r.KeptRegadom()
	if !k.Present {
		return 0, ErrNoRegadom
	}
	if err := Priv.RemoveAll(r.join(KeptRegadomPath)); err != nil {
		return 0, err
	}
	freed := k.Bytes
	bak := r.join(KeptRegadomPath + ".bak")
	if st, err := os.Lstat(bak); err == nil && st.Mode().IsRegular() {
		if err := Priv.RemoveAll(bak); err == nil {
			freed += st.Size()
		}
	}
	return freed, nil
}
