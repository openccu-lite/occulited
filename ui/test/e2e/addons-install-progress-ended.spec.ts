import {type Page} from '@playwright/test';
import {expect, test} from './fixtures';
import {fitsWindow} from './scroll';

// occulited task 165 (maintainer, 2026-11-21): a finished install's progress bar does not stay on
// the Addons page for ever. A done run is gone after the next reload (one that ended within the
// last minute still shows, so a reload right after the install says how it went); a failed one
// stays until its close button is pressed, or a day; the close button removes either at once.

const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

/** GET /catalog/progress answering one ended run, as the system keeps it until the next starts */
async function ended(page: Page, phase: 'done' | 'failed', finishedAgoMs: number) {
    const run = {
        addon_id: 'redmatic',
        phase,
        percent: phase === 'done' ? 100 : 40,
        started: ago(finishedAgoMs + 30_000),
        finished: ago(finishedAgoMs),
        ...(phase === 'failed' ? {message: 'the installer exited 101'} : {result: {exit: 0, meaning: 'installed', reboot_required: false}}),
    };
    await page.route('**/api/system/v1/catalog/progress', (route) => route.fulfill({json: {progress: run, busy: null}}));
}

test('an install finished on this page: the bar and its close button, gone after a reload', async ({page, context, baseURL}) => {
    await context.addCookies([{name: 'stub-session', value: `ended-${test.info().project.name}-${Date.now()}`, url: baseURL!}]);
    await page.goto('/addons');
    await page.locator('#addon-row-redmatic [data-addon-update]').click();
    await page.getByRole('dialog').getByRole('button', {name: 'Update', exact: true}).click();
    const progress = page.locator('.ol-install-progress[data-phase]');
    await expect(progress).toContainText('redmatic');
    await expect(progress).toHaveAttribute('data-phase', 'done', {timeout: 15_000});
    await expect(progress.locator('[data-install-progress-close]')).toBeVisible();
    // the stub's run ended seconds ago: a reload right after still shows it ...
    await page.reload();
    await expect(progress).toHaveAttribute('data-phase', 'done');
    // ... the close button removes it at once, and it stays away after the next reload
    await progress.locator('[data-install-progress-close]').click();
    await expect(progress).toHaveCount(0);
    await page.reload();
    await expect(page.locator('#addon-row-redmatic')).toBeVisible();
    await expect(progress).toHaveCount(0);
});

test('a done run of long ago is no notice', async ({page}) => {
    await ended(page, 'done', 3 * 24 * 3600_000);
    await page.goto('/addons');
    await expect(page.locator('#addon-row-redmatic')).toBeVisible();
    // the first answer is in: nothing shown, also after a poll
    await page.waitForResponse('**/api/system/v1/catalog/progress');
    await page.waitForTimeout(1800);
    await expect(page.locator('.ol-install-progress')).toHaveCount(0);
});

test('a done run of a few seconds ago is still shown after a reload, with its close button', async ({page}) => {
    await ended(page, 'done', 5000);
    await page.goto('/addons');
    const progress = page.locator('.ol-install-progress[data-phase="done"]');
    await expect(progress).toContainText('redmatic');
    await expect(progress.getByRole('button', {name: 'Close'})).toBeVisible();
    expect(await fitsWindow(page)).toBe(true);
});

test('a failed run stays until it is closed, and a reload does not bring it back', async ({page}) => {
    await ended(page, 'failed', 3 * 3600_000);
    await page.goto('/addons');
    const progress = page.locator('.ol-install-progress[data-phase="failed"]');
    await expect(progress).toContainText('the installer exited 101');
    await page.reload();
    await expect(progress).toContainText('the installer exited 101');
    await progress.getByRole('button', {name: 'Close'}).click();
    await expect(progress).toHaveCount(0);
    await page.reload();
    await expect(page.locator('#addon-row-redmatic')).toBeVisible();
    await page.waitForTimeout(1800);
    await expect(progress).toHaveCount(0);
});

test('a failed run of days ago is no notice', async ({page}) => {
    await ended(page, 'failed', 3 * 24 * 3600_000);
    await page.goto('/addons');
    await expect(page.locator('#addon-row-redmatic')).toBeVisible();
    await page.waitForTimeout(1800);
    await expect(page.locator('.ol-install-progress')).toHaveCount(0);
});

test('German: the close button says Schließen', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await ended(page, 'failed', 60_000);
    await page.goto('/addons');
    await expect(page.locator('.ol-install-progress[data-phase="failed"]').getByRole('button', {name: 'Schließen'})).toBeVisible();
});
