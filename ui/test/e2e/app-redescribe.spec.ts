import {expect, test} from './fixtures';
import type {Page, Request} from '@playwright/test';

// occulited task 182: an open Control page reads a device's descriptions again when its interface
// announces it as new (an addon update that changed its firmware, so its paramset keys), readded,
// replaced or with a changed description (updateDevice hint 2) - without a reload. Only the tiles of
// that device are asked again; a links-only update (hint 1) asks nothing.

const LAMP = 'HmIP-RF.000DD8:3';
const lampState = (page: Page) => page.locator(`[data-app-tile="${LAMP}"] [data-app-state]`);
const isStream = (r: Request) => new URL(r.url()).pathname === '/api/rpc/v1/events';

async function push(page: Page, baseURL: string, app: string, event: string, data: Record<string, unknown>) {
    const r = await page.request.post(`${baseURL}/__stub/lite-push`, {data: {app, event, data}});
    expect(r.status()).toBe(200);
}

test('a device announced as new is described again on the open page', async ({page, baseURL}) => {
    const app = `redescribe-${Math.random().toString(36).slice(2)}`;
    await page.context().addCookies([{name: 'stub-app', value: app, url: baseURL!}]);
    const asked: string[] = [];
    page.on('request', (r) => {
        const u = new URL(r.url());
        if (r.method() !== 'POST') return;
        if (u.pathname === '/api/rpc/v1/paramsets/HmIP-RF') asked.push(`paramsets ${(r.postDataJSON() as {addresses: string[]}).addresses.join(',')}`);
        if (u.pathname === '/api/rpc/v1/json/HmIP-RF') {
            for (const c of r.postDataJSON() as {method: string; params: unknown[]}[]) if (c.method === 'getDeviceDescription') asked.push(`describe ${c.params[0]}`);
        }
    });
    const [req] = await Promise.all([page.waitForRequest(isStream), page.goto('/app/e/rooms/og/bad')]);
    // the stream asks for the devices' announcements too
    expect(new URL(req.url()).searchParams.get('type')).toBe('event,state,newDevices,readdedDevice,updateDevice,replaceDevice');
    await expect(lampState(page)).toHaveText('On · 60 %');
    const before = asked.length;
    expect(asked.some((a) => a.startsWith('paramsets') && a.includes('000DD8:3'))).toBe(true);

    // a links-only update: nothing is asked again
    await push(page, baseURL!, app, 'updateDevice', {interface: 'HmIP-RF', addresses: ['000DD8'], hint: 1, ts: new Date().toISOString()});
    await page.waitForTimeout(800);
    expect(asked.slice(before)).toEqual([]);

    // announced as new: the device's tiles are described again, nothing else, and the page stays
    const url = page.url();
    await push(page, baseURL!, app, 'newDevices', {interface: 'HmIP-RF', addresses: ['000DD8', '000DD8:3'], ts: new Date().toISOString()});
    await expect.poll(() => asked.slice(before)).toEqual(['describe 000DD8:3', 'paramsets 000DD8:3']);
    expect(page.url()).toBe(url);
    await expect(lampState(page)).toHaveText('On · 60 %');

    // the description changed (hint 2): the same once more
    const again = asked.length;
    await push(page, baseURL!, app, 'updateDevice', {interface: 'HmIP-RF', addresses: ['000DD8'], hint: 2, ts: new Date().toISOString()});
    await expect.poll(() => asked.slice(again)).toEqual(['describe 000DD8:3', 'paramsets 000DD8:3']);
});
