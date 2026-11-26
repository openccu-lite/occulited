/*
 * The catalogue's tags on the Addons page (occulited task 188): the fixed list the system sends
 * with the catalogue (`catalog.tags`, `{id, label: {de, en}}`), the tags each entry carries
 * (`tags`, set in the catalogue file), and the page's filter by them. Since task 189 (variant B,
 * the maintainer 2026-11-26) the filter is a list of checkboxes, one per tag some entry carries:
 * all checked is no filter (`null`); otherwise a card shows when it carries any checked tag (OR),
 * and a card without tags only while all are checked. The cards show their tags as icons, cut to
 * `PILLS_MAX` and "+n". The filter is remembered per browser. `catalogtags.test.ts` covers it.
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

/** per tag of the list, how many of the cards carry it (the cards the other filters let through) */
export function tagCounts(cards: {tags?: string[]}[], list: CatalogTag[]): Record<string, number> {
    const n: Record<string, number> = Object.fromEntries(list.map((t) => [t.id, 0]));
    for (const c of cards) for (const t of new Set(c.tags ?? [])) if (n[t] !== undefined) n[t] += 1;
    return n;
}

/**
 * The filter list's order (the maintainer's list has 15 tags): by how many cards of the whole
 * catalogue carry each, most first, the list's order breaking ties. Counted over all cards, not
 * the filtered ones, so the rows do not move while the user types or switches Installed only.
 */
export function orderTags(list: CatalogTag[], all: {tags?: string[]}[]): CatalogTag[] {
    const n = tagCounts(all, list);
    return list.map((t, i) => ({t, i})).sort((a, b) => (n[b.t.id] ?? 0) - (n[a.t.id] ?? 0) || a.i - b.i).map((x) => x.t);
}


/** the tags of the list some card of the catalogue carries: the filter's rows */
export function usedTags(list: CatalogTag[], all: {tags?: string[]}[]): CatalogTag[] {
    const used = new Set(all.flatMap((c) => c.tags ?? []));
    return list.filter((t) => used.has(t.id));
}

/** The filter: `null` is all tags checked (no filter), a list the checked tags' ids. */
export type TagFilter = string[] | null;

/**
 * The filter against the rows it is shown with (`ids`, the used tags): an id the rows do not have
 * is dropped, every id checked is `null`, and so is a filter none of whose tags is left. While
 * the rows are unknown (no catalogue yet) the filter is kept as it is, so a remembered one
 * survives the first paint.
 */
export function normalizeFilter(f: TagFilter, ids: readonly string[]): TagFilter {
    if (f === null || ids.length === 0) return f;
    const kept = ids.filter((id) => f.includes(id));
    // a remembered filter whose every tag is gone is no filter (an empty one stays: *None*)
    return kept.length === ids.length || (kept.length === 0 && f.length > 0) ? null : kept;
}

/** whether a card with these tags passes: all checked lets every card through, else any checked tag */
export function matchesFilter(tags: string[] | undefined, f: TagFilter): boolean {
    return f === null || (tags ?? []).some((t) => f.includes(t));
}

/** whether the tag's box is checked */
export function isChecked(f: TagFilter, id: string): boolean {
    return f === null || f.includes(id);
}

/** a box clicked (or a chip's ×): the tag on when it was off, off when it was on; in the rows' order */
export function toggleFilter(f: TagFilter, id: string, ids: readonly string[]): TagFilter {
    const cur = f ?? [...ids];
    const next = cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id];
    const order = (x: string) => {
        const i = ids.indexOf(x);
        return i < 0 ? ids.length : i;
    };
    return normalizeFilter(next.sort((a, b) => order(a) - order(b)), ids);
}

/** how many boxes are checked, for the button ("Tags: 3"); `null` is all */
export function checkedCount(f: TagFilter, ids: readonly string[]): number | null {
    const n = normalizeFilter(f, ids);
    return n === null ? null : n.length;
}

export const FILTER_KEY = 'ol.addons.tagFilter';

/** the remembered filter: "all", or a JSON list of ids; anything else (nothing stored) is all */
export function parseFilter(raw: string | null): TagFilter {
    if (!raw || raw === 'all') return null;
    try {
        const v = JSON.parse(raw);
        return Array.isArray(v) ? [...new Set(v.filter((s): s is string => typeof s === 'string' && s !== ''))] : null;
    } catch {
        return null;
    }
}

export function serializeFilter(f: TagFilter): string {
    return f === null ? 'all' : JSON.stringify(f);
}

/** How many icons a card shows before "+n": 15 tags would fit at a desktop width, not on a phone. */
export const PILLS_MAX = 9;

/**
 * A card's tags as icons, in the entry's order, those the list knows: up to `max`, and the rest
 * behind "+n" when more than one would go there (one more icon is as wide as "+1").
 */
export function cardPills(tags: string[] | undefined, list: CatalogTag[], max = PILLS_MAX): {shown: CatalogTag[]; rest: CatalogTag[]} {
    const known = (tags ?? []).map((id) => list.find((t) => t.id === id)).filter((t): t is CatalogTag => !!t);
    if (known.length <= max + 1) return {shown: known, rest: []};
    return {shown: known.slice(0, max), rest: known.slice(max)};
}
