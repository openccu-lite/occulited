import {expect, test} from '@playwright/test';

// occulited task 167: the CCU's ReGa database stays after the switch (lite-addon-logic imports
// programs and system variables from it); the Backup page shows it while it is there, and its
// removal is the user's choice, asked first.

test('no panel without a kept database', async ({page}) => {
    await page.goto('/system/backup');
    await expect(page.locator('[data-section="backup-addons"]')).toBeVisible();
    await expect(page.locator('[data-section="regadom"]')).toHaveCount(0);
});

test('the kept database, and its removal after the question', async ({page}) => {
    let deleted = 0;
    await page.route('**/api/system/v1/regadom', async (r) => {
        if (r.request().method() === 'DELETE') {
            deleted++;
            await r.fulfill({json: {removed: true, freed_bytes: 23068672}});
            return;
        }
        await r.fulfill({json: {present: true, bytes: 23068672, modified: '2026-11-01T12:00:00Z'}});
    });
    await page.goto('/system/backup');
    const panel = page.locator('[data-section="regadom"]');
    await expect(panel.locator('[data-regadom="present"]')).toContainText('Kept from the switch');
    await expect(panel).toContainText('Programs and system variables');
    // cancelled: nothing sent
    await panel.locator('[data-action="regadom-remove"]').click();
    await expect(page.getByRole('dialog')).toContainText('no longer be imported');
    await page.getByRole('dialog').getByRole('button', {name: 'Cancel'}).click();
    expect(deleted).toBe(0);
    await panel.locator('[data-action="regadom-remove"]').click();
    await page.getByRole('dialog').getByRole('button', {name: 'Remove', exact: true}).click();
    await expect(panel.locator('[data-regadom="removed"]')).toContainText('was removed');
    expect(deleted).toBe(1);
});

test('German', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await page.route('**/api/system/v1/regadom', (r) => r.fulfill({json: {present: true, bytes: 1048576, modified: '2026-11-01T12:00:00Z'}}));
    await page.goto('/system/backup');
    const panel = page.locator('[data-section="regadom"]');
    await expect(page.locator('#regadom')).toContainText('ReGa-Datenbank der CCU');
    await expect(panel).toContainText('Vom Umstieg erhalten');
    await expect(panel.locator('[data-action="regadom-remove"]')).toHaveText('ReGa-Datenbank entfernen…');
});
