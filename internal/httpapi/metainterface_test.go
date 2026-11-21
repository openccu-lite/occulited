package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hobbyquaker/occulited/internal/addoniface"
	"github.com/hobbyquaker/occulited/internal/auth"
)

// occulited task 169: an addon names the devices of the interface it provides - its own token with
// meta:interface, its own interface only, names only; meta:write is not it, and a person's token
// cannot carry the scope.
func TestInterfaceNames(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.Open(dir, auth.Options{SessionDir: filepath.Join(dir, "s")})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Setup("admin", "secret123"); err != nil {
		t.Fatal(err)
	}
	ms := mustMeta(t)
	mux := http.NewServeMux()
	a := &AuthAPI{Store: store}
	a.Register(mux)
	(&MetaAPI{Store: ms}).Register(mux)
	srv := httptest.NewServer(a.Middleware(mux))
	t.Cleanup(srv.Close)
	addoniface.Set([]addoniface.Entry{{Name: "Logic", Addon: "logic", URL: "xmlrpc://127.0.0.1:39310"}, {Name: "Other", Addon: "other", URL: "xmlrpc://127.0.0.1:39320"}})
	t.Cleanup(func() { addoniface.Set(nil) })
	bearer := func(sec string) map[string]string { return map[string]string{"Authorization": "Bearer " + sec} }
	logicSec, granted, _, err := store.MintAddonToken("logic", []string{"meta:interface"})
	if err != nil || len(granted) != 1 {
		t.Fatal(granted, err)
	}
	logic := bearer(logicSec)
	notInterface, _, _, _ := store.MintAddonToken("plain", []string{"meta:interface"})
	writer, _ := store.CreateToken("writer", auth.TokenOptions{Scopes: auth.Scopes{auth.ScopeMetaWrite}})
	const p = "/api/meta/v1/interfaces/Logic/objects:bulk"

	body := `{"set":{"Logic.var-regen":{"name":"Regen"},"Logic.var-regen:1":{"name":"Regen"}}}`
	if st, out, _ := do(t, srv, "POST", p, body, logic); st != 200 || out["revision"] == nil {
		t.Fatalf("names: %d %v", st, out)
	}
	if o, ok := ms.Snapshot().Objects["Logic.var-regen:1"]; !ok || o.Name != "Regen" {
		t.Fatalf("stored: %+v", o)
	}
	// a token with meta:write is a person's, not the addon's: the route is the addon's alone
	if st, _, _ := do(t, srv, "POST", p, body, bearer(writer)); st != 403 {
		t.Errorf("meta:write: %d", st)
	}
	// an addon that does not provide the interface, an interface not the caller's
	if st, out, _ := do(t, srv, "POST", p, body, bearer(notInterface)); st != 404 || out["error"] != "not-your-interface" {
		t.Errorf("another addon: %d %v", st, out)
	}
	if st, out, _ := do(t, srv, "POST", "/api/meta/v1/interfaces/Other/objects:bulk", `{"set":{"Other.x":{"name":"x"}}}`, logic); st != 404 || out["error"] != "not-your-interface" {
		t.Errorf("another interface: %d %v", st, out)
	}
	// refs of another interface, anything but a name
	if st, out, _ := do(t, srv, "POST", p, `{"set":{"HmIP-RF.ABC0000001:1":{"name":"x"}}}`, logic); st != 422 || out["error"] != "invalid-ref" {
		t.Errorf("a radio's ref: %d %v", st, out)
	}
	if st, _, _ := do(t, srv, "POST", p, `{"delete":["HmIP-RF.ABC0000001:1"]}`, logic); st != 422 {
		t.Errorf("a radio's ref deleted: %d", st)
	}
	if st, out, _ := do(t, srv, "POST", p, `{"set":{"Logic.var-regen:1":{"name":"Regen","enums":["rooms/kitchen"]}}}`, logic); st != 422 || out["error"] != "invalid-body" {
		t.Errorf("a room: %d %v", st, out)
	}
	if st, _, _ := do(t, srv, "POST", p, `{"delete":["Logic.var-regen"]}`, logic); st != 200 {
		t.Errorf("delete: %d", st)
	}
	if _, ok := ms.Snapshot().Objects["Logic.var-regen"]; ok {
		t.Error("not deleted")
	}
	// switched off: the interface is not listed, the route refuses
	addoniface.Set(nil)
	if st, _, _ := do(t, srv, "POST", p, body, logic); st != 404 {
		t.Errorf("switched off: %d", st)
	}
}
