import {describe, expect, it} from 'vitest';
import {ratePerHour, rowsIn, serviceID, shortOf, spanParts, unitLabel, windowIn, writtenIn, type WritesUnit} from './storagewrites';

const unit = (name: string, all: number, hour: number, day: number, extra: Partial<WritesUnit> = {}): WritesUnit => ({
    unit: name,
    kind: 'unit',
    running: true,
    read_bytes: 0,
    written_bytes: all,
    hour: {seconds: 3600, written_bytes: hour},
    day: {seconds: 86400, written_bytes: day},
    disks: [],
    ...extra,
});

describe('disk writes', () => {
    it('gives a rate once a minute is covered', () => {
        expect(ratePerHour({seconds: 1800, written_bytes: 1000})).toBe(2000);
        expect(ratePerHour({seconds: 30, written_bytes: 1000})).toBeNull();
        expect(ratePerHour({seconds: 0, written_bytes: 0})).toBeNull();
    });

    it('lists the rows that wrote in the span, the most first', () => {
        const units = [
            unit('rfd.service', 500, 0, 300),
            unit('addon-redmatic.service', 900, 50, 200),
            unit('kernel', 100, 60, 100, {kind: 'kernel'}),
            unit('idle.service', 0, 0, 0),
        ];
        expect(rowsIn(units, 'hour').map((u) => u.unit)).toEqual(['kernel', 'addon-redmatic.service']);
        expect(rowsIn(units, 'day').map((u) => u.unit)).toEqual(['rfd.service', 'addon-redmatic.service', 'kernel']);
        expect(rowsIn(units, 'all').map((u) => u.unit)).toEqual(['addon-redmatic.service', 'rfd.service', 'kernel']);
    });

    it('narrows a row to one disk', () => {
        const u = unit('a.service', 70, 7, 40, {
            disks: [
                {disk: 'mmcblk0', read_bytes: 0, written_bytes: 50, hour: {seconds: 3600, written_bytes: 5}, day: {seconds: 7200, written_bytes: 30}},
                {disk: 'sda', read_bytes: 0, written_bytes: 20, hour: {seconds: 3600, written_bytes: 2}, day: {seconds: 7200, written_bytes: 10}},
            ],
        });
        expect(writtenIn(u, 'all', 'sda')).toBe(20);
        expect(writtenIn(u, 'day', 'mmcblk0')).toBe(30);
        expect(writtenIn(u, 'hour', 'nvme0n1')).toBe(0);
        expect(windowIn(u, 'day', 'sda')).toEqual({seconds: 7200, written_bytes: 10});
        expect(windowIn(u, 'all')).toBeNull();
        expect(rowsIn([u], 'hour', 'nvme0n1')).toEqual([]);
    });

    it('names a unit as the Services page does', () => {
        expect(unitLabel('addon-redmatic.service')).toBe('addon-redmatic');
        expect(unitLabel('usr-local.mount')).toBe('usr-local.mount');
        expect(serviceID(unit('rfd.service', 1, 1, 1))).toBe('rfd');
        expect(serviceID(unit('init.scope', 1, 1, 1))).toBeNull();
        expect(serviceID(unit('kernel', 1, 1, 1, {kind: 'kernel'}))).toBeNull();
    });

    it('splits a span and knows a short history', () => {
        expect(spanParts(12 * 86400 + 4 * 3600 + 61)).toEqual({d: 12, h: 4, min: 1, s: 1});
        expect(shortOf({seconds: 600, written_bytes: 0}, 'hour')).toBe(true);
        expect(shortOf({seconds: 3590, written_bytes: 0}, 'hour')).toBe(false);
        expect(shortOf({seconds: 3600, written_bytes: 0}, 'day')).toBe(true);
        expect(shortOf(null, 'day')).toBe(false);
    });
});
