import {expect, test, type Page} from '@playwright/test';

// occulited task 174: an Ethernet interface that can do a gigabit may be held at 100 Mbit/s
// (gigabit disturbs the 868 MHz radio of a Pi 4/5). The select is the stored setting; a change
// asks first, because the link drops for a few seconds.

async function own(page: Page, baseURL: string | undefined, readOnly = false) {
    await page.context().addCookies([
        {name: 'stub-speed', value: `s-${Math.random().toString(36).slice(2)}`, url: baseURL!},
        ...(readOnly ? [{name: 'stub-speed-ro', value: '1', url: baseURL!}] : []),
    ]);
    await page.goto('/system/network');
}
const card = (page: Page, name: string) => page.locator(`[data-iface="${name}"]`);

test('eth0 offers the setting, the other interfaces do not', async ({page, baseURL}) => {
    await own(page, baseURL);
    const select = card(page, 'eth0').locator('[data-action="speed-limit"]');
    await expect(select).toHaveValue('auto');
    await expect(card(page, 'eth0').getByLabel('Ethernet speed')).toBeVisible();
    await expect(select.locator('option')).toHaveText(['automatic', '100 Mbit/s']);
    // the USB adapter cannot do a gigabit: no row at all
    await expect(card(page, 'eth1')).toBeVisible();
    await expect(card(page, 'eth1').locator('[data-speed-limit]')).toHaveCount(0);
    await expect(page.locator('[data-speed-limit]')).toHaveCount(1);
});

test('100 Mbit/s: the question, the request, the new state; the select keeps its box', async ({page, baseURL}) => {
    await own(page, baseURL);
    const eth0 = card(page, 'eth0');
    const select = eth0.locator('[data-action="speed-limit"]');
    await expect(eth0.locator('.ol-linkline')).toContainText('1000 Mbit/s');
    const before = await select.boundingBox();

    // Cancel: nothing is sent and the select goes back
    await select.selectOption('100');
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('The link of eth0 is negotiated anew and drops for a few seconds');
    await dialog.getByRole('button', {name: 'Cancel'}).click();
    await expect(select).toHaveValue('auto');

    const [req] = await Promise.all([
        page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/system/v1/network/speed-limit')),
        (async () => {
            await select.selectOption('100');
            await page.getByRole('dialog').getByRole('button', {name: 'Apply'}).click();
        })(),
    ]);
    expect(req.postDataJSON()).toEqual({interface: 'eth0', limit: '100'});
    await expect(eth0.locator('[data-speed-limit]')).toHaveAttribute('data-speed-limit', '100');
    await expect(select).toHaveValue('100');
    await expect(eth0.locator('.ol-linkline')).toContainText('100 Mbit/s · full duplex');
    const after = await select.boundingBox();
    expect(after!.width).toBe(before!.width);
    expect(after!.height).toBe(before!.height);

    // and back
    await select.selectOption('auto');
    await page.getByRole('dialog').getByRole('button', {name: 'Apply'}).click();
    await expect(eth0.locator('[data-speed-limit]')).toHaveAttribute('data-speed-limit', 'auto');
    await expect(eth0.locator('.ol-linkline')).toContainText('1000 Mbit/s');
});

test('an image that cannot apply it shows the setting as text', async ({page, baseURL}) => {
    await own(page, baseURL, true);
    const eth0 = card(page, 'eth0');
    await expect(eth0.locator('[data-speed-limit]')).toHaveText('automatic');
    await expect(eth0.locator('[data-action="speed-limit"]')).toHaveCount(0);
});

test('in German', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await own(page, baseURL);
    const eth0 = card(page, 'eth0');
    await expect(eth0.getByLabel('Ethernet-Geschwindigkeit')).toBeVisible();
    await expect(eth0.locator('[data-action="speed-limit"] option')).toHaveText(['automatisch', '100 Mbit/s']);
});
