package catalog

import "github.com/hobbyquaker/occulited/internal/manifest"

// Tag is one of the catalogue's fixed tags (occulited task 188): what the Addons page filters by.
// The catalogue maintainers assign them per entry in catalog.json; an addon's manifest has no say.
type Tag struct {
	ID    string        `json:"id"`
	Label manifest.Text `json:"label"`
}

// Tags is the fixed list (the maintainer's, 2026-11-25), in its order; the page orders the chips
// by how many entries carry each, this order breaking ties. A tag is added here, in
// docs/catalog-format.md and in the tests in one commit; the repository's catalogue may use no
// other (TestRepositoryCatalogueTags), and a system ignores one it does not know, so a catalogue
// with a newer tag still loads on an older system.
var Tags = []Tag{
	{ID: "control", Label: manifest.Text{"de": "Bedienung", "en": "Control"}},
	{ID: "automation", Label: manifest.Text{"de": "Automation", "en": "Automation"}},
	{ID: "integration", Label: manifest.Text{"de": "Anbindung", "en": "Integration"}},
	{ID: "devices", Label: manifest.Text{"de": "Geräte", "en": "Devices"}},
	{ID: "device-management", Label: manifest.Text{"de": "Geräteverwaltung", "en": "Device management"}},
	{ID: "monitoring", Label: manifest.Text{"de": "Monitoring", "en": "Monitoring"}},
	{ID: "history", Label: manifest.Text{"de": "Historie", "en": "History"}},
	{ID: "remote", Label: manifest.Text{"de": "Fernzugriff", "en": "Remote access"}},
	{ID: "tools", Label: manifest.Text{"de": "Werkzeuge", "en": "Tools"}},
	{ID: "mqtt", Label: manifest.Text{"de": "MQTT", "en": "MQTT"}},
	{ID: "matter", Label: manifest.Text{"de": "Matter", "en": "Matter"}},
	{ID: "home-assistant", Label: manifest.Text{"de": "Home Assistant", "en": "Home Assistant"}},
	{ID: "homekit", Label: manifest.Text{"de": "HomeKit", "en": "HomeKit"}},
	{ID: "mcp", Label: manifest.Text{"de": "MCP", "en": "MCP"}},
	{ID: "alarm", Label: manifest.Text{"de": "Alarmsystem", "en": "Alarm system"}},
}

// KnownTag says whether id is one of Tags.
func KnownTag(id string) bool {
	for _, t := range Tags {
		if t.ID == id {
			return true
		}
	}
	return false
}

// knownTags is the entry's tags this system knows, each once, in the entry's order; nil for none.
func knownTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		if KnownTag(t) && !contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
