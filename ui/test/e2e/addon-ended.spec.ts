import {type Page} from '@playwright/test';
import {expect, test} from './fixtures';

// openccu-lite B-158: an addon that keeps a daemon, whose unit is empty - the daemon ended - is red
// and says Exited (not Completed) on the Services and the Addons page, with the time of its last
// log line, the lines behind "why", and Start offered.

async function endMosquitto(page: Page) {
    await page.route('**/api/system/v1/services', async (route) => {
        if (route.request().method() !== 'GET') return route.fallback();
        const response = await route.fetch();
        const body = (await response.json()) as {services: Record<string, unknown>[]};
        Object.assign(body.services.find((s) => s.id === 'addon-mosquitto')!, {
            running: false, pid: undefined, procs: undefined, memory_bytes: undefined, oneshot: true, result: 'success',
            ended: true, ended_at: '2026-09-24T08:07:39Z', ended_log: ['Opening ipv4 listen socket on port 8883.', 'Error: Address in use'],
        });
        await route.fulfill({response, json: body});
    });
}

test('the Services page: Exited in red, the reason behind why, Start offered', async ({page}) => {
    await endMosquitto(page);
    await page.addInitScript(() => localStorage.setItem('ol.language', 'en'));
    await page.setViewportSize({width: 1600, height: 900});
    await page.goto('/system/services');
    const row = page.locator('table.ol-table').first().locator('tbody tr').filter({has: page.locator('.sv-id', {hasText: /^addon-mosquitto$/})});
    await expect(row.locator('td[data-cell="status"]')).toContainText('Exited · ');
    await expect(row.locator('.ol-dot')).toHaveClass(/\berr\b/);
    await expect(row.getByRole('button', {name: 'Start', exact: true})).toBeVisible();
    await expect(row.getByRole('button', {name: 'Stop', exact: true})).toHaveCount(0);
    await row.getByText('why').click();
    await expect(page.getByText('Error: Address in use')).toBeVisible();
});

test('the Addons page: the card is red and says Exited, in German Beendet', async ({page}) => {
    await endMosquitto(page);
    await page.addInitScript(() => localStorage.setItem('ol.language', 'en'));
    await page.goto('/addons');
    const card = page.locator('#addon-row-mosquitto');
    await expect(card.locator('[data-addon-dot]')).toHaveAttribute('data-addon-dot', 'ended');
    await expect(card.locator('[data-addon-dot]')).toHaveClass(/\berr\b/);
    await expect(card.locator('[data-addon-state]')).toHaveText('· Exited');
    await expect(card).toHaveClass(/\berr\b/);
    await page.evaluate(() => localStorage.setItem('ol.language', 'de'));
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await page.reload();
    await expect(page.locator('#addon-row-mosquitto [data-addon-state]')).toHaveText('· Beendet');
});

// occulited B-166: an addon whose start left nothing running by its own choice (hm2mqtt without a
// broker) is not running - not Exited, not red, no warning - and says why behind "why".
async function idleHm2mqtt(page: Page) {
    const idle = {running: false, pid: undefined, procs: undefined, memory_bytes: undefined, oneshot: true, result: 'success',
        idle: true, ended_at: '2026-11-22T14:35:56Z', ended_log: ['no MQTT broker configured - open Systemsteuerung -> hm2mqtt', 'hm2mqtt has no broker configured']};
    await page.route('**/api/system/v1/services', async (route) => {
        if (route.request().method() !== 'GET') return route.fallback();
        const response = await route.fetch();
        const body = (await response.json()) as {services: Record<string, unknown>[]};
        Object.assign(body.services.find((s) => s.id === 'addon-hm2mqtt')!, idle);
        await route.fulfill({response, json: body});
    });
}

test('an addon that chose not to run: Not running on the Services page, the reason behind why, Start offered', async ({page}) => {
    await idleHm2mqtt(page);
    await page.addInitScript(() => localStorage.setItem('ol.language', 'en'));
    await page.setViewportSize({width: 1600, height: 900});
    await page.goto('/system/services');
    const row = page.locator('table.ol-table').first().locator('tbody tr').filter({has: page.locator('.sv-id', {hasText: /^addon-hm2mqtt$/})});
    await expect(row.locator('td[data-cell="status"]')).toContainText('Not running');
    await expect(row.locator('td[data-cell="status"]')).not.toContainText('Exited');
    await expect(row.locator('.ol-dot')).not.toHaveClass(/\berr\b/);
    await expect(row.getByRole('button', {name: 'Start', exact: true})).toBeVisible();
    await row.getByText('why').click();
    await expect(page.getByText('hm2mqtt has no broker configured')).toBeVisible();
    await expect(page.getByText('that is its own choice')).toBeVisible();
});

test('an addon that chose not to run: the Addons card says Not running, without red; in German Läuft nicht', async ({page}) => {
    await idleHm2mqtt(page);
    await page.addInitScript(() => localStorage.setItem('ol.language', 'en'));
    await page.goto('/addons');
    const card = page.locator('#addon-row-hm2mqtt');
    await expect(card.locator('[data-addon-dot]')).toHaveAttribute('data-addon-dot', 'idle');
    await expect(card.locator('[data-addon-dot]')).not.toHaveClass(/\berr\b/);
    await expect(card.locator('[data-addon-state]')).toHaveText('· Not running');
    await expect(card.locator('[data-addon-state]')).toHaveAttribute('title', 'Not running: hm2mqtt has no broker configured');
    await expect(card).not.toHaveClass(/\berr\b/);
    await page.evaluate(() => localStorage.setItem('ol.language', 'de'));
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await page.reload();
    await expect(page.locator('#addon-row-hm2mqtt [data-addon-state]')).toHaveText('· Läuft nicht');
});
