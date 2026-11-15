package system

import (
	"github.com/hobbyquaker/occulited/internal/manifest"
)

// openccu-lite #23: an addon adopted from OpenCCU - installed before the update to openccu-lite,
// not through occulited - runs as root with no stored manifest, no API token of its own and no
// own user, even when the manifest it ships in its own directory says it can run as its own user
// (OpenCCU-Loom copies its openccu-lite.json there). The Status page and the addon's card say so,
// and that installing it again from its package stores the manifest, issues the token and gives it
// its own user. A hint only: the file in the addon's directory is read for the hint alone and
// grants nothing - the same reasoning as regaFromManifest. occulited never adopts it as the stored
// manifest: a root addon could have written anything there.

// AdoptedManifest is what the hint needs from the manifest in an adopted addon's directory.
type AdoptedManifest struct {
	// Token: the manifest declares API scopes, so an install would issue the addon a token.
	Token bool
}

// confinableManifestInDir reads /usr/local/addons/<id>/openccu-lite.json and answers it when an
// install of the package would confine the addon (ApplyManifest's rule): the file parses and
// validates, names this id, carries a runtime block that does not say root and that the policy
// would accept, and the running system meets its requires.lite. A manifest without a runtime
// block declares nothing, so it is not taken as a promise that the addon runs as its own user.
func (r Root) confinableManifestInDir(id string) *AdoptedManifest {
	if !addonIDRe.MatchString(id) {
		return nil
	}
	m, err := manifest.ParseFile(r.join("/usr/local/addons/" + id + "/" + manifest.FileName))
	if err != nil || m.ID != id || m.Runtime == nil || m.Runtime.Root {
		return nil
	}
	if RuntimeFromManifest(m.Runtime).validate() != nil || !m.LiteMet(r.ReadVersion().Lite) {
		return nil
	}
	return &AdoptedManifest{Token: len(m.Runtime.APIScopes) > 0}
}

// AdoptedConfinable says whether an addon runs as root only because it came along from before
// openccu-lite, while an install of its package would give it its own user: root with the policy
// the adoption wrote ("migrated") or none at all, no stored manifest, the box's default for a new
// install is its own user, and the manifest in its directory would confine it. Not for root the
// operator chose (source "user"), nor a confinement that failed ("fallback": an install would
// fail the same way), nor an addon a package or the catalogue already declared.
func (a *SystemdAddons) AdoptedConfinable(id string, v AddonPolicyView) *AdoptedManifest {
	if v.Mode != "root" || (v.Source != "migrated" && v.Source != "") {
		return nil
	}
	if a.defaultMode() != AddonDefaultMode {
		return nil
	}
	root := a.Scripts.Root
	if root.ReadAddonManifest(id) != nil {
		return nil
	}
	return root.confinableManifestInDir(id)
}
