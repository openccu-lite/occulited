import {expect, test} from './fixtures';

// occulited task 162: the Status page polled GET /radio/health whole every 10 s - 8 hours of history
// and rates, 160 to 230 KiB an answer. It now asks with the last answer's cursor and appends the
// delta; the catalogue, the addons and the certificate come once a minute instead of every 10 s;
// a hidden browser tab polls nothing and refreshes when it is shown again.

const NEW_RATE = 99;

test('the radio history arrives in deltas and the charts keep their samples', async ({page}) => {
    await page.clock.install();
    const urls: string[] = [];
    await page.route('**/api/system/v1/radio/health*', async (route) => {
        const url = route.request().url();
        urls.push(url);
        const response = await route.fetch({url: url.replace(/\?.*$/, '')});
        const body = await response.json();
        if (urls.length === 1) {
            Object.assign(body, {history_cursor: 'c1', history_delta: false, history_rows: 500});
        } else {
            // the delta: no duty-cycle sample, one new event rate on HmIP-RF
            const last = body.rates['HmIP-RF'].at(-1);
            Object.assign(body, {
                history: {},
                rates: {'HmIP-RF': [{t: new Date(Date.parse(last.t) + 60000).toISOString(), in: NEW_RATE, up: true}]},
                history_cursor: `c${urls.length}`,
                history_delta: true,
                history_rows: 500,
            });
        }
        await route.fulfill({response, json: body});
    });
    await page.goto('/');
    const radio = page.getByRole('img', {name: 'duty cycle history'}).first();
    await expect(radio).toBeVisible();
    const peakBefore = await page.locator('.ol-gauge', {has: radio}).first().textContent();
    const hmip = page.locator('.ol-components [data-component="HmIP-RF"]');
    await expect(hmip.locator('[data-rate="in"]')).toBeVisible();
    expect(urls).toHaveLength(1);
    expect(new URL(urls[0]).search).toBe('');

    await page.clock.runFor(10_500);
    await expect.poll(() => urls.length).toBe(2);
    expect(new URL(urls[1]).searchParams.get('after')).toBe('c1');
    // the newest rate is the delta's, and the duty-cycle chart still has its whole history
    await expect(hmip.locator('[data-rate="in"]')).toContainText(`${NEW_RATE}/min`);
    await expect(radio).toBeVisible();
    await expect(page.locator('.ol-gauge', {has: radio}).first()).toHaveText(peakBefore!);
    await expect(hmip.getByRole('img', {name: 'event rate history'})).toBeVisible();

    await page.clock.runFor(10_000);
    await expect.poll(() => urls.length).toBe(3);
    expect(new URL(urls[2]).searchParams.get('after')).toBe('c2');
});

test('the catalogue once a minute, nothing in a hidden tab, a refresh when it is shown again', async ({page}) => {
    await page.clock.install();
    const count: Record<string, number> = {health: 0, catalog: 0};
    page.on('request', (r) => {
        const p = new URL(r.url()).pathname;
        if (p === '/api/system/v1/radio/health') count.health++;
        if (p === '/api/system/v1/catalog') count.catalog++;
    });
    await page.goto('/');
    await expect(page.getByRole('img', {name: 'duty cycle history'}).first()).toBeVisible();
    await expect.poll(() => count.catalog).toBe(1);
    expect(count.health).toBe(1);

    // five polls of the radio, none of the catalogue; the sixth brings it
    await page.clock.runFor(50_500);
    await expect.poll(() => count.health).toBe(6);
    expect(count.catalog).toBe(1);
    await page.clock.runFor(10_000);
    await expect.poll(() => count.health).toBe(7);
    await expect.poll(() => count.catalog).toBe(2);

    // the tab hidden: no polls for two minutes
    const setVisibility = (state: 'hidden' | 'visible') =>
        page.evaluate((s) => {
            Object.defineProperty(document, 'visibilityState', {value: s, configurable: true});
            document.dispatchEvent(new Event('visibilitychange'));
        }, state);
    await setVisibility('hidden');
    const hiddenAt = {...count};
    await page.clock.runFor(120_000);
    expect(count).toEqual(hiddenAt);

    // shown again: everything at once
    await setVisibility('visible');
    await expect.poll(() => count.health).toBe(hiddenAt.health + 1);
    await expect.poll(() => count.catalog).toBe(hiddenAt.catalog + 1);
});
