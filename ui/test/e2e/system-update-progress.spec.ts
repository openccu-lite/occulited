import {expect, test, type Page} from './fixtures';

// occulited task 170 (the maintainer, 2026-11-22: a 299 MB download said only "… wird
// heruntergeladen…" for 76 s): the download is a job - the POST answers at once - and the page
// follows its bytes: a determinate bar with the bytes, the percent and the rate, "no data for N s"
// when the transfer stalls, the check of the file at the end, then the staged file's note or the
// failure in words. The bar keeps its box across the states.

const NAME = 'openccu-lite-aarch64-rpi3-2.0.0-rc.1.zip';
const TOTAL = 313_916_678;
const available = {version: '2.0.0-rc.1', name: NAME, size: TOTAL, newer: true};
const staged = {file: NAME, size: TOTAL, kind: 'zip', version: '2.0.0-rc.1', board: 'aarch64-rpi3', recovery_armed: false};

interface Step { busy: boolean; download?: Record<string, unknown>; staged?: boolean }

/** GET /system-update: the stub's answer with the steps laid over it, a step every 3 s from the start (the page polls every 2 s; the last stays) */
async function serve(page: Page, steps: Step[]) {
    let started = 0;
    await page.route('**/api/system/v1/system-update', async (route) => {
        if (route.request().method() !== 'GET') return route.fallback();
        const response = await route.fetch();
        const j = await response.json();
        j.feed.available = available;
        j.staged = null;
        j.busy = null;
        if (started) {
            const s = steps[Math.min(Math.floor((Date.now() - started) / 3000), steps.length - 1)]!;
            j.busy = s.busy ? {phase: 'download', file: NAME} : null;
            if (s.download) j.feed.download = {file: NAME, total: TOTAL, rate: 0, idle_s: 0, received: 0, ...s.download};
            if (s.staged) j.staged = staged;
        }
        await route.fulfill({response, json: j});
    });
    await page.route('**/api/system/v1/system-update/download', async (route) => {
        expect(route.request().postDataJSON()).toEqual({background: true});
        started = Date.now();
        await route.fulfill({status: 202, json: {file: NAME, download: {file: NAME, phase: 'downloading', received: 0, total: 0}, busy: {phase: 'download', file: NAME}}});
    });
}

const box = async (page: Page, sel: string) => {
    const b = (await page.locator(sel).boundingBox())!;
    return `${Math.round(b.width)}x${Math.round(b.height)}`;
};

test('the bar follows the bytes, then the check, then the staged file', async ({page}) => {
    await serve(page, [
        {busy: true, download: {phase: 'downloading', received: 78_000_000, rate: 4_120_000}},
        {busy: true, download: {phase: 'downloading', received: 235_000_000, rate: 4_200_000}},
        {busy: true, download: {phase: 'checking', received: TOTAL}},
        {busy: false, download: {phase: 'done', received: TOTAL, warning: 'sha256 verified'}, staged: true},
    ]);
    await page.goto('/system/updates');
    await page.locator('[data-update-download]').click();
    const prog = page.locator('[data-download-progress]');
    await expect(page.locator('[data-update-busy]')).toHaveAttribute('data-update-busy', 'download');
    await expect(page.locator('[data-update-cancel]')).toBeVisible();
    await expect(prog.locator('[data-download-figures]')).toHaveText('74.4 / 299 MB · 24 % · 3.9 MB/s', {timeout: 5000});
    await expect(prog.locator('[role="progressbar"]')).toHaveAttribute('aria-valuenow', '24');
    const running = await box(page, '[data-download-progress]');
    await expect(prog.locator('[data-download-figures]')).toHaveText('224 / 299 MB · 74 % · 4.0 MB/s', {timeout: 6000});
    await expect(prog).toHaveAttribute('data-download-progress', 'checking', {timeout: 6000});
    await expect(prog.locator('[data-download-figures]')).toHaveText('Checking the file…');
    expect(await box(page, '[data-download-progress]')).toBe(running);
    // done: the busy line goes, the staged file's note stays, the file is staged
    await expect(page.locator('[data-update-busy]')).toHaveCount(0, {timeout: 6000});
    await expect(page.getByText('sha256 verified', {exact: true})).toBeVisible();
    await expect(page.locator('[data-update-install]')).toBeVisible();
});

test('a stalled transfer says how long nothing came, in the same box', async ({page}) => {
    await serve(page, [
        {busy: true, download: {phase: 'downloading', received: 120_000_000, rate: 3_000_000}},
        {busy: true, download: {phase: 'downloading', received: 120_000_000, rate: 0, idle_s: 25}},
    ]);
    await page.goto('/system/updates');
    await page.locator('[data-update-download]').click();
    const prog = page.locator('[data-download-progress]');
    await expect(prog.locator('[data-download-rate]')).toHaveText('2.9 MB/s', {timeout: 5000});
    const running = [await box(page, '[data-download-progress]'), await box(page, '[data-update-busy]')];
    await expect(prog.locator('[data-download-stalled]')).toHaveText('no data for 25 s', {timeout: 6000});
    await expect(prog.locator('[data-download-figures]')).toHaveText('114 / 299 MB · 38 % · no data for 25 s');
    expect([await box(page, '[data-download-progress]'), await box(page, '[data-update-busy]')]).toEqual(running);
});

test('a download that fails in the background is worded as the synchronous answer was', async ({page}) => {
    await serve(page, [
        {busy: true, download: {phase: 'downloading', received: 0, rate: 0}},
        {busy: false, download: {phase: 'failed', received: 0, error: 'not enough space', error_code: 'no-space', error_detail: {free: 1_400_000_000, required: 2_667_108_864, download: true}}},
    ]);
    await page.goto('/system/updates');
    await page.locator('[data-update-download]').click();
    await expect(page.getByText('Not enough space for this update: 1.4 GB free on the system, 2.7 GB needed to store and unpack it.', {exact: false})).toBeVisible({timeout: 6000});
    await expect(page.locator('[data-update-busy]')).toHaveCount(0);
    await expect(page.locator('[data-update-download]')).toBeEnabled();
});

test('the bar in German, on a phone-width page without overflow', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await serve(page, [
        {busy: true, download: {phase: 'downloading', received: 120_000_000, rate: 0, idle_s: 12}},
    ]);
    await page.goto('/system/updates');
    await page.locator('[data-update-download]').click();
    await expect(page.locator('[data-download-stalled]')).toHaveText('seit 12 s keine Daten', {timeout: 5000});
    // the figures are not cut, and the page does not overflow
    for (const sel of ['[data-download-progress]', '[data-download-figures]']) expect(await page.locator(sel).evaluate((e) => e.scrollWidth > e.clientWidth + 1)).toBe(false);
});
