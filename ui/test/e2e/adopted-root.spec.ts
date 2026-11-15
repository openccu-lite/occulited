import type {Page, Route} from '@playwright/test';
import {expect, test} from './fixtures';

// openccu-lite #23: an addon adopted from OpenCCU runs as root although the manifest in its own
// directory says it can run as its own user. The Status page names it in a quiet notice (info: no
// edge, no dot) with the install that gives it its own user, and its button leads to the Addons
// page, where the addon's root badge says the same; the badge keeps its words and its box.

const NOTICE = {
    id: 'addon-adopted-root', variant: 'mosquitto', severity: 'info', href: '/addons', since: new Date().toISOString(),
    params: {addons: [{id: 'mosquitto', name: 'Mosquitto', enabled: true, token: true}]},
};

async function only(page: Page, warnings: object[]) {
    await page.route('**/api/system/v1/warnings', async (route: Route) => {
        if (route.request().method() !== 'GET') return route.fallback();
        await route.fulfill({json: {warnings, periods: [1, 7, 90]}});
    });
}

// the addon list as a systemd system answers it for such an addon
async function adopted(page: Page, id: string, mark: boolean) {
    await page.route('**/api/system/v1/addons', async (route: Route) => {
        if (route.request().method() !== 'GET') return route.fallback();
        const response = await route.fetch();
        const j = await response.json();
        for (const a of j.addons ?? []) {
            if (a.id === id) Object.assign(a, {policy_mode: 'root', policy_source: 'migrated', undeclared: true, from_ccu: true}, mark ? {adopted_confinable: true, adopted_token: true} : {});
        }
        await route.fulfill({response, json: j});
    });
}

test('the Status page names the adopted root addon and the install that confines it', async ({page}) => {
    await only(page, [NOTICE]);
    await page.goto('/');
    const notice = page.locator('[data-warnings] [data-notice="addon-adopted-root"]');
    await expect(notice).toBeVisible();
    await expect(notice).toContainText('Mosquitto runs as root, although its manifest says it can run as its own user');
    await expect(notice).toContainText('Installing the addon again from its package (a file on the Addons page, or the catalogue) stores its manifest, issues the API token it declares and gives it its own user.');
    // a notice: no severity colour
    await expect(notice).not.toHaveAttribute('data-severity', /./);
    const link = notice.getByRole('link', {name: 'Addons'});
    await expect(link).toHaveAttribute('href', '/addons');
    await adopted(page, 'mosquitto', true);
    await link.click();
    await expect(page).toHaveURL(/\/addons$/);
    await expect(page.locator('#addon-row-mosquitto .ad-adopted')).toHaveText('root (unsafe)');
});

test('German', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await only(page, [NOTICE]);
    await page.goto('/');
    const notice = page.locator('[data-warnings] [data-notice="addon-adopted-root"]');
    await expect(notice).toContainText('Mosquitto läuft als root, obwohl das Manifest des Addons sagt, dass es unter eigenem Benutzer laufen kann');
    await expect(notice).toContainText('stellt das API-Token aus, das es deklariert, und gibt ihm einen eigenen Benutzer.');
});

test('the root badge keeps its box with and without the hint', async ({page}) => {
    await only(page, []);
    await adopted(page, 'mosquitto', false);
    await page.goto('/addons');
    const card = page.locator('#addon-row-mosquitto');
    const plain = card.locator('.ol-badge.warn', {hasText: 'root (unsafe)'}).first();
    await expect(plain).toBeVisible();
    await expect(card.locator('.ad-adopted')).toHaveCount(0);
    const before = (await plain.boundingBox())!;
    await page.unroute('**/api/system/v1/addons');
    await adopted(page, 'mosquitto', true);
    await page.reload();
    const marked = card.locator('.ad-adopted');
    await expect(marked).toHaveText('root (unsafe)');
    const after = (await marked.boundingBox())!;
    expect(after.width).toBeCloseTo(before.width, 0);
    expect(after.height).toBeCloseTo(before.height, 0);
    // its reason names the way out
    await marked.click();
    await expect(page.getByText('It came along from before openccu-lite, and its manifest says it can run as its own user')).toBeVisible();
});
