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
