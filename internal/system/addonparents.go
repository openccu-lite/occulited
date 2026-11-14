package system

import (
	"fmt"
	"syscall"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// occulited B-145: the directories above the addons' own. The ownership walk that gives a confined
// addon its directories (internal/ownwalk, lookupSafe) refuses a directory when one it looks a name
// up in on the way can be changed by a user other than root: owned by another user, world-writable,
// or group-writable by a group other than root's. A system switched from OpenCCU had /usr/local
// 0777 (where from is not known), so every addon fell back to running as root and could not be
// switched to its own user, and nothing said why.
//
// So at every start (no marker: the bit may come back) the daemon checks priv.AddonParentDirs and
// takes go-w off a refused root-owned one through the helper's addon-parent-tighten, which only
// takes bits away and never follows a link: the group bit of root's own group as well, which the
// walk would let pass - sshd's StrictModes does not (a group-writable /usr/local, 0775 root:root,
// refused the lab key on a lab system; with 0755 it was accepted again). A parent the walk
// accepts is left as it is (the stock config directories are 0775 root:root). What it cannot fix - a parent of another owner, a link to a
// directory others can change, a chmod that failed - stays unsafe, and the Status page's
// addon-parents warning, evaluated live from the same check, names it with the command that
// fixes it.
//
// The walk lets a sticky world-writable directory pass when the entry looked up in it is root's;
// the check here does not: below /usr/local/addons and the two config directories the entries are
// the addons' own, so a sticky bit there does not help, and above them a world-writable /usr/local
// is worth taking away anyway. The rule is stricter than the walk's, never looser.

// parentRootUID is the owner a safe parent has: root. A test sets its own uid.
var parentRootUID uint32

// AddonParent is a parent of the addons' directories that the ownership walk refuses.
type AddonParent struct {
	Path string `json:"path"`
	UID  uint32 `json:"uid"`
	GID  uint32 `json:"gid"`
	// Mode is the permission bits in octal, four digits ("0777"); for a link, its target's
	Mode string `json:"mode"`
	// Reason: "mode" (root's, writable by others: a restart of occulited fixes it), "owner" (another
	// owner), "link" (a link whose target others can change), "chmod" (the helper's fix failed)
	Reason string `json:"reason"`
	// Err is the helper's answer when its fix failed, for the log
	Err error `json:"-"`
}

// AddonParentFix is one parent the hardening changed.
type AddonParentFix struct {
	Path string
	Old  uint32
	New  uint32
}

// parentUnsafe says why a directory with this mode, owner and group is refused as a parent ("" when
// it is not): another owner than root, or writable by others than root.
func parentUnsafe(mode, uid, gid uint32) string {
	if uid != parentRootUID {
		return "owner"
	}
	if mode&0o002 != 0 || (mode&0o020 != 0 && gid != 0) {
		return "mode"
	}
	return ""
}

// tightened is mode without the group- and world-writable bits (go-w).
func tightened(mode uint32) uint32 { return mode & 0o7777 &^ 0o022 }

// addonParentState is one parent as it is now: its stat (a link's target), whether it is a link,
// and why it is refused. ok is false when it does not exist or cannot be read: nothing to say then.
func (r Root) addonParentState(p string) (st syscall.Stat_t, link bool, why string, ok bool) {
	full := r.join(p)
	if err := syscall.Lstat(full, &st); err != nil {
		return st, false, "", false
	}
	if st.Mode&syscall.S_IFMT == syscall.S_IFLNK {
		// the walk follows a link only when root owns it; then the directory it leads to counts
		if st.Uid != parentRootUID {
			return st, true, "owner", true
		}
		if err := syscall.Stat(full, &st); err != nil {
			return st, true, "", false
		}
		if parentUnsafe(st.Mode, st.Uid, st.Gid) != "" {
			return st, true, "link", true
		}
		return st, true, "", true
	}
	return st, false, parentUnsafe(st.Mode, st.Uid, st.Gid), true
}

func unsafeParent(p string, st syscall.Stat_t, why string) AddonParent {
	return AddonParent{Path: p, UID: st.Uid, GID: st.Gid, Mode: fmt.Sprintf("%04o", st.Mode&0o7777), Reason: why}
}

// UnsafeAddonParents is the live check, without changing anything: the parents the ownership walk
// refuses now, in priv.AddonParentDirs' order. The Status page's addon-parents warning reads it.
func (r Root) UnsafeAddonParents() []AddonParent {
	var out []AddonParent
	for _, p := range priv.AddonParentDirs {
		st, _, why, ok := r.addonParentState(p)
		if ok && why != "" {
			out = append(out, unsafeParent(p, st, why))
		}
	}
	return out
}

// HardenAddonParents takes go-w off every root-owned parent of the addons' directories that the
// ownership walk refuses (through the helper; never
// through a link), and answers what it changed and what stays refused. Not while the ReGa runs (the
// CCU's own processes may need the directories as they are); then it does nothing.
func (r Root) HardenAddonParents() (fixed []AddonParentFix, unsafe []AddonParent) {
	if r.HasReGa() {
		return nil, nil
	}
	for _, p := range priv.AddonParentDirs {
		st, link, why, ok := r.addonParentState(p)
		if !ok || why == "" {
			continue
		}
		if why != "mode" || link {
			unsafe = append(unsafe, unsafeParent(p, st, why))
			continue
		}
		old := st.Mode & 0o7777
		want := tightened(st.Mode)
		if err := Priv.TightenAddonParent(r.join(p), want); err != nil {
			u := unsafeParent(p, st, "chmod")
			u.Err = err
			unsafe = append(unsafe, u)
			continue
		}
		fixed = append(fixed, AddonParentFix{Path: p, Old: old, New: want})
	}
	return fixed, unsafe
}
