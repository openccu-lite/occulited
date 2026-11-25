import {describe, expect, it} from 'vitest';

import {rebootAsked} from './installprogress';

// occulited B-178: an exit 10 whose addon the system started itself asks for no reboot
describe('rebootAsked', () => {
    it('asks after an exit 10 that started nothing', () => {
        expect(rebootAsked({reboot_required: true})).toBe(true);
        expect(rebootAsked({reboot_required: true, started: []})).toBe(true);
    });
    it('does not ask when the system started the addon', () => {
        expect(rebootAsked({reboot_required: true, started: ['logic']})).toBe(false);
    });
    it('asks when an addon was left stopped', () => {
        expect(rebootAsked({reboot_required: true, started: ['a'], stopped_addons: [{id: 'b', starts_at_boot: true}]})).toBe(true);
    });
    it('does not ask without a reboot request', () => {
        expect(rebootAsked({started: ['a']})).toBe(false);
        expect(rebootAsked(null)).toBe(false);
    });
});
