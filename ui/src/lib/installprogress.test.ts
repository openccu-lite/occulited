import {describe, expect, it} from 'vitest';

import {DONE_GRACE_MS, FAILED_MAX_MS, runKey, shownAtOpen} from './installprogress';

// occulited task 165: which run that had ended before the page opened is still a notice
describe('shownAtOpen', () => {
    const now = Date.parse('2026-11-21T12:00:00Z');
    const run = (phase: string, agoMs: number | null) => ({
        addon_id: 'redmatic',
        phase,
        started: '2026-11-21T11:00:00Z',
        ...(agoMs === null ? {} : {finished: new Date(now - agoMs).toISOString()}),
    });
    it('shows a run that is still on', () => {
        expect(shownAtOpen(run('installing', null), now, '')).toBe(true);
    });
    it('shows a done run within a minute of its end, not later', () => {
        expect(shownAtOpen(run('done', 5000), now, '')).toBe(true);
        expect(shownAtOpen(run('done', DONE_GRACE_MS), now, '')).toBe(true);
        expect(shownAtOpen(run('done', DONE_GRACE_MS + 1), now, '')).toBe(false);
        expect(shownAtOpen(run('done', 30 * 24 * 3600_000), now, '')).toBe(false);
    });
    it('does not show a done run whose end lies ahead of this clock (the system clock is off)', () => {
        expect(shownAtOpen(run('done', -3600_000), now, '')).toBe(false);
    });
    it('shows a failed run until it is a day old', () => {
        expect(shownAtOpen(run('failed', 3 * 3600_000), now, '')).toBe(true);
        expect(shownAtOpen(run('failed', FAILED_MAX_MS), now, '')).toBe(true);
        expect(shownAtOpen(run('failed', FAILED_MAX_MS + 1), now, '')).toBe(false);
        expect(shownAtOpen(run('failed', -3600_000), now, '')).toBe(true);
    });
    it('does not show the run this browser closed, but a later one', () => {
        const r = run('failed', 1000);
        expect(shownAtOpen(r, now, runKey(r))).toBe(false);
        expect(shownAtOpen(r, now, 'redmatic@2026-11-20T09:00:00Z')).toBe(true);
    });
    it('takes an unreadable end as old', () => {
        expect(shownAtOpen({addon_id: 'x', phase: 'done', started: 's', finished: 'garbage'}, now, '')).toBe(false);
        expect(shownAtOpen({addon_id: 'x', phase: 'failed', started: 's', finished: 'garbage'}, now, '')).toBe(false);
    });
});

describe('runKey', () => {
    it('is the addon and its start', () => {
        expect(runKey({addon_id: 'mosquitto', phase: 'done', started: '2026-11-21T11:00:00Z'})).toBe('mosquitto@2026-11-21T11:00:00Z');
        expect(runKey(null)).toBe('');
    });
});
