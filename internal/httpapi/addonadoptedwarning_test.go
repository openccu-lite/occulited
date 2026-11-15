package httpapi

import (
	"context"
	"testing"

	"github.com/hobbyquaker/occulited/internal/system"
	"github.com/hobbyquaker/occulited/internal/warnings"
)

// openccu-lite #23: addon-adopted-root is a notice naming the running addons the list marks as
// adopted and confinable, with whether an install would also issue the token; an addon that is
// switched off or lacks its program is not named, and without a marked addon there is none.
func TestAddonAdoptedRootWarning(t *testing.T) {
	a := &SystemAPI{Addons: payloadLister{list: []system.Addon{
		{ID: "loom", Name: "OpenCCU-Loom", Enabled: true, AdoptedConfinable: true, AdoptedToken: true},
		{ID: "bare", Enabled: true, AdoptedConfinable: true},
		{ID: "off", Enabled: false, AdoptedConfinable: true},
		{ID: "gone", Enabled: true, AdoptedConfinable: true, PayloadMissing: true, ReinstallDismissed: true},
		{ID: "plain", Enabled: true},
	}}}
	ws, ok := a.addonWarnings(context.Background())
	if !ok || len(ws) != 1 {
		t.Fatalf("%+v %v", ws, ok)
	}
	w := ws[0]
	if w.ID != "addon-adopted-root" || w.Severity != warnings.SeverityInfo || w.Href != "/addons" || w.Variant != "bare,loom" {
		t.Errorf("%+v", w)
	}
	list, _ := w.Params["addons"].([]warnAddon)
	if len(list) != 2 || list[0].ID != "bare" || list[0].Token || list[1].ID != "loom" || !list[1].Token || list[1].Name != "OpenCCU-Loom" {
		t.Errorf("params %+v", w.Params)
	}
	// installed properly or removed: the list no longer marks it, the notice goes
	a.Addons = payloadLister{list: []system.Addon{{ID: "loom", Enabled: true, PolicyMode: "confined"}}}
	if ws, ok := a.addonWarnings(context.Background()); !ok || len(ws) != 0 {
		t.Errorf("after: %+v %v", ws, ok)
	}
}
