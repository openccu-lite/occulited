import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';

// a sessionStorage and a window for node: api.ts reads the one and signals on the other
class MemStorage {
    m = new Map<string, string>();
    getItem(k: string) {
        return this.m.has(k) ? this.m.get(k)! : null;
    }
    setItem(k: string, v: string) {
        this.m.set(k, v);
    }
    removeItem(k: string) {
        this.m.delete(k);
    }
}

describe('the stored session id (occulited task 127)', () => {
    let store: MemStorage;
    let events: {sid?: string}[];
    beforeEach(() => {
        store = new MemStorage();
        events = [];
        vi.stubGlobal('sessionStorage', store);
        vi.stubGlobal('window', {dispatchEvent: (e: CustomEvent<{sid?: string}>) => events.push(e.detail)});
        vi.stubGlobal('CustomEvent', class {
            constructor(public type: string, public init?: {detail?: unknown}) {}
            get detail() {
                return this.init?.detail;
            }
        });
    });
    afterEach(() => {
        vi.unstubAllGlobals();
        vi.restoreAllMocks();
    });

    it('the logout forgets it; a 401 forgets only the id it answered', async () => {
        const {forgetSid} = await import('./api');
        store.setItem('ol.sid', 'A1');
        forgetSid('B2');
        expect(store.getItem('ol.sid')).toBe('A1');
        forgetSid('A1');
        expect(store.getItem('ol.sid')).toBeNull();
        store.setItem('ol.sid', 'A1');
        forgetSid();
        expect(store.getItem('ol.sid')).toBeNull();
    });

    it('a 401 drops the id the request sent, and the next request sends none', async () => {
        const {api} = await import('./api');
        store.setItem('ol.sid', 'A1');
        const sent: (string | undefined)[] = [];
        vi.stubGlobal('fetch', async (_: string, init: {headers: Record<string, string>}) => {
            sent.push(init.headers['Authorization']);
            return new Response(JSON.stringify({error: 'unauthenticated', message: 'no session'}), {status: 401});
        });
        await expect(api.get('/api/system/v1/status')).rejects.toThrow();
        expect(store.getItem('ol.sid')).toBeNull();
        expect(events).toEqual([{sid: 'A1'}]);
        await expect(api.get('/api/system/v1/status')).rejects.toThrow();
        expect(sent).toEqual(['Bearer A1', undefined]);
    });

    it('a 401 for an old id leaves a newer one alone (a login while the request was on its way)', async () => {
        const {api} = await import('./api');
        store.setItem('ol.sid', 'A1');
        vi.stubGlobal('fetch', async () => {
            store.setItem('ol.sid', 'N3');
            return new Response('{}', {status: 401});
        });
        await expect(api.get('/api/system/v1/status')).rejects.toThrow();
        expect(store.getItem('ol.sid')).toBe('N3');
    });

    it('a wrong password typed into a confirmation keeps the session', async () => {
        const {api} = await import('./api');
        store.setItem('ol.sid', 'A1');
        vi.stubGlobal('fetch', async () => new Response(JSON.stringify({error: 'invalid-credentials', message: 'wrong'}), {status: 401}));
        await expect(api.post('/api/auth/v1/password', {})).rejects.toThrow();
        expect(store.getItem('ol.sid')).toBe('A1');
        expect(events).toEqual([]);
    });
});

describe('a tagged GET revalidates its held answer itself (occulited task 162)', () => {
    beforeEach(() => {
        vi.stubGlobal('sessionStorage', new MemStorage());
        vi.stubGlobal('window', {dispatchEvent: () => true});
    });
    afterEach(() => {
        vi.unstubAllGlobals();
        vi.restoreAllMocks();
    });

    it('sends the ETag it holds, takes a 304 as the held answer and a 200 as the new one', async () => {
        const {api} = await import('./api');
        const sent: {inm?: string; cache?: string}[] = [];
        const answers: Response[] = [
            new Response(JSON.stringify({samples: [1]}), {status: 200, headers: {ETag: '"a"'}}),
            new Response(null, {status: 304, headers: {ETag: '"a"'}}),
            new Response(JSON.stringify({samples: [1, 2]}), {status: 200, headers: {ETag: '"b"'}}),
            new Response(JSON.stringify({error: 'unsupported', message: 'no'}), {status: 501}),
            new Response(JSON.stringify({samples: [3]}), {status: 200}),
        ];
        vi.stubGlobal('fetch', async (_: string, init: {headers: Record<string, string>; cache?: string}) => {
            sent.push({inm: init.headers['If-None-Match'], cache: init.cache});
            return answers.shift()!;
        });
        const path = '/api/system/v1/status/history';
        expect(await api.getTagged(path)).toEqual({samples: [1]});
        expect(await api.getTagged(path)).toEqual({samples: [1]});
        expect(await api.getTagged(path)).toEqual({samples: [1, 2]});
        await expect(api.getTagged(path)).rejects.toThrow('no');
        // an error leaves the held answer: its tag goes out again; an untagged 200 drops it
        expect(await api.getTagged(path)).toEqual({samples: [3]});
        expect(sent).toEqual([
            {inm: undefined, cache: 'no-store'},
            {inm: '"a"', cache: 'no-store'},
            {inm: '"a"', cache: 'no-store'},
            {inm: '"b"', cache: 'no-store'},
            {inm: '"b"', cache: 'no-store'},
        ]);
        answers.push(new Response(JSON.stringify({samples: [4]}), {status: 200}));
        await api.getTagged(path);
        expect(sent.at(-1)).toEqual({inm: undefined, cache: 'no-store'});
    });
});
