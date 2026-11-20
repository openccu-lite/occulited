package priv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"

	"github.com/hobbyquaker/occulited/internal/netmount"
)

// occulited B-153: a directory target on the userfs (/usr/local/backup/...) lies inside the tree
// createBackup.sh archives, so every backup took in all the earlier ones of that folder - each one
// about the size of all before it, until the userfs was full. The script's tar runs with
// --exclude-tag=.nobackup: a folder holding that file stays in the archive, its contents do not.
// The system writes the tag into a directory target's folder when the target is saved (this
// operation, as root: the folder is root's) and at every run (the root half of the backup,
// backuptarget.Pipeline). On a USB stick it changes nothing; the tag is written there too.

// opBackupTag takes the directory target's folder.
const opBackupTag = "backuptag"

// NoBackupTag is the file createBackup.sh's tar --exclude-tag looks for.
const NoBackupTag = ".nobackup"

// TagNoBackup writes an empty NoBackupTag into dir, which must exist: created when it is missing,
// left as it is when it is there. The tag itself is never followed when it is a link, and never
// written when it is not a regular file (a FIFO is not waited on).
func TagNoBackup(dir string) error {
	path := filepath.Join(dir, NoBackupTag)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

// BackupTag writes the tag into a directory target's folder as root, the folder made when it is
// missing (as the write test does).
func (c Client) BackupTag(ctx context.Context, dir string) error {
	_, err := c.call(ctx, request{Op: opBackupTag, Path: dir})
	return err
}

// backupTag: the write test's paths (under /media or /usr/local/backup, resolved as far as they
// exist), except a share's mount point - a share is not in the archive, and the folder there is
// the NAS's.
func (s *Server) backupTag(ctx context.Context, req request) response {
	if !reflect.DeepEqual(req, request{Op: req.Op, Path: req.Path}) {
		return refuse("backuptag takes a directory and nothing else")
	}
	if err := s.Policy.writeTestAllowed(req.Path); err != nil {
		s.log("helper: refused a backup tag in %s: %v", req.Path, err)
		return refuse("backuptag: " + err.Error())
	}
	if rel, ok := s.Policy.rel(req.Path); !ok || strings.HasPrefix(rel+"/", netmount.Base+"/") {
		s.log("helper: refused a backup tag in %s: a share", req.Path)
		return refuse("backuptag: not on a share")
	}
	if err := s.ops().BackupTag(ctx, req.Path); err != nil {
		return response{Error: err.Error()}
	}
	return response{OK: true}
}

// BackupTag makes dir when it is missing and writes the tag into it.
func (Local) BackupTag(_ context.Context, dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return errors.New("not an absolute path")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return TagNoBackup(dir)
}
