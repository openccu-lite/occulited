package priv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"golang.org/x/sys/unix"
)

// occulited B-145: the parents of the addons' directories. The ownership walk (internal/ownwalk)
// refuses an addon's directory when a directory it looks a name up in on the way there can be
// changed by a user other than root - one switched system had /usr/local 0777, and every addon fell
// back to running as root. At every start the daemon takes the group- and world-writable bits off
// such a parent through this operation (go-w: also the group bit of root's own group - sshd's
// StrictModes refuses every key while a directory above authorized_keys is group-writable, and
// /usr/local is one, measured on a lab system):
//   - exactly AddonParentDirs, nothing below or beside them;
//   - never through a link: every component is opened with O_NOFOLLOW, the directory itself too,
//     and the mode is changed through that descriptor - what was checked is what changes;
//   - only a directory that belongs to root (the helper's own user);
//   - only bits taken away, and only those two (go-w): a mode that adds a bit, or takes any other
//     one, is refused.

// opAddonParentTighten takes go-w off an addon parent.
const opAddonParentTighten = "addon-parent-tighten"

// AddonParentDirs are the directories above the addons' three standard directories
// (/usr/local/addons/<id>, <config>/addons/<id>, <config>/addons/www/<id>) on the userfs.
var AddonParentDirs = []string{
	"/usr/local",
	"/usr/local/etc",
	"/usr/local/addons",
	"/usr/local/etc/config",
	"/usr/local/etc/config/addons",
	"/usr/local/etc/config/addons/www",
}

// TightenAddonParent asks the helper to set an addon parent's mode to mode, which may only take the
// group- and world-writable bits away.
func (c Client) TightenAddonParent(path string, mode uint32) error {
	return c.fileOp(request{Op: opAddonParentTighten, Path: path, Mode: mode})
}

// TightenAddonParent changes path's mode to mode through a descriptor opened without following a
// link anywhere in the path, after checking the directory it holds (tightenAllowed). The same mode
// is no change and no error.
func (Local) TightenAddonParent(path string, mode uint32) error {
	dir, base := splitPath(path)
	if base == "/" || base == "." {
		return fmt.Errorf("%s: not an addon parent", path)
	}
	dfd, err := openDir(dir, false, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dfd)
	fd, err := unix.Openat(dfd, base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			err = fmt.Errorf("a symbolic link or not a directory: %w", err)
		}
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if err := tightenAllowed(st.Mode, st.Uid, mode, uint32(os.Geteuid())); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if st.Mode&0o7777 == mode {
		return nil
	}
	if err := unix.Fchmod(fd, mode); err != nil {
		return &os.PathError{Op: "chmod", Path: path, Err: err}
	}
	return nil
}

// tightenAllowed: the entry (its st_mode and owner) is a directory of root (the helper's uid; root
// in production), and want is its mode with nothing added and nothing taken but g+w and o+w.
func tightenAllowed(stMode, uid, want, root uint32) error {
	if stMode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("not a directory")
	}
	if uid != root {
		return fmt.Errorf("owned by uid %d, not by root", uid)
	}
	if want&^0o7777 != 0 {
		return fmt.Errorf("mode %o is not a permission mode", want)
	}
	cur := stMode & 0o7777
	if added := want &^ cur; added != 0 {
		return fmt.Errorf("mode %04o would add %04o to %04o", want, added, cur)
	}
	const may = uint32(0o022)
	if taken := cur &^ want; taken&^may != 0 {
		return fmt.Errorf("mode %04o would take %04o from %04o; only %04o may go", want, taken, cur, may)
	}
	return nil
}

// addonParentAllowed: path (relative to the policy's root) is exactly one of AddonParentDirs.
func (p Policy) addonParentAllowed(path string) bool {
	rel, ok := p.rel(path)
	return ok && slices.Contains(AddonParentDirs, rel)
}

// addonParent is the operation at the boundary: a path and a mode, nothing else.
func (s *Server) addonParent(req request) response {
	if !reflect.DeepEqual(req, request{Op: req.Op, Path: req.Path, Mode: req.Mode}) {
		s.log("helper: refused an addon parent request with more than a path and a mode")
		return refuse("addon-parent-tighten takes a path and a mode and nothing else")
	}
	if !s.Policy.addonParentAllowed(req.Path) {
		s.log("helper: refused tightening %s: not an addon parent", req.Path)
		return refuse("addon parent " + req.Path)
	}
	path := filepath.Clean(req.Path)
	if err := s.ops().TightenAddonParent(path, req.Mode); err != nil {
		s.log("helper: did not tighten %s: %v", path, err)
		return response{Error: err.Error()}
	}
	s.log("helper: %s is mode %04o", path, req.Mode)
	return response{OK: true}
}
