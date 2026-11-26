package priv

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"time"
)

// VoltageLog (occulited B-186): rpi_volt's undervoltage lines from the kernel's ring buffer, for
// the temperature panel's episodes. The journal is no reliable source for them: on the SD-card
// products it is volatile and size-capped, and on the maintainer's production Pi, 13 h after the
// boot, its kernel lines began at 46673 s - the boot dip at 9.5 s ("Undervoltage detected!") and
// its end two seconds later ("Voltage normalised") were in the ring buffer only. The daemon cannot
// read the ring buffer itself: lite sets kernel.dmesg_restrict=1 and occulited's unit has no
// capability. One typed operation with nothing in it: the helper runs exactly `dmesg -r` and
// answers rpi_volt's lines, nothing else of the kernel log.

// opVoltageLog is the operation's name on the wire.
const opVoltageLog = "voltage-log"

// dmesgPaths are where the products put dmesg (util-linux in /usr/bin, busybox in /bin); a
// variable so a test can point it at a script.
var dmesgPaths = []string{"/usr/bin/dmesg", "/bin/dmesg"}

// VoltageLogArgs is the whole argument list of the read.
func VoltageLogArgs() []string { return []string{"-r"} }

// rpiVoltRe is rpi_volt's two lines, the older kernels' "Under-voltage detected!" too.
var rpiVoltRe = regexp.MustCompile(`(?i)\bunder-?voltage detected|\bvoltage normali[sz]ed`)

// FilterVoltageLog keeps rpi_volt's lines of `dmesg -r`.
func FilterVoltageLog(out []byte) []byte {
	var b bytes.Buffer
	for _, l := range bytes.Split(out, []byte("\n")) {
		if rpiVoltRe.Match(l) {
			b.Write(l)
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}

// VoltageLog runs `dmesg -r` and answers FilterVoltageLog's lines.
func (l Local) VoltageLog(ctx context.Context) (Result, error) {
	path := ""
	for _, p := range dmesgPaths {
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		return Result{}, fmt.Errorf("%w: dmesg is not installed", ErrNotAvailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := l.Run(ctx, path, VoltageLogArgs(), nil)
	res.Stdout = FilterVoltageLog(res.Stdout)
	return res, err
}

// VoltageLog asks the helper for rpi_volt's lines of the ring buffer. Nothing but the operation travels.
func (c Client) VoltageLog(ctx context.Context) (Result, error) {
	res, err := c.call(ctx, request{Op: opVoltageLog})
	return Result{Stdout: res.Stdout, Stderr: res.Stderr, Exit: res.Exit}, err
}
