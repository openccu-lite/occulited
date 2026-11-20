package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIOAccountingSwitchAndReplay(t *testing.T) {
	dir := t.TempDir()
	dropIn := filepath.Join(dir, "run", "50.conf")
	var calls []string
	a := &IOAccounting{
		File:   filepath.Join(dir, "state", "io-accounting"),
		DropIn: dropIn,
		Set: func(_ context.Context, on bool) error {
			if on {
				calls = append(calls, "set on")
				_ = os.MkdirAll(filepath.Dir(dropIn), 0o755)
				return os.WriteFile(dropIn, []byte("x"), 0o644)
			}
			calls = append(calls, "set off")
			_ = os.Remove(dropIn)
			return nil
		},
		Reload: func(context.Context) error { calls = append(calls, "reload"); return nil },
	}
	ctx := context.Background()
	if a.Enabled() {
		t.Fatal("on before anybody switched it")
	}
	// nothing stored, nothing applied: a start does nothing at all
	if changed, err := a.Replay(ctx); changed || err != nil || len(calls) != 0 {
		t.Fatalf("replay with the switch off: %v %v %v", changed, err, calls)
	}
	if err := a.Switch(ctx, true); err != nil || !a.Enabled() {
		t.Fatalf("on: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"set on", "reload"}) {
		t.Fatalf("on: %v", calls)
	}
	// occulited restarts: /run still holds the drop-in, systemd is not reloaded again
	calls = nil
	if changed, err := a.Replay(ctx); changed || err != nil || len(calls) != 0 {
		t.Fatalf("replay with the drop-in there: %v %v %v", changed, err, calls)
	}
	// a boot: /run is empty
	_ = os.Remove(dropIn)
	if changed, err := a.Replay(ctx); !changed || err != nil || !reflect.DeepEqual(calls, []string{"set on", "reload"}) {
		t.Fatalf("replay after a boot: %v %v %v", changed, err, calls)
	}
	calls = nil
	if err := a.Switch(ctx, false); err != nil || a.Enabled() {
		t.Fatalf("off: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"set off", "reload"}) {
		t.Fatalf("off: %v", calls)
	}
	if _, err := os.Stat(dropIn); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the drop-in after off: %v", err)
	}
	// a drop-in without the stored switch (the state directory was restored): removed at start
	_ = os.WriteFile(dropIn, []byte("x"), 0o644)
	calls = nil
	if changed, err := a.Replay(ctx); !changed || err != nil || !reflect.DeepEqual(calls, []string{"set off", "reload"}) {
		t.Fatalf("replay with a stale drop-in: %v %v %v", changed, err, calls)
	}
}

func TestIOAccountingErrors(t *testing.T) {
	dir := t.TempDir()
	a := &IOAccounting{File: filepath.Join(dir, "io-accounting"), DropIn: filepath.Join(dir, "none"),
		Set:    func(context.Context, bool) error { return errors.New("refused") },
		Reload: func(context.Context) error { t.Error("reloaded after a failed write"); return nil }}
	if err := a.Switch(context.Background(), true); err == nil {
		t.Error("a failed write is not an error")
	}
	var none *IOAccounting
	if none.Enabled() {
		t.Error("nil is on")
	}
	if err := none.Switch(context.Background(), true); err == nil {
		t.Error("nil can be switched")
	}
	if changed, err := none.Replay(context.Background()); changed || err != nil {
		t.Error("nil replays")
	}
}
