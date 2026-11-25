/*
 * The catalogue's tags on the Addons page (occulited task 188): the fixed list the system sends
 * with the catalogue (`catalog.tags`, `{id, label: {de, en}}`), the tags each entry carries
 * (`tags`, set in the catalogue file), and the page's filter by them - chips with counts, several
 * at a time; a card shows when it carries any of the chosen tags. The choice is remembered per
 * browser. `catalogtags.test.ts` covers it.
 */
import type {Text} from './addoncards';

// a label as the manifest's texts are read (addoncards.asText, here without the import cycle)
function asText(x: unknown): Text | undefined {
    if (typeof x === 'string') return x ? {en: x} : undefined;
    if (!x || typeof x !== 'object' || Array.isArray(x)) return undefined;
    const o = x as Record<string, unknown>;
    const out: Text = {};
    if (typeof o.de === 'string') out.de = o.de;
    if (typeof o.en === 'string') out.en = o.en;
    return out.de !== undefined || out.en !== undefined ? out : undefined;
}

export interface CatalogTag { id: string; label: Text }

/** the system's tag list, each a tag with an id and a label; anything else is dropped, an id twice is one */
export function readTags(x: unknown): CatalogTag[] {
    if (!Array.isArray(x)) return [];
    const out: CatalogTag[] = [];
    for (const t of x) {
        if (!t || typeof t !== 'object') continue;
        const id = (t as {id?: unknown}).id;
        const label = asText((t as {label?: unknown}).label);
        if (typeof id !== 'string' || !id || !label || out.some((o) => o.id === id)) continue;
        out.push({id, label});
    }
    return out;
}

/** an entry's tags: strings only, each once */
export function readEntryTags(x: unknown): string[] | undefined {
    if (!Array.isArray(x)) return undefined;
    const out = [...new Set(x.filter((s): s is string => typeof s === 'string' && s !== ''))];
    return out.length ? out : undefined;
}

/** true when no tag is chosen, or the card carries one of the chosen */
export function matchesTags(tags: string[] | undefined, chosen: readonly string[]): boolean {
    return chosen.length === 0 || (tags ?? []).some((t) => chosen.includes(t));
}

/** per tag of the list, how many of the cards carry it (the cards the other filters let through) */
export function tagCounts(cards: {tags?: string[]}[], list: CatalogTag[]): Record<string, number> {
    const n: Record<string, number> = Object.fromEntries(list.map((t) => [t.id, 0]));
    for (const c of cards) for (const t of new Set(c.tags ?? [])) if (n[t] !== undefined) n[t] += 1;
    return n;
}

/** the chips to show: the tags of the list that at least one card of the catalogue carries, and every chosen one */
export function visibleTags(list: CatalogTag[], all: {tags?: string[]}[], chosen: readonly string[]): CatalogTag[] {
    const used = new Set(all.flatMap((c) => c.tags ?? []));
    return list.filter((t) => used.has(t.id) || chosen.includes(t.id));
}

export const TAGS_KEY = 'ol.addons.tags';

/** the remembered choice: a JSON list of ids; anything else is no choice */
export function parseChosen(raw: string | null): string[] {
    if (!raw) return [];
    try {
        const v = JSON.parse(raw);
        return Array.isArray(v) ? [...new Set(v.filter((s): s is string => typeof s === 'string' && s !== ''))] : [];
    } catch {
        return [];
    }
}

/** the choice without the tags the list no longer has - kept while the list is not known yet */
export function knownChosen(chosen: readonly string[], list: CatalogTag[]): string[] {
    if (list.length === 0) return [...chosen];
    return chosen.filter((id) => list.some((t) => t.id === id));
}

/** a chip clicked: on when it was off, off when it was on, in the list's order */
export function toggleTag(chosen: readonly string[], id: string, list: CatalogTag[]): string[] {
    const next = chosen.includes(id) ? chosen.filter((x) => x !== id) : [...chosen, id];
    const order = (x: string) => {
        const i = list.findIndex((t) => t.id === x);
        return i < 0 ? list.length : i;
    };
    return next.sort((a, b) => order(a) - order(b));
}

/**
 * The chips' order (the maintainer's list has 15 tags): by how many cards of the whole catalogue
 * carry each, most first, the list's order breaking ties. Counted over all cards, not the filtered
 * ones, so the chips do not move while the user types or switches Installed only.
 */
export function orderTags(list: CatalogTag[], all: {tags?: string[]}[]): CatalogTag[] {
    const n = tagCounts(all, list);
    return list.map((t, i) => ({t, i})).sort((a, b) => (n[b.t.id] ?? 0) - (n[a.t.id] ?? 0) || a.i - b.i).map((x) => x.t);
}

/** How many chips show while the row is collapsed. */
export const TAGS_COLLAPSED = 8;

/**
 * The chips shown: all when expanded; collapsed the first `TAGS_COLLAPSED` of the order plus every
 * chosen one beyond them (a choice is never hidden). `more` is how many the collapsed row leaves out.
 */
export function shownTags(ordered: CatalogTag[], chosen: readonly string[], expanded: boolean, max = TAGS_COLLAPSED): {tags: CatalogTag[]; more: number} {
    if (expanded || ordered.length <= max + 1) return {tags: ordered, more: 0};
    const tags = ordered.filter((t, i) => i < max || chosen.includes(t.id));
    return {tags, more: ordered.length - tags.length};
}

export const TAGS_MORE_KEY = 'ol.addons.tagsMore';
