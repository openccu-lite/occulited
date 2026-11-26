import {expect, test} from './fixtures';
import type {Page} from '@playwright/test';

// occulited B-181: "Reinstall from the catalogue" replaced the CCU3's RedMatic 10.0.0-beta.0 with
// the stable 9.20.0 - a downgrade - after a question that named no version. The question names the
// installed version and the one that comes; a downgrade is said plainly, Keep is the default (Enter
// keeps it) and Downgrade is in the warning colour; an unknown installed version says it may be one.

const card = (page: Page, id: string) => page.locator(`#addon-row-${id}`);

/** RedMatic installed at `version` ('' for one whose version cannot be read); the catalogue offers 9.4.1 and no update */
async function redmaticAt(page: Page, version: string, posts: string[]) {
    await page.route('**/api/system/v1/addons', async (r) => {
        if (r.request().method() !== 'GET') return r.fallback();
        const res = await r.fetch();
        const body = await res.json();
        body.addons = body.addons.map((a: {id: string}) => (a.id === 'redmatic' ? {...a, version} : a));
        await r.fulfill({response: res, json: body});
    });
    await page.route('**/api/system/v1/catalog', async (r) => {
        if (r.request().method() !== 'GET') return r.fallback();
        const res = await r.fetch();
        const j = await res.json();
        for (const e of j.catalog.addons as Record<string, unknown>[]) if (e.id === 'redmatic') e.update_available = false;
        j.installed = {...j.installed, redmatic: version};
        await r.fulfill({response: res, json: j});
    });
    await page.route('**/api/system/v1/catalog/redmatic/install', (r) => {
        posts.push('install redmatic');
        return r.fulfill({status: 202, json: {ok: true, addon_id: 'redmatic'}});
    });
}

async function openReinstall(page: Page) {
    const red = card(page, 'redmatic');
    await red.getByRole('button', {name: 'More actions'}).click();
    await red.locator('[data-addon-reinstall-catalogue]').click();
    return page.getByRole('dialog');
}

test('the same version: both versions named, an ordinary Reinstall that installs', async ({page}) => {
    const posts: string[] = [];
    await redmaticAt(page, '9.4.1', posts);
    await page.goto('/addons');
    await expect(card(page, 'redmatic').locator('[data-addon-update]')).toHaveCount(0);
    const dialog = await openReinstall(page);
    await expect(dialog.getByRole('heading')).toHaveText('Reinstall RedMatic');
    await expect(dialog).toContainText('Reinstall RedMatic from the catalogue? The addon replaces its own installation; its configuration is kept.');
    await expect(dialog).toContainText('Installed: 9.4.1');
    await expect(dialog).toContainText('From the catalogue: 9.4.1');
    await expect(dialog).not.toContainText('downgrade');
    // never the update question ("Update RedMatic from 9.4.1 to 9.4.1?")
    await expect(dialog).not.toContainText('Update RedMatic');
    const ok = dialog.getByRole('button', {name: 'Reinstall', exact: true});
    await expect(ok).not.toHaveClass(/\bdanger\b/);
    await ok.click();
    await expect.poll(() => posts).toEqual(['install redmatic']);
    await page.unrouteAll({behavior: 'wait'});
});

test('a downgrade: the warning, Keep is the default, Downgrade in the warning colour', async ({page}) => {
    const posts: string[] = [];
    await redmaticAt(page, '10.0.0-beta.0', posts);
    await page.goto('/addons');
    let dialog = await openReinstall(page);
    await expect(dialog).toContainText('Installed: 10.0.0-beta.0');
    await expect(dialog).toContainText('From the catalogue: 9.4.1');
    await expect(dialog).toContainText("9.4.1 is older than the installed 10.0.0-beta.0: this reinstall is a downgrade, and an older version may not read what the newer one stored. The catalogue offers no newer version; a newer one comes through the addon's own update or an upload of its package.");
    const keep = dialog.getByRole('button', {name: 'Keep 10.0.0-beta.0'});
    const down = dialog.getByRole('button', {name: 'Downgrade to 9.4.1'});
    await expect(keep).toBeFocused();
    await expect(down).toHaveClass(/\bdanger\b/);
    // Enter keeps the installed version: nothing is installed
    await page.keyboard.press('Enter');
    await expect(dialog).toHaveCount(0);
    expect(posts).toEqual([]);
    // the downgrade takes the explicit button
    dialog = await openReinstall(page);
    await dialog.getByRole('button', {name: 'Downgrade to 9.4.1'}).click();
    await expect.poll(() => posts).toEqual(['install redmatic']);
    await page.unrouteAll({behavior: 'wait'});
});

test('an installed version that cannot be read: said, and that it may be a downgrade', async ({page}) => {
    const posts: string[] = [];
    await redmaticAt(page, '', posts);
    await page.goto('/addons');
    const dialog = await openReinstall(page);
    await expect(dialog).toContainText('Installed: unknown version');
    await expect(dialog).toContainText('From the catalogue: 9.4.1');
    await expect(dialog).toContainText('The installed version is not known; if it is newer than 9.4.1, this reinstall is a downgrade.');
    await expect(dialog.getByRole('button', {name: 'Reinstall', exact: true})).toBeVisible();
    await dialog.getByRole('button', {name: 'Cancel'}).click();
    expect(posts).toEqual([]);
    await page.unrouteAll({behavior: 'wait'});
});

test('in German: the versions, the downgrade warning and the buttons', async ({page}) => {
    const posts: string[] = [];
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await redmaticAt(page, '10.0.0-beta.0', posts);
    await page.goto('/addons');
    const red = card(page, 'redmatic');
    await red.getByRole('button', {name: 'Weitere Aktionen'}).click();
    await red.locator('[data-addon-reinstall-catalogue]').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByRole('heading')).toHaveText('RedMatic neu installieren');
    await expect(dialog).toContainText('RedMatic aus dem Katalog neu installieren? Das Addon ersetzt seine eigene Installation; seine Konfiguration bleibt erhalten.');
    await expect(dialog).toContainText('Installiert: 10.0.0-beta.0');
    await expect(dialog).toContainText('Aus dem Katalog: 9.4.1');
    await expect(dialog).toContainText('9.4.1 ist älter als die installierte 10.0.0-beta.0: Diese Neuinstallation ist ein Downgrade');
    await expect(dialog.getByRole('button', {name: '10.0.0-beta.0 behalten'})).toBeFocused();
    await expect(dialog.getByRole('button', {name: 'Auf 9.4.1 zurückgehen'})).toBeVisible();
    await page.keyboard.press('Escape');
    expect(posts).toEqual([]);
    await page.unrouteAll({behavior: 'wait'});
});

test('after a restore: a reinstall that would downgrade warns there too', async ({page}) => {
    const posts: string[] = [];
    await redmaticAt(page, '10.0.0-beta.0', posts);
    await page.route('**/api/system/v1/addons', async (r) => {
        if (r.request().method() !== 'GET') return r.fallback();
        const res = await r.fetch();
        const body = await res.json();
        body.addons = body.addons.map((a: {id: string}) => (a.id === 'redmatic' ? {...a, version: '10.0.0-beta.0', running: false, payload_missing: true, payload_missing_dirs: ['bin']} : a));
        await r.fulfill({response: res, json: body});
    });
    await page.goto('/addons');
    await page.locator('[data-reinstall="redmatic"] [data-action="reinstall"]').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Reinstall RedMatic 9.4.1 from the catalogue? The program files come back; the settings and data the restore brought stay.');
    await expect(dialog).toContainText('Installed: 10.0.0-beta.0');
    await expect(dialog).toContainText('this reinstall is a downgrade');
    await expect(dialog.getByRole('button', {name: 'Keep 10.0.0-beta.0'})).toBeFocused();
    await dialog.getByRole('button', {name: 'Keep 10.0.0-beta.0'}).click();
    expect(posts).toEqual([]);
    await page.unrouteAll({behavior: 'wait'});
});
