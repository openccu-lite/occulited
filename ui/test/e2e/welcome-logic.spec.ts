import {expect, test} from './fixtures';

// occulited task 168: the welcome page offers lite-addon-logic in one click beside Homematic
// Manager - only when the catalogue lists it - and right after its install, while the CCU's ReGa
// database is kept (task 167), the import of the programs and system variables: the addon's page
// its manifest names (ui.regadom_import_url).

const LOGIC = 'https://github.com/openccu-lite/lite-addon-logic';
const HM = {git: 'https://github.com/hobbyquaker/homematic-manager', manifest_path: 'x/openccu-lite.json', format: 1, id: 'hmm', name: 'Homematic Manager', latest: {version: '3.1.3'}};
const LOGIC_ENTRY = {git: LOGIC, manifest_path: 'openccu-lite.json', format: 1, id: 'logic', name: {de: 'Programme und Systemvariablen', en: 'Programs and system variables'}, latest: {version: '1.0.0'}};

test('not in the catalogue: no logic step', async ({page}) => {
    await page.goto('/welcome');
    await expect(page.locator('[data-welcome-frontend]')).toBeVisible();
    await expect(page.locator('[data-welcome-logic]')).toHaveCount(0);
});

test('listed: one click installs, then the import of the kept ReGa database is offered', async ({page}) => {
    let installed: Record<string, string> = {hmm: '3.1.3'};
    let finished = false;
    const posts: string[] = [];
    await page.route(/\/api\/system\/v1\/catalog(\?refresh=1)?$/, (route) => route.fulfill({json: {catalog: {format: 1, source: 'published', addons: [HM, LOGIC_ENTRY]}, installed, arch: 'x86_64', daily: false}}));
    await page.route('**/api/system/v1/catalog/*/install', (route) => {
        posts.push(new URL(route.request().url()).pathname);
        return route.fulfill({status: 202, json: {started: true}});
    });
    await page.route('**/api/system/v1/catalog/progress', (route) => {
        if (posts.length === 0) return route.fulfill({json: {progress: null, busy: null}});
        if (!finished) {
            finished = true;
            return route.fulfill({json: {progress: {addon_id: 'logic', phase: 'installing', started: new Date().toISOString()}, busy: {kind: 'catalog', addon_id: 'logic', started: new Date().toISOString()}}});
        }
        installed = {...installed, logic: '1.0.0'};
        return route.fulfill({json: {progress: {addon_id: 'logic', phase: 'done', started: new Date().toISOString(), finished: new Date().toISOString(), result: {exit: 0}}, busy: null}});
    });
    await page.route('**/api/system/v1/regadom', (route) => route.fulfill({json: {present: true, bytes: 1048576, modified: '2026-11-01T12:00:00Z'}}));
    await page.route('**/api/system/v1/addons', (route) => route.fulfill({json: {addons: [{id: 'logic', name: 'Programs and system variables', version: '1.0.0', regadom_import_url: '/addons/logic/#/import'}]}}));
    await page.goto('/welcome');
    const sec = page.locator('[data-welcome-logic]');
    await expect(sec.getByRole('heading', {name: /Programs and system variables$/})).toBeVisible();
    await expect(sec).toHaveAttribute('data-state', 'ready');
    const button = sec.locator('[data-action="welcome-logic"]');
    await expect(button.locator('.wl-live')).toHaveText('Install the programs and system variables');
    await expect(sec.locator('[data-action="welcome-logic-import"]')).toHaveCount(0);
    const before = (await button.boundingBox())!;
    await button.click();
    await expect.poll(() => posts).toEqual(['/api/system/v1/catalog/logic/install']);
    await expect(sec).toHaveAttribute('data-state', 'installed', {timeout: 15_000});
    await expect(button.locator('.wl-live')).toHaveText('Open the programs and system variables');
    const after = (await button.boundingBox())!;
    expect(after.width).toBeCloseTo(before.width, 0);
    const imp = sec.locator('[data-action="welcome-logic-import"]');
    await expect(imp).toHaveText('Import programs and system variables');
    await expect(imp).toHaveAttribute('href', '/addons/logic/#/import');
    await expect(imp).toHaveAttribute('target', '_blank');
    await expect(sec.locator('[data-welcome-logic-status]')).toContainText('can be imported now');
    // Homematic Manager's step does not show the other addon's run
    await expect(page.locator('[data-welcome-frontend] .ol-install-progress')).toHaveCount(0);
});

test('installed without a kept database: no import; German', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await page.route(/\/api\/system\/v1\/catalog(\?refresh=1)?$/, (route) => route.fulfill({json: {catalog: {format: 1, source: 'published', addons: [HM, LOGIC_ENTRY]}, installed: {hmm: '3.1.3', logic: '1.0.0'}, arch: 'x86_64', daily: false}}));
    await page.goto('/welcome');
    const sec = page.locator('[data-welcome-logic]');
    await expect(sec).toHaveAttribute('data-state', 'installed');
    await expect(sec.getByRole('heading', {name: /Programme und Systemvariablen$/})).toBeVisible();
    await expect(sec.locator('[data-action="welcome-logic"] .wl-live')).toHaveText('Programme und Systemvariablen öffnen');
    await expect(sec.locator('[data-action="welcome-logic-import"]')).toHaveCount(0);
    await sec.locator('[data-action="welcome-logic"]').click();
    await expect(page).toHaveURL(/\/nav\/logic$/);
});

// B-179: the intro counts the numbered steps the page really shows - with Logic's step one more
// than without it - and the last heading's number is that count
const WORDS: Record<string, number> = {Three: 3, Four: 4, Five: 5, Six: 6, Seven: 7, Drei: 3, Vier: 4, 'Fünf': 5, Sechs: 6, Sieben: 7};
async function counted(page: import('@playwright/test').Page) {
    const intro = (await page.locator('[data-welcome-intro]').textContent()) ?? '';
    const word = Object.keys(WORDS).find((w) => intro.includes(` ${w} `));
    expect(word, intro).toBeTruthy();
    const nums = (await page.locator('h2').allTextContents()).map((h) => /^(\d+) ·/.exec(h.trim())?.[1]).filter(Boolean).map(Number);
    return {said: WORDS[word!], shown: nums.length, last: Math.max(...nums)};
}

for (const lang of ['en', 'de']) {
    test(`the intro's count matches the numbered steps, with and without Logic (${lang})`, async ({page}) => {
        if (lang === 'de') await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
        await page.goto('/welcome');
        await expect(page.locator('[data-welcome-frontend]')).toBeVisible();
        const without = await counted(page);
        expect(without.said).toBe(without.shown);
        expect(without.last).toBe(without.shown);

        await page.route(/\/api\/system\/v1\/catalog(\?refresh=1)?$/, (route) => route.fulfill({json: {catalog: {format: 1, source: 'published', addons: [HM, LOGIC_ENTRY]}, installed: {hmm: '3.1.3'}, arch: 'x86_64', daily: false}}));
        await page.reload();
        await expect(page.locator('[data-welcome-logic]')).toBeVisible();
        await expect.poll(async () => (await counted(page)).said).toBe(without.said + 1);
        const withLogic = await counted(page);
        expect(withLogic.shown).toBe(without.shown + 1);
        expect(withLogic.last).toBe(withLogic.shown);
    });
}

test('German: Homematic-Scripts, never Skript or Scripte (B-179)', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await page.goto('/welcome');
    const about = page.locator('.about');
    await expect(about).toContainText('Homematic-Scripts');
    await expect(page.locator('body')).not.toContainText(/Skript|Scripte\b/);
});
