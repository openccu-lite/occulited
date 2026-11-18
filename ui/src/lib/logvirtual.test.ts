import {describe, expect, it} from 'vitest';
import {joinPages, jumpButtons, LOG_AT_END, rowAt, rowOffsets, rowWindow} from './logvirtual';

// task 178: the windowed list's arithmetic and the pages' join

describe('row offsets', () => {
    it('are each row\'s top and, at the end, the height of all', () => {
        const o = rowOffsets(3, (i) => [20, 40, 20][i] ?? 0);
        expect(Array.from(o)).toEqual([0, 20, 60, 80]);
        expect(Array.from(rowOffsets(0, () => 20))).toEqual([0]);
    });

    it('find the row at a position', () => {
        const o = rowOffsets(4, () => 20);
        expect(rowAt(o, -5)).toBe(0);
        expect(rowAt(o, 0)).toBe(0);
        expect(rowAt(o, 19.9)).toBe(0);
        expect(rowAt(o, 20)).toBe(1);
        expect(rowAt(o, 79)).toBe(3);
        expect(rowAt(o, 80)).toBe(4);
        expect(rowAt(o, 1000)).toBe(4);
        expect(rowAt(rowOffsets(0, () => 20), 10)).toBe(0);
    });
});

describe('the window', () => {
    const o = rowOffsets(1000, () => 20); // 20 000 px

    it('covers the view plus the margin on both sides', () => {
        // at the top: nothing above, the view (600) and the margin (600) below = 60 rows
        expect(rowWindow(o, 0, 600, 600)).toEqual({start: 0, end: 61});
        // in the middle: 600 px above the view, the view, 600 px below
        expect(rowWindow(o, 10_000, 600, 600)).toEqual({start: 470, end: 561});
        // at the bottom: the rows end
        expect(rowWindow(o, 19_400, 600, 600)).toEqual({start: 940, end: 1000});
    });

    it('is empty without rows', () => {
        expect(rowWindow(rowOffsets(0, () => 20), 0, 600)).toEqual({start: 0, end: 0});
    });
});

describe('joining pages', () => {
    const run = [3, 4, 5];

    it('adds a page at the old or the new end', () => {
        expect(joinPages(run, [1, 2], 'old')).toEqual({lines: [1, 2, 3, 4, 5], dropped: null});
        expect(joinPages(run, [6, 7], 'new')).toEqual({lines: [3, 4, 5, 6, 7], dropped: null});
    });

    it('cuts the far end past the limit and says which', () => {
        expect(joinPages(run, [1, 2], 'old', 4)).toEqual({lines: [1, 2, 3, 4], dropped: 'new'});
        expect(joinPages(run, [6, 7], 'new', 4)).toEqual({lines: [4, 5, 6, 7], dropped: 'old'});
        expect(joinPages([], [1], 'new', 1)).toEqual({lines: [1], dropped: null});
    });
});

// occulited task 156: the two floating jump buttons
describe('the jump buttons', () => {
    const none = {moreTop: false, moreBottom: false};

    it('hide the one whose end the view stands at, and show both in between', () => {
        expect(jumpButtons({top: 0, bottom: 5000, ...none})).toEqual({top: false, bottom: true});
        expect(jumpButtons({top: 2500, bottom: 2500, ...none})).toEqual({top: true, bottom: true});
        expect(jumpButtons({top: 5000, bottom: 0, ...none})).toEqual({top: true, bottom: false});
    });

    it('take a few pixels from an end as the end', () => {
        expect(jumpButtons({top: LOG_AT_END, bottom: LOG_AT_END, ...none})).toEqual({top: false, bottom: false});
        expect(jumpButtons({top: LOG_AT_END + 1, bottom: LOG_AT_END + 1, ...none})).toEqual({top: true, bottom: true});
        expect(jumpButtons({top: 12, bottom: 5, ...none}, 10)).toEqual({top: true, bottom: false});
    });

    it('show neither for a log that fits its box', () => {
        expect(jumpButtons({top: 0, bottom: 0, ...none})).toEqual({top: false, bottom: false});
    });

    it('show a button at the end of the loaded run while the log goes on beyond it', () => {
        expect(jumpButtons({top: 0, bottom: 5000, moreTop: true, moreBottom: false})).toEqual({top: true, bottom: true});
        expect(jumpButtons({top: 5000, bottom: 0, moreTop: false, moreBottom: true})).toEqual({top: true, bottom: true});
    });
});
