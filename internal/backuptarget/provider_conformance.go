package backuptarget

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

// ProviderConformance runs the contract's calls against a provider's destination and folder, as
// the system's own client makes them: info names the destination, the write test passes, a small
// file is put, listed with its size, read back byte for byte and deleted. It is what
// `occulited -backup-provider-check` runs, for an addon's own tests and for a look at a running
// provider; it leaves nothing behind when it passes. step is called before each step (nil = not).
func ProviderConformance(ctx context.Context, c *ProviderClient, dest, folder string, step func(string)) error {
	say := func(s string) {
		if step != nil {
			step(s)
		}
	}
	say("info")
	info, err := c.Info(ctx)
	if err != nil {
		return fmt.Errorf("info: %w", err)
	}
	found := false
	for _, d := range info.Destinations {
		if d.ID == dest {
			found = true
			if d.State != StateWritable {
				return fmt.Errorf("info: the destination %s is %s", dest, d.State)
			}
		}
	}
	if !found {
		return fmt.Errorf("info: no destination %q (it names %d)", dest, len(info.Destinations))
	}
	say("test")
	if _, err := c.Test(ctx, dest, folder); err != nil {
		return fmt.Errorf("test: %w", err)
	}
	body := make([]byte, 256*1024+17)
	if _, err := rand.Read(body); err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	name := "conformance-0-" + time.Now().Format("2006-01-02-1504") + ".sbk.age"
	say("put " + name)
	put, err := c.Put(ctx, dest, folder, name, bytes.NewReader(body), int64(len(body)), hex.EncodeToString(sum[:]), nil)
	if err != nil {
		return fmt.Errorf("put: %w", err)
	}
	clean := func() { _ = c.Delete(context.WithoutCancel(ctx), dest, folder, name) }
	say("list")
	files, err := c.List(ctx, dest, folder)
	if err != nil {
		clean()
		return fmt.Errorf("list: %w", err)
	}
	listed := false
	for _, f := range files {
		if f.Name == name {
			listed = true
			if f.Size != int64(len(body)) {
				clean()
				return fmt.Errorf("list: %s has %d bytes, %d were put", name, f.Size, len(body))
			}
		}
		if f.Name == name+".partial" {
			clean()
			return fmt.Errorf("list: a .partial is left beside %s", name)
		}
	}
	if !listed {
		clean()
		return fmt.Errorf("list: %s is not in the list after a put that answered %s", name, put.Verified)
	}
	say("get")
	rc, _, err := c.Get(ctx, dest, folder, name)
	if err != nil {
		clean()
		return fmt.Errorf("get: %w", err)
	}
	back, err := io.ReadAll(io.LimitReader(rc, int64(len(body))+1))
	rc.Close()
	if err != nil || !bytes.Equal(back, body) {
		clean()
		return fmt.Errorf("get: the file read back differs from the one put (%d bytes, err %v)", len(back), err)
	}
	say("put with a wrong hash is refused")
	bad := "conformance-1-" + time.Now().Format("2006-01-02-1504") + ".sbk.age"
	if _, err := c.Put(ctx, dest, folder, bad, bytes.NewReader(body), int64(len(body)), hex.EncodeToString(make([]byte, 32)), nil); err == nil {
		_ = c.Delete(ctx, dest, folder, bad)
		clean()
		return fmt.Errorf("put: a body that does not match X-SHA256 was accepted")
	}
	if files, err := c.List(ctx, dest, folder); err == nil {
		for _, f := range files {
			if f.Name == bad {
				_ = c.Delete(ctx, dest, folder, bad)
				clean()
				return fmt.Errorf("put: the refused file %s is in the list", bad)
			}
		}
	}
	say("delete")
	if err := c.Delete(ctx, dest, folder, name); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	files, err = c.List(ctx, dest, folder)
	if err != nil {
		return fmt.Errorf("list after delete: %w", err)
	}
	for _, f := range files {
		if f.Name == name {
			return fmt.Errorf("delete: %s is still listed", name)
		}
	}
	return nil
}
