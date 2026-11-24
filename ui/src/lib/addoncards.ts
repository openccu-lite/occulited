/**
 * The Addons page's cards from what the system answers (occulited B-157).
 *
 * One entry the page does not expect - a catalogue item or an installed addon with a field of the
 * wrong type, no id, no repository, a manifest text that is no text - must not take the page down:
 * before, a throw in the list's derivation left the whole page on "Loading…", and a value the page
 * reads above the list (the update count, the reinstall hints) could do the same. So the answers are
 * read here entry by entry:
 *
 * - every field the page reads outside a card is coerced to its type (a wrong one is dropped);
 * - an entry that cannot be a card at all (not an object, an id that is no text, neither an id nor
 *   a repository) becomes a broken card - its id where it has one, a short reason - and the console
 *   says which; the rest of the list stays;
 * - the cards are built and sorted so that one card cannot throw for all of them.
 *
 * What fails while a card renders is caught by that card's own `<svelte:boundary>` on the page.
 */
import type {Addon} from './api';
import type {AddonImages} from './addonimages';
import {compareEntries} from './catalog';

export interface Text { de?: string; en?: string }
// a catalogue item (D-119): the entry - repository, manifest path, untested - and the addon's
// manifest flattened into it once the user's check has fetched it (id, name, description, ...)
export interface Entry {
    git: string; manifest_path: string; untested?: boolean; root_access?: boolean; adapter?: boolean;
    id?: string; name?: Text; description?: Text; homepage?: string; licence?: string;
    release?: {github?: string; prerelease?: boolean}; requires?: {architectures?: string[]; lite?: string}; lite_unmet?: boolean;
    ui?: {own_updater?: boolean}; runtime?: {note?: Text};
    /** openccu-lite task 100: the manifest's images as the check fetched them, kind → URL on this origin */
    images?: AddonImages;
    tag?: string; fetched?: string; error?: string; stars?: number;
    latest?: {version: string; asset?: string; notes_url?: string; prerelease?: boolean}; update_available?: boolean;
    /** task 26: the release notes of the offered version (the manifest's changelog, else the release's page) */
    release_notes?: string;
    /** occulited B-174: how the system would run it once installed; absent while no manifest is known */
    install_mode?: 'root' | 'confined';
}

/**
 * Why an entry is no card: it is not an object, it has no id (an installed addon), its id is no
 * text, it names neither an id nor a repository (a catalogue entry), the list itself is no list;
 * else the message of what threw.
 */
export type BrokenWhy = 'not-object' | 'no-id' | 'id-not-text' | 'no-id-no-repo' | 'not-a-list' | (string & {});
export const BROKEN_CODES = ['not-object', 'no-id', 'id-not-text', 'no-id-no-repo', 'not-a-list'] as const;

/** One card; `broken` is the reason an entry could not become one (the card says so). */
export interface Card { id: string; name: string; entry: Entry | null; addon: Addon | null; update: string; broken?: BrokenWhy }

/** An answer entry that cannot be a card, for a card of its own. */
export interface Broken { id: string; reason: BrokenWhy }

type Obj = Record<string, unknown>;
const isObj = (x: unknown): x is Obj => typeof x === 'object' && x !== null && !Array.isArray(x);
const str = (x: unknown): string | undefined => (typeof x === 'string' ? x : undefined);
const strs = (x: unknown): string[] | undefined => (Array.isArray(x) ? x.filter((s): s is string => typeof s === 'string') : undefined);
const yes = (x: unknown): boolean | undefined => (x === true ? true : undefined);
const num = (x: unknown): number | undefined => (typeof x === 'number' && Number.isFinite(x) ? x : undefined);

/** A manifest text: `{de, en}` with strings only; a plain string is taken as the English one. */
export function asText(x: unknown): Text | undefined {
    if (typeof x === 'string') return x ? {en: x} : undefined;
    if (!isObj(x)) return undefined;
    const out: Text = {};
    if (typeof x.de === 'string') out.de = x.de;
    if (typeof x.en === 'string') out.en = x.en;
    return out.de !== undefined || out.en !== undefined ? out : undefined;
}

/** The text in `lang`, else English, else German, else ''. */
export function textIn(x: Text | undefined, lang: string): string {
    const v = (x as Record<string, unknown> | undefined)?.[lang] ?? x?.en ?? x?.de;
    return typeof v === 'string' ? v : '';
}

/** the repository's path (owner/name), for an entry known by its repository alone */
export const repoName = (e: {git?: string}): string => (e.git ?? '').replace(/^https?:\/\/[^/]+\//, '').replace(/\/+$/, '');

/** the card's id: the manifest's, else one made of the repository */
export const cardId = (e: Entry): string => e.id ?? `repo-${repoName(e).replace(/[^A-Za-z0-9_.-]+/g, '-')}`;

/** What an entry's own words name it by, for a broken card. */
function idOf(x: unknown, fallback: string): string {
    if (isObj(x)) {
        if (typeof x.id === 'string' && x.id) return x.id;
        if (typeof x.git === 'string' && x.git) return `repo-${repoName({git: x.git}).replace(/[^A-Za-z0-9_.-]+/g, '-')}`;
    }
    return fallback;
}

/** One catalogue entry, its fields in their types; a reason when it cannot be a card. */
export function readEntry(x: unknown): Entry | BrokenWhy {
    if (!isObj(x)) return 'not-object';
    if (x.id !== undefined && x.id !== null && (typeof x.id !== 'string' || x.id === '')) return 'id-not-text';
    const id = str(x.id);
    const git = str(x.git) ?? '';
    if (!id && !git) return 'no-id-no-repo';
    const latest = isObj(x.latest) && typeof x.latest.version === 'string' ? {version: x.latest.version, asset: str(x.latest.asset), notes_url: str(x.latest.notes_url), prerelease: yes(x.latest.prerelease)} : undefined;
    const images = isObj(x.images) ? (Object.fromEntries(Object.entries(x.images).filter(([, v]) => typeof v === 'string')) as AddonImages) : undefined;
    return {
        ...x,
        git,
        manifest_path: str(x.manifest_path) ?? '',
        id,
        name: asText(x.name),
        description: asText(x.description),
        homepage: str(x.homepage),
        licence: str(x.licence),
        untested: yes(x.untested),
        root_access: yes(x.root_access),
        adapter: yes(x.adapter),
        lite_unmet: yes(x.lite_unmet),
        release: isObj(x.release) ? {github: str(x.release.github), prerelease: yes(x.release.prerelease)} : undefined,
        requires: isObj(x.requires) ? {architectures: strs(x.requires.architectures), lite: str(x.requires.lite)} : undefined,
        ui: isObj(x.ui) ? {own_updater: yes(x.ui.own_updater)} : undefined,
        runtime: isObj(x.runtime) ? {...x.runtime, note: asText(x.runtime.note)} : undefined,
        images,
        tag: str(x.tag),
        fetched: str(x.fetched),
        error: str(x.error),
        stars: num(x.stars),
        latest,
        update_available: yes(x.update_available),
        release_notes: str(x.release_notes),
        install_mode: x.install_mode === 'root' || x.install_mode === 'confined' ? x.install_mode : undefined,
    };
}

/**
 * One installed addon, the fields the page reads outside its card in their types; a reason when
 * it cannot be a card. The rest stays as the system sent it - a card that trips over it shows its
 * own notice.
 */
export function readAddon(x: unknown): Addon | BrokenWhy {
    if (!isObj(x)) return 'not-object';
    if (typeof x.id !== 'string' || x.id === '') return 'no-id';
    return {
        ...(x as unknown as Addon),
        name: str(x.name) ?? '',
        version: str(x.version) ?? '',
        operations: strs(x.operations) ?? [],
        payload_missing_dirs: strs(x.payload_missing_dirs),
        update: str(x.update),
    };
}

// the list is derived again on every change; the console names a broken entry once
const logged = new Set<string>();
function logOnce(msg: string, err?: unknown): void {
    if (logged.has(msg)) return;
    logged.add(msg);
    if (err === undefined) console.error(msg);
    else console.error(msg, err);
}

function readAll<T>(list: unknown, read: (x: unknown) => T | BrokenWhy, prefix: string): {ok: T[]; broken: Broken[]} {
    const ok: T[] = [];
    const broken: Broken[] = [];
    if (list === undefined || list === null) return {ok, broken};
    if (!Array.isArray(list)) {
        broken.push({id: prefix, reason: 'not-a-list'});
        return {ok, broken};
    }
    list.forEach((x, i) => {
        let r: T | BrokenWhy;
        try {
            r = read(x);
        } catch (err) {
            r = (err as Error)?.message ?? String(err);
        }
        if (typeof r === 'string') broken.push({id: idOf(x, `${prefix}-${i + 1}`), reason: r});
        else ok.push(r);
    });
    return {ok, broken};
}

/** The catalogue's entries the page can use, and the ones it cannot. */
export const readEntries = (list: unknown) => readAll(list, readEntry, 'catalogue-entry');
/**
 * The installed addons the page can use, and the ones it cannot. An id twice is one addon - the
 * first - since every list of them is keyed by the id (a key twice blanks a Svelte list).
 */
export function readAddons(list: unknown): {ok: Addon[]; broken: Broken[]} {
    const r = readAll(list, readAddon, 'installed-addon');
    const seen = new Set<string>();
    return {ok: r.ok.filter((a) => !seen.has(a.id) && !!seen.add(a.id)), broken: r.broken};
}

/**
 * The cards: every catalogue entry, every installed addon the catalogue does not know, and a
 * broken card for each entry that could not be read. Installed first - those with an update at
 * the top, then by name - then the rest by stars; the broken ones last. One entry that throws
 * while it becomes a card becomes a broken card itself.
 */
export function buildCards(entries: Entry[], addons: Addon[], broken: Broken[], lang: string): Card[] {
    const byId = new Map(addons.map((a) => [a.id, a]));
    const out: Card[] = [];
    const seen = new Set<string>();
    const bad: Card[] = [];
    // a broken card's id is its entry's, made unique: it is the key of the page's list
    const fail = (base: string, reason: string) => {
        let id = base;
        for (let n = 2; seen.has(id); n++) id = `${base}-${n}`;
        seen.add(id);
        bad.push({id, name: base, entry: null, addon: null, update: '', broken: reason});
    };
    for (const e of entries) {
        let id = '';
        try {
            const a = e.id ? byId.get(e.id) ?? null : null;
            id = cardId(e);
            // B-52: two entries whose manifests name one id would be one key twice - the list would
            // not render at all; the first entry wins, as the system's merge does for a repository
            if (seen.has(id)) continue;
            const card: Card = {id, name: textIn(e.name, lang) || e.id || repoName(e), entry: e, addon: a, update: a && e.update_available && e.latest ? e.latest.version : ''};
            seen.add(id);
            if (a) seen.add(a.id);
            out.push(card);
        } catch (err) {
            logOnce(`addons: ${id || e.id || 'a catalogue entry'} could not become a card`, err);
            fail(id || e.id || 'catalogue-entry', (err as Error)?.message ?? String(err));
        }
    }
    for (const a of addons) {
        // an id twice in the installed list is one card, as two would be one key twice
        if (seen.has(a.id) || seen.has(a.id.replace(/^hm-/, ''))) continue;
        seen.add(a.id);
        out.push({id: a.id, name: a.name || a.id, entry: null, addon: a, update: ''});
    }
    for (const b of broken) {
        logOnce(`addons: ${b.id} could not be read: ${b.reason}`);
        fail(b.id, b.reason);
    }
    const rank = (c: Card) => (c.addon ? (c.update ? 0 : 1) : 2);
    const sorted = out.sort((x, y) => {
        try {
            const ix = rank(x);
            const iy = rank(y);
            if (ix !== iy) return ix - iy;
            if (ix < 2) return x.name.localeCompare(y.name, undefined, {sensitivity: 'base'}) || x.id.localeCompare(y.id);
            return compareEntries({id: x.id, name: x.name, stars: x.entry?.stars, git: x.entry?.git}, {id: y.id, name: y.name, stars: y.entry?.stars, git: y.entry?.git});
        } catch {
            return x.id < y.id ? -1 : x.id > y.id ? 1 : 0;
        }
    });
    return [...sorted, ...bad];
}
