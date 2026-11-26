package httpapi

// openccu-lite task 228, phase 1: System → Storage - the USB sticks, Format and Safely remove.
// The listing is the daemon's (system.USBDisks); formatting and ejecting are the helper's, which
// checks the disk itself (priv.usbDiskOK) and runs the fixed steps.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/backuptarget"
	"github.com/hobbyquaker/occulited/internal/priv"
	"github.com/hobbyquaker/occulited/internal/store"
	"github.com/hobbyquaker/occulited/internal/system"
	"github.com/hobbyquaker/occulited/internal/warnings"
)

// usbDiskHelper is what the routes need of the privilege boundary.
type usbDiskHelper interface {
	USBFormat(ctx context.Context, s priv.USBFormatSpec) error
	USBEject(ctx context.Context, dev string) error
}

// usbHelperOf is the boundary's disk operations; a test replaces it.
var usbHelperOf = func() (usbDiskHelper, bool) {
	h, ok := system.Priv.(usbDiskHelper)
	return h, ok
}

// one format or eject at a time
var usbDiskMu sync.Mutex

func (a *SystemAPI) registerStorageUSB(mux *http.ServeMux, p string) {
	route(mux, auth.ScopeSystemRead, "GET "+p+"/storage/usb", a.storageUSB)
	route(mux, auth.ScopeSystemWrite, "POST "+p+"/storage/usb/{disk}/format", a.storageUSBFormat)
	route(mux, auth.ScopeSystemWrite, "POST "+p+"/storage/usb/{disk}/eject", a.storageUSBEject)
	route(mux, auth.ScopeSystemWrite, "PUT "+p+"/storage/usb/settings", a.storageUSBSettings)
}

// usbUse is what uses a stick: the journal's target, a backup target.
type usbUse struct {
	Kind string `json:"kind"` // journal, backup
	Name string `json:"name,omitempty"`
	ID   string `json:"id,omitempty"`
}

type usbDiskView struct {
	system.USBDisk
	Uses []usbUse `json:"uses"`
}

// usbUses names what uses each partition: the journal's usb:<label>/ target by label, a backup
// directory target by the mount point its path lies on (/media/usb0 is the linked stick).
func (a *SystemAPI) usbUses(ctx context.Context, d system.USBDisk) []usbUse {
	uses := []usbUse{}
	jc := a.Root.ReadJournalSetting()
	label, _, isUSB := system.ParseJournalUSBTarget(jc.Target)
	var views []backuptarget.View
	if a.BackupTargets != nil {
		views, _ = a.BackupTargets.Views(ctx)
	}
	linked := ""
	for _, s := range a.Root.USBSticks() {
		if s.Linked {
			linked = s.Mount
		}
	}
	storeLabel := ""
	if a.DataStore != nil {
		// task 229: the database's copy on a stick
		if l, _, err := store.ParseUSB(a.DataStore.Status().Location); err == nil {
			storeLabel = l
		}
	}
	for _, p := range d.Partitions {
		if isUSB && p.LabelID != "" && p.LabelID == label {
			uses = append(uses, usbUse{Kind: "journal"})
		}
		if storeLabel != "" && p.LabelID == storeLabel {
			uses = append(uses, usbUse{Kind: "store"})
		}
		if p.Mount == "" {
			continue
		}
		for _, v := range views {
			if !v.OnUSB() {
				continue
			}
			dir := v.Dir() + "/"
			if strings.HasPrefix(dir, p.Mount+"/") || (p.Mount == linked && strings.HasPrefix(dir, "/media/usb0/")) {
				uses = append(uses, usbUse{Kind: "backup", Name: v.Name, ID: v.ID})
			}
		}
	}
	return uses
}

func (a *SystemAPI) usbView(ctx context.Context) map[string]any {
	disks := []usbDiskView{}
	for _, d := range a.Root.USBDisks() {
		disks = append(disks, usbDiskView{USBDisk: d, Uses: a.usbUses(ctx, d)})
	}
	// task 35: the check of a stick that was not unmounted cleanly, at its next mount
	return map[string]any{"disks": disks, "filesystems": a.Root.MkfsAvailable(), "fsck_at_dirty_mount": a.Root.USBFsckAtDirtyMount()}
}

// storageUSBSettings is task 35's switch: PUT {fsck_at_dirty_mount}. The fork's mount reads it at the
// next mount; a stick mounted now is not checked again.
func (a *SystemAPI) storageUSBSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fsck *bool `json:"fsck_at_dirty_mount"`
	}
	if err := readJSON(r, &body); err != nil {
		badBody(w, err)
		return
	}
	if body.Fsck == nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "invalid-body", Message: "fsck_at_dirty_mount is required"})
		return
	}
	if err := a.Root.SetUSBFsckAtDirtyMount(*body.Fsck); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "internal", Message: err.Error()})
		return
	}
	slog.Info("storage: the check of a USB stick that was not unmounted cleanly", "on", *body.Fsck)
	a.reevaluateWarnings()
	writeJSON(w, 200, a.usbView(r.Context()))
}

// usbCheckWarning (task 35): a stick the mount found not unmounted cleanly and did not check (the
// switch is off), or whose check failed. The variant is the partitions with their state and time,
// so the next dirty mount is a new warning; it goes away when the stick is removed or mounted
// cleanly. A repair is no warning: the Storage page says it.
func (a *SystemAPI) usbCheckWarning(context.Context) ([]warnings.Warning, bool) {
	type stick struct {
		Name  string `json:"name"`
		Label string `json:"label,omitempty"`
		State string `json:"state"`
		Exit  *int   `json:"exit,omitempty"`
	}
	labels := map[string]string{}
	for _, d := range a.Root.USBDisks() {
		for _, p := range d.Partitions {
			labels[p.Name] = p.Label
		}
	}
	checks := a.Root.USBChecks()
	var list []stick
	var variant []string
	for _, n := range system.USBCheckNames(checks) {
		c := checks[n]
		if c.State != system.USBCheckUnchecked && c.State != system.USBCheckFailed {
			continue
		}
		list = append(list, stick{Name: n, Label: labels[n], State: c.State, Exit: c.Exit})
		variant = append(variant, n+":"+c.State+":"+strconv.FormatInt(c.At.Unix(), 10))
	}
	if len(list) == 0 {
		return nil, true
	}
	return []warnings.Warning{{ID: "usb-unchecked", Variant: strings.Join(variant, ","), Severity: warnings.SeverityWarning,
		Href: "/system/storage#usb", Params: map[string]any{"sticks": list, "fsck": a.Root.USBFsckAtDirtyMount()}}}, true
}

func (a *SystemAPI) storageUSB(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.usbView(r.Context()))
}

func (a *SystemAPI) usbDiskFor(w http.ResponseWriter, r *http.Request) (system.USBDisk, usbDiskHelper, bool) {
	h, ok := usbHelperOf()
	if !ok {
		writeJSON(w, http.StatusNotImplemented, apiError{Error: "unsupported", Message: "formatting and removing sticks needs the privilege helper"})
		return system.USBDisk{}, nil, false
	}
	name := r.PathValue("disk")
	for _, d := range a.Root.USBDisks() {
		if d.Name == name {
			// a backup being written to it is waited for by the user, not cut off
			for _, u := range a.usbUses(r.Context(), d) {
				if u.Kind == "backup" && a.backupRunning(r.Context(), u.ID) {
					writeJSON(w, http.StatusConflict, apiError{Error: "busy", Message: "a backup is being written to this stick; try again when it has finished"})
					return system.USBDisk{}, nil, false
				}
			}
			return d, h, true
		}
	}
	writeJSON(w, http.StatusNotFound, apiError{Error: "not_found", Message: "no USB stick " + name})
	return system.USBDisk{}, nil, false
}

func (a *SystemAPI) backupRunning(ctx context.Context, id string) bool {
	if a.BackupTargets == nil || id == "" {
		return false
	}
	v, err := a.BackupTargets.View(ctx, id)
	return err == nil && v.State.State == "running"
}

func helperError(w http.ResponseWriter, err error) {
	if errors.Is(err, priv.ErrRefused) {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "refused", Message: strings.TrimPrefix(err.Error(), "privilege helper: ")})
		return
	}
	writeJSON(w, http.StatusInternalServerError, apiError{Error: "failed", Message: err.Error()})
}

func (a *SystemAPI) storageUSBFormat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FS    string `json:"fs"`
		Label string `json:"label"`
	}
	if err := readJSON(r, &body); err != nil {
		badBody(w, err)
		return
	}
	if err := priv.ValidUSBLabel(body.FS, body.Label); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "invalid", Message: err.Error()})
		return
	}
	usbDiskMu.Lock()
	defer usbDiskMu.Unlock()
	d, h, ok := a.usbDiskFor(w, r)
	if !ok {
		return
	}
	reqLog(r).Warn("storage: formatting a USB stick", "disk", d.Name, "vendor", d.Vendor, "model", d.Model, "serial", d.Serial, "size_bytes", d.Size, "fs", body.FS, "label", body.Label)
	if err := h.USBFormat(r.Context(), priv.USBFormatSpec{Device: d.Name, FS: body.FS, Label: body.Label}); err != nil {
		slog.Error("storage: formatting failed", "disk", d.Name, "err", err)
		helperError(w, err)
		return
	}
	reqLog(r).Info("storage: USB stick formatted", "disk", d.Name, "fs", body.FS, "label", body.Label)
	out := a.usbView(r.Context())
	out["formatted"] = d.Name
	writeJSON(w, 200, out)
}

func (a *SystemAPI) storageUSBEject(w http.ResponseWriter, r *http.Request) {
	usbDiskMu.Lock()
	defer usbDiskMu.Unlock()
	d, h, ok := a.usbDiskFor(w, r)
	if !ok {
		return
	}
	if err := h.USBEject(r.Context(), d.Name); err != nil {
		helperError(w, err)
		return
	}
	reqLog(r).Info("storage: USB stick removed safely", "disk", d.Name, "model", d.Model)
	out := a.usbView(r.Context())
	out["ejected"] = d.Name
	writeJSON(w, 200, out)
}
