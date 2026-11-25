import {readFileSync} from 'node:fs';
import {describe, expect, it} from 'vitest';

// B-116: the service worker (src/sw.js) runs here against fakes of its globals. Node's Response is
// the real one: its body can be read once, and clone() after that throws "Response body is already
// used" - what the browser said at sw.js:34 and :38. The cache opens a macrotask later, as a real
// one does, so the page has read the body by then.
const source = readFileSync(new URL('../sw.js', import.meta.url), 'utf8');

type Handler = (ev: unknown) => void;

function worker(network: (url: string) => Promise<Response>) {
    const handlers: Record<string, Handler> = {};
    const stored = new Map<string, Response>();
    const errors: unknown[] = [];
    const later = <T>(v: T) => new Promise<T>((res) => setTimeout(() => res(v), 0));
    const cache = {
        put: async (key: Request | string, r: Response) => { stored.set(typeof key === 'string' ? key : new URL(key.url).pathname, r); },
        match: async (key: Request | string) => stored.get(typeof key === 'string' ? key : new URL(key.url).pathname),
        addAll: async () => undefined,
    };
    const caches = {open: () => later(cache), match: cache.match, keys: async () => [], delete: async () => true};
    const self = {addEventListener: (name: string, h: Handler) => { handlers[name] = h; }, skipWaiting: () => undefined, clients: {claim: () => undefined}};
    const fetch = (req: Request) => network(req.url);
    new Function('self', 'caches', 'fetch', 'location', source)(self, caches, fetch, {origin: 'https://ccu.example'});
    const onError = (e: unknown) => errors.push(e);
    process.on('unhandledRejection', onError);
    async function get(path: string, mode: string) {
        const request = new Request('https://ccu.example' + path);
        Object.defineProperty(request, 'mode', {value: mode});
        let answer: Promise<Response> | undefined;
        handlers.fetch!({request, respondWith: (p: Promise<Response>) => { answer = p; }});
        expect(answer).toBeDefined();
        const r = await answer!;
        const body = await r.text(); // the page reads the answer at once
        await new Promise((res) => setTimeout(res, 10));
        return body;
    }
    /** whether the worker answers a request at all (it leaves the API to the network) */
    function answers(path: string, mode = 'cors'): boolean {
        const request = new Request('https://ccu.example' + path);
        Object.defineProperty(request, 'mode', {value: mode});
        let answered = false;
        handlers.fetch!({request, respondWith: () => { answered = true; }});
        return answered;
    }
    return {get, answers, stored, errors, done: () => process.off('unhandledRejection', onError)};
}

// occulited's own answers carry the mark (internal/ui ShellHeader, openccu-lite task 505)
const SHELL = {'X-Occulited-Shell': '1'};

describe('the service worker (B-116)', () => {
    it('a navigation answers from the network and keeps a readable copy of the shell', async () => {
        const w = worker(async () => new Response('<html>v2</html>', {status: 200, headers: SHELL}));
        try {
            expect(await w.get('/settings', 'navigate')).toBe('<html>v2</html>');
            expect(await w.stored.get('/')?.text()).toBe('<html>v2</html>');
            expect(w.errors).toEqual([]);
        } finally {
            w.done();
        }
    });
    it('a shell file (an icon, the manifest) is refreshed the same way', async () => {
        const w = worker(async () => new Response('png', {status: 200, headers: SHELL}));
        try {
            expect(await w.get('/icons/icon-192.png', 'no-cors')).toBe('png');
            expect(await w.stored.get('/icons/icon-192.png')?.text()).toBe('png');
            expect(w.errors).toEqual([]);
        } finally {
            w.done();
        }
    });
    it('an error answer is passed on and not kept; offline, the kept shell answers', async () => {
        let online = true;
        const w = worker(async () => {
            if (!online) throw new TypeError('offline');
            return new Response('down', {status: 503});
        });
        try {
            expect(await w.get('/', 'navigate')).toBe('down');
            expect(w.stored.has('/')).toBe(false);
            w.stored.set('/', new Response('<html>kept</html>'));
            online = false;
            expect(await w.get('/', 'navigate')).toBe('<html>kept</html>');
            expect(w.errors).toEqual([]);
        } finally {
            w.done();
        }
    });
});

// openccu-lite task 505: the same origin is the recovery system's while it runs - its lighttpd serves
// its own menu there over HTTPS and redirects an unknown path to it. The worker passes those answers
// on and keeps none of them, so the App's shell is never the recovery's page.
describe('the service worker and the recovery system (task 505)', () => {
    const RECOVERY = '<html><title>CCU Recovery System</title></html>';
    it("a navigation answered by the recovery's page is shown but not kept as the shell", async () => {
        const w = worker(async () => new Response(RECOVERY, {status: 200, headers: {'Content-Type': 'text/html; charset=iso-8859-1'}}));
        try {
            w.stored.set('/', new Response('<html>shell</html>', {headers: SHELL}));
            expect(await w.get('/system/updates', 'navigate')).toBe(RECOVERY);
            expect(await w.stored.get('/')?.text()).toBe('<html>shell</html>');
        } finally {
            w.done();
        }
    });
    it("an icon or a bundle the recovery answers with its menu (a redirect) is not kept", async () => {
        const w = worker(async () => {
            const r = new Response(RECOVERY, {status: 200, headers: SHELL});
            Object.defineProperty(r, 'redirected', {value: true});
            return r;
        });
        try {
            await w.get('/icons/icon-192.png', 'no-cors');
            await w.get('/assets/index-abc123.js', 'no-cors');
            expect(w.stored.size).toBe(0);
        } finally {
            w.done();
        }
    });
    it('a 200 without the mark is never kept, a marked one is', async () => {
        let marked = false;
        const w = worker(async () => new Response('x', {status: 200, headers: marked ? SHELL : {}}));
        try {
            await w.get('/assets/a-1.js', 'no-cors');
            await w.get('/app.webmanifest', 'no-cors');
            expect(w.stored.size).toBe(0);
            marked = true;
            await w.get('/assets/a-1.js', 'no-cors');
            expect(w.stored.has('/assets/a-1.js')).toBe(true);
        } finally {
            w.done();
        }
    });
    it("the API and the raw log are never the worker's: the recovery's 503 and its log go straight to the page", () => {
        const w = worker(async () => new Response('{}', {status: 503}));
        try {
            expect(w.answers('/api/system/v1/health')).toBe(false);
            expect(w.answers('/fwinstall.log')).toBe(false);
            expect(w.answers('/', 'navigate')).toBe(true);
        } finally {
            w.done();
        }
    });
});
