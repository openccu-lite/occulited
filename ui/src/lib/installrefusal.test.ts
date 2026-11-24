import {describe, expect, it} from 'vitest';
import {refusalPhrases, type Phrase} from './installrefusal';
// the catalogue as text, as ids.test.ts reads it: the i18n module itself needs the Svelte runtime
import catalogue from './i18n.svelte.ts?raw';

/** the English text the phrases stand for: each key with its values filled in */
const english = (phrases: Phrase[]) => phrases.map(([key, params]) => key.replace(/\{(\w+)\}/g, (_, k: string) => params?.[k] ?? `{${k}}`)).join(' ');

// occulited task 134 and B-167: the system's refusals before a package's code runs
describe('refusalPhrases', () => {
    const refusals = [
        'hmm requires openccu-lite 1.1.0 or later; this system runs 1.0.0. Nothing was installed.',
        'This package of battery is for aarch64; this system is x86_64. Take the package for x86_64: battery-x86_64-1.5.5.tar.gz. Nothing was installed.',
        'This package of x is for armv7l, aarch64; this system is x86_64. The addon has no package for x86_64. Nothing was installed.',
        'This package is for x86_64; this system is aarch64. Take the package for aarch64, where the addon has one. Nothing was installed.',
    ];
    it('takes every refusal apart into phrases that say the same', () => {
        for (const message of refusals) {
            const phrases = refusalPhrases(message);
            expect(phrases, message).not.toBeNull();
            expect(english(phrases!)).toBe(message);
        }
    });
    it('uses only phrases the catalogue has in German', () => {
        for (const message of refusals) {
            for (const [key] of refusalPhrases(message)!) expect(catalogue, key).toContain(`'${key}': {de: '`);
        }
    });
    it('names the values', () => {
        expect(refusalPhrases(refusals[1]!)).toEqual([
            ['This package of {id} is for {package}; this system is {system}.', {id: 'battery', package: 'aarch64', system: 'x86_64'}],
            ['Take the package for {system}: {file}.', {system: 'x86_64', file: 'battery-x86_64-1.5.5.tar.gz'}],
            ['Nothing was installed.'],
        ]);
    });
    it('leaves any other message alone', () => {
        for (const message of ['archive could not be unpacked', 'This package is odd', 'This package is for a; this system is b. Something else. Nothing was installed.']) expect(refusalPhrases(message)).toBeNull();
    });
});
