import {describe, expect, it} from 'vitest';
import {compareVersions, prereleaseKind, prereleaseLabel} from './prerelease';

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
        ['0.1.0', 'beta'], // major 0: not stable yet
        ['v0.2.0', 'beta'],
        ['0.9', 'beta'],
        ['0.3.0+1', 'beta'],
        ['0.1.0-dev.28', 'other'], // the prerelease part decides
        ['0.9.0-rc.1', 'rc'],
        ['10.1.0', ''],
        ['0', ''],
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
        expect(prereleaseKind('0.1.0', true)).toBe('beta');
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

describe('compareVersions (occulited B-181)', () => {
    it.each([
        ['10.0.0-beta.0', '9.20.0', 1],
        ['9.20.0', '10.0.0-beta.0', -1],
        ['3.6.0-beta', '3.6.0', -1],
        ['3.6.0', '3.6.0-beta', 1],
        ['3.0.0-dev.30', '3.0.0-beta.9', -1],
        ['3.0.0-beta.16', '3.0.0-beta.9', 1],
        ['3.0.0-rc.1', '3.0.0-beta.16', 1],
        ['1.10', '1.9', 1],
        ['v1.2', '1.2.0', 0],
        ['2.1.2+3', '2.1.2+2', 1],
        ['2.1.2', '2.1.2+2', -1],
        ['9.20.0', '9.20.0', 0],
    ] as const)('%s vs %s → %d', (a, b, want) => {
        expect(compareVersions(a, b)).toBe(want);
    });
    it.each([
        ['', '1.0'],
        ['1.0', undefined],
        ['nightly', '1.0'],
        ['abc123', 'def456'],
        ['2.1.2+a', '2.1.2+b'],
    ] as const)('%s vs %s cannot be ordered', (a, b) => {
        expect(compareVersions(a, b)).toBeNull();
    });
});
