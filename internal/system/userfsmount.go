package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/hobbyquaker/occulited/internal/atomicfile"
	"github.com/hobbyquaker/occulited/internal/priv"
)

// UserfsMountOptions is the userfs's write behaviour (occulited task 160): the two ext4 mount
// options System → Storage offers.
type UserfsMountOptions struct {
	// Lazytime: inode timestamps are kept in memory and written with another change of the inode,
	// at a sync, or within a day. No data is at risk; a power cut can lose a timestamp.
	Lazytime bool `json:"lazytime"`
	// CommitS is the ext4 journal's commit interval in seconds: how long a change nobody synced
	// may wait in memory - and what a power cut can take.
	CommitS int `json:"commit_s"`
}

// DefaultUserfsMount is what the image's fstab mounts the userfs with (noatime,nodiratime,commit=30).
var DefaultUserfsMount = UserfsMountOptions{Lazytime: false, CommitS: 30}

// UserfsMount is the setting and its remount. The image's fstab is on the read-only root, so
// the choice is a file in the state directory on the userfs - written when it differs from the
// image's, removed when it is the image's again - and Replay applies it at each start, as the
// disk-write recording's switch is (IOAccounting). Between the mount and occulited's start a
// boot runs with the image's options.
type UserfsMount struct {
	// File is the stored choice (JSON); it exists only while the choice is not the image's.
	File string
	// Mounts is the kernel's mount table; "" = the system's own (/proc/1/mounts), else this
	// process's (/proc/mounts). Dir is the userfs's mount point; "" = /usr/local.
	Mounts string
	Dir    string
	// Set is the remount; nil = the helper's (Priv.SetUserfsMount).
	Set func(ctx context.Context, lazytime bool, commit int) error
}

// UserfsMountState is what the mount table says about the userfs.
type UserfsMountState struct {
	// Mounted: the userfs is a mount of its own. FSType and Options as the kernel lists them.
	Mounted bool   `json:"mounted"`
	FSType  string `json:"fstype,omitempty"`
	Options string `json:"options,omitempty"`
	// Supported: an ext4 mount, the only one the two options are offered for.
	Supported bool `json:"supported"`
	// Active are the two options as they are mounted now (CommitS 5, ext4's default, when the
	// table names no commit=).
	Active UserfsMountOptions `json:"active"`
}

func (u *UserfsMount) dir() string {
	if u.Dir != "" {
		return u.Dir
	}
	return "/usr/local"
}

// State reads the mount table.
func (u *UserfsMount) State() UserfsMountState {
	// the daemon's own mount namespace shows the userfs with its unit's flags (ro, nosuid): the
	// system's table is the first process's
	tables := []string{u.Mounts}
	if u.Mounts == "" {
		tables = []string{"/proc/1/mounts", "/proc/mounts"}
	}
	var b []byte
	var err error
	for _, table := range tables {
		if b, err = os.ReadFile(table); err == nil {
			break
		}
	}
	if err != nil {
		return UserfsMountState{}
	}
	fstype, opts, ok := priv.UserfsMountLine(string(b), u.dir())
	if !ok {
		return UserfsMountState{}
	}
	st := UserfsMountState{Mounted: true, FSType: fstype, Options: opts, Supported: fstype == "ext4", Active: UserfsMountOptions{CommitS: 5}}
	for _, o := range strings.Split(opts, ",") {
		if o == "lazytime" {
			st.Active.Lazytime = true
		} else if v, found := strings.CutPrefix(o, "commit="); found {
			if n, err := strconv.Atoi(v); err == nil {
				st.Active.CommitS = n
			}
		}
	}
	return st
}

// ValidUserfsMount says whether o is a choice the system offers.
func ValidUserfsMount(o UserfsMountOptions) error {
	if !slices.Contains(priv.UserfsCommitChoices, o.CommitS) {
		return fmt.Errorf("commit_s is one of %v, not %d", priv.UserfsCommitChoices, o.CommitS)
	}
	return nil
}

// Stored answers the stored choice; custom is false (and the choice the image's) while there is
// none, or the file is not one of the choices.
func (u *UserfsMount) Stored() (o UserfsMountOptions, custom bool) {
	if u == nil || u.File == "" {
		return DefaultUserfsMount, false
	}
	b, err := os.ReadFile(u.File)
	if err != nil || json.Unmarshal(b, &o) != nil || ValidUserfsMount(o) != nil {
		return DefaultUserfsMount, false
	}
	return o, true
}

func (u *UserfsMount) remount(ctx context.Context, o UserfsMountOptions) error {
	set := u.Set
	if set == nil {
		if Priv == nil {
			return errors.New("no privilege helper")
		}
		set = Priv.SetUserfsMount
	}
	return set(ctx, o.Lazytime, o.CommitS)
}

// Apply remounts the userfs with o and, when that worked, stores the choice - a remount the
// kernel refused leaves the stored choice as it was, so the page shows what is.
func (u *UserfsMount) Apply(ctx context.Context, o UserfsMountOptions) error {
	if u == nil || u.File == "" {
		return errors.New("no state directory: the choice cannot be kept")
	}
	if err := ValidUserfsMount(o); err != nil {
		return err
	}
	if !u.State().Supported {
		return priv.ErrUserfsNotExt4
	}
	if err := u.remount(ctx, o); err != nil {
		return err
	}
	if o == DefaultUserfsMount {
		if err := os.Remove(u.File); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(u.File), 0o750); err != nil {
		return err
	}
	b, _ := json.Marshal(o)
	return atomicfile.Write(u.File, append(b, '\n'), 0o640)
}

// Replay brings the mount in line with the stored choice at start. It does nothing - no helper
// call - while there is no stored choice, the userfs is no ext4 mount, or the mount already is
// what was chosen (every start but the first after a boot).
func (u *UserfsMount) Replay(ctx context.Context) (changed bool, err error) {
	o, custom := u.Stored()
	if !custom {
		return false, nil
	}
	st := u.State()
	if !st.Supported || st.Active == o {
		return false, nil
	}
	return true, u.remount(ctx, o)
}
