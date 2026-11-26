import type {Page} from '@playwright/test';
import {expect, test} from './fixtures';

// occulited task 35 (openccu-lite B-316): the check of a USB stick that was not unmounted cleanly -
// the switch on the Storage page (off by default), what the mount found per partition, and the
// Status warning of a stick that was not checked or whose check failed, pointing to the switch.

async function own(page: Page, baseURL: string | undefined) {
    await page.context().addCookies([{name: 'stub-usb', value: `c-${Math.random().toString(36).slice(2)}`, url: baseURL!}]);
}

/** the stub's list, with a record on sda1 */
async function withCheck(page: Page, check: Record<string, unknown>) {
    await page.route('**/api/system/v1/storage/usb', async (r) => {
        const res = await r.fetch();
        const v = await res.json();
        v.disks[0].partitions[0].check = check;
        await r.fulfill({json: v});
    });
}

test('the switch: off by default, one click switches it on and off, the row keeps its size', async ({page, baseURL}) => {
    await own(page, baseURL);
    await page.goto('/system/storage');
    const box = page.locator('[data-action="usb-fsck"]');
    const row = page.locator('[data-usb-check] label');
    await expect(row).toContainText("Check and repair a USB stick's file system when it was not unmounted properly");
    await expect(page.locator('[data-usb-check]')).toContainText('the services that use it wait for it');
    await expect(box).not.toBeChecked();
    const before = (await row.boundingBox())!;
    const put = page.waitForRequest((r) => r.method() === 'PUT' && r.url().endsWith('/api/system/v1/storage/usb/settings'));
    await box.check();
    expect((await put).postDataJSON()).toEqual({fsck_at_dirty_mount: true});
    await expect(box).toBeChecked();
    await expect(box).toBeEnabled();
    const after = (await row.boundingBox())!;
    expect(after.width).toBeCloseTo(before.width, 0);
    expect(after.height).toBeCloseTo(before.height, 0);
    await page.reload();
    await expect(box).toBeChecked();
    await box.uncheck();
    await expect(box).not.toBeChecked();
    await page.reload();
    await expect(box).not.toBeChecked();
});

test('what the mount found: unchecked and failed as warnings, a repair muted', async ({page, baseURL}) => {
    await own(page, baseURL);
    await withCheck(page, {state: 'unchecked', fstype: 'exfat', at: '2026-11-26T12:00:00Z'});
    await page.goto('/system/storage');
    const line = page.locator('[data-part="sda1"] [data-part-check]');
    await expect(line).toHaveText('not unmounted properly - not checked at this mount');
    await expect(line).toHaveClass(/ol-warn/);
    await page.unroute('**/api/system/v1/storage/usb');
    await withCheck(page, {state: 'repaired', fstype: 'exfat', exit: 1});
    await page.reload();
    await expect(line).toHaveText('not unmounted properly - checked and repaired at the mount');
    await expect(line).toHaveClass(/ol-muted/);
    await page.unroute('**/api/system/v1/storage/usb');
    await withCheck(page, {state: 'failed', fstype: 'exfat', exit: 4});
    await page.reload();
    await expect(line).toHaveText('not unmounted properly - the check at the mount ended with an error (exit 4); mounted anyway');
    await expect(line).toHaveClass(/ol-warn/);
});

test('German: the switch and the record', async ({page, baseURL}) => {
    await own(page, baseURL);
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await withCheck(page, {state: 'clean', fstype: 'vfat', exit: 0});
    await page.goto('/system/storage');
    await expect(page.locator('[data-usb-check] label')).toContainText('Dateisystem eines USB-Sticks prüfen und reparieren, wenn er nicht sauber ausgehängt wurde');
    await expect(page.locator('[data-part="sda1"] [data-part-check]')).toHaveText('nicht sauber ausgehängt - beim Einhängen geprüft, nichts zu reparieren');
});

const STATUS_WARNING = (fsck: boolean, sticks: unknown[]) => ({
    warnings: [{id: 'usb-unchecked', variant: 'sda1:unchecked:1795700000', severity: 'warning', href: '/system/storage#usb', params: {sticks, fsck}}],
    periods: [1, 7, 90],
});

for (const lang of [
    {code: 'en', off: 'USB stick not unmounted properly and not checked: LOGSTICK (sda1). Check it on a PC, or switch on the automatic check on the Storage page.', on: 'The automatic check is on now and runs at its next mount', failed: 'The check of the USB stick BACKUP (sdb1) ended with an error (exit 4); the stick is mounted anyway.', link: 'Storage'},
    {code: 'de', off: 'USB-Stick nicht sauber ausgehängt und nicht geprüft: LOGSTICK (sda1). Prüfen Sie ihn an einem PC, oder schalten Sie auf der Seite Speicher die automatische Prüfung ein.', on: 'Die automatische Prüfung ist jetzt eingeschaltet', failed: 'Die Prüfung des USB-Sticks BACKUP (sdb1) endete mit einem Fehler (Exit 4); der Stick ist trotzdem eingehängt.', link: 'Speicher'},
]) {
    test(`Status: the warning points to the switch (${lang.code})`, async ({page}) => {
        await page.addInitScript((c) => localStorage.setItem('ol.language', c), lang.code);
        let fsck = false;
        await page.route('**/api/system/v1/warnings', (r) => r.fulfill({json: STATUS_WARNING(fsck, [{name: 'sda1', label: 'LOGSTICK', state: 'unchecked'}, {name: 'sdb1', label: 'BACKUP', state: 'failed', exit: 4}])}));
        await page.goto('/');
        const w = page.locator('[data-notice="usb-unchecked"]');
        await expect(w).toContainText(lang.off);
        await expect(w).toContainText(lang.failed);
        const link = w.getByRole('link', {name: lang.link, exact: true});
        await expect(link).toHaveAttribute('href', '/system/storage#usb');
        fsck = true;
        await page.reload();
        await expect(w).toContainText(lang.on);
    });
}
