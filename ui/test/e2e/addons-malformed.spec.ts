import {expect, test, type Page} from './fixtures';

// occulited B-157: the Addons page with answers it does not expect - catalogue entries and installed
// addons with missing fields, fields of the wrong type, unknown kinds, an id twice, a list that is no
// list, an install run it cannot draw. Each entry that cannot be a card becomes a card that says why
// (and the console names it); fields of the wrong type are dropped; every other card stays.

// a value that cannot be turned into text: interpolating it throws, as an unexpected object would
const UNPRINTABLE = {toString: 1, valueOf: 1};

async function malformed(page: Page) {
    await page.route('**/api/system/v1/catalog', async (route) => {
        const res = await route.fetch();
        const body = await res.json();
        body.catalog.addons.push(
            null,
            'text',
            {git: 'https://github.com/x/numeric-id', id: 5},
            {manifest_path: 'nothing-else.json'},
            {git: 'https://github.com/x/odd-fields', id: 'odd-fields', name: 5, description: ['x'], stars: 'many', requires: {architectures: 'arm64'}, latest: {version: 2}, update_available: 'yes', release: 'x', images: {logo: 7}, runtime: {note: {de: 1}}, unknown_kind: {deep: [1, 2]}},
        );
        await route.fulfill({response: res, json: body});
    });
    await page.route('**/api/system/v1/addons', async (route) => {
        const res = await route.fetch();
        const body = await res.json();
        const mosquitto = body.addons.find((a: {id: string}) => a.id === 'mosquitto');
        body.addons.push(
            {name: 'nameless'},
            7,
            structuredClone(mosquitto),
            {id: 'strange', name: 5, version: null, operations: 'restart', running: 'yes', enabled: true, payload_missing: true, payload_missing_dirs: 'www'},
        );
        await route.fulfill({response: res, json: body});
    });
}

test('malformed entries: a card each that says why, the other cards and the sections stay', async ({page}) => {
    const errors: string[] = [];
    page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()); });
    page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`));
    await malformed(page);
    await page.goto('/addons');
    // the well-formed cards with their actions
    await expect(page.locator('#addon-row-redmatic [data-addon-update]')).toBeVisible();
    await expect(page.locator('#addon-row-mosquitto')).toHaveCount(1);
    await expect(page.locator('[data-cards-error]')).toHaveCount(0);
    // the entry with fields of the wrong type is an ordinary card, named by its id
    const odd = page.locator('#addon-row-odd-fields');
    await expect(odd).not.toHaveAttribute('data-card-error', /.*/);
    await expect(odd.locator('.ad-name')).toHaveText('odd-fields');
    // the installed addon with wrong fields is an ordinary card too, and in the reinstall list
    await expect(page.locator('#addon-row-strange')).not.toHaveAttribute('data-card-error', /.*/);
    await expect(page.locator('[data-section="reinstall"]')).toContainText('strange');
    // a card for each entry that is none, with the reason
    const broken = page.locator('[data-card-error="read"]');
    await expect(broken).toHaveCount(6);
    await expect(page.locator('#addon-row-catalogue-entry-7')).toContainText('the system sent an entry that is not an object');
    await expect(page.locator('#addon-row-repo-x-numeric-id')).toContainText('its id is not text');
    await expect(page.locator('#addon-row-catalogue-entry-10')).toContainText('it names neither an id nor a repository');
    await expect(page.locator('[data-addon-card^="installed-addon-"]').first()).toContainText('the system sent an installed addon without an id');
    // the sections below the list are there
    await expect(page.getByRole('heading', {name: 'Install addon'})).toBeVisible();
    expect(errors.some((e) => e.includes('addons: repo-x-numeric-id could not be read: id-not-text'))).toBe(true);
    expect(errors.filter((e) => e.startsWith('pageerror'))).toEqual([]);
});

test('German: the reason of a broken card', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await malformed(page);
    await page.goto('/addons');
    await expect(page.locator('#addon-row-repo-x-numeric-id')).toContainText('Die Karte dieser Zusatzsoftware konnte nicht angezeigt werden: ihre ID ist kein Text');
});

test('a catalogue list that is no list: one card says so, the installed addons stay', async ({page}) => {
    await page.route('**/api/system/v1/catalog', async (route) => {
        const res = await route.fetch();
        const body = await res.json();
        body.catalog.addons = {mosquitto: {}};
        await route.fulfill({response: res, json: body});
    });
    await page.goto('/addons');
    await expect(page.locator('#addon-row-catalogue-entry')).toContainText('the system sent no list');
    await expect(page.locator('#addon-row-mosquitto')).toBeVisible();
    await expect(page.locator('[data-cards-error]')).toHaveCount(0);
});

test('an install run the page cannot draw: a notice, the cards stay', async ({page}) => {
    const errors: string[] = [];
    page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()); });
    await page.route('**/api/system/v1/catalog/progress', (route) =>
        route.fulfill({json: {progress: {addon_id: UNPRINTABLE, phase: 'downloading', started: '2026-11-22T10:00:00Z'}, busy: null}}),
    );
    await page.goto('/addons');
    await expect(page.locator('[data-progress-error]')).toContainText('The install progress could not be shown:');
    await expect(page.locator('#addon-row-redmatic')).toBeVisible();
    expect(errors.some((e) => e.includes('addons: the install progress could not be rendered'))).toBe(true);
});
