import {expect, test} from './fixtures';

// occulited task 183: the unassigned list needs nearly every channel - its types and FLAGS come from one
// listDevices per interface (2-4x faster there than describing the channels one by one, measured); a
// room's few tiles are still described address by address.

function methods(page: import('@playwright/test').Page): string[] {
    const seen: string[] = [];
    page.on('request', (r) => {
        if (r.method() !== 'POST' || !new URL(r.url()).pathname.startsWith('/api/rpc/v1/json/')) return;
        const body = r.postDataJSON() as {method: string}[] | {method: string};
        for (const c of Array.isArray(body) ? body : [body]) seen.push(c.method);
    });
    return seen;
}

test('the unassigned list: one listDevices per interface, no getDeviceDescription for its channels', async ({page, baseURL}) => {
    await page.context().addCookies([{name: 'stub-app', value: `whole-${Math.random().toString(36).slice(2)}`, url: baseURL!}]);
    const seen = methods(page);
    await page.goto('/app/unassigned');
    await expect(page.locator('[data-app-tile]').first()).toBeVisible();
    expect(seen).toContain('listDevices');
    expect(seen.filter((m) => m === 'getDeviceDescription')).toEqual([]);
});

test('a room: its tiles are described one by one, the whole list is not read', async ({page, baseURL}) => {
    await page.context().addCookies([{name: 'stub-app', value: `room-${Math.random().toString(36).slice(2)}`, url: baseURL!}]);
    const seen = methods(page);
    await page.goto('/app/e/rooms/og/bad');
    await expect(page.locator('[data-app-tile]').first()).toBeVisible();
    expect(seen).toContain('getDeviceDescription');
    expect(seen).not.toContain('listDevices');
});
