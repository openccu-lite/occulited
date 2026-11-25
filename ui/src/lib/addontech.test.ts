import {describe, expect, it} from 'vitest';

import {derivedFacts, factText, technicalText} from './addontech';
// the catalogue as text, as installrefusal.test.ts reads it: the i18n module itself needs the Svelte runtime
import catalogue from './i18n.svelte.ts?raw';

// English: the key with its values filled in
const en = (key: string, params?: Record<string, string | number>) => key.replace(/\{(\w+)\}/g, (_, k: string) => String(params?.[k] ?? `{${k}}`));

// occulited task 188: what the system derives from a runtime block
const rt = {
    ports: [1883, 8883, 51800, 1883, 70000],
    port_info: {'8883': {proto: 'tcp', tls: true, label: {de: 'MQTT über TLS', en: 'MQTT over TLS'}}, '51800': {to: 51899, proto: 'udp'}},
    capabilities: ['CAP_NET_RAW'], groups: ['dialout', 3], paths: [], api_scopes: ['meta:read', 'rpc:read'], read_addon_data: true,
};

describe('derivedFacts', () => {
    it('reads the mode, each declared port once, the lists and the read right', () => {
        const f = derivedFacts(rt, 'confined');
        expect(f.map((x) => x.kind)).toEqual(['mode', 'port', 'port', 'port', 'capabilities', 'groups', 'scopes', 'read-data']);
        expect(f[2]).toEqual({kind: 'port', port: 8883, to: undefined, proto: 'tcp', tls: true, label: {de: 'MQTT über TLS', en: 'MQTT over TLS'}, open: undefined});
        expect(f[3]).toMatchObject({port: 51800, to: 51899, proto: 'udp'});
        expect(f[5]).toEqual({kind: 'groups', list: ['dialout']});
    });
    it('says the firewall state of an installed addon, nothing before an install', () => {
        const f = derivedFacts(rt, 'root', {1883: true});
        expect(f.filter((x) => x.kind === 'port').map((x) => (x as {open?: boolean}).open)).toEqual([true, false, false]);
        expect(derivedFacts(undefined, undefined)).toEqual([]);
        expect(derivedFacts('nope', 'other')).toEqual([]);
    });
});

describe('the lines', () => {
    it('in English', () => {
        const f = derivedFacts(rt, 'confined');
        expect(factText(f[0]!, 'en', en)).toBe('Runs as its own user (confined)');
        expect(factText(f[1]!, 'en', en)).toBe('Port 1883/tcp: closed until opened');
        expect(factText(f[2]!, 'en', en)).toBe('Port 8883/tcp · TLS · MQTT over TLS: closed until opened');
        expect(factText(f[3]!, 'en', en)).toBe('Port 51800-51899/udp: closed until opened');
        expect(technicalText('Reads nothing.', f.slice(0, 2), 'en', en)).toBe('Reads nothing.\n• Runs as its own user (confined)\n• Port 1883/tcp: closed until opened');
        expect(technicalText('', [], 'en', en)).toBe('');
    });
});

describe('every phrase is in the catalogue (de)', () => {
    it('has each key', () => {
        for (const key of ['Runs as root', 'Runs as its own user (confined)', 'closed until opened', 'Port {port}: {state}', 'Capabilities: {list}', 'Groups: {list}', 'Writable paths: {list}', 'API token: {scopes}', "can read other addons\' data", 'open', 'closed']) {
            expect(catalogue.includes(`'${key.replace(/'/g, "\\'")}':`) || catalogue.includes(`    ${key}:`), key).toBe(true);
        }
    });
});
