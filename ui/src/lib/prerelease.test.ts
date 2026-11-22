import {describe, expect, it} from 'vitest';
import {prereleaseKind, prereleaseLabel} from './prerelease';

describe('prereleaseKind (occulited task 171)', () => {
    it.each([
        ['1.0.0-beta.0', 'beta'],
        ['v2.0.0-rc.1', 'rc'],
        ['3.0.0-rc1', 'rc'],
        ['3.0.0-RC.2', 'rc'],
        ['1.0.0-alpha', 'alpha'],
        ['0.1-beta2', 'beta'],
        ['1.1.0-dev.6', 'other'],
        ['1.0.0-preview.3', 'other'],
        ['1.0.0-rc.1+3', 'rc'],
        [' v1.0.0-beta.1 ', 'beta'],
        ['2.1.2', ''],
        ['v9.19.0', ''],
        ['2.1.2+1', ''],
        ['2.1.4+0-x', ''],
        ['6.3', ''],
        ['2026-05-01', ''],
        ['abc-def', ''],
        ['1.0.0-', ''],
        ['', ''],
        [undefined, ''],
    ] as const)('%s → %s', (v, want) => {
        expect(prereleaseKind(v)).toBe(want);
    });
    it('a version GitHub flags is a pre-release without a prerelease part too', () => {
        expect(prereleaseKind('1.0.0', true)).toBe('other');
        expect(prereleaseKind('1.0.0-beta.0', true)).toBe('beta');
        expect(prereleaseKind(undefined, true)).toBe('other');
    });
});

describe('prereleaseLabel', () => {
    it('names the kind, the generic word for the rest', () => {
        expect(prereleaseLabel('alpha', 'Vorabversion')).toBe('Alpha');
        expect(prereleaseLabel('beta', 'Vorabversion')).toBe('Beta');
        expect(prereleaseLabel('rc', 'Vorabversion')).toBe('RC');
        expect(prereleaseLabel('other', 'Vorabversion')).toBe('Vorabversion');
        expect(prereleaseLabel('', 'Vorabversion')).toBe('');
    });
});
