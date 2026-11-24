package httpapi

// occulited task 160: System → Storage's "Write behaviour of the system storage" - the userfs's
// two ext4 mount options that decide how often the SD card is written to without anybody asking
// for it: lazytime and the journal's commit interval. The choice is system.UserfsMount's (a file
// in the state directory, a remount through the helper at once and at each start).

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/priv"
	"github.com/hobbyquaker/occulited/internal/system"
)

func (a *SystemAPI) registerStorageMount(mux *http.ServeMux, p string) {
	route(mux, auth.ScopeSystemRead, "GET "+p+"/storage/mount-options", a.storageMount)
	route(mux, auth.ScopeSystemWrite, "PUT "+p+"/storage/mount-options", a.storageMountPut)
}

// mountOptionsView is what the page reads.
type mountOptionsView struct {
	// Mount is the userfs's mount point. Mounted, FSType, Options, Supported and Active are the
	// kernel's word on it (system.UserfsMountState): Supported is an ext4 mount of its own.
	Mount string `json:"mount"`
	system.UserfsMountState
	// Setting is the stored choice, Default the image's (what the userfs is mounted with at
	// boot); Custom: the choice differs from the image's and is applied at each start.
	Setting system.UserfsMountOptions `json:"setting"`
	Default system.UserfsMountOptions `json:"default"`
	Custom  bool                      `json:"custom"`
	// CommitChoices are the commit intervals the system offers, in seconds.
	CommitChoices []int `json:"commit_choices"`
}

func (a *SystemAPI) mountOptionsView() mountOptionsView {
	v := mountOptionsView{Mount: "/usr/local", Setting: system.DefaultUserfsMount, Default: system.DefaultUserfsMount, CommitChoices: priv.UserfsCommitChoices}
	if a.UserfsMount == nil {
		return v
	}
	v.UserfsMountState = a.UserfsMount.State()
	v.Setting, v.Custom = a.UserfsMount.Stored()
	return v
}

func (a *SystemAPI) storageMount(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, a.mountOptionsView())
}

func (a *SystemAPI) storageMountPut(w http.ResponseWriter, r *http.Request) {
	if a.UserfsMount == nil {
		writeJSON(w, http.StatusNotImplemented, apiError{Error: "unsupported", Message: "this system does not set the system storage's mount options"})
		return
	}
	var body struct {
		Lazytime *bool `json:"lazytime"`
		CommitS  *int  `json:"commit_s"`
	}
	if err := readJSON(r, &body); err != nil {
		badBody(w, err)
		return
	}
	if body.Lazytime == nil || body.CommitS == nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "invalid-body", Message: "lazytime and commit_s are required"})
		return
	}
	o := system.UserfsMountOptions{Lazytime: *body.Lazytime, CommitS: *body.CommitS}
	if err := system.ValidUserfsMount(o); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "invalid-body", Message: err.Error()})
		return
	}
	if err := a.UserfsMount.Apply(r.Context(), o); err != nil {
		if errors.Is(err, priv.ErrUserfsNotExt4) {
			writeJSON(w, http.StatusConflict, apiError{Error: "unsupported", Message: "the system storage is not an ext4 file system mounted on its own: its mount options cannot be set"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "internal", Message: err.Error()})
		return
	}
	slog.Info("storage: the system storage's mount options were set", "lazytime", o.Lazytime, "commit_s", o.CommitS)
	writeJSON(w, 200, a.mountOptionsView())
}
