package httpapi

// occulited task 169: an addon that provides an interface (runtime.rpc_interface) keeps its devices'
// names in the metadata - lite-addon-logic's variables and programs are named by the addon, and
// every client reads names from the metadata. The scope meta:interface lets the addon's own token
// write exactly that: the names of objects on its own interface, nothing else (no rooms, no
// functions, no other interface), in the shape of POST /objects:bulk.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hobbyquaker/occulited/internal/addoniface"
	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/meta"
)

func (a *MetaAPI) registerInterfaceNames(mux *http.ServeMux, p string) {
	route(mux, auth.ScopeMetaInterface, "POST "+p+"/interfaces/{interface}/objects:bulk", a.interfaceNames)
}

// interfaceNames is POST /interfaces/{interface}/objects:bulk: {set: {"<ref>": {name}}, delete:
// ["<ref>"]}, every ref on {interface}, which must be the calling addon's (or, for Full access, any
// addon's: 404 unknown-interface otherwise). 403 addon-only for any other caller, 404 not-your-interface for an interface the addon does not provide (or provides
// while it is switched off), 422 invalid-ref for a ref of another interface, 422 invalid-body for
// anything but a name.
func (a *MetaAPI) interfaceNames(w http.ResponseWriter, r *http.Request) {
	iface := r.PathValue("interface")
	sess := SessionFrom(r)
	switch addon, ok := addonOfSession(sess); {
	case ok:
		if e, ok := addoniface.OfAddon(addon); !ok || e.Name != iface {
			writeJSON(w, http.StatusNotFound, apiError{Error: "not-your-interface", Message: "the addon " + addon + " does not provide the interface " + iface + " (or it is switched off)"})
			return
		}
	case sess != nil && sess.Scopes.Full():
		// Full access may do what the addon does, on an addon's interface
		if !addoniface.Is(iface) {
			writeJSON(w, http.StatusNotFound, apiError{Error: "unknown-interface", Message: iface + " is no interface an addon provides"})
			return
		}
	default:
		writeJSON(w, http.StatusForbidden, apiError{Error: "addon-only", Message: "this route serves an addon's own API token, for the interface it provides"})
		return
	}
	im, err := ifMatch(r)
	if err != nil {
		badBody(w, err)
		return
	}
	var b struct {
		Set    map[string]map[string]json.RawMessage `json:"set"`
		Delete []string                              `json:"delete"`
	}
	if err := readJSON(r, &b); err != nil {
		badBody(w, err)
		return
	}
	prefix := iface + "."
	set := make(map[string]meta.ObjectPatch, len(b.Set))
	for ref, raw := range b.Set {
		if !strings.HasPrefix(ref, prefix) {
			writeErr(w, &meta.Error{Code: meta.ErrInvalidRef, Message: "ref " + ref + " is not on the interface " + iface})
			return
		}
		for k := range raw {
			if k != "name" {
				writeErr(w, &meta.Error{Code: meta.ErrInvalidBody, Message: "only the name of an object can be set here, not " + k})
				return
			}
		}
		p, err := ParsePatch(raw)
		if err != nil {
			writeErr(w, err)
			return
		}
		set[ref] = p
	}
	for _, ref := range b.Delete {
		if !strings.HasPrefix(ref, prefix) {
			writeErr(w, &meta.Error{Code: meta.ErrInvalidRef, Message: "ref " + ref + " is not on the interface " + iface})
			return
		}
	}
	rev, changed, err := a.Store.Bulk(im, set, b.Delete)
	mutated(w, rev, changed, err, 200)
}
