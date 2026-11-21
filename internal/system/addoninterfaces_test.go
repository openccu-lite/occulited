package system

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hobbyquaker/occulited/internal/addoniface"
	"github.com/hobbyquaker/occulited/internal/radio"
)

const ifaceTemplate = `<?xml version="1.0" encoding="utf-8" ?> 
<interfaces v="1.0">
	<ipc>
	 	<name>BidCos-RF</name>
	 	<url>xmlrpc_bin://127.0.0.1:32001</url> 
	 	<info>BidCos-RF</info> 
	</ipc>
	<ipc>
	 	<name>HmIP-RF</name>
	 	<url>xmlrpc://127.0.0.1:32010</url>
	 	<info>HmIP-RF</info>
	</ipc>
</interfaces>
`

// installInterfaceAddon is an installed addon whose stored manifest declares runtime.rpc_interface.
func installInterfaceAddon(t *testing.T, root Root, id, name string, port int) {
	t.Helper()
	installProfileAddon(t, root, id, "")
	rt := map[string]any{"daemon": true, "rpc_interface": map[string]any{"name": name, "port": port}}
	m, _ := json.Marshal(map[string]any{"format": 1, "id": id, "name": id, "runtime": rt})
	if err := os.WriteFile(root.join(AddonPolicyDir+"/"+id+AddonManifestSuffix), m, 0o644); err != nil {
		t.Fatal(err)
	}
}

func interfaceNames(t *testing.T, root Root) string {
	t.Helper()
	var n []string
	for _, i := range radio.ParseInterfaces(readFile(root.join(InterfacesListPath))) {
		n = append(n, i.Name+"="+i.URL)
	}
	return strings.Join(n, " ")
}

// occulited task 169: an addon's interface is listed while the addon is installed and switched on,
// follows a port change, goes when the addon is disabled, switched off or uninstalled; the other
// entries stay byte for byte; a name the file has already is never taken over; the record follows.
func TestSyncAddonInterfaces(t *testing.T) {
	root := profileBox(t)
	t.Cleanup(func() { addoniface.Set(nil) })
	_ = os.MkdirAll(root.join("/etc/config"), 0o755)
	if err := os.WriteFile(root.join(InterfacesListPath), []byte(ifaceTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	off := map[string]bool{}
	sync := func() string {
		return strings.Join(root.SyncAddonInterfaces(func(id string) bool { return off[id] }), " ")
	}

	if got := sync(); got != "" {
		t.Fatalf("nothing declared: %q", got)
	}
	if _, err := os.Stat(root.join(addoniface.File)); !os.IsNotExist(err) {
		t.Fatal("a record for nothing")
	}
	installInterfaceAddon(t, root, "logic", "Logic", 39310)
	installProfileAddon(t, root, "plain", "")
	if got := sync(); got != "+Logic" {
		t.Fatalf("install: %q", got)
	}
	text := readFile(root.join(InterfacesListPath))
	if !strings.HasPrefix(text, strings.TrimSuffix(ifaceTemplate, "</interfaces>\n")) {
		t.Fatalf("the other entries changed:\n%s", text)
	}
	if got := interfaceNames(t, root); got != "BidCos-RF=xmlrpc_bin://127.0.0.1:32001 HmIP-RF=xmlrpc://127.0.0.1:32010 Logic=xmlrpc://127.0.0.1:39310" {
		t.Fatal(got)
	}
	if e, ok := addoniface.Lookup("Logic"); !ok || e.Addon != "logic" || !addoniface.Is("Logic") || addoniface.Is("HmIP-RF") {
		t.Fatalf("%+v %v", e, ok)
	}
	rec := addoniface.Parse([]byte(readFile(root.join(addoniface.File))))
	if len(rec) != 1 || rec[0].URL != "xmlrpc://127.0.0.1:39310" {
		t.Fatalf("record %+v", rec)
	}
	if got := sync(); got != "" {
		t.Fatalf("again: %q", got)
	}
	// an update moves the port: the entry follows
	installInterfaceAddon(t, root, "logic", "Logic", 39311)
	if got := sync(); got != "-Logic +Logic" {
		t.Fatalf("port: %q", got)
	}
	if got := interfaceNames(t, root); !strings.HasSuffix(got, "Logic=xmlrpc://127.0.0.1:39311") {
		t.Fatal(got)
	}
	// the Services page's switch, then Disable on the Addons page
	off["logic"] = true
	if got := sync(); got != "-Logic" || addoniface.Is("Logic") {
		t.Fatalf("switched off: %q", got)
	}
	if _, err := os.Stat(root.join(addoniface.File)); !os.IsNotExist(err) {
		t.Fatal("the record of nothing stayed")
	}
	off["logic"] = false
	if got := sync(); got != "+Logic" {
		t.Fatalf("on again: %q", got)
	}
	if err := os.Chmod(root.join("/usr/local/etc/config/rc.d/logic"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := sync(); got != "-Logic" {
		t.Fatalf("disabled: %q", got)
	}
	if err := os.Chmod(root.join("/usr/local/etc/config/rc.d/logic"), 0o755); err != nil {
		t.Fatal(err)
	}
	sync()
	// uninstalled: the stored manifest is gone
	_ = os.Remove(root.join(AddonPolicyDir + "/logic" + AddonManifestSuffix))
	if got := sync(); got != "-Logic" {
		t.Fatalf("uninstalled: %q", got)
	}
	if readFile(root.join(InterfacesListPath)) != ifaceTemplate {
		t.Fatalf("not back to the template:\n%s", readFile(root.join(InterfacesListPath)))
	}

	// a name the file has of its own (the user's CCU-Jack-like entry) is never taken over, nor removed
	own := strings.Replace(ifaceTemplate, "</interfaces>", "\t<ipc>\n\t\t<name>Extra</name>\n\t\t<url>xmlrpc://127.0.0.1:2121</url>\n\t</ipc>\n</interfaces>", 1)
	_ = os.WriteFile(root.join(InterfacesListPath), []byte(own), 0o644)
	installInterfaceAddon(t, root, "squatter", "Extra", 40000)
	if got := sync(); got != "" || addoniface.Is("Extra") {
		t.Fatalf("a taken name: %q", got)
	}
	if readFile(root.join(InterfacesListPath)) != own {
		t.Fatal("the foreign entry was touched")
	}
	_ = os.Remove(root.join(AddonPolicyDir + "/squatter" + AddonManifestSuffix))
	if got := sync(); got != "" || readFile(root.join(InterfacesListPath)) != own {
		t.Fatalf("the foreign entry was removed: %q", got)
	}

	// two addons with one name: the first by id; no list yet: the record alone, for the plan
	_ = os.Remove(root.join(InterfacesListPath))
	installInterfaceAddon(t, root, "a-logic", "Logic", 39310)
	installInterfaceAddon(t, root, "b-logic", "logic", 39320)
	sync()
	if l := addoniface.List(); len(l) != 1 || l[0].Addon != "a-logic" {
		t.Fatalf("%+v", l)
	}
	if rec := addoniface.Parse([]byte(readFile(root.join(addoniface.File)))); len(rec) != 1 {
		t.Fatalf("record %+v", rec)
	}
	addoniface.Set(nil)
	root.LoadAddonInterfaces()
	if !addoniface.Is("Logic") {
		t.Fatal("not loaded from the record")
	}
}
