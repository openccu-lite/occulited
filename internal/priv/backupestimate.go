package priv

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// occulited B-146: a full backup of a /usr/local that holds gigabytes outside .nobackup ran the
// disk full or froze a small system. Before createBackup.sh writes a byte, the daemon asks what the
// archive will hold: this operation walks /usr/local the way the script's tar does and answers the
// total, the free space, and the largest directories, so that a backup that cannot fit is refused
// with their names (the operator marks them .nobackup). It runs as root because the daemon cannot
// read every addon's tree; it answers sizes and the names of directories, never a file's content.

// opBackupEstimate takes nothing: the tree is fixed.
const opBackupEstimate = "backup-estimate"

// BackupEstimateRoot is the tree createBackup.sh archives; a variable for the tests.
var BackupEstimateRoot = "/usr/local"

// backupEstimateTimeout bounds the walk: a tree of millions of files on an SD card is slow, and an
// estimate that does not arrive must not hold the backup for ever (the caller goes on without it).
const backupEstimateTimeout = 2 * time.Minute

// backupEstimateLargest is how many directories the answer names, and backupEstimateMinListed
// the size below which one is not worth naming.
const (
	backupEstimateLargest   = 5
	backupEstimateMinListed = 1 << 20
)

// BackupEstimate is what a full backup of BackupEstimateRoot will hold.
type BackupEstimate struct {
	// Bytes is the apparent size of the regular files the archive takes in: the upper bound of
	// the compressed usr_local.tar.gz (data that is compressed already does not shrink).
	Bytes int64 `json:"bytes"`
	Files int64 `json:"files"`
	// Free is what the file system of BackupEstimateRoot has free for anyone but root, in bytes:
	// the blocks reserved for root are what keeps the journal and the services writing when the
	// backup has taken the rest, so the archive is not counted into them.
	Free int64 `json:"free"`
	// Largest are the directories (or files directly below the root) with the most of Bytes,
	// largest first; none under backupEstimateMinListed.
	Largest []BackupDirSize `json:"largest,omitempty"`
	// Partial: the walk ran out of time, Bytes is a lower bound.
	Partial bool `json:"partial,omitempty"`
}

// BackupDirSize is one directory of the estimate.
type BackupDirSize struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// BackupEstimate is the client side.
func (c Client) BackupEstimate(ctx context.Context) (BackupEstimate, error) {
	var out BackupEstimate
	ctx, cancel := context.WithTimeout(ctx, backupEstimateTimeout+30*time.Second)
	defer cancel()
	res, err := c.call(ctx, request{Op: opBackupEstimate})
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return out, err
	}
	return out, nil
}

// BackupEstimate walks BackupEstimateRoot with createBackup.sh's exclusions.
func (Local) BackupEstimate(ctx context.Context) (BackupEstimate, error) {
	ctx, cancel := context.WithTimeout(ctx, backupEstimateTimeout)
	defer cancel()
	return estimateBackup(ctx, BackupEstimateRoot)
}

// backupExcludedTop are the entries directly below the root that createBackup.sh's tar leaves out
// (--exclude=usr/local/tmp, usr/local/lost+found, usr/local/eQ-3-Backup; "usr/local/.*" is
// matched apart).
var backupExcludedTop = map[string]bool{"tmp": true, "lost+found": true, "eQ-3-Backup": true}

// estimateBackup is the walk: tar's --exclude-tag=.nobackup (a directory holding the tag keeps
// the directory and the tag, not the rest), --one-file-system, the top-level exclusions.
func estimateBackup(ctx context.Context, root string) (BackupEstimate, error) {
	var est BackupEstimate
	rootSt, err := os.Lstat(root)
	if err != nil {
		return est, err
	}
	dev := deviceOf(rootSt)
	var sfs syscall.Statfs_t
	if err := syscall.Statfs(root, &sfs); err == nil {
		est.Free = int64(sfs.Bavail) * int64(sfs.Bsize) //nolint:unconvert // the field types differ per architecture
	}
	// sizes per directory: the first two levels below the root
	sizes := map[string]int64{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			est.Partial = true
			return filepath.SkipAll
		}
		if err != nil {
			// tar's --ignore-failed-read: an unreadable entry is left out
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) == 1 && (backupExcludedTop[parts[0]] || strings.HasPrefix(parts[0], ".")) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return filepath.SkipDir
			}
			if deviceOf(info) != dev {
				return filepath.SkipDir // --one-file-system
			}
			if _, err := os.Lstat(filepath.Join(path, ".nobackup")); err == nil {
				return filepath.SkipDir // --exclude-tag: the directory's content stays out
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		n := info.Size()
		est.Bytes += n
		est.Files++
		sizes[parts[0]] += n // a file directly below the root is named by itself
		if len(parts) > 2 {
			sizes[filepath.Join(parts[0], parts[1])] += n
		}
		return nil
	})
	if walkErr != nil {
		return est, walkErr
	}
	est.Largest = largestDirs(root, sizes, backupEstimateLargest)
	return est, nil
}

// largestDirs names the n largest entries below root (a directory, or a file directly below it); a
// top directory whose largest subdirectory holds at least half of it is named by that subdirectory
// instead (the operator marks the one that matters).
func largestDirs(root string, sizes map[string]int64, n int) []BackupDirSize {
	var top []string
	for k := range sizes {
		if !strings.Contains(k, string(filepath.Separator)) {
			top = append(top, k)
		}
	}
	var out []BackupDirSize
	for _, t := range top {
		name, size := t, sizes[t]
		for k, v := range sizes {
			if strings.HasPrefix(k, t+string(filepath.Separator)) && v*2 >= sizes[t] && (name == t || v > sizes[name]) {
				name = k
			}
		}
		if name != t {
			size = sizes[name]
		}
		if size >= backupEstimateMinListed {
			out = append(out, BackupDirSize{Path: filepath.Join(root, name), Bytes: size})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func deviceOf(info fs.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev) //nolint:unconvert // the field's type differs per architecture
	}
	return 0
}

// backupEstimate is the operation at the boundary: nothing in the request.
func (s *Server) backupEstimate(req request) response {
	if req.Name != "" || req.Path != "" || len(req.Args) > 0 || len(req.Data) > 0 || len(req.Stdin) > 0 || len(req.Env) > 0 || req.Dir != "" || req.Src != "" || req.Dst != "" || req.Target != "" || req.Hash != "" {
		s.log("helper: refused a backup estimate with arguments")
		return refuse("backup-estimate takes nothing")
	}
	est, err := s.ops().BackupEstimate(context.Background())
	if err != nil {
		return response{Error: err.Error()}
	}
	b, err := json.Marshal(est)
	if err != nil {
		return response{Error: err.Error()}
	}
	return response{OK: true, Stdout: b}
}
