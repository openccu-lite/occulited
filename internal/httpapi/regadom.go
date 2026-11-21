package httpapi

// occulited task 167: the CCU's ReGa database kept after the switch (system.KeptRegadom) - whether
// it is there, the file itself for lite-addon-logic's import (its own scope, regadom:read), and its
// removal, which is the user's choice on the Backup page.

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/system"
)

func (a *SystemAPI) registerRegadom(mux *http.ServeMux, p string) {
	route(mux, auth.ScopeSystemRead, "GET "+p+"/regadom", a.regadomInfo)
	route(mux, auth.ScopeRegadomRead, "GET "+p+"/regadom/file", a.regadomFile)
	route(mux, auth.ScopeSystemWrite, "DELETE "+p+"/regadom", a.regadomRemove)
}

// regadomInfo is GET /regadom: {present, bytes?, modified?}.
func (a *SystemAPI) regadomInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, a.Root.KeptRegadom())
}

// regadomFile is GET /regadom/file: the database as it is (application/octet-stream), 404
// no-regadom when none is kept.
func (a *SystemAPI) regadomFile(w http.ResponseWriter, r *http.Request) {
	b, err := a.Root.ReadKeptRegadom()
	if errors.Is(err, system.ErrNoRegadom) {
		writeJSON(w, http.StatusNotFound, apiError{Error: "no-regadom", Message: err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "read", Message: err.Error()})
		return
	}
	reqLog(r).Info("regadom: the kept ReGa database was read", "bytes", len(b))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="homematic.regadom"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// regadomRemove is DELETE /regadom: removes the database (and a .bak still there) - {removed,
// freed_bytes}; 404 no-regadom when none is kept.
func (a *SystemAPI) regadomRemove(w http.ResponseWriter, r *http.Request) {
	freed, err := a.Root.RemoveKeptRegadom()
	if errors.Is(err, system.ErrNoRegadom) {
		writeJSON(w, http.StatusNotFound, apiError{Error: "no-regadom", Message: err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "remove", Message: err.Error()})
		return
	}
	reqLog(r).Info("regadom: the kept ReGa database removed", "freed_bytes", freed)
	writeJSON(w, 200, map[string]any{"removed": true, "freed_bytes": freed})
}
