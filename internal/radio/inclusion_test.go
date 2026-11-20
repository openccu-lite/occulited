package radio

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Task 158: hmipserver's lines about an inclusion, as the journal carries them (a thread and the
// access point before the message), and the neighbours that must not count.
func TestParseInclusionLine(t *testing.T) {
	const sgtin = "3014F711A0001F0000000A04"
	const pre = "[vert.x-eventloop-thread-2] AP 3014F711A000040000000A01: "
	cases := []struct {
		name, line string
		want       InclusionLine
		ok         bool
	}{
		{"asked", pre + "Inclusion " + sgtin + ": Step 1 Request handled 1A2B3C (1A2B3C)", InclusionLine{Kind: InclusionAsked, SGTIN: sgtin}, true},
		{"included", pre + "Device included " + sgtin, InclusionLine{Kind: InclusionIncluded, SGTIN: sgtin}, true},
		{"declined", pre + "The inclusion for device " + sgtin + " is declined, the local key is wrong", InclusionLine{Kind: InclusionKeyMismatch, SGTIN: sgtin}, true},
		{"invalid key", pre + "Invalid local key (size) of device " + sgtin + " in local key map", InclusionLine{Kind: InclusionKeyInvalid, SGTIN: sgtin}, true},
		{"another SGTIN", pre + "Whitelist inclusion mode. Wrong SGTIN " + sgtin + ". Do nothing", InclusionLine{Kind: InclusionOtherDevice, SGTIN: sgtin}, true},
		{"no install mode", pre + "Not in inclusion mode. Do nothing. Device " + sgtin + " IP-Address 1A2B3C MAC-Address 1A2B3C", InclusionLine{Kind: InclusionNoInstallMode, SGTIN: sgtin}, true},
		{"unknown type", pre + "No device description found for manufacturer code: 1 device type 4711 sgtin " + sgtin, InclusionLine{Kind: InclusionUnsupported, SGTIN: sgtin}, true},
		{"rejected", pre + "Device " + sgtin + " rejected, create transaction for removing", InclusionLine{Kind: InclusionRejected, SGTIN: sgtin}, true},
		{"aborted", pre + "Aborting inclusion process; device " + sgtin + " will stay in inclusion state REQUESTED", InclusionLine{Kind: InclusionAborted, SGTIN: sgtin}, true},
		{"lower case", pre + "Device included " + strings.ToLower(sgtin), InclusionLine{Kind: InclusionIncluded, SGTIN: sgtin}, true},
		{"http status", "[vert.x-worker-thread-3] AP 3014F711A000040000000A01: Frontend Server responded with HTTP Status 503 Service Unavailable, ignore response", InclusionLine{Kind: inclusionHTTPStatus, Status: 503}, true},
		// the neighbours: not an outcome
		{"key from the map", pre + "Add local key of device " + sgtin + " for inclusion from map to whitelist", InclusionLine{}, false},
		{"already in progress", pre + "Inclusion for device " + sgtin + " already in progress (state: REQUESTED, process state: X); Ignore the new inclusion request", InclusionLine{}, false},
		{"step 2", pre + "Inclusion " + sgtin + ": Step 2 response from get inclusion data 1A (1A) -> Router null", InclusionLine{}, false},
		{"re-inclusion", pre + "Accept re inclusion " + sgtin + ", no matter which inclusion mode is active", InclusionLine{}, false},
		{"rejection ignored", pre + "Inclusion ignore rejection of device " + sgtin + ", it was included in the mean time", InclusionLine{}, false},
		{"the access point's exchange", "Adapter exchange was rejected by key server", InclusionLine{}, false},
		{"empty", "", InclusionLine{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseInclusionLine(c.line)
			if ok != c.ok || got != c.want {
				t.Errorf("ParseInclusionLine(%q) = %+v, %v; want %+v, %v", c.line, got, ok, c.want, c.ok)
			}
		})
	}
}

// journalctl pre-filters on InclusionLinesPattern (case-insensitive): every line the parser reads
// must pass it, or the journal would never hand it over.
func TestInclusionLinesPatternLetsEveryLineThrough(t *testing.T) {
	re := regexp.MustCompile("(?i)" + InclusionLinesPattern)
	const s = "3014F711A0001F0000000A04"
	for _, l := range []string{
		"AP 30: Inclusion " + s + ": Step 1 Request handled 1A (1A)",
		"AP 30: Device included " + s,
		"AP 30: The inclusion for device " + s + " is declined, the local key is wrong",
		"AP 30: Invalid local key (size) of device " + s + " in local key map",
		"AP 30: Whitelist inclusion mode. Wrong SGTIN " + s + ". Do nothing",
		"AP 30: Not in inclusion mode. Do nothing. Device " + s + " IP-Address 1A MAC-Address 1A",
		"AP 30: No device description found for manufacturer code: 1 device type 2 sgtin " + s,
		"AP 30: Device " + s + " rejected, create transaction for removing",
		"AP 30: Aborting inclusion process; device " + s + " will stay in inclusion state REQUESTED",
		"AP 30: Frontend Server responded with HTTP Status 502 Bad Gateway, ignore response",
	} {
		if _, ok := ParseInclusionLine(l); !ok {
			t.Errorf("the parser does not read %q", l)
		}
		if !re.MatchString(l) {
			t.Errorf("the journal's filter would not pass %q", l)
		}
	}
}

func TestInclusionAttempts(t *testing.T) {
	const a, b = "3014F711A0001F0000000A04", "3014F711A0001F0000000B05"
	l := func(at, msg string) InclusionLogLine {
		return InclusionLogLine{Message: "AP 30: " + msg, At: "2026-11-20T10:00:" + at + "+01:00"}
	}
	asked := func(s string) string { return "Inclusion " + s + ": Step 1 Request handled 1A (1A)" }
	aborted := func(s string) string {
		return "Aborting inclusion process; device " + s + " will stay in inclusion state REQUESTED"
	}
	declined := func(s string) string { return "The inclusion for device " + s + " is declined, the local key is wrong" }
	notInMode := func(s string) string {
		return "Not in inclusion mode. Do nothing. Device " + s + " IP-Address 1A MAC-Address 1A"
	}

	t.Run("a retry's asked line does not hide the failure", func(t *testing.T) {
		got := InclusionAttempts([]InclusionLogLine{l("00", asked(a)), l("01", aborted(a)), l("10", asked(a))})
		if len(got) != 1 || got[0].Outcome != InclusionAborted || got[0].At != "2026-11-20T10:00:01+01:00" || got[0].Requests != 2 || got[0].HTTPStatus != 0 {
			t.Fatalf("%+v", got)
		}
		if got[0].Line != "AP 30: "+aborted(a) {
			t.Errorf("line %q", got[0].Line)
		}
	})
	t.Run("the key server's HTTP status joins the abort that follows it", func(t *testing.T) {
		got := InclusionAttempts([]InclusionLogLine{l("00", asked(a)), l("01", "Frontend Server responded with HTTP Status 503 Service Unavailable, ignore response"), l("01", aborted(a))})
		if len(got) != 1 || got[0].HTTPStatus != 503 {
			t.Fatalf("%+v", got)
		}
		// a status long before is another request's
		got = InclusionAttempts([]InclusionLogLine{{Message: "Frontend Server responded with HTTP Status 503", At: "2026-11-20T09:00:00+01:00"}, l("01", aborted(a))})
		if got[0].HTTPStatus != 0 {
			t.Errorf("an old status joined: %+v", got)
		}
	})
	t.Run("a wrong moment after a real failure keeps the failure", func(t *testing.T) {
		got := InclusionAttempts([]InclusionLogLine{l("00", declined(a)), l("20", notInMode(a))})
		if got[0].Outcome != InclusionKeyMismatch {
			t.Fatalf("%+v", got)
		}
		// but on its own it is said
		got = InclusionAttempts([]InclusionLogLine{l("00", asked(a)), l("20", notInMode(a))})
		if got[0].Outcome != InclusionNoInstallMode {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("a pairing that worked ends the story, and a later failure starts a new one", func(t *testing.T) {
		got := InclusionAttempts([]InclusionLogLine{l("00", declined(a)), l("30", asked(a)), l("31", "Device included "+a)})
		if got[0].Outcome != InclusionIncluded {
			t.Fatalf("%+v", got)
		}
		got = InclusionAttempts([]InclusionLogLine{l("00", "Device included "+a), l("40", notInMode(a))})
		if got[0].Outcome != InclusionNoInstallMode {
			t.Fatalf("after the pairing: %+v", got)
		}
	})
	t.Run("one attempt per device, in the order they came; the access point's status alone is none", func(t *testing.T) {
		got := InclusionAttempts([]InclusionLogLine{l("00", asked(b)), l("01", "Frontend Server responded with HTTP Status 500"), l("02", declined(a)), l("03", "not about an inclusion")})
		if len(got) != 2 || got[0].SGTIN != b || got[0].Outcome != InclusionAsked || got[1].SGTIN != a {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("times that do not parse still join the status", func(t *testing.T) {
		got := InclusionAttempts([]InclusionLogLine{{Message: "Frontend Server responded with HTTP Status 404"}, {Message: aborted(a)}})
		if got[0].HTTPStatus != 404 {
			t.Fatalf("%+v", got)
		}
	})
	if got := InclusionAttempts(nil); got == nil || len(got) != 0 {
		t.Errorf("nothing: %#v", got)
	}
}

func TestPairingReason(t *testing.T) {
	cases := []struct {
		outcome, mode, want string
		failure             bool
	}{
		{InclusionIncluded, "", "", false},
		{InclusionAsked, "", PairingIncomplete, false},
		{InclusionRejected, KeyServerLocal, PairingNoKey, true},
		{InclusionRejected, KeyServerLocalFallback, PairingKeyServerRefused, true},
		{InclusionRejected, KeyServerKeyServerOnly, PairingKeyServerRefused, true},
		{InclusionAborted, KeyServerLocalFallback, PairingKeyServerUnreachable, true},
		{InclusionKeyMismatch, "", PairingKeyMismatch, true},
		{InclusionKeyInvalid, "", PairingKeyInvalid, true},
		{InclusionUnsupported, "", PairingUnsupported, true},
		{InclusionOtherDevice, "", PairingOtherDevice, false},
		{InclusionNoInstallMode, "", PairingNoInstallMode, false},
	}
	for _, c := range cases {
		got := PairingReason(InclusionAttempt{Outcome: c.outcome}, c.mode)
		if got != c.want || PairingFailure(got) != c.failure {
			t.Errorf("%s under %q: %q (failure %v), want %q (%v)", c.outcome, c.mode, got, PairingFailure(got), c.want, c.failure)
		}
	}
}

func TestPairingMessage(t *testing.T) {
	const s = "3014F711A0001F0000000A04"
	m := PairingMessage(PairingKeyServerUnreachable, s, 0)
	if !strings.Contains(m.EN, s) || !strings.Contains(m.DE, s) || strings.Contains(m.EN, "{") || strings.Contains(m.EN, "HTTP") {
		t.Errorf("unreachable: %+v", m)
	}
	m = PairingMessage(PairingKeyServerUnreachable, s, 503)
	if !strings.Contains(m.EN, "HTTP status 503") || !strings.Contains(m.DE, "HTTP-Status 503") {
		t.Errorf("with a status: %+v", m)
	}
	if m := PairingMessage("", s, 0); m != (PairingText{}) {
		t.Errorf("a pairing that worked has no sentence: %+v", m)
	}
	for reason, txt := range pairingTexts {
		if txt.EN == "" || txt.DE == "" || !strings.Contains(txt.EN, "{sgtin}") || !strings.Contains(txt.DE, "{sgtin}") {
			t.Errorf("%s: %+v", reason, txt)
		}
	}
}

// The Status page's warning hmip-pairing-failed says the same as the API: every sentence is in the
// UI's catalogue with the same German, word for word (D-12), so the two cannot drift apart.
func TestPairingTextsAreTheUIs(t *testing.T) {
	cat, err := os.ReadFile("../../ui/src/lib/i18n.svelte.ts")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := os.ReadFile("../../ui/src/lib/warnings.ts")
	if err != nil {
		t.Fatal(err)
	}
	for reason, txt := range pairingTexts {
		entry := `"` + txt.EN + `": {de: "` + txt.DE + `"}`
		if !strings.Contains(string(cat), entry) {
			t.Errorf("%s: the catalogue has no entry\n    %s", reason, entry)
		}
		if !strings.Contains(string(ws), `"`+txt.EN+`"`) {
			t.Errorf("%s: warnings.ts does not say %q", reason, txt.EN)
		}
	}
}
