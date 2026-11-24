package priv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// The userfs's write behaviour (occulited task 160): the two ext4 mount options that decide how
// often the system storage is written to without anybody asking for it - lazytime (inode
// timestamps stay in memory and go to the disk with another change of the inode, at a sync, or
// within a day) and the journal's commit interval (how long changes that nobody synced wait in
// memory; what a power cut can take). The image mounts /usr/local with commit=30 and without
// lazytime; the daemon keeps the administrator's choice on the userfs and has it applied by a
// remount, at once and at each start (system.UserfsMount).
//
// One typed operation with a switch and one of a handful of intervals: the mount point, the
// option names and the mount binary are the helper's, so the daemon cannot remount anything
// else, nor /usr/local with any other option (rw/ro, exec, suid, …).

// opUserfsMount is the operation's name on the wire.
const opUserfsMount = "userfs-mount"

// UserfsCommitChoices are the commit intervals the operation takes, in seconds: ext4's own
// default, the image's, and two longer ones.
var UserfsCommitChoices = []int{5, 30, 60, 120}

// ErrUserfsNotExt4 is the refusal on a userfs that is no ext4 mount of its own.
var ErrUserfsNotExt4 = errors.New("the userfs is not an ext4 mount of its own")

var (
	// userfsDir is the userfs's mount point and userfsMounts the kernel's mount table; variables
	// so a test can point them elsewhere.
	userfsDir    = "/usr/local"
	userfsMounts = "/proc/mounts"
	// userfsRemount is the remount itself.
	userfsRemount = func(ctx context.Context, dir, opts string) error {
		out, err := exec.CommandContext(ctx, "/bin/mount", "-o", opts, dir).CombinedOutput()
		if err != nil {
			return fmt.Errorf("mount -o %s %s: %w: %s", opts, dir, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
)

// UserfsMountLine answers the file system type and the options of dir's mount in a mount table's
// text - the last line for it, which is the mount on top; ok is false when dir is no mount point.
func UserfsMountLine(table, dir string) (fstype, opts string, ok bool) {
	for _, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[1] == dir {
			fstype, opts, ok = f[2], f[3], true
		}
	}
	return fstype, opts, ok
}

// SetUserfsMount remounts the userfs with lazytime on or off and the commit interval.
func (Local) SetUserfsMount(ctx context.Context, lazytime bool, commit int) error {
	if !slices.Contains(UserfsCommitChoices, commit) {
		return fmt.Errorf("userfs-mount: the commit interval is one of %v seconds, not %d", UserfsCommitChoices, commit)
	}
	table, err := os.ReadFile(userfsMounts)
	if err != nil {
		return fmt.Errorf("userfs-mount: %w", err)
	}
	if fstype, _, ok := UserfsMountLine(string(table), userfsDir); !ok || fstype != "ext4" {
		return fmt.Errorf("userfs-mount: %w", ErrUserfsNotExt4)
	}
	opts := "remount,nolazytime,commit=" + strconv.Itoa(commit)
	if lazytime {
		opts = "remount,lazytime,commit=" + strconv.Itoa(commit)
	}
	if err := userfsRemount(ctx, userfsDir, opts); err != nil {
		return fmt.Errorf("userfs-mount: %w", err)
	}
	return nil
}

// SetUserfsMount asks the helper for the remount.
func (c Client) SetUserfsMount(ctx context.Context, lazytime bool, commit int) error {
	_, err := c.call(ctx, request{Op: opUserfsMount, Enabled: lazytime, Commit: commit})
	if err != nil && strings.Contains(err.Error(), ErrUserfsNotExt4.Error()) {
		return ErrUserfsNotExt4
	}
	return err
}
