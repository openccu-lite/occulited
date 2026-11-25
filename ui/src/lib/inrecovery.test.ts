import {describe, expect, it} from 'vitest';
import {NOT_IN_RECOVERY, trackRecovery, type RecoveryTrack} from './recoverystatus';

// openccu-lite task 505: the shell's "in the recovery system" state, answer by answer
// (lib/inrecovery.svelte.ts applies it; its polling is covered by e2e/recovery-mode.spec.ts)
const REC = (update?: unknown) => ({error: 'updating', recovery: true, ...(update ? {update} : {})});
const step = (s: number, m: number, phase = m === 7 ? 'prepare' : 'install') => REC({running: true, phase, step: s, steps: m});

function run(answers: [number | null, unknown, number][]): RecoveryTrack {
    let t = NOT_IN_RECOVERY;
    for (const [status, body, now] of answers) t = trackRecovery(t, status, body, now);
    return t;
}

describe('in the recovery system', () => {
    it("the recovery's 503 sets it, with the update object; an older recovery's without", () => {
        const t = run([[503, step(5, 7), 1000]]);
        expect(t).toMatchObject({active: true, update: {running: true, phase: 'prepare', step: 5, steps: 7}, since: 1000, stepSince: 1000, lastSeen: 1000});
        expect(run([[503, {error: 'updating', message: 'x'}, 2000]])).toMatchObject({active: true, update: null});
    });

    it('other answers do not set it', () => {
        expect(run([[503, {error: 'starting'}, 1], [null, null, 2], [200, {uptime_s: 3}, 3], [404, null, 4]])).toBe(NOT_IN_RECOVERY);
    });

    it("the system answering ends it - occulited's 200, its lighttpd's 503, a login wall - and no answer does not", () => {
        const on = run([[503, REC(), 1]]);
        expect(trackRecovery(on, null, null, 2)).toBe(on);
        expect(trackRecovery(on, 404, null, 2)).toBe(on);
        expect(trackRecovery(on, 503, {error: 'starting'}, 3).active).toBe(false);
        expect(trackRecovery(on, 200, {uptime_s: 2}, 3).active).toBe(false);
        expect(trackRecovery(on, 401, {error: 'unauthenticated'}, 3).active).toBe(false);
    });

    it('a new step restarts the step clock; the same step keeps it', () => {
        let t = run([[503, step(5, 7), 10_000], [503, step(5, 7), 70_000]]);
        expect(t.stepSince).toBe(10_000);
        expect(t.lastSeen).toBe(70_000);
        t = trackRecovery(t, 503, step(6, 7), 80_000);
        expect(t.stepSince).toBe(80_000);
    });

    it('the shown fraction never falls, not even for an answer that says less', () => {
        let t = run([[503, step(5, 7), 0], [503, step(5, 7), 100_000]]);
        const crept = t.shown;
        expect(crept).toBeGreaterThan(0);
        t = trackRecovery(t, 503, step(2, 7), 101_000);
        expect(t.shown).toBe(crept);
        t = trackRecovery(t, 503, step(6, 7), 102_000);
        expect(t.shown).toBeGreaterThan(crept);
    });

    it('a new recovery after the old one ended starts from nothing', () => {
        let t = run([[503, step(6, 7), 0], [200, {uptime_s: 3}, 10]]);
        expect(t).toBe(NOT_IN_RECOVERY);
        t = trackRecovery(t, 503, step(1, 7), 20);
        expect(t).toMatchObject({since: 20, shown: 0});
    });
});
