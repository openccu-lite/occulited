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
// in it: the helper runs exactly `vcgencmd get_throttled` (and, since task 177, `vcgencmd
// measure_clock arm`) and answers the output - vcgencmd never
// joins the Programs list, where it would take any command (`vcgencmd otp_dump`, `set_*`).

// opThrottled is the operation's name on the wire.
const opThrottled = "throttled"

// vcgencmdPath is where the Pi products put the program; a variable so a test can point it at a
// script. Where it is not installed (the VM, a container) the read answers ErrNotAvailable.
var vcgencmdPath = "/usr/bin/vcgencmd"

// ThrottledArgs is the whole argument list of the read.
func ThrottledArgs() []string { return []string{"get_throttled"} }

// ClockArgs is the second, fixed read of the same operation (occulited task 177): the ARM clock
// the firmware measures, the real one also while it caps the clock (cpufreq only knows what it
// asked for).
func ClockArgs() []string { return []string{"measure_clock", "arm"} }

// Throttled runs `vcgencmd get_throttled` and then `vcgencmd measure_clock arm`, and answers both
// outputs, one after the other ("throttled=0x0\nfrequency(48)=1500398464\n"). The clock is an
// extra: when its read fails the word alone is the answer, as before task 177.
func (l Local) Throttled(ctx context.Context) (Result, error) {
	if _, err := os.Stat(vcgencmdPath); os.IsNotExist(err) {
		return Result{}, fmt.Errorf("%w: %s is not installed", ErrNotAvailable, vcgencmdPath)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := l.Run(ctx, vcgencmdPath, ThrottledArgs(), nil)
	if err != nil || res.Exit != 0 {
		return res, err
	}
	if clk, cerr := l.Run(ctx, vcgencmdPath, ClockArgs(), nil); cerr == nil && clk.Exit == 0 {
		res.Stdout = append(res.Stdout, clk.Stdout...)
	}
	return res, nil
}

// Throttled asks the helper for the firmware's `get_throttled` answer. Nothing but the operation travels.
func (c Client) Throttled(ctx context.Context) (Result, error) {
	res, err := c.call(ctx, request{Op: opThrottled})
	return Result{Stdout: res.Stdout, Stderr: res.Stderr, Exit: res.Exit}, err
}
