import {describe, expect, it} from 'vitest';
import {HEALTH_PATH, healthPath, mergeHealth, type HistoryAnswer} from './healthdelta';

type A = HistoryAnswer<{t: string; dc: number}, {t: string; in: number}> & {busy: boolean};

describe('the radio history in deltas (occulited task 162)', () => {
    it('asks with the held answer\'s cursor', () => {
        expect(healthPath(null)).toBe(HEALTH_PATH);
        expect(healthPath({})).toBe(HEALTH_PATH);
        expect(healthPath({history_cursor: '17.4'})).toBe(`${HEALTH_PATH}?after=17.4`);
        expect(healthPath({history_cursor: 'a b'})).toBe(`${HEALTH_PATH}?after=a%20b`);
    });

    it('takes a whole answer as it comes', () => {
        const held: A = {busy: false, history: {x: [{t: '1', dc: 1}]}, history_cursor: '1.1'};
        const next: A = {busy: true, history: {y: [{t: '2', dc: 2}]}, history_cursor: '2.1', history_delta: false};
        expect(mergeHealth(held, next)).toBe(next);
        // a delta without anything held (the last poll failed) cannot be merged: it is shown as it is
        const delta: A = {busy: true, history: {}, history_delta: true};
        expect(mergeHealth(null, delta)).toBe(delta);
    });

    it('appends a delta, keeps the series without news and cuts to the ring', () => {
        const held: A = {
            busy: false,
            history: {a: [{t: '1', dc: 1}, {t: '2', dc: 2}, {t: '3', dc: 3}], b: [{t: '1', dc: 9}]},
            rates: {r: [{t: '1', in: 1}]},
            history_cursor: '5.3',
            history_rows: 3,
        };
        const next: A = {busy: true, history: {a: [{t: '4', dc: 4}], c: [{t: '4', dc: 7}]}, rates: {}, history_cursor: '5.5', history_delta: true, history_rows: 3};
        const m = mergeHealth(held, next);
        expect(m.busy).toBe(true);
        expect(m.history_cursor).toBe('5.5');
        expect((m.history?.a ?? []).map((s) => s.dc)).toEqual([2, 3, 4]);
        expect(m.history?.b).toEqual([{t: '1', dc: 9}]);
        expect(m.history?.c).toEqual([{t: '4', dc: 7}]);
        expect(m.rates).toEqual({r: [{t: '1', in: 1}]});
        // the held answer is not changed
        expect(held.history?.a).toHaveLength(3);
        expect(held.history?.a?.[2]?.dc).toBe(3);
    });
});
