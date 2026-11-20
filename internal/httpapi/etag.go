package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// writeJSONRevalidated is writeJSON for a read that pages poll and that changes far less often
// than they ask (occulited task 162: the Status page's GET /status/history, a new sample a minute
// against a poll every 10 s, and GET /catalog): the answer carries an ETag, the hash of its body,
// and "Cache-Control: no-cache" instead of no-store; a request with the tag in If-None-Match gets
// an unchanged answer as a 304 without a body. The shell sends the tag itself (api.getTagged):
// Chrome caches nothing over a certificate it does not trust. Only a 200 is tagged.
func writeJSONRevalidated(w http.ResponseWriter, r *http.Request, status int, v any) {
	if status != http.StatusOK {
		writeJSON(w, status, v)
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "internal", Message: err.Error()})
		return
	}
	sum := sha256.Sum256(buf.Bytes())
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("ETag", etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// etagMatches is If-None-Match's weak comparison against one strong tag: "*", or the tag in its
// comma-separated list, with or without W/.
func etagMatches(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == "*" || t == etag {
			return true
		}
	}
	return false
}
