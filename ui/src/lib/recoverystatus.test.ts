import {describe, expect, it} from 'vitest';
import {CREEP_CAP, installView, isLogAnswer, logText, monotonic, parseUpdate, recoveryOf, stepId} from './recoverystatus';

const u = (x: Record<string, unknown>) => parseUpdate({running: true, phase: 'prepare', ...x})!;

describe("the recovery system's 503", () => {
    it('is the recovery with the update object, an older one without, and nothing else', () => {
        const body = {error: 'updating', recovery: true, update: {running: true, phase: 'install', step: 5, steps: 5, flashing: 'rootfs', written: 10, total: 20, log_bytes: 99}};
        expect(recoveryOf(503, body)).toEqual({recovery: true, update: {running: true, phase: 'install', step: 5, steps: 5, flashing: 'rootfs', written: 10, total: 20, logBytes: 99}});
        expect(recoveryOf(503, {error: 'updating', message: 'old recovery'})).toEqual({recovery: true, update: null});
        expect(recoveryOf(503, {error: 'starting'})).toBeNull();
        expect(recoveryOf(200, {uptime_s: 3})).toBeNull();
        expect(recoveryOf(null, null)).toBeNull();
    });

    it('takes nothing it cannot trust from the object', () => {
        expect(parseUpdate(null)).toBeNull();
        expect(parseUpdate('x')).toBeNull();
        expect(parseUpdate({phase: 'hacking'})).toBeNull();
        expect(u({step: 9, steps: 7})).toEqual({running: true, phase: 'prepare'});
        expect(u({step: 0, steps: 7})).toEqual({running: true, phase: 'prepare'});
        expect(u({step: 1.5, steps: 7})).toEqual({running: true, phase: 'prepare'});
        expect(u({step: '3', steps: 7})).toEqual({running: true, phase: 'prepare'});
        expect(u({flashing: '<img src=x>'})).toEqual({running: true, phase: 'prepare'});
        expect(u({written: 5, total: 10})).toEqual({running: true, phase: 'prepare'});
        expect(u({flashing: 'rootfs', written: 50, total: 10})).toMatchObject({written: 10, total: 10});
        expect(u({flashing: 'rootfs', written: -1, total: 10})).toEqual({running: true, phase: 'prepare', flashing: 'rootfs'});
        expect(u({log_bytes: -3})).toEqual({running: true, phase: 'prepare'});
        expect(u({running: 'yes'}).running).toBe(false);
    });
});

describe('the install bar', () => {
    it('weighs the steps by their measured lengths: the storage test, the unpacking, the check and the flash are nearly all', () => {
        const at = (x: Record<string, unknown>) => installView(u(x)).fraction;
        expect(at({phase: 'starting'})).toBe(0);
        expect(at({step: 1, steps: 7})).toBe(0);
        expect(at({step: 4, steps: 7})).toBeCloseTo(7 / 362, 3);
        expect(at({step: 5, steps: 7})).toBeCloseTo(35 / 362, 3);
        expect(at({step: 6, steps: 7})).toBeCloseTo(154 / 362, 3);
        expect(at({phase: 'install', step: 1, steps: 5})).toBeCloseTo(207.5 / 362, 3);
        expect(at({phase: 'install', step: 5, steps: 5})).toBeCloseTo(208.5 / 362, 3);
    });

    it('is real inside the flash steps: bytes on the card', () => {
        const boot = installView(u({phase: 'install', step: 5, steps: 5, flashing: 'bootfs', written: 50, total: 100}));
        expect(boot.key).toBe('bootfs');
        expect(boot.flashPercent).toBe(50);
        expect(boot.fraction).toBeCloseTo((209 + 10) / 362, 3);
        const root = installView(u({phase: 'install', step: 5, steps: 5, flashing: 'rootfs', written: 3, total: 4}));
        expect(root.key).toBe('rootfs');
        expect(root.flashPercent).toBe(75);
        expect(root.fraction).toBeCloseTo((229 + 133 * 0.75) / 362, 3);
        expect(installView(u({phase: 'install', step: 5, steps: 5, flashing: 'rootfs', written: 4, total: 4})).fraction).toBeCloseTo(1, 5);
    });

    it('a partition without figures: the step, no percentage', () => {
        const v = installView(u({phase: 'install', step: 5, steps: 5, flashing: 'mmcblk0p2'}));
        expect(v.key).toBe('rootfs');
        expect(v.flashPercent).toBeNull();
    });

    it('creeps inside a step without figures, never past its cap', () => {
        const x = u({step: 5, steps: 7});
        const start = installView(x, 0).fraction;
        const half = installView(x, 60).fraction;
        const long = installView(x, 10_000).fraction;
        expect(half).toBeGreaterThan(start);
        expect(long).toBeCloseTo((35 + 119 * CREEP_CAP) / 362, 3);
        expect(long).toBeLessThan(installView(u({step: 6, steps: 7})).fraction);
    });

    it('done is full; failed keeps where it stood and says so', () => {
        expect(installView(u({phase: 'done'}))).toMatchObject({fraction: 1, done: true, key: 'done'});
        const f = installView(u({phase: 'failed', step: 6, steps: 7}));
        expect(f).toMatchObject({failed: true, key: 'failed'});
        expect(f.fraction).toBeCloseTo(154 / 362, 3);
    });

    it('the overall step counts both phases: 12 in all', () => {
        expect(installView(u({step: 3, steps: 7}))).toMatchObject({step: 3, steps: 12, key: 'p3'});
        expect(installView(u({phase: 'install', step: 2, steps: 5}))).toMatchObject({step: 9, key: 'check'});
        expect(installView(u({phase: 'starting'}))).toMatchObject({step: null, key: 'starting'});
    });

    it('never goes back, and a new step is a new step', () => {
        expect(monotonic(0.5, 0.4)).toBe(0.5);
        expect(monotonic(0.5, 0.6)).toBe(0.6);
        expect(stepId(u({step: 5, steps: 7}))).not.toBe(stepId(u({step: 6, steps: 7})));
        expect(stepId(u({phase: 'install', step: 5, steps: 5, flashing: 'bootfs'}))).not.toBe(stepId(u({phase: 'install', step: 5, steps: 5, flashing: 'rootfs'})));
        expect(stepId(null)).toBe('');
    });
});

describe('the raw output (Details)', () => {
    it("is the recovery's text log, not the system's page or the redirect to the menu", () => {
        expect(isLogAnswer(200, 'text/plain', false)).toBe(true);
        expect(isLogAnswer(206, 'text/plain; charset=utf-8', false)).toBe(true);
        expect(isLogAnswer(200, 'text/html; charset=utf-8', false)).toBe(false);
        expect(isLogAnswer(200, 'text/plain', true)).toBe(false);
        expect(isLogAnswer(404, 'text/plain', false)).toBe(false);
        expect(isLogAnswer(200, null, false)).toBe(false);
    });

    it('turns the HTML line ends into lines and drops other tags', () => {
        expect(logText('Starting firmware update:<br/>\n[1/7] Checking... OK<br>\n[2/7] x<b>y</b> &lt;3 &amp;')).toBe('Starting firmware update:\n[1/7] Checking... OK\n[2/7] xy <3 &');
        expect(logText('a\r\n\n\n\nb')).toBe('a\n\nb');
    });
});
