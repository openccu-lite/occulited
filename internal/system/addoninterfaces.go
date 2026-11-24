package system

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/hobbyquaker/occulited/internal/addoniface"
	"github.com/hobbyquaker/occulited/internal/radio"
)

// occulited task 169: an addon may provide an interface (runtime.rpc_interface) - lite-addon-logic's
// variables and programs as devices. The system lists it in InterfacesList.xml while the addon is
// installed and switched on (its rc.d entry executable, its unit not switched off on the Services
// page): a confined addon cannot write the file. One sync decides it, from the stored manifests,
// after an install, an update, an uninstall, a switch either way and at every start - as runtime.bin's
// profile snippets (addonprofile.go). The file is edited in place (radio.RenderInterfaces: the other
// entries byte for byte), as syncWiredInterface does; the list also goes to addoniface.File, which
// the boot's radio plan reads, so the entries survive its re-rendering from the template (D-97 drops
// foreign entries - an addon's declared one is not foreign). The subscriber notices the changed file
// within its watch interval and registers or drops the interface.

// addonInterfacesWanted are the interfaces of the installed, switched-on addons whose stored
// manifest declares one, sorted by addon id; a name two addons declare goes to the first.
func (r Root) addonInterfacesWanted(off func(id string) bool) []addoniface.Entry {
	entries, err := os.ReadDir(r.join(AddonPolicyDir))
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), AddonManifestSuffix); ok && addonIDRe.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []addoniface.Entry
	taken := map[string]string{}
	for _, id := range ids {
		m := r.ReadAddonManifest(id)
		if m == nil || m.Runtime == nil || m.Runtime.RPCInterface == nil || m.Runtime.RPCInterface.Validate() != nil {
			continue
		}
		if !r.AddonEnabled(id) || (off != nil && off(id)) {
			continue
		}
		ri := m.Runtime.RPCInterface
		if other, ok := taken[strings.ToLower(ri.Name)]; ok {
			slog.Warn("addons: rpc_interface: the name is another addon's", "addon", id, "interface", ri.Name, "other", other)
			continue
		}
		taken[strings.ToLower(ri.Name)] = id
		out = append(out, addoniface.Entry{Name: ri.Name, Addon: id, URL: ri.URL()})
	}
	return out
}

// InterfacesListPath is the CCU's interface list (/usr/local/etc/config).
const InterfacesListPath = "/etc/config/InterfacesList.xml"

// SyncAddonInterfaces brings InterfacesList.xml's addon entries and addoniface's list in line with
// the addons. Answers the names added and removed ("+Logic", "-Logic"). An entry of another kind
// under an addon interface's name - a radio's, one the user added - is never touched: the addon's
// is left out and logged.
func (r Root) SyncAddonInterfaces(off func(id string) bool) []string {
	want := r.addonInterfacesWanted(off)
	// what the system listed before: the record, and this process's list
	owned := map[string]bool{}
	for _, e := range addoniface.Parse([]byte(readFile(r.join(addoniface.File)))) {
		owned[e.Name] = true
	}
	for _, e := range addoniface.List() {
		owned[e.Name] = true
	}
	path := r.join(InterfacesListPath)
	text := readFile(path)
	var changes []string
	var listed []addoniface.Entry
	if text != "" {
		cur := radio.ParseInterfaces(text)
		wantBy := map[string]addoniface.Entry{}
		for _, e := range want {
			wantBy[e.Name] = e
		}
		var next []radio.Interface
		have := map[string]bool{}
		removed := false
		for _, i := range cur {
			w, wanted := wantBy[i.Name]
			switch {
			case owned[i.Name] && (!wanted || w.URL != i.URL):
				changes = append(changes, "-"+i.Name) // gone, off, or moved to another port
				removed = true
				continue
			case !owned[i.Name] && wanted:
				slog.Warn("addons: rpc_interface: InterfacesList.xml has an entry of that name already; the addon's is left out", "addon", w.Addon, "interface", i.Name, "url", i.URL)
				delete(wantBy, i.Name)
			}
			have[i.Name] = true
			next = append(next, i)
		}
		for _, e := range want {
			if _, ok := wantBy[e.Name]; !ok {
				continue
			}
			listed = append(listed, e)
			if !have[e.Name] {
				next = append(next, radio.Interface{Name: e.Name, URL: e.URL, Info: e.Name})
				changes = append(changes, "+"+e.Name)
			}
		}
		if len(changes) > 0 {
			out := text
			if removed {
				// RenderInterfaces keeps a block by its name: cut first, so an entry that moved
				// to another port is appended anew
				kept := make([]radio.Interface, 0, len(next))
				for _, i := range next {
					if have[i.Name] {
						kept = append(kept, i)
					}
				}
				out = radio.RenderInterfaces(out, kept)
			}
			if err := writeFileAtomic(path, []byte(radio.RenderInterfaces(out, next)), 0o644); err != nil {
				slog.Warn("addons: rpc_interface: InterfacesList.xml could not be written", "err", err)
				return nil
			}
		}
	} else {
		listed = want // no list yet (the radio plan writes it): the record alone, for the plan
	}
	addoniface.Set(listed)
	rec := r.join(addoniface.File)
	enc := addoniface.Encode(listed)
	if readFile(rec) != string(enc) {
		if len(listed) == 0 {
			if _, err := os.Stat(rec); err == nil {
				_ = Priv.Remove(rec)
			}
		} else if err := writeFileAtomic(rec, enc, 0o644); err != nil {
			slog.Warn("addons: rpc_interface: the record could not be written", "file", addoniface.File, "err", err)
		}
	}
	for _, c := range changes {
		msg := "addons: rpc_interface listed in InterfacesList.xml"
		if c[0] == '-' {
			msg = "addons: rpc_interface removed from InterfacesList.xml"
		}
		slog.Info(msg, "interface", c[1:])
	}
	return changes
}

// LoadAddonInterfaces sets addoniface's list from the record at start, before the first sync: the
// radio views leave the interfaces out from the first poll on.
func (r Root) LoadAddonInterfaces() {
	addoniface.Set(addoniface.Parse([]byte(readFile(r.join(addoniface.File)))))
}

// forgetInterfaceNames removes the metadata's objects of the interface an uninstalled addon
// provided (occulited B-160): the addon named its devices there (POST
// /interfaces/{interface}/objects:bulk), and with the addon gone nothing lists, renames or removes
// them. Only after an uninstall that removed the addon (its rc.d entry is gone - a failed
// uninstall keeps the addon and its names), never at a switch-off, and never when the name is
// still listed in InterfacesList.xml (another addon's, a radio's or one the user added) or
// declared by another installed addon. Answers
// its line for the uninstall's list of what the system removed, "" for nothing.
func (a *SystemdAddons) forgetInterfaceNames(id, iface string) string {
	if iface == "" || a.InterfaceGone == nil {
		return ""
	}
	root := a.Scripts.Root
	if _, err := os.Lstat(root.join("/usr/local/etc/config/rc.d/" + id)); err == nil {
		return ""
	}
	if addoniface.Is(iface) {
		return ""
	}
	for _, i := range radio.ParseInterfaces(readFile(root.join(InterfacesListPath))) {
		if strings.EqualFold(i.Name, iface) {
			return ""
		}
	}
	// an installed addon that is switched off keeps the name too: its manifest is still stored
	if entries, err := os.ReadDir(root.join(AddonPolicyDir)); err == nil {
		for _, e := range entries {
			other, ok := strings.CutSuffix(e.Name(), AddonManifestSuffix)
			if !ok || other == id {
				continue
			}
			if m := root.ReadAddonManifest(other); m != nil && m.Runtime != nil && m.Runtime.RPCInterface != nil && strings.EqualFold(m.Runtime.RPCInterface.Name, iface) {
				return ""
			}
		}
	}
	n, err := a.InterfaceGone(iface)
	if err != nil {
		slog.Warn("addons: rpc_interface: the interface's names could not be removed from the metadata", "addon", id, "interface", iface, "err", err)
		return ""
	}
	if n == 0 {
		return ""
	}
	slog.Info("addons: rpc_interface: the interface's names removed from the metadata", "addon", id, "interface", iface, "objects", n)
	return fmt.Sprintf("metadata: %d names of %s", n, iface)
}
