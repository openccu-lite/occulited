import {type Page} from '@playwright/test';
import {expect, test} from './fixtures';
import {fitsWindow} from './scroll';

// B-21 (maintainer, 2026-09-26): a catalogue install that cannot read the addon's release list
// refuses and installs nothing, and the Addons page says why - for GitHub's rate limit with the
// wait from its reset header - in English and German. The last check's unread list is a notice of
// its own: the versions shown are the ones from before.

async function refusedRun(page: Page, error: string, minutes?: number): Promise<void> {
    await page.route('**/api/system/v1/catalog/progress', (route) =>
        route.fulfill({
            json: {
                progress: {
                    addon_id: 'redmatic',
                    phase: 'failed',
                    message: 'GitHub rate limit, try again in 23 min: the release list of rdmtc/RedMatic cannot be read now; nothing was installed',
                    // task 165: a failed run older than a day is no notice any more - this one is recent
                    started: new Date(Date.now() - 2000).toISOString(),
                    finished: new Date(Date.now() - 1000).toISOString(),
                    percent: 2,
                    error,
                    ...(minutes ? {retry_minutes: minutes} : {}),
                },
            },
        }),
    );
}

async function uncheckedReleases(page: Page, code: string, minutes?: number): Promise<void> {
    await page.route('**/api/system/v1/catalog', async (route) => {
        const res = await route.fetch();
        const body = await res.json();
        body.catalog.releases_error = {code, repo: 'rdmtc/RedMatic', message: 'GitHub rate limit, try again in 23 min: the release list of rdmtc/RedMatic cannot be read now', at: '2026-09-26T21:10:00Z', ...(minutes ? {retry_minutes: minutes} : {})};
        await route.fulfill({response: res, json: body});
    });
}

test('a rate-limited install says so with the wait, and that nothing was installed', async ({page}) => {
    await refusedRun(page, 'github-rate-limit', 23);
    await page.goto('/addons');
    const progress = page.locator('.ol-install-progress');
    await expect(progress).toHaveAttribute('data-phase', 'failed');
    await expect(progress).toHaveClass(/error/);
    await expect(progress.locator('[data-refused="github-rate-limit"]')).toHaveText('GitHub rate limit, try again in 23 min. Nothing was installed.');
    // the page's own words, not the box's English message
    await expect(progress).not.toContainText('cannot be read now');
    expect(await fitsWindow(page)).toBe(true);
});

test('German: the rate limit and the unread check', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await refusedRun(page, 'github-rate-limit', 5);
    await uncheckedReleases(page, 'github-rate-limit', 5);
    await page.goto('/addons');
    await expect(page.locator('[data-refused]')).toHaveText('GitHub-Ratenlimit, in 5 Min. erneut versuchen. Es wurde nichts installiert.');
    const notice = page.locator('[data-releases-error="github-rate-limit"]');
    await expect(notice).toContainText('Die letzte Prüfung konnte die Releases von rdmtc/RedMatic nicht lesen');
    await expect(notice).toContainText('GitHub-Ratenlimit, in 5 Min. erneut versuchen.');
});

test('without a wait, and without an answer at all', async ({page}) => {
    await refusedRun(page, 'releases-unreachable');
    await uncheckedReleases(page, 'github-rate-limit');
    await page.goto('/addons');
    await expect(page.locator('[data-refused="releases-unreachable"]')).toHaveText('The list of releases could not be read from GitHub. Nothing was installed.');
    const notice = page.locator('[data-releases-error]');
    await expect(notice).toContainText('The last check could not read the releases of rdmtc/RedMatic: the versions and updates shown are from before it.');
    await expect(notice).toContainText('GitHub rate limit, try again later.');
    expect(await fitsWindow(page)).toBe(true);
});

test('no notice while the last check read every list', async ({page}) => {
    await page.goto('/addons');
    await expect(page.locator('#addon-row-redmatic')).toBeVisible();
    await expect(page.locator('[data-releases-error]')).toHaveCount(0);
});

// B-172 (F2): the refused run is said at the card too - the user clicked Install there, scrolled
// far below the notice at the top of the page
test('the refused run stands at the card of its addon', async ({page}) => {
    await refusedRun(page, 'github-rate-limit', 23);
    await page.goto('/addons');
    const at = page.locator('#addon-row-redmatic [data-card-run="failed"]');
    await expect(at).toHaveText('failed — GitHub rate limit, try again in 23 min. Nothing was installed.');
    await expect(at).toHaveAttribute('role', 'alert');
    await expect(page.locator('[data-card-run]')).toHaveCount(1);
    // the close button of the notice at the top takes the card's line with it
    await page.locator('[data-install-progress-close]').click();
    await expect(page.locator('[data-card-run]')).toHaveCount(0);
});

test('a run in progress shows its phase and percent at the card', async ({page}) => {
    await page.route('**/api/system/v1/catalog/progress', (route) =>
        route.fulfill({json: {progress: {addon_id: 'redmatic', phase: 'downloading', started: new Date().toISOString(), percent: 42, bytes: 1, total: 10}, busy: {kind: 'catalog', addon_id: 'redmatic', started: new Date().toISOString()}}}),
    );
    await page.goto('/addons');
    await expect(page.locator('#addon-row-redmatic [data-card-run="downloading"]')).toContainText('42 %');
});
