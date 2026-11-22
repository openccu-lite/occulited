/*
 * occulited task 171: a pre-release on the Addons page. A version with a SemVer prerelease part
 * (1.0.0-beta.0, v2.0.0-rc.1, 3.0.0-rc1) is one - the same rule as the system's
 * catalog.IsPrereleaseVersion -, and so is a release GitHub flags as one (`latest.prerelease`).
 * A version of major 0 (0.1.0, 0.9) is marked as a beta too: SemVer says nothing in it is stable yet.
 * The page marks it with an orange pill; this is which word the pill says.
 */

/** the pill's kind: '' for a stable version; `other` says the generic *Pre-release* */
export type PrereleaseKind = '' | 'alpha' | 'beta' | 'rc' | 'other';

const versionRe = /^\d+(\.\d+)+$/;

/**
 * The kind of a version's prerelease part, from its first identifier: `alpha…`, `beta…`, `rc…`
 * (case ignored, so `RC1` and `beta2` count), anything else - `dev.31`, `preview`, `snapshot` -
 * `other`. Build metadata (`2.1.2+1`) is no prerelease, nor is anything that is not a dotted
 * number of two parts or more before the `-` (a date, a git hash). `flagged` (GitHub's mark on a
 * version without a prerelease part) makes a stable-looking version `other`; a major-0 version
 * without a prerelease part is `beta`.
 */
export function prereleaseKind(version: string | undefined, flagged = false): PrereleaseKind {
    const v = (version ?? '').trim().replace(/^v/, '').split('+')[0] ?? '';
    const dash = v.indexOf('-');
    if (dash > 0 && versionRe.test(v.slice(0, dash)) && dash < v.length - 1) {
        const first = (v.slice(dash + 1).split('.')[0] ?? '').toLowerCase();
        for (const k of ['alpha', 'beta', 'rc'] as const) if (first.startsWith(k)) return k;
        return 'other';
    }
    // major version 0 (0.1.0): not declared stable yet, so a beta too (the maintainer, 2026-11-22)
    if (versionRe.test(v) && /^0\./.test(v)) return 'beta';
    return flagged ? 'other' : '';
}

/** the pill's text; `other` is the caller's translated *Pre-release* */
export function prereleaseLabel(kind: PrereleaseKind, generic: string): string {
    switch (kind) {
        case 'alpha':
            return 'Alpha';
        case 'beta':
            return 'Beta';
        case 'rc':
            return 'RC';
        case 'other':
            return generic;
    }
    return '';
}
