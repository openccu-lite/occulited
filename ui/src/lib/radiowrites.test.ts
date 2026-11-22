import {describe, expect, it} from 'vitest';
import {bucketTimes, clientKey, dutyTop, lastReading, lineRuns, minutesAgo, noopShare, spanNoopShare, tokenClient, writesTop, type RWView} from './radiowrites';

// occulited task 163: what the panel and the token list derive from GET /radio/writes

const minutes = (at: Record<number, number>) => Array.from({length: 60}, (_, i) => at[i] ?? 0);

describe('the radio writes', () => {
    it('the share of writes that changed nothing, in whole percent', () => {
        expect(noopShare({hour: 340, noop: 211})).toBe(62);
        expect(noopShare({hour: 3, noop: 0})).toBe(0);
        expect(noopShare({hour: 3, noop: 3})).toBe(100);
        expect(noopShare({hour: 0, noop: 0})).toBeNull();
    });
    it('one scale for every client: the next readable step at or above the busiest minute', () => {
        expect(writesTop([])).toBe(10);
        expect(writesTop([{minutes: minutes({})}])).toBe(10);
        expect(writesTop([{minutes: minutes({3: 10})}])).toBe(10);
        expect(writesTop([{minutes: minutes({3: 11})}, {minutes: minutes({5: 2})}])).toBe(20);
        expect(writesTop([{minutes: minutes({3: 4})}, {minutes: minutes({59: 84})}])).toBe(100);
        expect(writesTop([{minutes: minutes({0: 6200})}])).toBe(7000);
    });
    it('the duty cycle is drawn to 25 % at the least', () => {
        expect(dutyTop([])).toBe(25);
        expect(dutyTop([{buckets: [null, 3, 24]}])).toBe(25);
        expect(dutyTop([{buckets: [null, 3, 26]}])).toBe(50);
        expect(dutyTop([{buckets: [3]}, {buckets: [80]}])).toBe(100);
    });
    it('the last reading of a series with holes', () => {
        expect(lastReading([1, 2, null])).toBe(2);
        expect(lastReading([null, 0])).toBe(0);
        expect(lastReading([null, null])).toBeNull();
        expect(lastReading([])).toBeNull();
    });
    it('the last entry is the running minute', () => {
        expect(minutesAgo(59, 60)).toBe(0);
        expect(minutesAgo(0, 60)).toBe(59);
    });
    it('a line breaks where there was no reading', () => {
        const runs = lineRuns([0, 10, null, 20, null, null, 5], 20, 70, 20, 0);
        expect(runs.map((r) => r.length)).toEqual([2, 1, 1]);
        // x: the middle of each of the seven columns of 10 px; y: from the bottom
        expect(runs[0]).toEqual(['5.0,20.0', '15.0,10.0']);
        expect(runs[1]).toEqual(['35.0,0.0']);
        expect(runs[2]).toEqual(['65.0,15.0']);
        // a reading above the top stays at the top edge
        expect(lineRuns([40], 20, 10, 20, 0)).toEqual([['5.0,0.0']]);
        expect(lineRuns([], 20, 10, 20)).toEqual([]);
    });
    it("a token's client is found by kind and name", () => {
        const client = (kind: 'token' | 'user', name: string) => ({kind, name, per_minute: 0, hour: 1, noop: 0, minutes: [], interfaces: []});
        const view = {clients: [client('user', 'hm2mqtt'), client('token', 'hm2mqtt')]} as unknown as RWView;
        expect(tokenClient(view, 'hm2mqtt')?.kind).toBe('token');
        expect(tokenClient(view, 'other')).toBeUndefined();
        expect(tokenClient(null, 'hm2mqtt')).toBeUndefined();
        expect(clientKey(view.clients[0]!)).toBe('user:hm2mqtt');
    });

    // openccu-lite task 489: the span's share, and when a bucket of 6 h or 24 h begins and ends
    it('the span\'s share of writes that changed nothing', () => {
        expect(spanNoopShare({writes: 340, writes_noop: 211})).toBe(62);
        expect(spanNoopShare({writes: 0, writes_noop: 0})).toBeNull();
    });
    it('a bucket\'s start and end: the last holds now, each starts at a multiple of its length', () => {
        const at = '2026-11-22T12:07:30Z';
        const last = bucketTimes(at, 5, 71, 72);
        expect(last.start.toISOString()).toBe('2026-11-22T12:05:00.000Z');
        expect(last.end.toISOString()).toBe('2026-11-22T12:10:00.000Z');
        expect(bucketTimes(at, 5, 0, 72).start.toISOString()).toBe('2026-11-22T06:10:00.000Z');
        expect(bucketTimes(at, 15, 95, 96).start.toISOString()).toBe('2026-11-22T12:00:00.000Z');
        expect(bucketTimes(at, 15, 0, 96).start.toISOString()).toBe('2026-11-21T12:15:00.000Z');
    });
});
