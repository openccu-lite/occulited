/*
 * The catalogue tags' faces (occulited task 189, the maintainer 2026-11-26): every tag of the
 * fixed list (`internal/catalog/tags.go`) has an icon of lib/Icon.svelte and a colour token
 * `--ol-tag-<id>` in app.css (light and both dark blocks). The card shows a tag as its icon in its
 * colour; the filter's list and its chips put the same icon in the same colour before the tag's
 * name, so the user recognises a tag by its colour and shape. A tag added in Go gets its row here
 * and its token in app.css in the same commit (TestTagStylesMirrorTheList checks both).
 */
import type {IconName} from './Icon.svelte';

export const TAG_ICONS: Record<string, IconName> = {
    control: 'toggle-on',
    automation: 'zap',
    integration: 'link',
    devices: 'bulb',
    'device-management': 'settings',
    monitoring: 'activity',
    history: 'history',
    remote: 'globe',
    tools: 'wrench',
    mqtt: 'radio',
    matter: 'matter',
    'home-assistant': 'home',
    homekit: 'homekit',
    mcp: 'bot',
    alarm: 'shield',
};

/** a tag's icon; a tag this UI has no face for (a newer system's) gets the plain tag icon */
export function tagIcon(id: string): IconName {
    return TAG_ICONS[id] ?? 'tag';
}

/** a tag's colour as a CSS value: its token, the muted ink for a tag without one */
export function tagColor(id: string): string {
    return TAG_ICONS[id] ? `var(--ol-tag-${id})` : 'var(--hmm-fg-muted)';
}
