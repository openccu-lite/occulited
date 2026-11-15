package system

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// occulited B-146: createBackup.sh writes usr_local.tar.gz into /usr/local/tmp and then copies it
// into the .sbk beside it, so a full backup needs twice its archive free on the userfs while it
// runs. Nothing checked that: a /usr/local with gigabytes of leftovers outside .nobackup ran the
// disk full (the journal and the daemons' state live on it too) or froze a small system for
// minutes. Before the script runs, the daemon asks the helper what the archive will hold and
// refuses a backup that cannot fit, naming the largest directories so the operator can mark what
// need not be in it with a .nobackup file.

// BackupReserve is what stays free on the userfs after a backup's two copies: the journal,
// occulited's database and hmipserver keep writing there while it runs. The same figure as an
// upload's (UploadReserve).
var BackupReserve int64 = 128 << 20

// BackupRoomNeeded is the room a backup whose files hold bytes needs: the archive at its largest -
// the files themselves, which is what data that is compressed already stays at - twice (the
// usr_local.tar.gz and its copy in the .sbk), and BackupReserve.
func BackupRoomNeeded(bytes int64) int64 { return 2*bytes + BackupReserve }

// BackupNoSpaceError is a full backup the userfs has no room for.
type BackupNoSpaceError struct {
	Bytes, Need, Free int64
	Largest           []priv.BackupDirSize
}

func (e *BackupNoSpaceError) Error() string {
	var dirs []string
	for _, d := range e.Largest {
		dirs = append(dirs, fmt.Sprintf("%s (%s)", d.Path, megabytes(d.Bytes)))
	}
	msg := fmt.Sprintf("not enough free space for the backup: it holds %s, and making it needs %s free under /usr/local (the archive twice while it is built, and %s that stay free); %s are free. Nothing was written.",
		megabytes(e.Bytes), megabytes(e.Need), megabytes(BackupReserve), megabytes(e.Free))
	if len(dirs) > 0 {
		msg += " The largest directories and files: " + strings.Join(dirs, ", ") + ". A directory with a file named .nobackup is left out of the backup."
	}
	return msg
}

// Detail is the API's detail object of the error (code "no-space").
func (e *BackupNoSpaceError) Detail() map[string]any {
	largest := make([]map[string]any, 0, len(e.Largest))
	for _, d := range e.Largest {
		largest = append(largest, map[string]any{"path": d.Path, "bytes": d.Bytes})
	}
	return map[string]any{"backup_bytes": e.Bytes, "need_bytes": e.Need, "free_bytes": e.Free, "reserve_bytes": BackupReserve, "largest": largest}
}

func megabytes(n int64) string {
	if n >= 1e9 {
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	}
	return fmt.Sprintf("%d MB", n/1e6)
}

// EstimateBackup is Priv's BackupEstimate, a var so a test (here and in the API's) answers a fixed figure.
var EstimateBackup = func(ctx context.Context) (priv.BackupEstimate, error) { return Priv.BackupEstimate(ctx) }

// CheckBackupRoom refuses a full backup that cannot fit (*BackupNoSpaceError). Only on the real
// system (Root "/"): a development root has no /usr/local of its own. An estimate that fails or
// runs out of time does not stop the backup - it is logged, and the script runs as before.
func (r Root) CheckBackupRoom(ctx context.Context) error {
	if string(r) != "/" || Priv == nil {
		return nil
	}
	est, err := EstimateBackup(ctx)
	if err != nil {
		slog.Warn("backup: the size of the backup could not be estimated; it runs without the space check", "err", err)
		return nil
	}
	need := BackupRoomNeeded(est.Bytes)
	slog.Info("backup: estimated", "bytes", est.Bytes, "files", est.Files, "free", est.Free, "need", need, "partial", est.Partial)
	if est.Free >= need { // a partial walk's Bytes is a lower bound: too large already is too large
		return nil
	}
	return &BackupNoSpaceError{Bytes: est.Bytes, Need: need, Free: est.Free, Largest: est.Largest}
}
