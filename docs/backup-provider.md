# Backup targets an addon provides: the provider contract

**Normative** for protocol 1. occulited's side is `internal/backuptarget/provider.go`; an addon's side is whatever
serves the six calls below. From the first openccu-lite image after 2.0.0-beta.6.

An installed addon can offer places for the system's backup: a cloud account, a bucket, a second system. The addon
declares it in its manifest and serves six HTTP calls on a Unix socket. Its places then appear under *Add target* on
the Backup page beside *USB / directory*, *Network share* and *SSH server (SFTP)*, with the same card, states, *Keep
N*, *Test*, *Back up now*, *Backups here* and *Restore this*. Schedule, retention, encryption, history and restore
stay the system's; the addon is the transport.

**Words.** A **provider** is an addon that declares the extension. A **destination** is one place a provider offers
(the user's account "Privat", an S3 bucket). A **target** is the system's, as for every other kind, and names a
provider, a destination and a folder.

## What a provider can rely on

- **It only ever gets encrypted backups.** A target of this kind has no encryption switch: it cannot be saved without
  a recovery key, the system refuses to send a plain file, and every file name it sends ends in `.sbk.age`. Neither
  the addon nor its destination ever holds a key of the system.
- **It gets bytes, never a path.** The system reads the file from wherever it is (its working copy, a USB stick, a
  share) and writes it into the socket. The addon needs no right on any of them.
- **It needs no scope and no token.** The system calls the addon; the addon calls nothing for this.
- **It is called by the system's service only**, as the user `occulite` (the deliver half of a backup run, and the
  daemon for the page's listings and tests). The root half of a run only opens the socket to see that the provider is
  there and reads nothing from it.

## The declaration

In the addon's manifest `openccu-lite.json` ([manifest-format.md](manifest-format.md#backup--the-addons-part-in-the-system-backup)):

```json
"backup": {
  "targets": {
    "protocol": 1,
    "label": {"de": "Cloud-Speicher", "en": "Cloud storage"},
    "setup": "/addon-settings/sync#accounts"
  }
}
```

| Key | Meaning |
| --- | --- |
| `protocol` | `1` |
| `label` | The kind's word, at most 40 characters per language: the *Add target* button and the card's "· Cloud storage" |
| `setup` | The in-app path where the user makes destinations, a path under `/addons/` or `/addon-settings/`: the link behind *Connect an account…*. The page appends `?return=/backup%23targets` (or `&return=…`); the addon's page should offer the way back |

Declare `requires.lite` with the first version that knows the block: an older system refuses a `backup` block without
`hook` and `staging` and installs the package as if it carried no manifest.

## The socket

Before every start of the addon's unit the system makes `/run/occulite/backup-targets/<id>/` (as root, in the unit's
ownership step): owned by `occulite` with the group `addon-<id>`, mode `0770`, and without a socket a previous run
left. The addon's daemon:

1. binds `target.sock` in it (a stream socket) and serves HTTP/1.1 on it;
2. sets the socket's mode to `0666` - the directory is what keeps everyone else out, and a connecting process needs
   the write bit on the socket itself;
3. accepts a connection only when the peer's uid (`SO_PEERCRED`) is `occulite`'s or `0`.

The system connects only to a socket that is a socket and belongs to the user `addon-<id>` (or to root, for an addon
the user runs as root). A missing socket, a foreign one, or a refused connection is the state **`addon-missing`**: the
target stays configured, a run skips it and delivers to the others, the Status page says which addon is needed.

## The calls

All paths are relative to the socket; `{id}` is a destination's id, `folder` the target's folder on it (one to six
segments of letters, digits, space, `.`, `_`, `-`; the provider creates it when it is missing).

| Call | Request | Answer |
| --- | --- | --- |
| `GET /v1/info` | — | `{protocol: 1, destinations: [Destination]}` |
| `POST /v1/destinations/{id}/test` | `{"folder": "…"}` | Writes, reads back and deletes a small file in the folder → `{state, message?, action?, free_bytes?, total_bytes?}` |
| `PUT /v1/destinations/{id}/files/{name}?folder=…` | **The backup as the body**, streamed, with `Content-Length` and `X-SHA256` (hex) | Uploads it as `<name>.partial`, verifies, renames → `{state, size, verified: "hash"\|"size", duration_ms?, message?, action?}` |
| `GET /v1/destinations/{id}/files?folder=…` | — | `{files: [{name, size, time}]}` (`time` RFC 3339); a folder that does not exist is an empty list |
| `GET /v1/destinations/{id}/files/{name}?folder=…` | — | `200` with the file as the body (and `Content-Length` when known) |
| `DELETE /v1/destinations/{id}/files/{name}?folder=…` | — | `204` |

**`Destination`** is `{id, name, detail?, state, message?, action?}`:

- `id` is **stable and opaque** (`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`), generated once, so a target survives a rename
  of the account;
- `name` is the user's name for it, `detail` one line such as "OneDrive · max@example.org";
- `state` is the destination's state now, as far as the provider knows without contacting anything.

**States.** Every `state` is one of the targets' own: `writable` (in order), `unreachable`, `auth-failed`, `full`,
`read-only`, `error`. `message` is the provider's own sentence for it, `{"de": "…", "en": "…"}`, shown under the
card's badge; `action` is `{label: {de, en}, href}`, an in-app link (under `/addons/` or `/addon-settings/`) the card
shows as a button - *Log in again* → the addon's page. A state outside the list, and an `action` that leads anywhere
else, are dropped (the state becomes `error`).

**Errors.** A call that cannot be served answers a non-2xx status with `{state, message?, action?}`; a `put`, a `test`
and `info` may also answer `200` with a state other than `writable`. Anything else - no JSON, an unknown state, an
answer over 1 MiB - is `error`.

**`put`, precisely.**

- `name` is `<hostname>-<version>-<date>.sbk.age`; the system sends no other shape and the provider should refuse one.
- The provider computes the SHA-256 while it reads and **refuses a body that does not match `X-SHA256` or
  `Content-Length`** (`state: "error"`), leaving no file under the final name.
- It uploads under `<name>.partial` (or by whatever means the destination has to make the final name appear only when
  complete), verifies - the destination's own hash against what it read where the destination has one
  (`verified: "hash"`), else the size (`verified: "size"`) - and renames.
- `size` in the answer is the stored file's size; the system compares it with what it sent, and then asks for the
  list, which must show the name with that size. A provider's word alone is not a delivery.
- A failed `put` leaves at most a `.partial`, which the next `put` of that name replaces. `list` never shows a
  `.partial`.

**Limits the system applies.** Connect 5 s; `info` 10 s (the page asks with 4 s); `test`, `list`, `delete` 60 s; a
`put` and a `get` the file's size at 32 KiB/s, at least 10 minutes, and a `put` is given up when the provider takes no
byte for 2 minutes. A listing is cut at 5000 files; names that are not a backup's are ignored.

**What the system does with it.** In a run: the `put` (tried up to three times when the state is `unreachable` or
`error`), the `list`, then - when the target uploads from a local target (below) - the catch-up, then retention:
this system's files beyond *Keep N* are deleted, oldest first by the time their names carry, never on an
append-only target. A target that failed gets the next night's backup; nothing is kept for a retry in between.
Progress is the system's own (it knows what it has written); the provider reports none.

## Where the upload reads from

One run makes the backup once, into the system's working copy, for all its targets. An addon target's `source` says
where its upload reads the file from:

| `addon.source` | Reads |
| --- | --- |
| empty (**automatic**, the default) | from the first enabled directory or share target that keeps encrypted backups, when there is one; else from the working copy |
| `staging` | always from the working copy |
| a directory or share target's id | from that target |

With a local target as the source, tonight's file is read there once that target's own delivery verified it (else from
the working copy, so the destination still gets tonight's backup when the stick is missing), and **the run catches
up**: this system's newest encrypted backups on the local target that the destination's list lacks are sent as well,
newest first, at most *Keep N* and at most 10 a run. Nights without a network heal by themselves.

## Checking a provider

```
occulited -backup provider-check <addon id> <destination> <folder> [--socket PATH]
```

runs the calls as the system's client makes them - `info` names the destination as `writable`, `test`, a 256 KiB
`put`, the `list` with its size and no `.partial`, the `get` byte for byte, a `put` with a wrong hash refused,
`delete` - and exits 0 when every step passed. It leaves nothing behind. With `--socket` the socket's owner is not
checked (an addon's own tests); without, it is the system's check.

## A minimal provider

A provider that keeps the files in a directory, in Go (the peer check left out for brevity):

```go
func main() {
	const dir = "/usr/local/addons/example/var/backups"
	sock := "/run/occulite/backup-targets/example/target.sock"
	l, _ := net.Listen("unix", sock)
	_ = os.Chmod(sock, 0o666)
	say := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	at := func(r *http.Request, name string) string { return filepath.Join(dir, filepath.Clean("/"+r.URL.Query().Get("folder")), filepath.Base(name)) }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) {
		say(w, map[string]any{"protocol": 1, "destinations": []map[string]any{{"id": "local", "name": "Example", "state": "writable"}}})
	})
	mux.HandleFunc("POST /v1/destinations/local/test", func(w http.ResponseWriter, r *http.Request) {
		say(w, map[string]any{"state": "writable"})
	})
	mux.HandleFunc("PUT /v1/destinations/local/files/{name}", func(w http.ResponseWriter, r *http.Request) {
		final := at(r, r.PathValue("name"))
		_ = os.MkdirAll(filepath.Dir(final), 0o700)
		f, _ := os.Create(final + ".partial")
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(f, h), r.Body)
		f.Close()
		if err != nil || n != r.ContentLength || hex.EncodeToString(h.Sum(nil)) != r.Header.Get("X-SHA256") {
			os.Remove(final + ".partial")
			say(w, map[string]any{"state": "error", "message": map[string]string{"en": "The file arrived damaged."}})
			return
		}
		_ = os.Rename(final+".partial", final)
		say(w, map[string]any{"state": "writable", "size": n, "verified": "hash"})
	})
	mux.HandleFunc("GET /v1/destinations/local/files", func(w http.ResponseWriter, r *http.Request) {
		files := []map[string]any{}
		entries, _ := os.ReadDir(filepath.Dir(at(r, "x")))
		for _, e := range entries {
			if i, err := e.Info(); err == nil && strings.HasSuffix(e.Name(), ".sbk.age") {
				files = append(files, map[string]any{"name": e.Name(), "size": i.Size(), "time": i.ModTime()})
			}
		}
		say(w, map[string]any{"files": files})
	})
	mux.HandleFunc("GET /v1/destinations/local/files/{name}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, at(r, r.PathValue("name")))
	})
	mux.HandleFunc("DELETE /v1/destinations/local/files/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = os.Remove(at(r, r.PathValue("name")))
		w.WriteHeader(http.StatusNoContent)
	})
	_ = http.Serve(l, mux)
}
```
