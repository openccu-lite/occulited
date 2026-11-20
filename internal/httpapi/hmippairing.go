package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/hobbyquaker/occulited/internal/radio"
	"github.com/hobbyquaker/occulited/internal/system"
	"github.com/hobbyquaker/occulited/internal/warnings"
)

// Occulited task 158 (the maintainer, 2026-11-20): no reachability check before a pairing; a
// pairing that fails says why. hmipserver tells no RPC client why it turned a device down, so the
// reasons come from the lines it writes (internal/radio's inclusion.go): two Status warnings for
// the failures that need a hand - hmip-key-declined (task 201) and hmip-pairing-failed - and
// GET /radio/hmip/pairing for a pairing client, which asks for its own window.

// pairingLines is how many of hmipserver's matching lines one read takes, newest first: a device
// in pairing mode asks about every ten seconds and each request writes a line or two, so this is
// a quarter of an hour of a device that keeps trying - and the newest line per device is what
// counts.
const pairingLines = 500

// readInclusionLines reads hmipserver's inclusion lines of this boot, since a time when given.
func (a *SystemAPI) readInclusionLines(since time.Time) ([]radio.InclusionLogLine, error) {
	q := system.LogQuery{Unit: hmipserverUnit, Boot: "0", Contains: radio.InclusionLinesPattern, Limit: pairingLines}
	if !since.IsZero() {
		// journalctl's --since has whole seconds; the lines are cut to the exact time below
		q.Since = "@" + strconv.FormatInt(since.Unix(), 10)
	}
	lines, err := a.Journal.Read(q)
	if err != nil {
		return nil, err
	}
	out := make([]radio.InclusionLogLine, 0, len(lines))
	for _, l := range lines {
		at := l.Timestamp
		if at == "" {
			at = l.Time
		}
		if !since.IsZero() {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil && t.Before(since) {
				continue
			}
		}
		out = append(out, radio.InclusionLogLine{Message: l.Message, At: at})
	}
	return out, nil
}

// keyServerMode is HmIP-RF's key-server mode as hmip_user.conf says it (the shipped template's
// KEYSERVER_LOCAL when unset).
func (a *SystemAPI) keyServerMode() string { return system.HmIPPairing(a.Root).KeyServerMode }

// hmipPairingAttempt is one device's attempt as GET /radio/hmip/pairing answers it.
type hmipPairingAttempt struct {
	SGTIN   string `json:"sgtin"`
	Address string `json:"address,omitempty"`
	// Outcome is what hmipserver's lines say (radio.Inclusion*), Reason what it means under the
	// key-server mode (radio.Pairing*; empty for a pairing that worked)
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
	// Failed: the pairing failed for a reason that needs a hand (the Status page warns of these);
	// false for a pairing that worked, one that may still be running, and a request at the wrong
	// moment (no install mode, another SGTIN's pairing)
	Failed     bool               `json:"failed"`
	At         string             `json:"at,omitempty"`
	HTTPStatus int                `json:"http_status,omitempty"`
	Requests   int                `json:"requests"`
	Message    *radio.PairingText `json:"message,omitempty"`
	// Line is hmipserver's own line the outcome was read from
	Line string `json:"line"`
}

// hmipPairingView is GET /radio/hmip/pairing's answer.
type hmipPairingView struct {
	// Now is the system's clock: a client takes it as the start of its window and passes it back
	// as since, so its own clock never meets the system's
	Now           string               `json:"now"`
	Since         string               `json:"since,omitempty"`
	KeyServerMode string               `json:"keyserver_mode"`
	Attempts      []hmipPairingAttempt `json:"attempts"`
}

func (a *SystemAPI) hmipPairingAttempts(lines []radio.InclusionLogLine, mode string) []hmipPairingAttempt {
	out := []hmipPairingAttempt{}
	for _, at := range radio.InclusionAttempts(lines) {
		reason := radio.PairingReason(at, mode)
		p := hmipPairingAttempt{SGTIN: at.SGTIN, Address: radio.DeviceAddressOf(at.SGTIN), Outcome: at.Outcome, Reason: reason, Failed: radio.PairingFailure(reason),
			At: at.At, HTTPStatus: at.HTTPStatus, Requests: at.Requests, Line: at.Line}
		if reason != "" {
			m := radio.PairingMessage(reason, at.SGTIN, at.HTTPStatus)
			p.Message = &m
		}
		out = append(out, p)
	}
	return out
}

// hmipPairing is GET /radio/hmip/pairing[?since=<RFC 3339>]: the HmIP pairing attempts of this
// boot, or since the time given (the now of an earlier answer), one per device with why it failed.
func (a *SystemAPI) hmipPairing(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	var since time.Time
	if s := r.URL.Query().Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Message: "since is a time in RFC 3339 (the now of an earlier answer)"})
			return
		}
		since = t
	}
	v := hmipPairingView{Now: now.Format(time.RFC3339Nano), KeyServerMode: a.keyServerMode(), Attempts: []hmipPairingAttempt{}}
	if !since.IsZero() {
		v.Since = since.Format(time.RFC3339Nano)
	}
	if a.Journal == nil {
		writeJSON(w, http.StatusNotImplemented, apiError{Error: "unsupported", Message: "no journal to read hmipserver's lines from"})
		return
	}
	lines, err := a.readInclusionLines(since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "journal", Message: err.Error()})
		return
	}
	v.Attempts = a.hmipPairingAttempts(lines, v.KeyServerMode)
	writeJSON(w, http.StatusOK, v)
}

// hmipPairingWarnings is the Status page's two pairing warnings, from one read of this boot's
// journal:
//
//   - hmip-key-declined (task 201, decided in task 154's Q&A): hmipserver turned an inclusion
//     request down because the device's local key it was given (in sgtin.map, or entered for the
//     pairing) is not that device's key. Without it the device simply never appears.
//   - hmip-pairing-failed (task 158): the other failures that need a hand - eQ-3's key server not
//     reached, or refusing the device; no key in local key mode; a stored key of the wrong length;
//     a device type the system's HmIP software does not know. params.reason says which.
//
// The image's log4j2 template keeps the logger of the inclusion steps at debug whatever
// LOGLEVEL_HMIP says, so its lines are there at the warn default too. Read on demand, pre-filtered
// by journalctl, so the usual answer costs one grep that finds nothing. The devices are listed only
// when there is a failure to clear: a pairing that has since worked takes its warning away, which is
// the only way it can go - the lines stay in the journal. One warning per device, the variant the
// SGTIN and the time of its newest failure line (and for hmip-pairing-failed the reason between
// them), so a further failure after a silence is new.
func (a *SystemAPI) hmipPairingWarnings(ctx context.Context) ([]warnings.Warning, bool) {
	if a.Journal == nil {
		return nil, true
	}
	lines, err := a.readInclusionLines(time.Time{})
	if err != nil {
		return nil, false
	}
	if len(lines) == 0 {
		return nil, true
	}
	var failed []hmipPairingAttempt
	var sgtins []string
	for _, p := range a.hmipPairingAttempts(lines, a.keyServerMode()) {
		if p.Failed {
			failed = append(failed, p)
			sgtins = append(sgtins, p.SGTIN)
		}
	}
	if len(failed) == 0 {
		return nil, true
	}
	paired := map[string]bool{}
	if a.HmIPDeviceKeys != nil {
		p, ok := a.HmIPDeviceKeys.PairedSGTINs(ctx, sgtins)
		if !ok {
			// hmipserver did not answer: whether the device is in now is unknown, and unknown is
			// not "still failed" - the silences stay, nothing is shown (warnings.Source's ok)
			return nil, false
		}
		paired = p
	}
	var out []warnings.Warning
	for _, p := range failed {
		if paired[p.SGTIN] {
			continue
		}
		params := map[string]any{"sgtin": p.SGTIN, "address": p.Address}
		if p.At != "" {
			params["at"] = p.At
		}
		if p.Reason == radio.PairingKeyMismatch {
			out = append(out, warnings.Warning{ID: "hmip-key-declined", Variant: p.SGTIN + "@" + p.At, Severity: warnings.SeverityWarning,
				Href: "/system/keys#device-keys", Params: params})
			continue
		}
		params["reason"] = p.Reason
		if p.HTTPStatus != 0 {
			params["http_status"] = p.HTTPStatus
		}
		// the reason in the variant: a panel the warning leads to names it (the UI's warnedge), and
		// another reason after a silence is a new warning
		out = append(out, warnings.Warning{ID: "hmip-pairing-failed", Variant: p.SGTIN + "@" + p.Reason + "@" + p.At, Severity: warnings.SeverityWarning,
			Href: pairingFailedHref[p.Reason], Params: params})
	}
	return out, true
}

// pairingFailedHref is where hmip-pairing-failed's matter is handled, by reason: the key list for
// a missing or broken key, the network for an unreachable key server, the updates for an unknown
// device type; a device the key server refused is handled at the other central unit (none).
var pairingFailedHref = map[string]string{
	radio.PairingNoKey:                "/system/keys#device-keys",
	radio.PairingKeyInvalid:           "/system/keys#device-keys",
	radio.PairingKeyServerUnreachable: "/system/network",
	radio.PairingUnsupported:          "/system/updates#system-update",
}
