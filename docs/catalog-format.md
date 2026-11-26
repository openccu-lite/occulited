# The addon catalogue

The catalogue is one JSON file in this repository, [`catalog/catalog.json`](../catalog/catalog.json). It says
**where the addons' manifests are** and nothing else: an addon describes itself in its own
[`openccu-lite.json`](manifest-format.md), and the system reads that — from the package at install time, from
the repository for the Addons page. To list an addon, open a pull request that adds one entry.

```json
{
  "format": 1,
  "addons": [
    {
      "git": "https://github.com/homematic-community/ccu-addon-mosquitto",
      "manifest": "addon_files/openccu-lite.json",
      "tags": ["integration", "mqtt"]
    },
    {
      "git": "https://github.com/example-author/ccu-addon-example",
      "manifest": "catalog/manifests/ccu-addon-example.json",
      "untested": true,
      "tags": ["devices"]
    }
  ]
}
```

| Field | Rules |
| --- | --- |
| `format` | `1`. A reader refuses another number. |
| `git` | The addon's repository, an `https://` URL without `.git`. Each repository once. For a GitHub repository the system finds the **latest release tag** through the GitHub API and reads the manifest at that tag, so the page describes what an install would get; a repository without a release is read at its default branch — and so is one whose latest release does not carry the manifest yet (the file was added after that release; a 404 at the tag), until a release has it. Other hosts are read at their default branch (`<git>/raw/branch/<default>/<manifest>`, the Gitea shape). |
| `manifest` | The path of `openccu-lite.json` **inside that repository** — usually where the packaging puts it at the root of the tarball (`addon_files/openccu-lite.json`). **An adapter manifest** for an addon whose author ships none lives in *this* repository under `catalog/manifests/<id>.json`, written by the catalogue maintainers; a path that begins with `catalog/manifests/` is read from here (bundled in the image, fetched from this repository on a refresh), never from `git`, while the manifest's own `release` names the author's repository. The system applies an adapter manifest at install when the package carries no manifest of its own. The catalogue holds two: JP-HB-Devices (`catalog/manifests/jp-hb-devices-addon.json`) and CCU-Jack (`catalog/manifests/ccu-jack.json`). HomeKit-CCU left it with its entry and adapter manifest (occulited task 188, 2026-11-25; JP-HB-Devices and CCU-Jack, taken out with it, are back since 2.0.0-rc.6). An addon installed before keeps the copy its install stored and runs and is managed as before (Services page, uninstall); the system no longer offers catalogue updates for one the catalogue does not list. |
| `untested` | Optional, `true` when **nobody has tried the addon on openccu-lite yet**; set by the catalogue maintainers. The Addons page shows it as the label *untested* ("install at your own risk"). It grants and refuses nothing — every addon's manifest is applied as declared, and the install is the same (D-119, revised 2026-09-25). An entry without it carries no label. Catalogues written before then have a `verified` flag instead; the system ignores it. |
| `root_access` | Optional, `true` for an addon **whose purpose is to give its user root on the system** - a web shell - although its own process runs confined; set by the catalogue maintainers (occulited task 153). The Addons page shows it as the label *root access* in the warning colour, with a sentence that says so. Like `untested` it grants and refuses nothing: what the addon's process may do is its manifest's `runtime` block, and an addon that itself runs as root says so there (`root`). |

| `tags` | Required for an entry of this repository (CI), one or more out of the fixed list below, each once; set by the catalogue maintainers, not by the addon (occulited task 188; openccu-lite from 2.0.0-rc.5). The Addons page filters by them. A system ignores a tag it does not know, so a catalogue with a newer tag still loads on an older system. |

**The tags** (`catalog.Tags` in `internal/catalog/tags.go`; a new tag goes there, here, into the tests and into the UI's `ui/src/lib/tagstyle.ts` and `ui/src/app.css` in one commit). Each card shows its tags as small icons in the tag's colour, the name in the tooltip, nine at most and *+n* for the rest. A button beside the search, *Tags: all*, opens a checkbox per tag at least one entry carries, the most used first (counted over the whole catalogue, so the rows stay put while the user filters; this table's order breaks ties), with *All* and *None*; the checked tags show as chips under the search with *Reset filter*. A card is shown when it carries any checked tag; a card without tags only while all are checked (occulited task 189).

| Tag | de | en | Icon | Colour |
| --- | --- | --- | --- | --- |
| `control` | Bedienung | Control | toggle | blue |
| `automation` | Automation | Automation | lightning bolt | orange |
| `integration` | Anbindung | Integration | chain link | violet |
| `devices` | Geräte | Devices | light bulb | gold |
| `device-management` | Geräteverwaltung | Device management | gear | teal |
| `monitoring` | Monitoring | Monitoring | pulse line | green |
| `history` | Historie | History | clock turned back | brown |
| `remote` | Fernzugriff | Remote access | globe | indigo |
| `tools` | Werkzeuge | Tools | wrench | slate |
| `mqtt` | MQTT | MQTT | antenna | pink |
| `matter` | Matter | Matter | three arrows meeting | lime |
| `home-assistant` | Home Assistant | Home Assistant | house with a branch | cyan |
| `homekit` | HomeKit | HomeKit | rounded house with a dot | amber |
| `mcp` | MCP | MCP | robot head | magenta |
| `alarm` | Alarmsystem | Alarm system | shield | red |

That is the whole format. Not in the catalogue, because the manifest has it: names, summaries, descriptions, icons, the
homepage, the release source, compatibility, the `runtime` block. Not anywhere: status notes, tiers, star counts
(the system fetches those from GitHub when the user runs an update check, and caches them).

## What the system does with it

- **The image carries a copy** of this file and of `catalog/manifests/` as `/etc/occulite/catalog.json` and
  `/etc/occulite/manifests/`, installed by the occulited package from the same commit as the binary. Until the
  user's first check the Addons page shows the entries by their repository name, with the adapter manifests'
  details already there, marked as *the built-in list of <date>*, and a notice that the catalogue is loaded from
  GitHub with a *Load the catalogue now* button.
- **The user's update check** (*Check for updates* on the Addons page) fetches this file from the configured URLs
  (`catalog.urls` in `occulited.json`; the published copy first, the bundled one as the fallback; the first entry
  per repository wins, except that an entry whose file has no `tags` field at all - a kept copy from before the tags -
  takes its tags from the next file that has the entry, occulited task 189), then every entry's manifest at its latest release tag, the star counts and the latest
  releases. Everything is cached in occulited's state directory and shown from the cache; no fetch happens on a
  page load or in the background (D-90). A check that cannot fetch the published file, or reads no addon's
  manifest, is a failed check: the page says *check failed* with the host and the reason and offers a retry, the
  list it had stays, and when the checks keep failing (three in a row, or over a day) the Status page warns.
- **An install from the page** resolves the release from the manifest's `release`, downloads the asset, checks a
  `<asset>.sha256` sidecar when the release has one, and hands the archive to the same installer a manual upload
  takes. The package's own manifest is what gets applied; the fetched one stands in only when the package carries
  none (an adapter, or a release older than the addon's first manifest).

## The pre-resolved catalogue (occulited task 124)

The check above asks `api.github.com` twice per entry (the release list, the star count). Unauthenticated, GitHub
allows 60 such calls an hour per public address, shared by every system, browser and script behind it - a lab, a
household with two systems, a provider's CGNAT ran into the limit. So a GitHub Action asks once, with a token, for
everyone:

- **`.github/workflows/catalog-resolve.yml`** runs every hour, on a change to `catalog/` and by hand. It runs
  `cmd/catalog-resolve`, which runs **the catalogue's own check** (`internal/catalog`: the catalogue file, every
  entry's latest tag and manifest at it, its images, the release lists, the star counts) against the API with the
  workflow's `GITHUB_TOKEN`, plus openccu-lite's own release list (100 releases) for the system update check, and
  writes down what the API answered.
- **The file** `catalog-resolved.json` is published as the only file of the branch **`catalog-resolved`**, which
  is appended to and never force-pushed (`scripts/catalog-resolved-publish.sh`): a run commits only when the answers
  changed (the file without its `generated_at`) or the published file is three hours old - re-stamped well inside
  the 24 hours a system takes it for (six until occulited B-172: GitHub's scheduler skips the hourly runs for
  up to half a day, and every system then asked the API per entry) - and creates the branch's first commit when it does not exist. Read from `https://raw.githubusercontent.com/openccu-lite/occulited/catalog-resolved/catalog-resolved.json`
  - the catalogue file's own host, so a system reaches nothing new (no other host to trust, pin or let through a
  firewall), and no Pages site to set up. A run that resolves nothing (no token, no network) writes nothing: the
  last good file stays until it is stale.
- **Format** (`internal/resolved`): `{format: 1, generated_at, catalog, repos: {"<owner>/<repo>": {releases, per_page,
  stars?, error?}}, entries: [{git, manifest, id?, tag?, release_repo?, images?: {<kind>: "ok" | why not}, error?}]}`.
  `releases` is GitHub's release list in GitHub's own shape (`tag_name`, `name`, `draft`, `prerelease`, `html_url`,
  `published_at`, `assets: [{name, size, browser_download_url}]`), `per_page` how many were asked for (10 for an
  addon, as a system asks; 100 for openccu-lite). An entry or a repository that failed keeps its `error` instead of
  being dropped - `entries[].images` shows an image the manifest declares that is not at the tag before any
  system runs into it. The resolver checks as an `aarch64` system (`--arch`, the common architecture), so an image
  that is only in the package is read from that architecture's package as a system's check reads it
  ([manifest-format.md](manifest-format.md), occulited task 128: at most 64 MB of download, streamed until the declared
  files are found) and shows as `ok`; an addon with no `aarch64` package shows such an image as not fetched.
- **A system's check** reads the file first, once per check, conditional on its ETag (`catalog.resolved` in
  `occulited.json`; absent = that URL, `"off"` = none). While it is valid - format 1, `generated_at` at most 24
  hours old (and not more than an hour in the future) - it answers the release lists and star counts; everything the
  system derives from them (the tag, the manifest at it, the asset for its architecture, an install's release, the
  update hints) is derived by the same code as before, so the file and the API path cannot drift. A repository the
  file does not hold, holds with an `error` or with a shorter list than asked for, and a file that is missing, stale
  or of another format, is asked of the API as before. The manifests, images and packages come from the raw host
  and the release downloads as before (neither is rate-limited). A system sends no token.
  Without a valid file (occulited B-172) a check reads each repository's release list once, asks for the star counts
  after all of them, and stops asking the API while GitHub's last answer left 10 or fewer requests of the address's
  hour (`X-RateLimit-Remaining`): those are an install's, which reads its release list anew.
- **The system update check** (`system_update.resolved`, the same default) takes the feed repository's list
  (`system_update.feed`'s `/repos/<owner>/<repo>/releases`) from the file the same way, per channel the same picks;
  `system_update.feed` stays the fallback. The file carries openccu-lite's list under both of its names,
  `openccu-lite/openccu-lite` and `hobbyquaker/openccu-lite` (the owner before the move to the openccu-lite
  organisation on 2026-11-13), so a system whose feed still names the old owner finds it too.

## The check on a pull request

`go test ./internal/manifest/` parses every file under `catalog/manifests/` and `catalog/catalog.json`: the JSON
must parse, an adapter manifest's `id` must equal its file name and it must name a `release`. `go test ./internal/catalog/`
checks the tags: every entry has at least one, each out of the fixed list and once, and every tag of the list is used. CI runs both. The
manifest at `<git>/<manifest>` of a new entry is read by the reviewer, not by CI: the catalogue makes no promise
about a repository it does not own.
