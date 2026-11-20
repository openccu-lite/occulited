package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hobbyquaker/occulited/internal/radio"
	"github.com/hobbyquaker/occulited/internal/system"
)

// a journal entry of hmipserver's, as journalctl -o json writes it
func pairingEntry(t *testing.T, us int64, msg string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"__REALTIME_TIMESTAMP": strconv.FormatInt(us, 10), "MESSAGE": "AP 3014F711A000040000000A01: " + msg, "_SYSTEMD_UNIT": "hmipserver.service"})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// pairingAPI is a SystemAPI over a fake journal and a root whose hmip_user.conf names mode.
func pairingAPI(t *testing.T, mode string, out *string, args *[]string) *SystemAPI {
	t.Helper()
	root := t.TempDir()
	if mode != "" {
		p := filepath.Join(root, "etc/config/crRFD/hmip_user.conf")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("KeyServer.Mode="+mode+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &SystemAPI{Root: system.Root(root), Journal: &system.JournalLog{Run: func(_ context.Context, _ string, a ...string) ([]byte, error) {
		if args != nil {
			*args = a
		}
		return []byte(*out), nil
	}}}
}

const (
	pairSGTIN = "3014F711A0001F0000000A04"
	pairOther = "3014F711A0001F0000000B05"
)

// Task 158: the failures that need a hand are warnings - one per device, the reason in params, the
// way out in href - and the key mismatch stays task 201's hmip-key-declined.
func TestHmIPPairingFailedWarning(t *testing.T) {
	asked := "Inclusion " + pairSGTIN + ": Step 1 Request handled 1A (1A)"
	aborted := "Aborting inclusion process; device " + pairSGTIN + " will stay in inclusion state REQUESTED"
	status := "Frontend Server responded with HTTP Status 503 Service Unavailable, ignore response"
	rejected := "Device " + pairSGTIN + " rejected, create transaction for removing"

	var out string
	a := pairingAPI(t, radio.KeyServerLocalFallback, &out, nil)

	// the key server not reached
	out = pairingEntry(t, 1_700_000_000_000_000, asked) + pairingEntry(t, 1_700_000_001_000_000, aborted)
	w, ok := a.hmipPairingWarnings(context.Background())
	if !ok || len(w) != 1 || w[0].ID != "hmip-pairing-failed" || w[0].Params["reason"] != radio.PairingKeyServerUnreachable || w[0].Href != "/system/network" {
		t.Fatalf("unreachable: %+v %v", w, ok)
	}
	if w[0].Params["sgtin"] != pairSGTIN || w[0].Params["address"] != "001F0000000A04" || w[0].Variant != pairSGTIN+"@key-server-unreachable@"+w[0].Params["at"].(string) || w[0].Params["http_status"] != nil {
		t.Errorf("params: %+v", w[0])
	}
	// an HTTP status other than 200 rides along
	out = pairingEntry(t, 1_700_000_000_000_000, asked) + pairingEntry(t, 1_700_000_001_000_000, status) + pairingEntry(t, 1_700_000_001_000_100, aborted)
	if w, _ := a.hmipPairingWarnings(context.Background()); len(w) != 1 || w[0].Params["http_status"] != 503 {
		t.Errorf("with a status: %+v", w)
	}

	// refused by eQ-3's key server: no page to go to
	out = pairingEntry(t, 1_700_000_000_000_000, asked) + pairingEntry(t, 1_700_000_002_000_000, rejected)
	if w, _ := a.hmipPairingWarnings(context.Background()); len(w) != 1 || w[0].Params["reason"] != radio.PairingKeyServerRefused || w[0].Href != "" {
		t.Errorf("refused: %+v", w)
	}
	// the same line in local key mode: the system has no key for it
	local := pairingAPI(t, radio.KeyServerLocal, &out, nil)
	if w, _ := local.hmipPairingWarnings(context.Background()); len(w) != 1 || w[0].Params["reason"] != radio.PairingNoKey || w[0].Href != "/system/keys#device-keys" {
		t.Errorf("no key: %+v", w)
	}

	// the key mismatch is task 201's warning, beside another device's failure
	out = pairingEntry(t, 1_700_000_000_000_000, "The inclusion for device "+pairOther+" is declined, the local key is wrong") +
		pairingEntry(t, 1_700_000_001_000_000, "No device description found for manufacturer code: 1 device type 4711 sgtin "+pairSGTIN)
	w, _ = a.hmipPairingWarnings(context.Background())
	if len(w) != 2 || w[0].ID != "hmip-key-declined" || w[0].Params["sgtin"] != pairOther || w[0].Params["reason"] != nil ||
		w[1].ID != "hmip-pairing-failed" || w[1].Params["reason"] != radio.PairingUnsupported || w[1].Href != "/system/updates#system-update" {
		t.Fatalf("two devices: %+v", w)
	}

	// a request at the wrong moment, one still running, and a pairing that worked are no warnings
	out = pairingEntry(t, 1_700_000_000_000_000, "Not in inclusion mode. Do nothing. Device "+pairSGTIN+" IP-Address 1A MAC-Address 1A") +
		pairingEntry(t, 1_700_000_001_000_000, "Whitelist inclusion mode. Wrong SGTIN "+pairOther+". Do nothing") +
		pairingEntry(t, 1_700_000_002_000_000, "Inclusion 3014F711A0001F0000000C06: Step 1 Request handled 1A (1A)") +
		pairingEntry(t, 1_700_000_003_000_000, rejected) + pairingEntry(t, 1_700_000_004_000_000, "Device included "+pairSGTIN)
	if w, ok := a.hmipPairingWarnings(context.Background()); !ok || len(w) != 0 {
		t.Errorf("no failure: %+v %v", w, ok)
	}
}

// GET /radio/hmip/pairing: every attempt with its reason and sentence, the system's clock to come
// back with, and since cutting the window to the exact time.
func TestHmIPPairingRoute(t *testing.T) {
	var out string
	var args []string
	a := pairingAPI(t, "", &out, &args)
	get := func(q string) (int, hmipPairingView) {
		t.Helper()
		rec := httptest.NewRecorder()
		a.hmipPairing(rec, httptest.NewRequest(http.MethodGet, "/api/system/v1/radio/hmip/pairing"+q, nil))
		var v hmipPairingView
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		return rec.Code, v
	}

	code, v := get("")
	if code != 200 || v.Now == "" || v.Since != "" || v.KeyServerMode != radio.KeyServerLocalFallback || v.Attempts == nil || len(v.Attempts) != 0 {
		t.Fatalf("quiet: %d %+v", code, v)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-u hmipserver.service", "--boot=0", "-g " + radio.InclusionLinesPattern} {
		if !strings.Contains(joined, want) {
			t.Errorf("journalctl args %q miss %q", joined, want)
		}
	}
	if strings.Contains(joined, "--since") {
		t.Errorf("no since asked, yet %q", joined)
	}

	// 2023-11-14T22:13:20Z and a second later
	out = pairingEntry(t, 1_700_000_000_000_000, "The inclusion for device "+pairOther+" is declined, the local key is wrong") +
		pairingEntry(t, 1_700_000_001_000_000, "Inclusion "+pairSGTIN+": Step 1 Request handled 1A (1A)") +
		pairingEntry(t, 1_700_000_001_500_000, "Frontend Server responded with HTTP Status 502 Bad Gateway, ignore response") +
		pairingEntry(t, 1_700_000_001_600_000, "Aborting inclusion process; device "+pairSGTIN+" will stay in inclusion state REQUESTED")
	code, v = get("")
	if code != 200 || len(v.Attempts) != 2 {
		t.Fatalf("two: %d %+v", code, v)
	}
	d, k := v.Attempts[0], v.Attempts[1]
	if d.SGTIN != pairOther || d.Outcome != radio.InclusionKeyMismatch || d.Reason != radio.PairingKeyMismatch || !d.Failed || d.Message == nil || !strings.Contains(d.Message.DE, pairOther) {
		t.Errorf("declined: %+v", d)
	}
	if k.Reason != radio.PairingKeyServerUnreachable || k.HTTPStatus != 502 || k.Requests != 1 || !strings.Contains(k.Message.EN, "HTTP status 502") || !strings.Contains(k.Line, "Aborting inclusion process") || k.Address != "001F0000000A04" {
		t.Errorf("unreachable: %+v", k)
	}

	// since: journalctl gets whole seconds, the answer the exact window
	code, v = get("?since=2023-11-14T22:13:20.5Z")
	if code != 200 || len(v.Attempts) != 1 || v.Attempts[0].SGTIN != pairSGTIN || v.Since == "" {
		t.Fatalf("since: %d %+v", code, v)
	}
	if !strings.Contains(strings.Join(args, " "), "--since @1700000000") {
		t.Errorf("journalctl args %q", args)
	}
	if code, _ := get("?since=yesterday"); code != 400 {
		t.Errorf("a since that is no time: %d", code)
	}

	// a pairing that worked has no reason and no sentence
	out = pairingEntry(t, 1_700_000_000_000_000, "Device included "+pairSGTIN)
	if _, v := get(""); len(v.Attempts) != 1 || v.Attempts[0].Reason != "" || v.Attempts[0].Failed || v.Attempts[0].Message != nil {
		t.Errorf("included: %+v", v)
	}

	// without a journal there is nothing to read
	rec := httptest.NewRecorder()
	(&SystemAPI{}).hmipPairing(rec, httptest.NewRequest(http.MethodGet, "/api/system/v1/radio/hmip/pairing", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("no journal: %d", rec.Code)
	}
}
