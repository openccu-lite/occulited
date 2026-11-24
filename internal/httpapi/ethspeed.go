package httpapi

// occulited task 174: an Ethernet interface held at 100 Mbit/s (system.EthSpeed). The setting
// is one of the interface's fields in GET /network (speed_limit); this route changes it.

import (
	"errors"
	"net/http"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/system"
)

func (a *SystemAPI) registerSpeedLimit(mux *http.ServeMux, p string) {
	route(mux, auth.ScopeSystemWrite, "POST "+p+"/network/speed-limit", a.speedLimitSet)
}

func (a *SystemAPI) speedLimitSet(w http.ResponseWriter, r *http.Request) {
	if a.hostManaged(w, "the network") {
		return
	}
	var body struct {
		Interface string `json:"interface"`
		Limit     string `json:"limit"`
	}
	if err := readJSON(r, &body); err != nil {
		badBody(w, err)
		return
	}
	changed, err := system.EthSpeed{Root: a.Root, Run: a.Run}.Set(r.Context(), body.Interface, body.Limit)
	switch {
	case errors.Is(err, system.ErrSpeedLimitInvalid):
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "invalid", Message: err.Error()})
		return
	case errors.Is(err, system.ErrSpeedLimitUnsupported):
		writeJSON(w, http.StatusNotImplemented, apiError{Error: "unsupported", Message: err.Error()})
		return
	case err != nil:
		writeErr(w, err)
		return
	}
	out := a.networkView()
	out["changed"] = changed
	writeJSON(w, 200, out)
}
