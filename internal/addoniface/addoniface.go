// Package addoniface is the system's list of the interfaces addons provide (occulited task 169):
// an addon whose manifest declares runtime.rpc_interface speaks the interface processes' XML-RPC on
// a loopback port, and the system lists it in InterfacesList.xml while the addon is installed and
// switched on (internal/system's SyncAddonInterfaces). lite-rpc then offers it like rfd or
// hmipserver. What is not a radio treats it differently: the state store keeps every datapoint of
// it (not only the chosen set), and the radio views - the radio health poll, the event rates, the
// duty cycle, the radio-write counters - leave it out. They ask here.
//
// The list is the process's: set by the sync at start and after every addon change. It is also
// what the sync writes beside InterfacesList.xml (File), which the boot's radio plan reads so the
// entries survive its re-rendering of InterfacesList.xml from the template.
package addoniface

import (
	"encoding/json"
	"sort"
	"sync"
)

// File is the list as the sync keeps it, for the radio plan (/usr/local/etc/config).
const File = "/etc/config/addon-interfaces.json"

// Entry is one addon interface.
type Entry struct {
	// Name is its name in InterfacesList.xml and lite-rpc.
	Name string `json:"name"`
	// Addon is the id of the addon that provides it.
	Addon string `json:"addon"`
	// URL is its InterfacesList.xml URL (xmlrpc://127.0.0.1:<port>[/path]).
	URL string `json:"url"`
}

var (
	mu   sync.RWMutex
	list []Entry
	by   map[string]Entry
)

// Set replaces the list.
func Set(entries []Entry) {
	cp := append([]Entry(nil), entries...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	m := make(map[string]Entry, len(cp))
	for _, e := range cp {
		m[e.Name] = e
	}
	mu.Lock()
	list, by = cp, m
	mu.Unlock()
}

// List answers the entries, sorted by name.
func List() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	return append([]Entry(nil), list...)
}

// Lookup answers the entry of an interface name.
func Lookup(name string) (Entry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := by[name]
	return e, ok
}

// Is says whether an interface is an addon's.
func Is(name string) bool {
	_, ok := Lookup(name)
	return ok
}

// OfAddon answers the interface an addon provides.
func OfAddon(id string) (Entry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	for _, e := range list {
		if e.Addon == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Parse reads File's content; a broken file is an empty list.
func Parse(b []byte) []Entry {
	var doc struct {
		Interfaces []Entry `json:"interfaces"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	var out []Entry
	for _, e := range doc.Interfaces {
		if e.Name != "" && e.URL != "" {
			out = append(out, e)
		}
	}
	return out
}

// Encode is File's content for entries.
func Encode(entries []Entry) []byte {
	if entries == nil {
		entries = []Entry{}
	}
	b, _ := json.MarshalIndent(map[string]any{"interfaces": entries}, "", "  ")
	return append(b, '\n')
}
