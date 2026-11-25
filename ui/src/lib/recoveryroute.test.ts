import {describe, expect, it} from 'vitest';
import {RECOVERY_PATH, recoveryRoute, returnPath, validRecoveryUrl} from './recoveryroute';

describe('the route while the system is in its recovery system', () => {
    it('every page goes to the recovery view while the recovery answers', () => {
        for (const p of ['/', '/system/updates', '/app', '/addons', '/nav/red', '/settings', '/licenses', '/login'])
            expect(recoveryRoute(p, true, '')).toBe(RECOVERY_PATH);
    });
    it('the recovery view itself stays', () => {
        expect(recoveryRoute(RECOVERY_PATH, true, '/system/updates')).toBeNull();
    });
    it('once the system answers, back to the page the recovery interrupted', () => {
        expect(recoveryRoute(RECOVERY_PATH, false, '/system/updates?x=1')).toBe('/system/updates?x=1');
    });
    it('a recovery view opened without a page before it goes to Status; one opened by hand stays', () => {
        expect(recoveryRoute(RECOVERY_PATH, false, RECOVERY_PATH)).toBe('/');
        expect(recoveryRoute(RECOVERY_PATH, false, '')).toBeNull();
    });
    it('no recovery, no other page: nothing to do', () => {
        expect(recoveryRoute('/system/updates', false, '/x')).toBeNull();
    });
    it('the way back keeps the query and never is the recovery view', () => {
        expect(returnPath('/system/log', '?unit=rfd')).toBe('/system/log?unit=rfd');
        expect(returnPath(RECOVERY_PATH, '')).toBe('');
    });
    it("the recovery's address is a link by IP only", () => {
        expect(validRecoveryUrl('http://192.0.2.7/')).toBe('http://192.0.2.7/');
        expect(validRecoveryUrl('http://[2001:db8::7]/')).toBe('http://[2001:db8::7]/');
        expect(validRecoveryUrl('http://openccu/')).toBe('');
        expect(validRecoveryUrl('javascript:alert(1)//')).toBe('');
        expect(validRecoveryUrl('http://192.0.2.7/x')).toBe('');
    });
});
