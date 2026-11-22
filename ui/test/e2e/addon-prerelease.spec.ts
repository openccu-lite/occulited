import {expect, test} from './fixtures';
import type {Page} from '@playwright/test';

// occulited task 171: a pre-release (a version with a prerelease part, or one GitHub flags) carries
// an orange pill beside the version it belongs to - on a catalogue card, beside an installed
// addon's version, beside the offered update - saying Beta, RC, Alpha or Pre-release, with its
// meaning behind it; the install and update question and the Update all list say it too. The pill
// keeps its box, and it stays inside the card at phone width.

const card = (page: Page, id: string) => page.locator(`#addon-row-${id}`);

async function catalogWith(page: Page, change: (e: Record<string, unknown>) => void) {
    await page.route('**/api/system/v1/catalog', async (route) => {
        if (route.request().method() !== 'GET') return route.fallback();
        const response = await route.fetch();
        const j = await response.json();
        for (const e of j.catalog.addons as Record<string, unknown>[]) change(e);
        await route.fulfill({response, json: j});
    });
}

test('the offered RC of an addon not installed: the pill beside the version, its meaning behind it', async ({page}, info) => {
    await page.goto('/addons');
    const tm = card(page, 'tm-devices');
    const pill = tm.locator('.ad-sub .ad-pre');
    await expect(pill).toHaveText('RC');
    await expect(pill).toHaveClass(/\bpre\b/);
    // the pill sits on the version's line, which stays one line on a desktop
    const sub = await tm.locator('.ad-sub').boundingBox();
    const ver = await tm.locator('.ad-version').boundingBox();
    const box = await pill.boundingBox();
    expect(sub && ver && box).toBeTruthy();
    expect(Math.abs(box!.y + box!.height / 2 - (ver!.y + ver!.height / 2))).toBeLessThan(4);
    if (info.project.name !== 'phone') expect(sub!.height).toBeLessThan(2 * ver!.height);
    // inside the card, also on a phone
    const c = await tm.boundingBox();
    expect(box!.x + box!.width).toBeLessThanOrEqual(c!.x + c!.width);
    // the meaning on a click (a phone has no hover), and the pill keeps its box while it is open
    await pill.click();
    await expect(tm.getByText('Pre-release: not all functions have been tested yet.')).toBeVisible();
    const open = (await pill.boundingBox())!;
    expect([open.width, open.height]).toEqual([box!.width, box!.height]);
    // a stable version carries none
    await expect(card(page, 'mosquitto').locator('.ad-pre')).toHaveCount(0);
    await expect(card(page, 'redmatic').locator('.ad-pre')).toHaveCount(0);
});

test('an installed beta, and a pre-release offered as the update: both marked, the question says it', async ({page}) => {
    await catalogWith(page, (e) => {
        if (e.id === 'redmatic') e.latest = {version: '9.5.0-beta.1', asset: 'redmatic-x86_64-9.5.0-beta.1.tar.gz', prerelease: true};
    });
    await page.goto('/addons');
    // hm2mqtt runs 3.6.0-beta
    await expect(card(page, 'hm2mqtt').locator('.ad-sub .ad-pre').first()).toHaveText('Beta');
    const red = card(page, 'redmatic');
    await expect(red.locator('.ol-upd')).toHaveText('→ 9.5.0-beta.1');
    await expect(red.locator('.ad-sub .ad-pre')).toHaveText(['Beta']);
    await red.locator('[data-addon-update]').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('9.5.0-beta.1 is a pre-release: not all functions have been tested yet.');
    await dialog.getByRole('button', {name: 'Cancel'}).click();
    await expect(dialog).toHaveCount(0);
});

test('Update all names the pre-release in its list', async ({page}) => {
    await catalogWith(page, (e) => {
        if (e.id === 'mosquitto') {
            e.latest = {version: '2.2.0-rc.1', asset: 'mosquitto-2.2.0-rc.1.tar.gz', prerelease: true};
            e.update_available = true;
        }
    });
    await page.goto('/addons');
    await expect(card(page, 'mosquitto').locator('.ad-sub .ad-pre')).toHaveText('RC');
    await page.locator('[data-update-all]').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Mosquitto: 2.1.2+3 → 2.2.0-rc.1 (RC)');
    await expect(dialog).toContainText('RedMatic: 9.4.0 → 9.4.1');
    await expect(dialog).not.toContainText('9.4.1 (');
    await dialog.getByRole('button', {name: 'Cancel'}).click();
});

test('in German, and a release GitHub flags without a prerelease part is a Vorabversion', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await catalogWith(page, (e) => {
        if (e.id === 'tm-devices') e.latest = {version: '1.0.0', asset: 'tm-devices-1.0.0.tgz', prerelease: true};
    });
    await page.goto('/addons');
    const pill = card(page, 'tm-devices').locator('.ad-sub .ad-pre');
    await expect(pill).toHaveText('Vorabversion');
    await pill.click();
    await expect(card(page, 'tm-devices').getByText('Vorabversion: noch nicht alle Funktionen sind getestet.')).toBeVisible();
    await expect(card(page, 'hm2mqtt').locator('.ad-sub .ad-pre').first()).toHaveText('Beta');
});
