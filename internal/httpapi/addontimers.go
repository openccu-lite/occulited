package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/system"
)

// An addon's own timers (occulited task 154): the routes of the scope timers:addon. They serve the
// API token of an addon (/run/occulite/addon-tokens/<id>.api) and only that addon's timers, the
// units local-<id>-<name>. The addon hands over a schedule - the .timer text, held to a closed
// list of directives; the .service is rendered by the system from the addon's policy and its
// manifest's runtime.timers (internal/system/addontimer.go, internal/addonunit/timer.go). A
// session that is not an addon's token is refused, an administrator's included: a person's own
// timers are /timers/own.
//
//	GET    /addon-timers                 the addon's timers, and the addons whose data one may read
//	GET    /addon-timers/{name}          one
//	PUT    /addon-timers/{name}          makes or changes one: {timer?, description?, read?, enabled?, timeout?, memory_mb?}
//	DELETE /addon-timers/{name}          removes one
//	POST   /addon-timers/{name}/run      starts its service now
//	POST   /addon-timers/{name}/stop     stops its service
//	POST   /addon-timers/{name}/enable   switches the timer on
//	POST   /addon-timers/{name}/disable  switches it off; both files stay ("manual only")

// addonTimerEditor is the part of the service manager the routes need; without systemd there are
// no timers (501).
type addonTimerEditor interface {
	AddonTimers(addon string) []system.AddonTimer
	AddonTimerReadable(addon string) []string
	ReadAddonTimer(addon, name string) (system.AddonTimer, error)
	PutAddonTimer(ctx context.Context, addon, name string, in system.AddonTimerInput) (system.AddonTimer, bool, error)
	DeleteAddonTimer(ctx context.Context, addon, name string) error
	RunAddonTimer(ctx context.Context, addon, name string) (string, error)
	StopAddonTimer(ctx context.Context, addon, name string) (string, error)
	SetAddonTimerEnabled(ctx context.Context, addon, name string, on bool) (string, error)
}

func (a *SystemAPI) registerAddonTimers(mux *http.ServeMux, p string) {
	route(mux, auth.ScopeTimersAddon, "GET "+p+"/addon-timers", a.addonTimersList)
	route(mux, auth.ScopeTimersAddon, "GET "+p+"/addon-timers/{name}", a.addonTimerGet)
	route(mux, auth.ScopeTimersAddon, "PUT "+p+"/addon-timers/{name}", a.addonTimerPut)
	route(mux, auth.ScopeTimersAddon, "DELETE "+p+"/addon-timers/{name}", a.addonTimerDelete)
	route(mux, auth.ScopeTimersAddon, "POST "+p+"/addon-timers/{name}/run", a.addonTimerRun)
	route(mux, auth.ScopeTimersAddon, "POST "+p+"/addon-timers/{name}/stop", a.addonTimerStop)
	route(mux, auth.ScopeTimersAddon, "POST "+p+"/addon-timers/{name}/enable", a.addonTimerEnable)
	route(mux, auth.ScopeTimersAddon, "POST "+p+"/addon-timers/{name}/disable", a.addonTimerDisable)
}

// addonOfSession answers the addon whose API token the session is: "token:addon:<id>" is the
// session of the ephemeral token the system mints for an addon, and no token a person makes can
// have that name (a token's name has no colon).
func addonOfSession(s *auth.Session) (string, bool) {
	if s == nil || !s.IsToken() {
		return "", false
	}
	id, ok := strings.CutPrefix(s.ID, auth.TokenSessionPrefix+auth.AddonTokenName(""))
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// timerAddon answers the calling addon and the editor; false has answered.
func (a *SystemAPI) timerAddon(w http.ResponseWriter, r *http.Request) (addonTimerEditor, string, bool) {
	ed, ok := a.Timers.(addonTimerEditor)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, apiError{Error: "not-systemd", Message: "an addon's timers need systemd"})
		return nil, "", false
	}
	addon, ok := addonOfSession(SessionFrom(r))
	if !ok {
		writeJSON(w, http.StatusForbidden, apiError{Error: "addon-only", Message: "these routes serve an addon's own API token and its timers; a person's own timers are /timers/own"})
		return nil, "", false
	}
	return ed, addon, true
}

// writeAddonTimerErr answers a failed operation: 404 for a timer the addon does not have, 409 for
// a name that is taken by a timer that is not the addon's, for the limit and for an addon that
// declares no timers, 422 for everything the rules refuse and what systemd did not load.
func writeAddonTimerErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, system.ErrLocalTimerNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Error: "not-found", Message: err.Error()})
	case errors.Is(err, system.ErrLocalTimerExists):
		writeJSON(w, http.StatusConflict, apiError{Error: "exists", Message: err.Error()})
	case errors.Is(err, system.ErrAddonTimerLimit):
		writeJSON(w, http.StatusConflict, apiError{Error: "too-many", Message: err.Error()})
	case errors.Is(err, system.ErrAddonTimersUndeclared):
		writeJSON(w, http.StatusConflict, apiError{Error: "undeclared", Message: err.Error()})
	default:
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "invalid", Message: err.Error()})
	}
}

func (a *SystemAPI) addonTimersList(w http.ResponseWriter, r *http.Request) {
	ed, addon, ok := a.timerAddon(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"addon": addon, "timers": ed.AddonTimers(addon), "max": system.AddonTimersMax, "readable": ed.AddonTimerReadable(addon)})
}

func (a *SystemAPI) addonTimerGet(w http.ResponseWriter, r *http.Request) {
	ed, addon, ok := a.timerAddon(w, r)
	if !ok {
		return
	}
	at, err := ed.ReadAddonTimer(addon, r.PathValue("name"))
	if err != nil {
		writeAddonTimerErr(w, err)
		return
	}
	writeJSON(w, 200, at)
}

func (a *SystemAPI) addonTimerPut(w http.ResponseWriter, r *http.Request) {
	ed, addon, ok := a.timerAddon(w, r)
	if !ok {
		return
	}
	var in system.AddonTimerInput
	if err := readJSON(r, &in); err != nil {
		badBody(w, err)
		return
	}
	at, created, err := ed.PutAddonTimer(r.Context(), addon, r.PathValue("name"), in)
	if err != nil {
		writeAddonTimerErr(w, err)
		return
	}
	st := http.StatusOK
	if created {
		st = http.StatusCreated
		reqLog(r).Info("addon timers: a timer was made", "addon", addon, "unit", at.Unit, "read", strings.Join(at.Read, " "))
	}
	writeJSON(w, st, at)
}

func (a *SystemAPI) addonTimerDelete(w http.ResponseWriter, r *http.Request) {
	ed, addon, ok := a.timerAddon(w, r)
	if !ok {
		return
	}
	if err := ed.DeleteAddonTimer(r.Context(), addon, r.PathValue("name")); err != nil {
		writeAddonTimerErr(w, err)
		return
	}
	reqLog(r).Info("addon timers: a timer was removed", "addon", addon, "timer", r.PathValue("name"))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *SystemAPI) addonTimerRun(w http.ResponseWriter, r *http.Request) {
	ed, addon, ok := a.timerAddon(w, r)
	if !ok {
		return
	}
	out, err := ed.RunAddonTimer(r.Context(), addon, r.PathValue("name"))
	if err != nil {
		writeAddonTimerErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "output": out})
}

func (a *SystemAPI) addonTimerStop(w http.ResponseWriter, r *http.Request) {
	ed, addon, ok := a.timerAddon(w, r)
	if !ok {
		return
	}
	out, err := ed.StopAddonTimer(r.Context(), addon, r.PathValue("name"))
	if err != nil {
		writeAddonTimerErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "output": out})
}

func (a *SystemAPI) addonTimerEnable(w http.ResponseWriter, r *http.Request) {
	a.addonTimerSwitch(w, r, true)
}

func (a *SystemAPI) addonTimerDisable(w http.ResponseWriter, r *http.Request) {
	a.addonTimerSwitch(w, r, false)
}

func (a *SystemAPI) addonTimerSwitch(w http.ResponseWriter, r *http.Request, on bool) {
	ed, addon, ok := a.timerAddon(w, r)
	if !ok {
		return
	}
	out, err := ed.SetAddonTimerEnabled(r.Context(), addon, r.PathValue("name"), on)
	if err != nil {
		writeAddonTimerErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "enabled": on, "output": out})
}
