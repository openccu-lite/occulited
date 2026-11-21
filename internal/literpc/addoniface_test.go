package literpc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/addoniface"
	"github.com/hobbyquaker/occulited/internal/devstate"
	"github.com/hobbyquaker/occulited/internal/rpcsub"
	"github.com/mdzio/go-hmccu/v2/itf/xmlrpc"
)

// occulited task 169, the integration of spike S-2: a fake addon interface "Logic" listed in
// InterfacesList.xml is registered by the subscriber, offered by lite-rpc with its addon, a call is
// forwarded to it, and its events - datapoints outside the chosen set - reach the state store whole.
func TestAddonInterfaceEndToEnd(t *testing.T) {
	addoniface.Set([]addoniface.Entry{{Name: "Logic", Addon: "logic"}})
	t.Cleanup(func() { addoniface.Set(nil) })
	fd := newFakeDaemon(t)
	list := filepath.Join(t.TempDir(), "InterfacesList.xml")
	_ = os.WriteFile(list, []byte(`<interfaces><ipc><name>Logic</name><url>`+fd.url()+`</url><info>Logic</info></ipc></interfaces>`), 0o644)
	state := &devstate.Store{}
	sub := rpcsub.New(rpcsub.Config{Listen: "127.0.0.1:0", Interfaces: list, Watch: 50 * time.Millisecond, PingAfter: time.Hour, Enrich: state.Enrich})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = sub.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for !(len(sub.Status()) == 1 && sub.Status()[0].State == "up") {
		if time.Now().After(deadline) {
			t.Fatalf("not registered: %+v", sub.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}
	svc := New(Config{Sub: sub, Heartbeat: 40 * time.Millisecond})
	ifs := svc.Interfaces()
	if len(ifs) != 1 || ifs[0].Name != "Logic" || ifs[0].Addon != "logic" || !ifs[0].Running {
		t.Fatalf("interfaces: %+v", ifs)
	}
	i, _ := svc.Lookup("Logic")
	if _, err := svc.Forward(context.Background(), i, Call{Method: "setValue", Params: []*xmlrpc.Value{xmlrpc.NewString("var-regen:1"), xmlrpc.NewString("VALUE"), xmlrpc.NewBool(true)}}); err != nil {
		t.Fatal(err)
	}
	fd.mu.Lock()
	got := append([]string(nil), fd.set...)
	fd.mu.Unlock()
	if len(got) != 1 || got[0] != "var-regen:1 VALUE=true" {
		t.Fatalf("the addon got %v", got)
	}
	fd.send(t, [][3]string{{"var-regen:1", "VALUE", "es regnet"}, {"prg-markise:1", "LAST_RUN", "1"}}, true)
	deadline = time.Now().Add(5 * time.Second)
	for {
		v, ok1 := state.Get("Logic", "var-regen:1", "VALUE")
		_, ok2 := state.Get("Logic", "prg-markise:1", "LAST_RUN")
		if ok1 && ok2 && v.Value == "es regnet" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the state store has not got the events: %d entries", state.Count())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
