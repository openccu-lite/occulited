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

const rank: Record<string, number> = {dev: 1, alpha: 2, beta: 3, rc: 4};
const digitsRe = /^\d+$/;
const dottedRe = /^\d+(\.\d+)*$/;

function compareIdent(x: string, y: string): number {
    const xd = digitsRe.test(x);
    const yd = digitsRe.test(y);
    if (xd && yd) {
        const a = x.replace(/^0+/, '');
        const b = y.replace(/^0+/, '');
        return a.length !== b.length ? Math.sign(a.length - b.length) : a < b ? -1 : a > b ? 1 : 0;
    }
    if (xd) return -1;
    if (yd) return 1;
    const r = (rank[x] ?? 0) - (rank[y] ?? 0);
    if (r) return Math.sign(r);
    return x < y ? -1 : x > y ? 1 : 0;
}

function comparePre(a: string, b: string): number {
    const as = a.split('.');
    const bs = b.split('.');
    for (let i = 0; i < as.length && i < bs.length; i++) {
        const c = compareIdent(as[i] ?? '', bs[i] ?? '');
        if (c) return c;
    }
    return Math.sign(as.length - bs.length);
}

function compareDotted(a: string, b: string): number {
    const as = a.split('.');
    const bs = b.split('.');
    for (let i = 0; i < as.length || i < bs.length; i++) {
        const x = Number(as[i] ?? 0);
        const y = Number(bs[i] ?? 0);
        if (x !== y) return x > y ? 1 : -1;
    }
    return 0;
}

/**
 * occulited B-181: orders two versions as the system's catalog.compareVersions does - a leading
 * "v" ignored, the dotted core numerically, a prerelease older than the release it precedes, two
 * prereleases of one core by their identifiers (numbers as numbers, dev < alpha < beta < rc), a
 * "+build" only between otherwise equal versions. `null` when the two cannot be ordered (empty, a
 * date, a git hash, two builds that are no numbers).
 */
export function compareVersions(a: string | undefined, b: string | undefined): number | null {
    const norm = (v: string | undefined) => (v ?? '').trim().replace(/^v/, '');
    const [av, abuild = ''] = norm(a).split(/\+(.*)/s);
    const [bv, bbuild = ''] = norm(b).split(/\+(.*)/s);
    if (!av || !bv) return null;
    const cut = (v: string): [string, string] => {
        const i = v.indexOf('-');
        return i < 0 ? [v, ''] : [v.slice(0, i), v.slice(i + 1)];
    };
    const [ac, apre] = cut(av);
    const [bc, bpre] = cut(bv);
    if (!dottedRe.test(ac) || !dottedRe.test(bc)) return null;
    const c = compareDotted(ac, bc);
    if (c) return c;
    if (apre !== bpre) {
        if (!apre) return 1;
        if (!bpre) return -1;
        const p = comparePre(apre, bpre);
        if (p) return p;
    }
    if (abuild === bbuild) return 0;
    if (!bbuild) return 1;
    if (!abuild) return -1;
    if (dottedRe.test(abuild) && dottedRe.test(bbuild)) return compareDotted(abuild, bbuild);
    return null;
}
