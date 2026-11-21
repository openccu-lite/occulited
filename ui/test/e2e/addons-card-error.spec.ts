import {expect, test} from './fixtures';

// occulited B-157: a card that cannot render stands as a notice of its own - its name, the reason
// and "Try again" - and every other card stays; before, one such card hid the whole list behind
// one notice. The addon here carries an Info line that is no text (a shape the page does not expect).

async function oneBadCard(page: import('@playwright/test').Page, id: string) {
    await page.route('**/api/system/v1/addons', async (route) => {
        const res = await route.fetch();
        const body = await res.json();
        for (const a of body.addons) if (a.id === id) a.info = 5;
        await route.fulfill({response: res, json: body});
    });
}

test('one card that throws: its own notice, the other cards stay', async ({page}) => {
    const errors: string[] = [];
    page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()); });
    await oneBadCard(page, 'hm2mqtt');
    await page.goto('/addons');
    const bad = page.locator('#addon-row-hm2mqtt');
    await expect(bad).toHaveAttribute('data-card-error', '');
    await expect(bad).toContainText("This addon's card could not be shown:");
    await expect(bad.getByRole('button', {name: 'Try again'})).toBeVisible();
    // the rest of the list is there, with its actions
    await expect(page.locator('#addon-row-redmatic [data-addon-update]')).toBeVisible();
    expect(await page.locator('[data-addon-card]:not([data-card-error])').count()).toBeGreaterThan(3);
    await expect(page.locator('[data-cards-error]')).toHaveCount(0);
    expect(errors.some((e) => e.includes('addons: the card of hm2mqtt could not be rendered'))).toBe(true);
});

test('German: the card notice', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await oneBadCard(page, 'hm2mqtt');
    await page.goto('/addons');
    const bad = page.locator('#addon-row-hm2mqtt');
    await expect(bad).toContainText('Die Karte dieser Zusatzsoftware konnte nicht angezeigt werden:');
    await expect(bad.getByRole('button', {name: 'Erneut versuchen'})).toBeVisible();
});
