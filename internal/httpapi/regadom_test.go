package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/system"
)

// occulited task 167: the kept ReGa database - its presence for system:read, the file for
// regadom:read only (system:write does not include it), its removal for system:write.
func TestKeptRegadomOverHTTP(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.Open(dir, auth.Options{SessionDir: filepath.Join(dir, "run", "sessions")})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Setup("admin", "secret123"); err != nil {
		t.Fatal(err)
	}
	token := func(name string, s ...auth.Scope) map[string]string {
		sec, err := store.CreateToken(name, auth.TokenOptions{Scopes: s})
		if err != nil {
			t.Fatal(err)
		}
		return map[string]string{"Authorization": "Bearer " + sec}
	}
	reader, logic, writer := token("reader", auth.ScopeSystemRead), token("logic", auth.ScopeRegadomRead), token("writer", auth.ScopeSystemWrite)
	root := t.TempDir()
	mux := http.NewServeMux()
	a := &AuthAPI{Store: store}
	a.Register(mux)
	(&SystemAPI{Root: system.Root(root)}).Register(mux)
	srv := httptest.NewServer(a.Middleware(mux))
	t.Cleanup(srv.Close)
	const p = "/api/system/v1/regadom"

	if st, out, _ := do(t, srv, "GET", p, "", reader); st != 200 || out["present"] != false {
		t.Fatalf("none kept: %d %v", st, out)
	}
	if st, out, _ := do(t, srv, "GET", p+"/file", "", logic); st != 404 || out["error"] != "no-regadom" {
		t.Fatalf("file of none: %d %v", st, out)
	}
	if st, _, _ := do(t, srv, "DELETE", p, "", writer); st != 404 {
		t.Fatalf("remove none: %d", st)
	}

	cfg := filepath.Join(root, "etc", "config")
	_ = os.MkdirAll(cfg, 0o755)
	db := []byte("ReGa database\x00bytes")
	_ = os.WriteFile(filepath.Join(cfg, "homematic.regadom"), db, 0o600)
	_ = os.WriteFile(filepath.Join(cfg, "homematic.regadom.bak"), []byte("old"), 0o600)

	if st, out, _ := do(t, srv, "GET", p, "", reader); st != 200 || out["present"] != true || out["bytes"] != float64(len(db)) || out["modified"] == "" {
		t.Fatalf("kept: %d %v", st, out)
	}
	// the file: regadom:read, and neither system:read nor system:write
	for _, h := range []map[string]string{reader, writer} {
		if st, _, _ := do(t, srv, "GET", p+"/file", "", h); st != 403 {
			t.Errorf("file without regadom:read: %d", st)
		}
	}
	req, _ := http.NewRequest("GET", srv.URL+p+"/file", nil)
	req.Header.Set("Authorization", logic["Authorization"])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != string(db) || res.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("file: %d %q %s", res.StatusCode, body, res.Header.Get("Content-Type"))
	}
	// removal: system:write, the .bak along
	for _, h := range []map[string]string{reader, logic} {
		if st, _, _ := do(t, srv, "DELETE", p, "", h); st != 403 {
			t.Errorf("remove without system:write: %d", st)
		}
	}
	if st, out, _ := do(t, srv, "DELETE", p, "", writer); st != 200 || out["removed"] != true || out["freed_bytes"] != float64(len(db)+3) {
		t.Fatalf("remove: %d %v", st, out)
	}
	for _, f := range []string{"homematic.regadom", "homematic.regadom.bak"} {
		if _, err := os.Lstat(filepath.Join(cfg, f)); !os.IsNotExist(err) {
			t.Errorf("%s still there", f)
		}
	}
	if st, out, _ := do(t, srv, "GET", p, "", reader); st != 200 || out["present"] != false {
		t.Fatalf("after: %d %v", st, out)
	}
}
