import {expect, test} from '@playwright/test';

// occulited task 150: an addon that provides backup targets is not uninstalled unseen - the
// question names the targets that use it; an addon no target uses is asked about as before.

async function plantTarget(page: import('@playwright/test').Page, addon: string) {
    const res = await page.request.get('/api/system/v1/backup/targets');
    const view = await res.json();
    view.targets.push({id: 'tcloud01', name: 'OneDrive', kind: 'addon', enabled: true, max_backups: 30, subdir: '', encrypt: true, append_only: false,
        addon: {id: addon, destination: 'acc1', folder: 'openccu-lite/openccu'}, state: {state: 'idle', failures: 0, mounted: false}});
    await page.route(/\/api\/system\/v1\/backup\/targets(\?.*)?$/, (r) => (r.request().method() === 'GET' ? r.fulfill({json: view}) : r.fallback()));
}

test('the uninstall question names the backup targets the addon provides', async ({page}) => {
    await plantTarget(page, 'redmatic');
    await page.goto('/addons');
    const row = page.locator('#addon-row-redmatic');
    await row.getByRole('button', {name: 'More actions'}).click();
    await row.locator('[data-addon-uninstall]').click();
    const dlg = page.getByRole('dialog');
    await expect(dlg).toContainText('The backup targets "OneDrive" use this addon: they stay set up, but get no backup until it is installed again.');
    await dlg.getByRole('button', {name: 'Cancel'}).click();
});

test('an addon no target uses is asked about as before', async ({page}) => {
    await plantTarget(page, 'some-other-addon');
    await page.goto('/addons');
    const row = page.locator('#addon-row-redmatic');
    await row.getByRole('button', {name: 'More actions'}).click();
    await row.locator('[data-addon-uninstall]').click();
    const dlg = page.getByRole('dialog');
    await expect(dlg).toContainText('Uninstall');
    await expect(dlg).not.toContainText('backup targets');
});
