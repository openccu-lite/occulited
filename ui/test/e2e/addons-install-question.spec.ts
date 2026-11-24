import {expect, test, type Page} from './fixtures';

// occulited B-174: the install question says what the system will do. It said "runs as root and
// can do anything" for every addon - also for Logic, whose card said it needs no extra right and
// which runs confined. Now root only for an addon that will run as root, its own user for a
// confined one, and that it is not known when no manifest was read (the API's install_mode).

async function withMode(page: Page, mode: 'root' | 'confined' | undefined) {
    await page.route('**/api/system/v1/catalog', async (route) => {
        const res = await route.fetch();
        const body = await res.json();
        for (const e of body.catalog.addons) {
            if (e.id !== 'xml-api') continue;
            if (mode) e.install_mode = mode;
            else delete e.install_mode;
        }
        await route.fulfill({response: res, json: body});
    });
}

for (const [mode, en, de] of [
    ['root', 'runs as root and can do anything on this system', 'läuft als root und kann auf diesem System alles'],
    ['confined', 'runs as its own user and can write only to its own directories', 'läuft als eigener Benutzer'],
    [undefined, 'whether it runs as its own user or as root is not known before the install', 'ob sie als eigener Benutzer oder als root läuft, ist vor der Installation nicht bekannt'],
] as const) {
    test(`the install question for ${mode ?? 'an unknown mode'}`, async ({page}) => {
        await withMode(page, mode);
        let posted = false;
        await page.route('**/api/system/v1/catalog/xml-api/install', (route) => {
            posted = true;
            return route.fulfill({status: 202, json: {}});
        });
        await page.goto('/addons');
        await page.locator('#addon-row-xml-api [data-addon-install]').click();
        const dialog = page.getByRole('dialog');
        await expect(dialog).toContainText(en);
        if (mode !== 'root') await expect(dialog).not.toContainText('as root and can do anything');
        await dialog.getByRole('button', {name: 'Cancel'}).click();
        await expect(dialog).toHaveCount(0);
        expect(posted).toBe(false);

        await page.evaluate(() => localStorage.setItem('ol.language', 'de'));
        await page.reload();
        await page.locator('#addon-row-xml-api [data-addon-install]').click();
        await expect(page.getByRole('dialog')).toContainText(de);
    });
}
