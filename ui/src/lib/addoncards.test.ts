import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';

import type {Addon} from './api';
import {asText, buildCards, readAddon, readAddons, readEntries, readEntry, textIn, type Entry} from './addoncards';

// occulited B-157: one entry of the system's answers the page does not expect - wrong types,
// missing fields, unknown kinds - must not take the Addons page down. It becomes a card that says
// why, or its wrong fields are dropped; every other card stays.

const addon = (id: string, extra: Partial<Addon> = {}): Addon => ({id, name: id.toUpperCase(), version: '1.0', operations: [], running: true, enabled: true, ...extra}) as Addon;
const entry = (id: string, extra: Record<string, unknown> = {}): Entry => readEntry({git: `https://github.com/x/${id}`, manifest_path: 'addon.json', id, name: {en: id}, ...extra}) as Entry;

let errors: unknown[][] = [];
beforeEach(() => {
    errors = [];
    vi.spyOn(console, 'error').mockImplementation((...a: unknown[]) => void errors.push(a));
});
afterEach(() => vi.restoreAllMocks());

describe('asText / textIn', () => {
    it('keeps the strings of {de, en} and nothing else', () => {
        expect(asText({de: 'Hallo', en: 'Hello'})).toEqual({de: 'Hallo', en: 'Hello'});
        expect(asText({de: 5, en: 'Hello', fr: 'Bonjour'})).toEqual({en: 'Hello'});
        expect(asText({de: {x: 1}})).toBeUndefined();
        expect(asText([1, 2])).toBeUndefined();
        expect(asText(null)).toBeUndefined();
        expect(asText(7)).toBeUndefined();
    });
    it('takes a plain string as the English text', () => {
        expect(asText('Plain')).toEqual({en: 'Plain'});
        expect(asText('')).toBeUndefined();
    });
    it('answers in the language, else English, else German, else nothing', () => {
        expect(textIn({de: 'D', en: 'E'}, 'de')).toBe('D');
        expect(textIn({en: 'E'}, 'de')).toBe('E');
        expect(textIn({de: 'D'}, 'en')).toBe('D');
        expect(textIn(undefined, 'en')).toBe('');
        expect(textIn({de: 5} as never, 'de')).toBe('');
    });
});

describe('readEntry', () => {
    it('refuses what cannot be a card, with a reason', () => {
        expect(readEntry(null)).toBe('not-object');
        expect(readEntry('x')).toBe('not-object');
        expect(readEntry([1])).toBe('not-object');
        expect(readEntry({git: 'https://github.com/a/b', id: 5})).toBe('id-not-text');
        expect(readEntry({git: 'https://github.com/a/b', id: ''})).toBe('id-not-text');
        expect(readEntry({})).toBe('no-id-no-repo');
        expect(readEntry({git: 7})).toBe('no-id-no-repo');
    });
    it('drops the fields of the wrong type and keeps the rest', () => {
        const e = readEntry({
            git: 'https://github.com/a/b', id: 'b', manifest_path: 3, name: 'Bee', description: {de: 1, en: 'An addon'},
            stars: 'many', latest: {version: 2}, update_available: 'yes', requires: {architectures: 'arm64', lite: 1},
            release: 'x', ui: [], runtime: {note: {en: 'Note'}, other: 1}, images: {logo: '/a.png', icon: 5},
            licence: 4, homepage: {}, error: 3, release_notes: false, untested: 'true', unknown_kind: {deep: true},
        }) as Entry;
        expect(e.id).toBe('b');
        expect(e.manifest_path).toBe('');
        expect(e.name).toEqual({en: 'Bee'});
        expect(e.description).toEqual({en: 'An addon'});
        expect(e.stars).toBeUndefined();
        expect(e.latest).toBeUndefined();
        expect(e.update_available).toBeUndefined();
        expect(e.requires).toEqual({architectures: undefined, lite: undefined});
        expect(e.release).toBeUndefined();
        expect(e.ui).toBeUndefined();
        expect(e.runtime?.note).toEqual({en: 'Note'});
        expect(e.images).toEqual({logo: '/a.png'});
        expect(e.licence).toBeUndefined();
        expect(e.homepage).toBeUndefined();
        expect(e.error).toBeUndefined();
        expect(e.release_notes).toBeUndefined();
        expect(e.untested).toBeUndefined();
        // a field the page does not know is passed along untouched
        expect((e as unknown as Record<string, unknown>).unknown_kind).toEqual({deep: true});
    });
    it('keeps a well-formed entry as it is', () => {
        const raw = {git: 'https://github.com/a/b', manifest_path: 'addon.json', id: 'b', name: {de: 'B', en: 'B'}, stars: 4, latest: {version: '2.0', asset: 'b.tgz'}, update_available: true, requires: {architectures: ['arm64'], lite: '1.2.0'}};
        const e = readEntry(raw) as Entry;
        expect(e).toMatchObject({id: 'b', stars: 4, latest: {version: '2.0', asset: 'b.tgz'}, update_available: true, requires: {architectures: ['arm64'], lite: '1.2.0'}});
    });
    it('takes an entry known by its repository alone', () => {
        expect(readEntry({git: 'https://github.com/a/b'})).toMatchObject({git: 'https://github.com/a/b', id: undefined});
    });
});

describe('readAddon', () => {
    it('refuses an addon without an id', () => {
        expect(readAddon(undefined)).toBe('not-object');
        expect(readAddon({name: 'x'})).toBe('no-id');
        expect(readAddon({id: 4})).toBe('no-id');
    });
    it('coerces what the page reads outside the card', () => {
        const a = readAddon({id: 'x', name: 5, version: null, operations: 'start', payload_missing_dirs: 'www', update: 1}) as Addon;
        expect(a).toMatchObject({id: 'x', name: '', version: '', operations: [], payload_missing_dirs: undefined, update: undefined});
        expect((readAddon({id: 'y', operations: ['start', 3], payload_missing_dirs: ['www', null]}) as Addon)).toMatchObject({operations: ['start'], payload_missing_dirs: ['www']});
    });
});

describe('readEntries / readAddons', () => {
    it('splits the usable entries from the broken ones, naming each broken one', () => {
        const r = readEntries([{git: 'https://github.com/a/good', id: 'good'}, null, {id: 7, git: 'https://github.com/a/seven'}, {git: 'https://github.com/a/repo-only'}, 'text']);
        expect(r.ok.map((e) => e.id ?? e.git)).toEqual(['good', 'https://github.com/a/repo-only']);
        expect(r.broken).toEqual([
            {id: 'catalogue-entry-2', reason: 'not-object'},
            {id: 'repo-a-seven', reason: 'id-not-text'},
            {id: 'catalogue-entry-5', reason: 'not-object'},
        ]);
    });
    it('a list that is no list is one broken card; none at all is nothing', () => {
        expect(readEntries({a: 1})).toEqual({ok: [], broken: [{id: 'catalogue-entry', reason: 'not-a-list'}]});
        expect(readAddons(undefined)).toEqual({ok: [], broken: []});
        expect(readAddons(null)).toEqual({ok: [], broken: []});
    });
    it('names an installed addon by its id when only its other fields are wrong', () => {
        const r = readAddons([{id: 'ok'}, {name: 'nameless'}, 3]);
        expect(r.ok.map((a) => a.id)).toEqual(['ok']);
        expect(r.broken).toEqual([{id: 'installed-addon-2', reason: 'no-id'}, {id: 'installed-addon-3', reason: 'not-object'}]);
    });
    it('takes an installed id twice as one addon, the first', () => {
        expect(readAddons([{id: 'a', name: 'first'}, {id: 'b'}, {id: 'a', name: 'second'}]).ok.map((a) => [a.id, a.name])).toEqual([['a', 'first'], ['b', '']]);
    });
});

describe('buildCards', () => {
    it('orders installed with an update, installed, the rest by stars, the broken last', () => {
        const cards = buildCards(
            [entry('cat', {stars: 3}), entry('top', {stars: 9}), entry('upd', {update_available: true, latest: {version: '2.0'}})],
            [addon('upd'), addon('mine')],
            [{id: 'catalogue-entry-4', reason: 'not-object'}],
            'en',
        );
        expect(cards.map((c) => [c.id, c.update, c.broken ?? ''])).toEqual([
            ['upd', '2.0', ''],
            ['mine', '', ''],
            ['top', '', ''],
            ['cat', '', ''],
            ['catalogue-entry-4', '', 'not-object'],
        ]);
        expect(errors.map((e) => e[0])).toEqual(['addons: catalogue-entry-4 could not be read: not-object']);
    });
    it('gives every card its own key: an id twice in the installed list, a broken id that a card holds', () => {
        const cards = buildCards([entry('a')], [addon('b'), addon('b'), addon('hm-a')], [{id: 'a', reason: 'id-not-text'}, {id: 'a', reason: 'id-not-text'}], 'en');
        const ids = cards.map((c) => c.id);
        expect(ids).toEqual(['b', 'a', 'a-2', 'a-3']);
        expect(new Set(ids).size).toBe(ids.length);
    });
    it('names a card by the language, its id, else its repository', () => {
        const repoOnly = readEntry({git: 'https://github.com/o/r/'}) as Entry;
        const cards = buildCards([entry('n', {name: {de: 'Deutsch', en: 'English'}}), entry('m', {name: 5}), repoOnly], [], [], 'de');
        expect(cards.map((c) => [c.id, c.name])).toEqual([['n', 'Deutsch'], ['m', 'm'], ['repo-o-r', 'o/r']]);
    });
    it('an entry that throws while it becomes a card is a broken card; the others stay', () => {
        const bad = entry('bad');
        Object.defineProperty(bad, 'name', {get: () => { throw new Error('boom'); }});
        const cards = buildCards([entry('good'), bad], [], [], 'en');
        expect(cards.map((c) => [c.id, c.broken ?? ''])).toEqual([['good', ''], ['bad', 'boom']]);
        expect(String(errors[0]?.[0])).toContain('addons: bad could not become a card');
    });
    it('logs a broken entry once, however often the list is built', () => {
        const b = [{id: 'once', reason: 'no-id' as const}];
        buildCards([], [], b, 'en');
        buildCards([], [], b, 'en');
        expect(errors.filter((e) => String(e[0]).includes('once'))).toHaveLength(1);
    });
});
