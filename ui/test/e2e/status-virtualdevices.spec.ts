import {expect, test} from './fixtures';

// occulited task 157 (the maintainer, 2026-11-20): VirtualDevices under Components, as the Interfaces
// page has it, its subtitle the interfaces it takes events from; BidCos-Wired's subtitle its LAN
// gateway(s) from hs485d.conf, the serials as the detail line - like the RF cards' radio

type Health = {answering?: string[]; errors?: Record<string, string>; feed: {interfaces: {name: string; state: string}[]}; virtual_devices_via?: string[]; wired_gateways?: {name?: string; serial: string; address?: string}[]};

function patchHealth(page: import('@playwright/test').Page, edit: (h: Health) => void) {
    return page.route('**/api/system/v1/radio/health', async (route) => {
        const response = await route.fetch();
        const body = (await response.json()) as Health;
        edit(body);
        await route.fulfill({response, json: body});
    });
}

test('VirtualDevices has a card with the interfaces it takes events from', async ({page}) => {
    await patchHealth(page, (h) => (h.virtual_devices_via = ['BidCos-RF', 'HmIP-RF', 'BidCos-Wired']));
    await page.goto('/');
    const card = page.locator('.ol-components [data-component="VirtualDevices"]');
    await expect(card.locator('.ol-if-title')).toHaveText('VirtualDevices · hmipserver');
    await expect(card.locator('.ol-card-body')).toHaveText('up');
    const sub = card.locator('.ol-card-sub a');
    await expect(sub).toHaveText('via BidCos-RF, HmIP-RF, BidCos-Wired');
    // its subscribers, as the other process cards
    await expect(card.locator('.ol-if-subs')).toHaveText('1 subscriber');
    await sub.click();
    await expect(page).toHaveURL(/\/system\/interfaces#subscribers$/);
});

test('VirtualDevices without a subscription, silent, not answering, and starting', async ({page, baseURL}) => {
    let state = 'up';
    await patchHealth(page, (h) => {
        h.virtual_devices_via = [];
        h.feed.interfaces.find((i) => i.name === 'VirtualDevices')!.state = state;
    });
    await page.goto('/');
    const card = page.locator('.ol-components [data-component="VirtualDevices"]');
    await expect(card.locator('.ol-card-body')).toHaveText('up');
    await expect(card.locator('.ol-card-sub')).toHaveCount(0);
    const height = (await card.locator('.ol-card-body').boundingBox())!.height;
    state = 'silent';
    await page.reload();
    await expect(card.locator('.ol-card-body')).toHaveText('silent');
    await expect(card.locator('.ol-dot')).toHaveClass(/\bwarn\b/);
    expect((await card.locator('.ol-card-body').boundingBox())!.height).toBe(height);
    state = 'down';
    await page.reload();
    await expect(card).toHaveClass(/\berr\b/);
    await expect(card.locator('.ol-card-body')).toHaveText('not answering');
    // hmipserver starting: VirtualDevices is starting with it, never down
    await page.context().addCookies([{name: 'stub-starting', value: '1', url: baseURL!}]);
    await page.reload();
    await expect(card).toHaveAttribute('data-state', 'starting');
    await expect(card.locator('.ol-card-body')).toHaveText(/^starting/);
});

test('no VirtualDevices card without the interface', async ({page}) => {
    await patchHealth(page, (h) => {
        h.feed.interfaces = h.feed.interfaces.filter((i) => i.name !== 'VirtualDevices');
        delete h.virtual_devices_via;
    });
    await page.goto('/');
    await expect(page.locator('.ol-components [data-component="HmIP-RF"]')).toBeVisible();
    await expect(page.locator('.ol-components [data-component="VirtualDevices"]')).toHaveCount(0);
});

test("BidCos-Wired's subtitle names its LAN gateways", async ({page}) => {
    await patchHealth(page, (h) => {
        h.answering = ['BidCos-Wired'];
        h.wired_gateways = [{name: 'HMW-LGW', serial: 'LEQ0000001', address: '192.0.2.57'}, {serial: 'LEQ0000002'}];
    });
    await page.goto('/');
    const card = page.locator('.ol-components [data-component="BidCos-Wired"]');
    // the stub's units have no hs485d: the title is the name alone
    await expect(card.locator('.ol-if-title')).toHaveText('BidCos-Wired');
    await expect(card.locator('.ol-card-body')).toHaveText('up');
    const subs = card.locator('.ol-card-sub a');
    await expect(subs).toHaveText(['HMW-LGW · 192.0.2.57', 'HMW-LGW · LAN']);
    await expect(subs.first()).toHaveAttribute('title', 'HMW-LGW · LEQ0000001 · 192.0.2.57');
    await expect(card.locator('.ol-card-detail')).toHaveText('LEQ0000001 · LEQ0000002');
    await subs.first().click();
    await expect(page).toHaveURL(/\/system\/lan-devices$/);
});

test('BidCos-Wired without a gateway has no subtitle; not answering keeps it', async ({page}) => {
    let gws: Health['wired_gateways'] = [];
    let down = false;
    await patchHealth(page, (h) => {
        if (down) h.errors = {'BidCos-Wired': 'dial tcp 127.0.0.1:32000: connect: connection refused'};
        else h.answering = ['BidCos-Wired'];
        h.wired_gateways = gws;
    });
    await page.goto('/');
    const card = page.locator('.ol-components [data-component="BidCos-Wired"]');
    await expect(card.locator('.ol-card-body')).toHaveText('up');
    await expect(card.locator('.ol-card-sub, .ol-card-detail')).toHaveCount(0);
    gws = [{serial: 'LEQ0000001', address: '192.0.2.57'}];
    down = true;
    await page.reload();
    await expect(card.locator('.ol-card-body')).toHaveText('not answering');
    await expect(card.locator('.ol-card-sub a')).toHaveText('HMW-LGW · 192.0.2.57');
});

test('the new cards in German, in both themes, on a phone', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await patchHealth(page, (h) => {
        h.answering = ['BidCos-Wired'];
        h.wired_gateways = [{name: 'HMW-LGW', serial: 'LEQ0000001', address: '192.0.2.57'}];
        h.virtual_devices_via = ['BidCos-RF', 'HmIP-RF', 'BidCos-Wired'];
    });
    for (const scheme of ['light', 'dark'] as const) {
        await page.emulateMedia({colorScheme: scheme});
        await page.setViewportSize({width: 360, height: 780});
        await page.goto('/');
        const vd = page.locator('.ol-components [data-component="VirtualDevices"]');
        await expect(vd.locator('.ol-card-body')).toHaveText('aktiv');
        await expect(vd.locator('.ol-card-sub a')).toHaveText('via BidCos-RF, HmIP-RF, BidCos-Wired');
        await expect(vd.locator('.ol-card-sub a')).toHaveAttribute('title', 'VirtualDevices hat die Ereignisse dieser Schnittstellen abonniert.');
        await expect(page.locator('[data-component="BidCos-Wired"] .ol-card-sub a')).toHaveText('HMW-LGW · 192.0.2.57');
        // nothing wider than the phone
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);
        const link = vd.locator('.ol-card-sub a');
        const [fg, bg] = await Promise.all([link.evaluate((e) => getComputedStyle(e).color), page.evaluate(() => getComputedStyle(document.body).backgroundColor)]);
        expect(fg).not.toBe(bg);
    }
});
