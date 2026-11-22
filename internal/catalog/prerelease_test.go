package catalog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hobbyquaker/occulited/internal/manifest"
)

// occulited task 171: a version with a SemVer prerelease part is a pre-release.
func TestIsPrereleaseVersion(t *testing.T) {
	for v, want := range map[string]bool{
		"1.0.0-beta.0":  true,
		"v2.0.0-rc.1":   true,
		"3.0.0-rc1":     true,
		"1.1.0-dev.6":   true,
		"0.1-alpha":     true,
		" v1.0.0-rc.2 ": true,
		"1.0.0-rc.1+3":  true,
		"2.1.2":         false,
		"v9.19.0":       false,
		"2.1.2+1":       false, // build metadata, mosquitto's own versions
		"2.1.4+0-x":     false, // the "-" is inside the build metadata
		"6.3":           false,
		"2026-05-01":    false, // a date is no prerelease
		"abc-def":       false,
		"":              false,
	} {
		if got := IsPrereleaseVersion(v); got != want {
			t.Errorf("IsPrereleaseVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

// Which release the resolver offers (task 171): pre-releases only with the manifest's
// release.prerelease, and then a stable release first; a tag with a prerelease part counts as a
// pre-release even when GitHub does not flag it.
func TestResolvePrereleaseChoice(t *testing.T) {
	rel := func(tag string, flagged bool, asset bool) string {
		assets := `[]`
		if asset {
			assets = `[{"name": "a-x86_64-` + tag + `.tar.gz", "size": 1, "browser_download_url": "https://example.org/` + tag + `"}]`
		}
		f := "false"
		if flagged {
			f = "true"
		}
		return `{"tag_name": "` + tag + `", "prerelease": ` + f + `, "html_url": "https://example.org/r/` + tag + `", "assets": ` + assets + `}`
	}
	draft := `{"tag_name": "9.9.9", "draft": true, "assets": [{"name": "a-x86_64-9.9.9.tar.gz", "browser_download_url": "https://example.org/d"}]}`
	for _, c := range []struct {
		name    string
		list    []string
		optIn   bool
		wantTag string // "" = no release
		wantPre bool
	}{
		{"stable only", []string{rel("1.0.0", false, true)}, false, "1.0.0", false},
		{"a flagged pre-release is skipped", []string{rel("1.1.0-beta.0", true, true), rel("1.0.0", false, true)}, false, "1.0.0", false},
		{"an unflagged prerelease part is skipped too", []string{rel("1.1.0-beta.0", false, true), rel("1.0.0", false, true)}, false, "1.0.0", false},
		{"only pre-releases, no opt-in: nothing", []string{rel("1.0.0-beta.1", true, true)}, false, "", false},
		{"only pre-releases, opted in: the newest", []string{draft, rel("1.0.0-beta.1", true, true), rel("1.0.0-beta.0", true, true)}, true, "1.0.0-beta.1", true},
		{"opted in, an unflagged beta", []string{rel("v1.0.0-beta.0", false, true)}, true, "v1.0.0-beta.0", true},
		{"a flagged release without a prerelease part is a pre-release", []string{rel("1.0.0", true, true)}, true, "1.0.0", true},
		{"opted in: a stable release wins over a newer beta", []string{rel("1.1.0-beta.0", true, true), rel("1.0.0", false, true), rel("1.0.0-beta.3", true, true)}, true, "1.0.0", false},
		{"opted in: a stable release without a package does not count", []string{rel("1.1.0-beta.0", true, true), rel("1.0.0", false, false)}, true, "1.1.0-beta.0", true},
		{"drafts never", []string{draft}, true, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := "[" + strings.Join(c.list, ",") + "]"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			s := New(nil, "x86_64", nil)
			s.GitHubAPI = srv.URL
			r, err := s.Resolve(t.Context(), &manifest.Release{GitHub: "o/a", Asset: "a-{arch}-{version}.tar.gz", Prerelease: c.optIn})
			if c.wantTag == "" {
				if err == nil {
					t.Fatalf("want no release, got %+v", r)
				}
				return
			}
			if err != nil || r.Tag != c.wantTag || r.Prerelease != c.wantPre {
				t.Fatalf("got %+v %v, want %s prerelease=%v", r, err, c.wantTag, c.wantPre)
			}
			// the manifest is read at the same tag when no stable release exists or it is the pick
			tag, err := s.latestTag(t.Context(), "https://github.com/o/a")
			if err != nil {
				t.Fatal(err)
			}
			if !c.wantPre && tag != c.wantTag {
				t.Errorf("latestTag %q, the resolver picked %q", tag, c.wantTag)
			}
		})
	}
}
