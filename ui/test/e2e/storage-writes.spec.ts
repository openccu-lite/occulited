import {expect, test, type Page} from '@playwright/test';
import {fitsWindow, pageWidth} from './scroll';

// occulited task 159: System → Storage's disk writes - each disk's totals and rates (always), the
// services once the recording is on, the hint on a busy SD card - and the Services page's switch
// "record disk writes per service".

async function open(page: Page, baseURL: string | undefined, path: string, variant = '') {
    await page.context().addCookies([{name: 'stub-writes', value: `${variant}-${Math.random().toString(36).slice(2)}`, url: baseURL!}]);
    await page.goto(path);
}
const disk = (page: Page, name: string) => page.locator(`[data-writes-disk="${name}"]`);
const row = (page: Page, unit: string) => page.locator(`[data-writes-unit="${unit}"]`);

test('the disks without the recording: totals since the start, the last day and hour, the way to the switch', async ({page, baseURL}) => {
    await open(page, baseURL, '/system/storage');
    await expect(page.getByRole('heading', {level: 2, name: 'Disk writes'})).toBeVisible();
    const card = disk(page, 'mmcblk0');
    await expect(card.locator('.ol-card-title')).toHaveText('SD card');
    await expect(card.locator('.ol-card-sub')).toHaveText('SanDisk SC32G · mmcblk0');
    await expect(card.locator('[data-writes-total]')).toHaveText('7.5 GB in 12 d 4 h');
    await expect(card.locator('[data-writes-day]')).toHaveText('9.0 MB 4.5 MB/h · over 2 h');
    await expect(card.locator('[data-writes-hour]')).toHaveText('4.0 MB 4.0 MB/h');
    await expect(card.locator('[data-writes-part="mmcblk0p3"]')).toHaveText('7.5 GB mounted at /usr/local');
    await expect(card.locator('[data-writes-hint]')).toHaveCount(0);
    await expect(disk(page, 'sda').locator('.ol-card-title')).toHaveText('USB drive');
    const off = page.locator('[data-writes-state="off"]');
    await expect(off).toContainText('Disk writes per service are not recorded.');
    await expect(page.locator('[data-writes-table]')).toHaveCount(0);
    await off.getByRole('link', {name: 'Switch it on under System → Services'}).click();
    await expect(page).toHaveURL(/\/system\/services#io-accounting$/);
    await expect(page.locator('[data-setting="io-accounting"]')).toBeVisible();
    expect(await fitsWindow(page)).toBe(true);
});

test('the Services page switches the recording on and off, and the Storage page follows', async ({page, baseURL}) => {
    await open(page, baseURL, '/system/services');
    const setting = page.locator('[data-setting="io-accounting"]');
    const box = setting.getByRole('checkbox', {name: 'Record disk writes per service'});
    await expect(box).not.toBeChecked();
    const before = (await setting.boundingBox())!;
    const put = page.waitForRequest((r) => r.method() === 'PUT' && r.url().endsWith('/storage/writes/recording'));
    await box.check();
    expect((await put).postDataJSON()).toEqual({enabled: true});
    await expect(box).toBeChecked();
    await expect(box).toBeEnabled();
    // the line keeps its size across the states
    const after = (await setting.boundingBox())!;
    expect(after.height).toBe(before.height);
    await setting.getByRole('link', {name: 'Shown under System → Storage'}).click();
    await expect(page).toHaveURL(/\/system\/storage#writes$/);
    await expect(page.locator('[data-writes-table]')).toBeVisible();
    await expect(row(page, 'addon-redmatic.service')).toBeVisible();
    await page.goto('/system/services');
    await page.locator('[data-setting="io-accounting"]').getByRole('checkbox').uncheck();
    await expect(page.locator('[data-setting="io-accounting"]').getByRole('checkbox')).not.toBeChecked();
    await page.goto('/system/storage');
    await expect(page.locator('[data-writes-state="off"]')).toBeVisible();
});

test('the services: the most written first, the span and the disk narrow the table', async ({page, baseURL}) => {
    await open(page, baseURL, '/system/storage', 'on');
    const table = page.locator('[data-writes-table]');
    await expect(table).toBeVisible();
    const names = () => table.locator('tbody tr').evaluateAll((trs) => trs.map((tr) => tr.getAttribute('data-writes-unit')));
    // the last 24 hours are the start
    await expect(page.locator('[data-writes-span="day"]')).toHaveAttribute('aria-pressed', 'true');
    expect(await names()).toEqual(['addon-redmatic.service', 'kernel', 'hmipserver.service', 'ended', 'local-backup.service']);
    const red = row(page, 'addon-redmatic.service');
    await expect(red.locator('td').nth(0)).toHaveText('addon-redmatic');
    await expect(red.getByRole('link', {name: 'addon-redmatic'})).toHaveAttribute('href', '/system/services#service-addon-redmatic');
    await expect(red.locator('td').nth(1)).toHaveText('40.5 MB');
    await expect(red.locator('td').nth(2)).toHaveText('20.3 MB/h');
    await expect(red.locator('td').nth(3)).toHaveText('mmcblk0 39.0 MB · sda 1.5 MB');
    await expect(row(page, 'kernel').locator('td').nth(0)).toHaveText('Kernel and file system journal');
    await expect(row(page, 'ended').locator('td').nth(0)).toHaveText('Ended runs, not attributable');
    await expect(row(page, 'local-backup.service').locator('td').nth(0)).toHaveText('local-backup · not running');
    await expect(row(page, 'local-backup.service').getByRole('link')).toHaveCount(0);
    await expect(page.locator('[data-writes-foot]')).toContainText('The history covers 2 h so far.');
    await expect(page.locator('[data-writes-foot]')).toContainText('2 more services wrote nothing in this time.');
    await expect(page.locator('[data-writes-foot]')).toContainText('Recorded for 2 h.');

    // the last hour: only what wrote in it; the buttons keep their width
    const bar = page.locator('.sw-bar .ol-seg').first();
    const width = (await bar.boundingBox())!.width;
    await page.locator('[data-writes-span="hour"]').click();
    expect(await names()).toEqual(['addon-redmatic.service', 'kernel']);
    await expect(red.locator('td').nth(1)).toHaveText('20.3 MB');
    expect((await bar.boundingBox())!.width).toBe(width);
    await expect(page.locator('[data-writes-foot]')).toContainText('4 more services wrote nothing in this time.');
    // everything recorded: no rate
    await page.locator('[data-writes-span="all"]').click();
    await expect(red.locator('td').nth(1)).toHaveText('61.5 MB');
    await expect(red.locator('td').nth(2)).toHaveText('—');
    expect((await bar.boundingBox())!.width).toBe(width);
    // one disk: the stick got RedMatic's 1.5 MB and nothing else
    await page.locator('[data-writes-diskpick="sda"]').click();
    expect(await names()).toEqual(['addon-redmatic.service']);
    await expect(red.locator('td').nth(1)).toHaveText('1.5 MB');
    await expect(table.locator('thead th')).toHaveCount(3);
    expect(await fitsWindow(page)).toBe(true);
});

test('a busy SD card carries a hint: with the top writers when recorded, with the way to the switch when not', async ({page, baseURL}) => {
    await open(page, baseURL, '/system/storage', 'hint-on');
    const hint = disk(page, 'mmcblk0').locator('[data-writes-hint]');
    await expect(hint).toContainText('On average 26.0 MB/h are written here');
    await expect(hint).toContainText('Most of it by: addon-redmatic, Kernel and file system journal.');
    await expect(disk(page, 'sda').locator('[data-writes-hint]')).toHaveCount(0);
    await open(page, baseURL, '/system/storage', 'hint');
    await expect(hint).toContainText('Which service writes it is shown once the recording is switched on:');
    await expect(hint.getByRole('link', {name: 'System → Services'})).toHaveAttribute('href', '/system/services#io-accounting');
});

test('a kernel without the io controller: no switch to use, and the Storage page says why', async ({page, baseURL}) => {
    await open(page, baseURL, '/system/services', 'unsupported');
    const setting = page.locator('[data-setting="io-accounting"]');
    await expect(setting.getByRole('checkbox')).toBeDisabled();
    await expect(setting.locator('[data-recording-state="unsupported"]')).toContainText('Not available');
    await page.goto('/system/storage');
    await expect(page.locator('[data-writes-state="unsupported"]')).toContainText('cannot record disk writes per service');
    await expect(disk(page, 'mmcblk0')).toBeVisible();
});

test('a user reads the figures and cannot switch', async ({page, baseURL}) => {
    await page.route('**/api/auth/v1/state', (r) => r.fulfill({json: {setup_required: false, authenticated: true, user: 'monitor', role: 'user', must_change_password: false, sid: 'U1'}}));
    await open(page, baseURL, '/system/services', 'on');
    await expect(page.locator('[data-setting="io-accounting"]').getByRole('checkbox')).toBeDisabled();
    await page.goto('/system/storage');
    await expect(row(page, 'addon-redmatic.service')).toBeVisible();
});

test('in German, on a phone, in both themes', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    for (const scheme of ['light', 'dark'] as const) {
        await page.emulateMedia({colorScheme: scheme});
        await page.setViewportSize({width: 360, height: 780});
        await open(page, baseURL, '/system/storage', 'hint-on');
        await expect(page.getByRole('heading', {level: 2, name: 'Schreibzugriffe'})).toBeVisible();
        const card = disk(page, 'mmcblk0');
        await expect(card.locator('.ol-card-title')).toHaveText('SD-Karte');
        await expect(card.locator('[data-writes-total]')).toHaveText('7.5 GB in 12 Tg. 4 Std.');
        await expect(card.locator('[data-writes-hint]')).toContainText('Im Schnitt werden hier 26.0 MB/Std. geschrieben');
        await expect(page.locator('[data-writes-span="day"]')).toHaveText('Letzte 24 Stunden');
        // a row is a block on a phone: the name on its own line, the figures labelled
        const red = row(page, 'addon-redmatic.service');
        await expect(red).toContainText('addon-redmatic');
        await expect(row(page, 'kernel')).toContainText('Kernel und Dateisystem-Journal');
        await expect(row(page, 'ended')).toContainText('Beendete Läufe, nicht zuordenbar');
        expect(await pageWidth(page)).toBeLessThanOrEqual(360);
        await open(page, baseURL, '/system/services', 'on');
        await expect(page.locator('[data-setting="io-accounting"]')).toContainText('Schreibzugriffe pro Dienst aufzeichnen');
        expect(await pageWidth(page)).toBeLessThanOrEqual(360);
    }
});
