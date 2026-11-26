import {describe, expect, it} from 'vitest';
import {reinstallKind, reinstallPlan} from './reinstall';
import type {Phrase} from './installrefusal';
// the catalogue as text: the i18n module itself needs the Svelte runtime
import catalogue from './i18n.svelte.ts?raw';

const fill = ([key, params]: Phrase) => key.replace(/\{(\w+)\}/g, (_, k: string) => params?.[k] ?? `{${k}}`);
const english = (paras: Phrase[][]) => paras.map((p) => p.map(fill).join('\n')).join('\n\n');
/** the German of a phrase, from the catalogue's text */
const german = ([key, params]: Phrase) => {
    const esc = key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const m = new RegExp(`^\\s{4}(?:'${esc.replace(/'/g, "\\\\'")}'|"${esc}"|${esc}): \\{de: '((?:[^'\\\\]|\\\\.)*)'\\}`, 'm').exec(catalogue);
    if (!m) throw new Error(`no German for ${key}`);
    return fill([m[1]!.replace(/\\'/g, "'"), params]);
};

// occulited B-181: "Reinstall from the catalogue" replaced RedMatic 10.0.0-beta.0 with 9.20.0 after a
// question that named no version
describe('reinstallPlan (occulited B-181)', () => {
    const red = {name: 'RedMatic', latest: {version: '9.20.0'}};

    it('knows a downgrade by version order, pre-releases included', () => {
        expect(reinstallKind('9.20.0', '10.0.0-beta.0')).toBe('downgrade');
        expect(reinstallKind('10.0.0', '10.0.0-beta.0')).toBe('same-or-newer');
        expect(reinstallKind('10.0.0-beta.0', '10.0.0')).toBe('downgrade');
        expect(reinstallKind('9.20.0', '9.20.0')).toBe('same-or-newer');
        expect(reinstallKind('9.20.0', ' ')).toBe('unknown');
        expect(reinstallKind('9.20.0', 'abc')).toBe('same-or-newer'); // cannot be ordered: no claim
    });

    it('a downgrade: both versions, the warning, Keep as the default and Downgrade in the warning colour', () => {
        const p = reinstallPlan(red, '10.0.0-beta.0');
        expect(english(p.paragraphs)).toBe(
            'Reinstall RedMatic from the catalogue? The addon replaces its own installation; its configuration is kept.\n\n' +
                'Installed: 10.0.0-beta.0\nFrom the catalogue: 9.20.0\n\n' +
                "9.20.0 is older than the installed 10.0.0-beta.0: this reinstall is a downgrade, and an older version may not read what the newer one stored. The catalogue offers no newer version; a newer one comes through the addon's own update or an upload of its package.",
        );
        expect(fill(p.confirm)).toBe('Downgrade to 9.20.0');
        expect(fill(p.cancel!)).toBe('Keep 10.0.0-beta.0');
        expect(p.danger).toBe(true);
    });

    it('the same version: both named, an ordinary Reinstall', () => {
        const p = reinstallPlan(red, '9.20.0');
        expect(english(p.paragraphs)).toContain('Installed: 9.20.0\nFrom the catalogue: 9.20.0');
        expect(english(p.paragraphs)).not.toContain('downgrade');
        expect(p).toEqual({paragraphs: p.paragraphs, confirm: ['Reinstall']});
    });

    it('an unknown installed version says it may be a downgrade', () => {
        const text = english(reinstallPlan(red, undefined).paragraphs);
        expect(text).toContain('Installed: unknown version\nFrom the catalogue: 9.20.0');
        expect(text).toContain('The installed version is not known; if it is newer than 9.20.0, this reinstall is a downgrade.');
    });

    it('a pre-release from the catalogue is said', () => {
        expect(english(reinstallPlan({name: 'Logic', latest: {version: '1.0.0-beta.4'}}, '1.0.0-beta.4').paragraphs)).toContain('1.0.0-beta.4 is a pre-release: not all functions have been tested yet.');
    });

    it('the restore section keeps its own first sentence', () => {
        expect(english(reinstallPlan(red, '9.20.0', ['Lead {x}.', {x: '1'}]).paragraphs).startsWith('Lead 1.\n\nInstalled: 9.20.0')).toBe(true);
    });

    it('every phrase is in German', () => {
        const all = [reinstallPlan(red, '10.0.0-beta.0'), reinstallPlan(red, ''), reinstallPlan({name: 'L', latest: {version: '1.0.0-rc.1'}}, '1.0.0-rc.1')];
        const phrases = all.flatMap((p) => [...p.paragraphs.flat(), p.confirm, ...(p.cancel ? [p.cancel] : [])]);
        for (const ph of phrases) expect(german(ph), ph[0]).not.toBe('');
        const p = reinstallPlan(red, '10.0.0-beta.0');
        expect(p.paragraphs.map((x) => x.map(german).join('\n')).join('\n\n')).toBe(
            'RedMatic aus dem Katalog neu installieren? Das Addon ersetzt seine eigene Installation; seine Konfiguration bleibt erhalten.\n\n' +
                'Installiert: 10.0.0-beta.0\nAus dem Katalog: 9.20.0\n\n' +
                '9.20.0 ist älter als die installierte 10.0.0-beta.0: Diese Neuinstallation ist ein Downgrade, und eine ältere Version kann womöglich nicht lesen, was die neuere gespeichert hat. Der Katalog bietet keine neuere Version an; eine neuere kommt über das eigene Update des Addons oder das Hochladen seines Pakets.',
        );
        expect(german(p.confirm)).toBe('Auf 9.20.0 zurückgehen');
        expect(german(p.cancel!)).toBe('10.0.0-beta.0 behalten');
        expect(german(['Installed: unknown version'])).toBe('Installiert: Version unbekannt');
    });
});
