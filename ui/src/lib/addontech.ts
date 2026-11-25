/*
 * The technical account of an addon (occulited task 188, D-90): what its manifest's
 * `runtime.technical` says in words, and what the system derives from the rest of the runtime
 * block itself - how it runs, its ports with their state in the firewall, capabilities, groups,
 * writable paths, the API token's scopes. Shown behind the card's Details and in the install
 * question; without `technical` only the derived part. `addontech.test.ts` covers it.
 */
// Pure, without the i18n state (a unit test loads it): the lines are the catalogue's phrases and
// their values, translated by the caller with t() (as installrefusal.ts does).
import type {Text} from './addoncards';

/** one phrase of the catalogue (i18n.svelte.ts) and its values */
export type Phrase = [key: string, params?: Record<string, string | number>];
/** the page's t() */
export type Translate = (key: string, params?: Record<string, string | number>) => string;

export type Fact =
    | {kind: 'mode'; mode: 'root' | 'confined'}
    | {kind: 'port'; port: number; to?: number; proto: string; tls: boolean; label?: Text; open?: boolean}
    | {kind: 'capabilities' | 'groups' | 'paths' | 'scopes'; list: string[]}
    | {kind: 'read-data'};

type Obj = Record<string, unknown>;
const isObj = (x: unknown): x is Obj => typeof x === 'object' && x !== null && !Array.isArray(x);
const strs = (x: unknown): string[] => (Array.isArray(x) ? x.filter((s): s is string => typeof s === 'string' && s !== '') : []);
const label = (x: unknown): Text | undefined => {
    if (typeof x === 'string') return x ? {en: x} : undefined;
    if (!isObj(x)) return undefined;
    const o: Text = {};
    if (typeof x.de === 'string') o.de = x.de;
    if (typeof x.en === 'string') o.en = x.en;
    return o.de !== undefined || o.en !== undefined ? o : undefined;
};

/**
 * The facts of a runtime block. `mode` is how the system runs it (the API's install_mode, or an
 * installed addon's policy); `open` the firewall's word per declared port of an installed addon -
 * absent before an install, when every declared port is closed until the user opens it (D-29).
 */
export function derivedFacts(runtime: unknown, mode: string | undefined, open?: Record<number, boolean>): Fact[] {
    const rt = isObj(runtime) ? runtime : {};
    const out: Fact[] = [];
    if (mode === 'root' || mode === 'confined') out.push({kind: 'mode', mode});
    const info = isObj(rt.port_info) ? rt.port_info : {};
    const ports = Array.isArray(rt.ports) ? rt.ports.filter((p): p is number => Number.isInteger(p) && p > 0 && p < 65536) : [];
    for (const port of [...new Set(ports)]) {
        const pi = isObj(info[String(port)]) ? (info[String(port)] as Obj) : {};
        const to = Number.isInteger(pi.to) && (pi.to as number) > port ? (pi.to as number) : undefined;
        out.push({kind: 'port', port, to, proto: pi.proto === 'udp' ? 'udp' : 'tcp', tls: pi.tls === true, label: label(pi.label), open: open ? open[port] === true : undefined});
    }
    for (const [kind, key] of [['capabilities', 'capabilities'], ['groups', 'groups'], ['paths', 'paths'], ['scopes', 'api_scopes']] as const) {
        const list = strs(rt[key]);
        if (list.length) out.push({kind, list});
    }
    if (rt.read_addon_data === true) out.push({kind: 'read-data'});
    return out;
}

const textIn = (x: Text | undefined, lang: string): string => (lang === 'de' ? (x?.de ?? x?.en) : (x?.en ?? x?.de)) ?? '';

/** One fact as a phrase; the port's state is a phrase of its own, translated first. */
export function factPhrase(f: Fact, lang: string, tr: Translate): Phrase {
    switch (f.kind) {
        case 'mode':
            return [f.mode === 'root' ? 'Runs as root' : 'Runs as its own user (confined)'];
        case 'port': {
            const range = f.to ? `${f.port}-${f.to}` : String(f.port);
            const what = [`${range}/${f.proto}`, f.tls ? 'TLS' : '', textIn(f.label, lang)].filter(Boolean).join(' · ');
            const state = f.open === undefined ? tr('closed until opened') : f.open ? tr('open') : tr('closed');
            return ['Port {port}: {state}', {port: what, state}];
        }
        case 'capabilities':
            return ['Capabilities: {list}', {list: f.list.join(', ')}];
        case 'groups':
            return ['Groups: {list}', {list: f.list.join(', ')}];
        case 'paths':
            return ['Writable paths: {list}', {list: f.list.join(', ')}];
        case 'scopes':
            return ['API token: {scopes}', {scopes: f.list.join(', ')}];
        case 'read-data':
            return ["can read other addons' data"];
    }
}

/** One fact as a line in the user's language. */
export const factText = (f: Fact, lang: string, tr: Translate): string => tr(...factPhrase(f, lang, tr));

/** The account as plain text for the install question: the addon's words, then the derived lines. */
export function technicalText(technical: string, facts: Fact[], lang: string, tr: Translate): string {
    const lines = facts.map((f) => `• ${factText(f, lang, tr)}`);
    return [technical, lines.join('\n')].filter(Boolean).join('\n');
}
