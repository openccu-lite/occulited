package radio

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- why an HmIP pairing failed (occulited task 158) -------------------------------------------
//
// The maintainer, 2026-11-20: no reachability check before a pairing; a pairing that fails says why.
// hmipserver tells no RPC client why it turned a device down - the device simply never appears -
// but it writes a line for most of the reasons, and those lines are what this reads. The logger of
// the inclusion steps is kept at debug by the image's log4j2 template whatever LOGLEVEL_HMIP says
// (task 201), so its lines reach the journal at the stock level; the key server's HTTP warning and
// the unknown-device warning are at warn anyway.
//
// What the lines tell apart, per device (its SGTIN):
//   - the device asked to join at all (a request was handled) - so "no device answered" is wrong
//   - the device's key given to the system does not match it (task 201's decline)
//   - the stored key is no key (the map entry has the wrong length)
//   - a pairing with a key was open for another SGTIN, and this device asked
//   - no install mode was open when it asked
//   - HmIP-RF does not know the device type (a device newer than the system's HmIP software)
//   - the key server, or the local key check, did not accept it (rejected)
//   - the key server was not reached: no answer, or an HTTP status other than 200 (aborted)
//   - it was paired
//
// Duty cycle and "paired elsewhere" have no line of their own: a device still paired with another
// central unit is refused by eQ-3's key server, which is the rejected case.

// The kinds of inclusion line, and the outcomes an attempt can end in.
const (
	InclusionAsked         = "asked"              // a request of the device was handled (step 1)
	InclusionIncluded      = "included"           // the device was paired
	InclusionKeyMismatch   = "key-mismatch"       // the device's key given to the system is not its key
	InclusionKeyInvalid    = "key-invalid"        // the stored key has the wrong length
	InclusionOtherDevice   = "other-device"       // a pairing with key was open for another SGTIN
	InclusionNoInstallMode = "no-install-mode"    // no install mode was open
	InclusionUnsupported   = "unsupported-device" // HmIP-RF does not know the device type
	InclusionRejected      = "rejected"           // the key server (or the local key check) did not accept it
	InclusionAborted       = "aborted"            // the key server was not reached
	// inclusionHTTPStatus is the key server's answer other than 200: it names the access point,
	// not the device, and is joined to the device's aborted line that follows it
	inclusionHTTPStatus = "http-status"
)

// The reasons a failed attempt is explained with: the outcomes, with rejected and aborted told
// apart by the key-server mode and the key server's answer.
const (
	PairingKeyMismatch          = InclusionKeyMismatch
	PairingKeyInvalid           = InclusionKeyInvalid
	PairingOtherDevice          = InclusionOtherDevice
	PairingNoInstallMode        = InclusionNoInstallMode
	PairingUnsupported          = InclusionUnsupported
	PairingNoKey                = "no-key"                 // rejected in local key mode: the system has no key for it
	PairingKeyServerRefused     = "key-server-refused"     // rejected by eQ-3's key server
	PairingKeyServerUnreachable = "key-server-unreachable" // aborted: the key server not reached
	PairingIncomplete           = "incomplete"             // it asked, and nothing else was heard
)

// InclusionLinesPattern is what journalctl pre-filters hmipserver's log on (-g, a regular
// expression, case-insensitive there): every line ParseInclusionLine reads carries one of these.
const InclusionLinesPattern = `Step 1 Request handled|Device included|` + InclusionDeclinedMatch +
	`|Invalid local key \(size\)|Wrong SGTIN|Not in inclusion mode|No device description found|rejected, create transaction|Aborting inclusion process|Frontend Server responded with HTTP Status`

// sgtinPart is how loosely a device is read out of a line: as InclusionDeclinedSGTIN does, an id a
// little shorter or longer than an SGTIN is still reported rather than the line dropped.
const sgtinPart = `([0-9A-Za-z]{6,32})`

var inclusionLineRes = []struct {
	kind string
	re   *regexp.Regexp
}{
	{InclusionAsked, regexp.MustCompile(`Inclusion ` + sgtinPart + `: Step 1 Request handled`)},
	{InclusionIncluded, regexp.MustCompile(`: Device included ` + sgtinPart + `\b`)},
	{InclusionKeyMismatch, inclusionDeclinedRe},
	{InclusionKeyInvalid, regexp.MustCompile(`Invalid local key \(size\) of device ` + sgtinPart + ` in local key map`)},
	{InclusionOtherDevice, regexp.MustCompile(`Whitelist inclusion mode\. Wrong SGTIN ` + sgtinPart + `\.`)},
	{InclusionNoInstallMode, regexp.MustCompile(`Not in inclusion mode\. Do nothing\. Device ` + sgtinPart + `\b`)},
	{InclusionUnsupported, regexp.MustCompile(`No device description found for manufacturer code: \S+ device type \S+ sgtin ` + sgtinPart + `\b`)},
	{InclusionRejected, regexp.MustCompile(`: Device ` + sgtinPart + ` rejected, create transaction for removing`)},
	{InclusionAborted, regexp.MustCompile(`Aborting inclusion process; device ` + sgtinPart + ` will stay in inclusion state`)},
}

var inclusionHTTPStatusRe = regexp.MustCompile(`Frontend Server responded with HTTP Status (\d{3})`)

// InclusionLine is one of hmipserver's lines about an inclusion: its kind, the device (empty for
// the key server's HTTP status, which names the access point only) and that status.
type InclusionLine struct {
	Kind   string
	SGTIN  string
	Status int
}

// ParseInclusionLine reads one line of hmipserver's log; false when it is none of the lines above.
func ParseInclusionLine(line string) (InclusionLine, bool) {
	if m := inclusionHTTPStatusRe.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1])
		return InclusionLine{Kind: inclusionHTTPStatus, Status: n}, true
	}
	for _, p := range inclusionLineRes {
		if m := p.re.FindStringSubmatch(line); m != nil {
			return InclusionLine{Kind: p.kind, SGTIN: strings.ToUpper(m[1])}, true
		}
	}
	return InclusionLine{}, false
}

// InclusionLogLine is a line as the journal gives it: the message and its time (RFC 3339).
type InclusionLogLine struct {
	Message string
	At      string
}

// InclusionAttempt is what one device's lines add up to: the outcome, when it was written, the
// line itself, and for an aborted attempt the key server's HTTP status when it answered one.
type InclusionAttempt struct {
	SGTIN      string
	Outcome    string
	At         string
	Line       string
	HTTPStatus int
	// Requests is how many of its requests hmipserver handled (step 1)
	Requests int
}

// outcomeRank orders the outcomes for one device: a later line replaces the outcome only when its
// rank is at least the outcome's. A failure outranks "asked", which every retry writes again, and
// the two that only say the request came at the wrong moment (no install mode, another SGTIN) do
// not hide a real failure - a device that keeps trying after its install mode ended must not turn
// a key mismatch into "no install mode". Included is reset by nothing but itself: a pairing that
// worked ends the story, and lines after it start a new one.
var outcomeRank = map[string]int{
	InclusionAsked:         1,
	InclusionNoInstallMode: 2,
	InclusionOtherDevice:   2,
	InclusionKeyMismatch:   3,
	InclusionKeyInvalid:    3,
	InclusionUnsupported:   3,
	InclusionRejected:      3,
	InclusionAborted:       3,
}

// httpStatusWindow is how close before a device's aborted line the key server's HTTP status must
// be to count as that attempt's: the status line and the abort are written within the same
// request, a few milliseconds apart; lines carry no request id that would join them.
const httpStatusWindow = 30 // seconds

// InclusionAttempts folds hmipserver's lines (oldest first) into one attempt per device, in the
// order the devices first appeared.
func InclusionAttempts(lines []InclusionLogLine) []InclusionAttempt {
	byDevice := map[string]*InclusionAttempt{}
	var order []string
	var lastStatus int
	var lastStatusAt string
	for _, l := range lines {
		p, ok := ParseInclusionLine(l.Message)
		if !ok {
			continue
		}
		if p.Kind == inclusionHTTPStatus {
			lastStatus, lastStatusAt = p.Status, l.At
			continue
		}
		a := byDevice[p.SGTIN]
		if a == nil {
			a = &InclusionAttempt{SGTIN: p.SGTIN}
			byDevice[p.SGTIN] = a
			order = append(order, p.SGTIN)
		}
		if p.Kind == InclusionAsked {
			a.Requests++
		}
		switch {
		case p.Kind == InclusionIncluded:
		case a.Outcome == InclusionIncluded:
			// a new story after a pairing that worked
		case outcomeRank[p.Kind] < outcomeRank[a.Outcome]:
			continue
		}
		a.Outcome, a.At, a.Line, a.HTTPStatus = p.Kind, l.At, strings.TrimSpace(l.Message), 0
		if p.Kind == InclusionAborted && lastStatus != 0 && within(lastStatusAt, l.At, httpStatusWindow) {
			a.HTTPStatus = lastStatus
		}
	}
	out := make([]InclusionAttempt, 0, len(order))
	for _, s := range order {
		out = append(out, *byDevice[s])
	}
	return out
}

// within says whether the earlier time is at most n seconds before the later one; two times that
// do not parse are taken as close (the lines were read in order, so the status came first).
func within(earlier, later string, n int) bool {
	e, err1 := time.Parse(time.RFC3339Nano, earlier)
	l, err2 := time.Parse(time.RFC3339Nano, later)
	if err1 != nil || err2 != nil {
		return true
	}
	d := l.Sub(e).Seconds()
	return d >= 0 && d <= float64(n)
}

// PairingReason explains an attempt's outcome under the key-server mode hmip_user.conf says
// (LOCAL, KEYSERVER, KEYSERVER_LOCAL): "" for a pairing that worked.
func PairingReason(a InclusionAttempt, keyServerMode string) string {
	switch a.Outcome {
	case InclusionIncluded:
		return ""
	case InclusionAsked:
		return PairingIncomplete
	case InclusionRejected:
		// in local key mode hmipserver asks no key server: the refusal is its own, for a device
		// it has no key for (a wrong key given for the pairing is the key-mismatch line instead)
		if keyServerMode == KeyServerLocal {
			return PairingNoKey
		}
		return PairingKeyServerRefused
	case InclusionAborted:
		return PairingKeyServerUnreachable
	}
	return a.Outcome
}

// PairingFailure says whether a reason is a failed pairing worth a Status warning: the ones that
// say only that the request came at the wrong moment (no install mode, another SGTIN's pairing)
// are what any device near the system in pairing mode writes, and an incomplete attempt may still
// be running. A pairing client asks for those within its own window instead.
func PairingFailure(reason string) bool {
	switch reason {
	case PairingKeyMismatch, PairingKeyInvalid, PairingUnsupported, PairingNoKey, PairingKeyServerRefused, PairingKeyServerUnreachable:
		return true
	}
	return false
}
