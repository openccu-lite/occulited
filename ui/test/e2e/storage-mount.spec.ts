import {expect, test, type Page} from '@playwright/test';
import {fitsWindow, pageWidth} from './scroll';

// occulited task 160: System → Storage's "Write behaviour of the system storage" - lazytime and
// the commit interval of the userfs, set at once, with what is mounted now.

async function open(page: Page, baseURL: string | undefined, variant = '') {
    await page.context().addCookies([{name: 'stub-mount', value: `${variant}-${Math.random().toString(36).slice(2)}`, url: baseURL!}]);
    await page.goto('/system/storage');
}
const section = (page: Page) => page.locator('[data-write-behaviour]');
const commit = (page: Page, s: number) => section(page).locator(`[data-commit="${s}"]`);

test("the image's options, then a longer interval and lazytime: each applies at once, nothing moves", async ({page, baseURL}) => {
    await open(page, baseURL);
    await expect(page.getByRole('heading', {level: 2, name: 'Write behaviour of the system storage'})).toBeVisible();
    const lazy = section(page).getByRole('checkbox', {name: 'Write file timestamps later (lazytime)'});
    await expect(lazy).not.toBeChecked();
    await expect(commit(page, 30)).toHaveAttribute('aria-pressed', 'true');
    for (const s of [5, 60, 120]) await expect(commit(page, s)).toHaveAttribute('aria-pressed', 'false');
    await expect(section(page).locator('[data-wb-risk]')).toHaveText("A power cut can take up to 30 seconds of changes that were not yet written. The system's default: 30 seconds, timestamps at once.");
    await expect(section(page).locator('[data-wb-mounted]')).toHaveText('Mounted now: /usr/local · ext4 · rw,noatime,nodiratime,commit=30');

    const seg = section(page).locator('.wb-seg');
    const before = (await seg.boundingBox())!;
    const line = (await section(page).locator('[data-setting="commit"]').boundingBox())!;
    let put = page.waitForRequest((r) => r.method() === 'PUT' && r.url().endsWith('/storage/mount-options'));
    await commit(page, 120).click();
    expect((await put).postDataJSON()).toEqual({lazytime: false, commit_s: 120});
    await expect(commit(page, 120)).toHaveAttribute('aria-pressed', 'true');
    await expect(commit(page, 30)).toHaveAttribute('aria-pressed', 'false');
    await expect(section(page).locator('[data-wb-risk]')).toContainText('up to 120 seconds');
    await expect(section(page).locator('[data-wb-mounted]')).toContainText('rw,noatime,nodiratime,commit=120');
    // the buttons and their line keep their size across the states
    const after = (await seg.boundingBox())!;
    expect(after.width).toBe(before.width);
    expect(after.height).toBe(before.height);
    expect((await section(page).locator('[data-setting="commit"]').boundingBox())!.height).toBe(line.height);

    put = page.waitForRequest((r) => r.method() === 'PUT' && r.url().endsWith('/storage/mount-options'));
    await lazy.check();
    expect((await put).postDataJSON()).toEqual({lazytime: true, commit_s: 120});
    await expect(lazy).toBeChecked();
    await expect(lazy).toBeEnabled();
    await expect(section(page).locator('[data-wb-mounted]')).toContainText('rw,lazytime,noatime,nodiratime,commit=120');
    // ext4's own five seconds are not named in the mount table
    await commit(page, 5).click();
    await expect(section(page).locator('[data-wb-mounted]')).toHaveText('Mounted now: /usr/local · ext4 · rw,lazytime,noatime,nodiratime');
    // the choice is the system's: a reload shows it
    await page.reload();
    await expect(commit(page, 5)).toHaveAttribute('aria-pressed', 'true');
    await expect(section(page).getByRole('checkbox')).toBeChecked();
    expect(await fitsWindow(page)).toBe(true);
});

test('a remount the system refuses is said, and the page shows what is mounted', async ({page, baseURL}) => {
    await open(page, baseURL, 'busy');
    await commit(page, 60).click();
    await expect(section(page).locator('[data-notice="wb-error"]')).toContainText('mount point is busy');
    await expect(commit(page, 30)).toHaveAttribute('aria-pressed', 'true');
    await expect(commit(page, 60)).toHaveAttribute('aria-pressed', 'false');
    await expect(commit(page, 60)).toBeEnabled();
});

test('a system storage that is no ext4 mount: said, nothing to set', async ({page, baseURL}) => {
    await open(page, baseURL, 'unsupported');
    await expect(section(page).locator('[data-wb-state="unsupported"]')).toHaveText('Not available: the system storage of this system is not an ext4 file system mounted on its own.');
    await expect(section(page).getByRole('checkbox')).toHaveCount(0);
    await expect(section(page).locator('[data-commit]')).toHaveCount(0);
    await expect(section(page).locator('[data-wb-mounted]')).toContainText('/usr/local · f2fs · rw,noatime');
});

test('a user reads the options and cannot set them', async ({page, baseURL}) => {
    await page.route('**/api/auth/v1/state', (r) => r.fulfill({json: {setup_required: false, authenticated: true, user: 'monitor', role: 'user', must_change_password: false, sid: 'U1'}}));
    await open(page, baseURL, 'custom');
    await expect(section(page).getByRole('checkbox')).toBeChecked();
    await expect(section(page).getByRole('checkbox')).toBeDisabled();
    await expect(commit(page, 120)).toHaveAttribute('aria-pressed', 'true');
    for (const s of [5, 30, 60, 120]) await expect(commit(page, s)).toBeDisabled();
});

test('in German, on a phone, in both themes', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    for (const scheme of ['light', 'dark'] as const) {
        await page.emulateMedia({colorScheme: scheme});
        await page.setViewportSize({width: 360, height: 780});
        await open(page, baseURL, 'custom');
        await expect(page.getByRole('heading', {level: 2, name: 'Schreibverhalten des Systemspeichers'})).toBeVisible();
        await expect(section(page).getByRole('checkbox', {name: 'Zeitstempel von Dateien später schreiben (lazytime)'})).toBeChecked();
        await expect(section(page).locator('[data-setting="commit"]')).toContainText('Änderungen spätestens schreiben nach');
        await expect(section(page).locator('[data-wb-risk]')).toHaveText('Ein Stromausfall kann bis zu 120 Sekunden an noch nicht geschriebenen Änderungen kosten. Standard des Systems: 30 Sekunden, Zeitstempel sofort.');
        await expect(section(page).locator('[data-wb-mounted]')).toContainText('Derzeit eingehängt: /usr/local · ext4 · rw,lazytime,noatime,nodiratime,commit=120');
        expect(await pageWidth(page)).toBeLessThanOrEqual(360);
    }
});
