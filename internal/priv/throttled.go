package priv

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Throttled (occulited task 148): the Raspberry Pi firmware's throttling and undervoltage word for
// the Status page's temperature panel. vcgencmd asks the firmware through /dev/vcio, which is
// root's alone, and the daemon's unit has DevicePolicy=closed. One typed operation with nothing
// in it: the helper runs exactly `vcgencmd get_throttled` and answers its output - vcgencmd never
// joins the Programs list, where it would take any command (`vcgencmd otp_dump`, `set_*`).

// opThrottled is the operation's name on the wire.
const opThrottled = "throttled"

// vcgencmdPath is where the Pi products put the program; a variable so a test can point it at a
// script. Where it is not installed (the VM, a container) the read answers ErrNotAvailable.
var vcgencmdPath = "/usr/bin/vcgencmd"

// ThrottledArgs is the whole argument list of the read.
func ThrottledArgs() []string { return []string{"get_throttled"} }

// Throttled runs `vcgencmd get_throttled`.
func (l Local) Throttled(ctx context.Context) (Result, error) {
	if _, err := os.Stat(vcgencmdPath); os.IsNotExist(err) {
		return Result{}, fmt.Errorf("%w: %s is not installed", ErrNotAvailable, vcgencmdPath)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return l.Run(ctx, vcgencmdPath, ThrottledArgs(), nil)
}

// Throttled asks the helper for the firmware's `get_throttled` answer. Nothing but the operation travels.
func (c Client) Throttled(ctx context.Context) (Result, error) {
	res, err := c.call(ctx, request{Op: opThrottled})
	return Result{Stdout: res.Stdout, Stderr: res.Stderr, Exit: res.Exit}, err
}
