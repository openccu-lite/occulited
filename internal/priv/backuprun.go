package priv

import (
	"os"
)

// occulited B-146: createBackup.sh tars and gzips all of /usr/local and then reads the archive
// four more times (gzip -t, the tar listing, the signature, the checksums) before it copies it into
// the .sbk. The helper ran it at the daemon's own priority, on the disk that holds the journal and
// the services' state; with gigabytes outside .nobackup a 2 GB VM froze until its hardware
// watchdog reset it. The helper now runs the script in a transient unit of its own at the lowest
// priority systemd gives: idle I/O class, nice 19, CPUWeight=idle - everything else on the system
// goes first, the backup takes what is left - and with a memory ceiling, so the archive's page
// cache is reclaimed inside the unit instead of pushing the services' working sets out.
// Off a systemd box (upstream's OCI image) the script runs as before.

// backupScript is the program the helper runs this way.
const backupScript = "/bin/createBackup.sh"

// backupSystemdRun and backupSystemdDir are the hooks' (backuphook.go).

// backupUnit is the transient unit's name: fixed, so a second script cannot start beside one that
// still runs (systemd refuses the name) - the daemon's job already keeps one at a time.
const backupUnit = "occulite-backup-script"

// backupMemoryHigh is the unit's memory.high: above it the kernel reclaims the unit's own pages
// (the archive's page cache, clean or written back) before anything else's.
const backupMemoryHigh = "256M"

// backupScriptCommand is the command line the helper runs for name and args: the script inside its
// low-priority unit, anything else unchanged.
func backupScriptCommand(name string, args []string) (string, []string) {
	if name != backupScript {
		return name, args
	}
	if _, err := os.Stat(backupSystemdDir); err != nil {
		return name, args
	}
	full := []string{"--wait", "--pipe", "--collect", "--quiet", "--unit=" + backupUnit,
		"-p", "IOSchedulingClass=idle", "-p", "Nice=19", "-p", "CPUWeight=idle",
		"-p", "MemoryHigh=" + backupMemoryHigh,
		"-p", "WorkingDirectory=/",
		"--", name}
	return backupSystemdRun, append(full, args...)
}
