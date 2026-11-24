package main

import (
	"testing"

	"github.com/hobbyquaker/occulited/internal/meta"
)

// occulited B-160: every object of the interface goes, as one revision, and nothing else.
func TestForgetInterfaceObjects(t *testing.T) {
	store, err := meta.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := func(s string) meta.ObjectPatch {
		return meta.ObjectPatch{Name: &s}
	}
	if _, _, err := store.Bulk(nil, map[string]meta.ObjectPatch{
		"Logic.var-1": name("A"), "Logic.var-1:1": name("B"), "Logic.prog-2": name("C"),
		"Logical.x": name("not it"), "HmIP-RF.0000000000000A": name("Bad"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	rev := store.Revision()
	for _, bad := range []string{"", "Logic.var-1", "Nothing"} {
		if n, err := forgetInterfaceObjects(store, bad); n != 0 || err != nil || store.Revision() != rev {
			t.Fatalf("%q: %d %v", bad, n, err)
		}
	}
	n, err := forgetInterfaceObjects(store, "Logic")
	if n != 3 || err != nil || store.Revision() != rev+1 {
		t.Fatalf("%d %v, revision %d after %d", n, err, store.Revision(), rev)
	}
	left := store.Snapshot().Objects
	if len(left) != 2 || left["Logical.x"] == nil || left["HmIP-RF.0000000000000A"] == nil {
		t.Fatalf("left: %v", left)
	}
}
