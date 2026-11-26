import {type Page, type Route} from '@playwright/test';
import {expect, test} from './fixtures';

// openccu-lite task 505: the shell while the system is in its recovery system. The recovery's
// lighttpd answers this origin (over HTTPS since 2.0.0): 503 {"error": "updating", "recovery": true,
// "update": {...}} on every /api/ path, its install log as text at /fwinstall.log. The shell must
// notice it from any API answer, put the bar under the top bar without moving the page, lock the
// menu (greyed, inert, the reason as the tooltip), route every path to the recovery's view with the
// install's progress and its raw output, and go back to the page it interrupted once the system
// answers again.

type Update = Record<string, unknown> | null;

/** A recovery the spec switches on and off; `update` is what its 503 says about the install. */
async function recoveryMock(page: Page) {
    const m = {on: false, update: null as Update, healthDown: 0, back: false, log: 'Starting firmware update (DO NOT INTERRUPT!!!):<br/>\n[1/7] Checking uploaded data... OK<br/>\n[5/7] Preparing uploaded data... zip identified, unarchiving......'};
    const recovery = (r: Route) => r.fulfill({status: 503, headers: {'Retry-After': '30', 'Cache-Control': 'no-store'}, json: {error: 'updating', message: 'the system is in its recovery system', recovery: true, ...(m.update ? {update: m.update} : {})}});
    await page.route(
        (url) => url.pathname.startsWith('/api/'),
        (r) => {
            const path = new URL(r.request().url()).pathname;
            if (path === '/api/system/v1/health') {
                if (m.healthDown > 0) {
                    m.healthDown--;
                    return r.abort('connectionrefused');
                }
                if (m.on) return recovery(r);
                if (m.back) return r.fulfill({json: {ok: true, uptime_s: 4}});
                return r.fallback();
            }
            return m.on ? recovery(r) : r.fallback();
        },
    );
    await page.route('**/fwinstall.log', (r) => (m.on ? r.fulfill({status: 200, contentType: 'text/plain', body: m.log}) : r.fulfill({status: 200, contentType: 'text/html', body: '<html>app</html>'})));
    return m;
}

const PREPARE = {running: true, phase: 'prepare', step: 5, steps: 7, log_bytes: 180};
const ROOTFS = {running: true, phase: 'install', step: 5, steps: 5, flashing: 'rootfs', written: 3 * 1024 ** 3 / 4, total: 1024 ** 3, log_bytes: 900};

for (const lang of [
    {name: 'English', code: 'en', bar: 'an update is being installed. Do not switch it off.', step5: 'Step 5 of 12: Unpacking the update', rootfs: 'Step 12 of 12: Writing the system partition – 75 %', lock: 'Not available while the system is in its recovery system', details: 'Details: the output of the installation'},
    {name: 'German', code: 'de', bar: 'ein Update wird installiert. Nicht ausschalten.', step5: 'Schritt 5 von 12: Update entpacken', rootfs: 'Schritt 12 von 12: System-Partition schreiben – 75 %', lock: 'Nicht verfügbar, solange das System im Recovery-System ist', details: 'Details: die Ausgabe der Installation'},
]) {
    test(`an API answer of the recovery locks the shell and shows the install (${lang.name})`, async ({page}) => {
        await page.addInitScript(([code]) => {
            localStorage.setItem('ol.language', code!);
            localStorage.setItem('occulite.recovery-url', 'http://192.0.2.7/');
        }, [lang.code]);
        const m = await recoveryMock(page);
        await page.goto('/system/updates');
        await expect(page.locator('h2').first()).toBeVisible();
        const header = page.locator('header.ol-header');
        const port = page.locator('.ol-scrollport');
        const headerBox = await header.boundingBox();
        const portBox = await port.boundingBox();

        // the recovery answers: the next API call of a page tells the shell
        m.on = true;
        m.update = PREPARE;
        await page.locator('.ol-nav a').first().click();
        const bar = page.locator('[data-recovery-bar]');
        await expect(bar).toBeVisible();
        await expect(bar).toContainText(lang.bar);
        await expect(page).toHaveURL(/\/recovery$/);

        // nothing moved: the bar lies over the page, the header kept its box
        expect(await header.boundingBox()).toEqual(headerBox);
        expect(await port.boundingBox()).toEqual(portBox);
        const barBox = (await bar.boundingBox())!;
        expect(Math.abs(barBox.y - (headerBox!.y + headerBox!.height))).toBeLessThan(2);
        expect(barBox.width).toBeGreaterThan(portBox!.width - 2);
        // and the view's heading starts below it (the lab run showed it under the bar at first)
        const h1 = (await page.locator('[data-recovery-view] h1').boundingBox())!;
        expect(h1.y).toBeGreaterThanOrEqual(barBox.y + barBox.height);

        // the menu: greyed, inert, the reason as its tooltip, a click goes nowhere
        await expect(header).toHaveAttribute('inert', '');
        await expect(page.locator('[data-nav-locked]')).toHaveAttribute('title', lang.lock);
        expect(Number(await header.locator('.ol-nav').evaluate((e) => getComputedStyle(e).opacity))).toBeLessThan(1);
        await page.mouse.click(headerBox!.x + 200, headerBox!.y + headerBox!.height / 2);
        await expect(page).toHaveURL(/\/recovery$/);

        // the install: the weighted step and the bar
        const view = page.locator('[data-recovery-view]');
        await expect(view.locator('[data-install-step]')).toHaveText(lang.step5);
        const progress = view.getByRole('progressbar').first();
        await expect(progress).toHaveAttribute('aria-valuenow', /^\d+$/);
        const before = Number(await progress.getAttribute('aria-valuenow'));
        expect(before).toBeGreaterThanOrEqual(9);
        expect(before).toBeLessThan(45);

        // the flash: a real percentage, the bar further on
        m.update = ROOTFS;
        await expect(view.locator('[data-install-step]')).toHaveText(lang.rootfs, {timeout: 8000});
        const flashing = Number(await progress.getAttribute('aria-valuenow'));
        expect(flashing).toBeGreaterThan(80);
        await expect(bar.locator('[data-recovery-bar-percent]')).toHaveText(`${flashing} %`);

        // a reconnect mid-update: two polls without an answer, then an answer that says less - the bar stays
        m.healthDown = 2;
        m.update = PREPARE;
        await page.waitForTimeout(9500);
        expect(Number(await progress.getAttribute('aria-valuenow'))).toBeGreaterThanOrEqual(flashing);
        await expect(bar).toBeVisible();

        // Details: the raw output, as lines
        await view.getByText(lang.details).click();
        await expect(view.locator('[data-install-log]')).toContainText('[5/7] Preparing uploaded data');
        await expect(view.locator('[data-install-log]')).not.toContainText('<br');

        // the recovery's own page: on this origin, and by IP address
        await expect(view.locator('[data-recovery-page]')).toHaveAttribute('href', '/');
        await expect(view.locator('[data-recovery-ip]')).toHaveAttribute('href', 'http://192.0.2.7/');

        // the system back: the bar goes, the menu comes back, the page it interrupted returns
        m.on = false;
        m.back = true;
        await expect(bar).toHaveCount(0, {timeout: 8000});
        await expect(header).not.toHaveAttribute('inert');
        await expect(page).toHaveURL(/\/$/);
        expect(await port.boundingBox()).toEqual(portBox);
    });
}

test('a direct URL while the recovery answers opens the recovery view, and the page after it', async ({page}) => {
    const m = await recoveryMock(page);
    m.on = true;
    m.update = {running: true, phase: 'prepare', step: 2, steps: 7};
    await page.goto('/system/log');
    await expect(page).toHaveURL(/\/recovery$/);
    await expect(page.locator('[data-recovery-bar]')).toBeVisible();
    await expect(page.locator('[data-install-step]')).toHaveText('Step 2 of 12: Calculating the checksum');
    m.on = false;
    m.back = true;
    await expect(page).toHaveURL(/\/system\/log$/, {timeout: 8000});
    await expect(page.locator('[data-recovery-bar]')).toHaveCount(0);
});

// occulited B-187: the update's boot watch reloads the page once the system answers; on an OVA the
// reload came before the shell had left the recovery view, and the way back was gone with the page
test('a reload on the recovery view keeps the way back: in the recovery it stays, after it the page returns', async ({page}) => {
    const m = await recoveryMock(page);
    m.on = true;
    m.update = {running: true, phase: 'prepare', step: 2, steps: 7};
    await page.goto('/system/log');
    await expect(page).toHaveURL(/\/recovery$/);
    // reloaded while the recovery still answers: the view stays, the page it interrupted does not flash
    const seen: string[] = [];
    await page.exposeFunction('seenPath', (p: string) => seen.push(p));
    await page.addInitScript(() => {
        for (const k of ['pushState', 'replaceState'] as const) {
            const orig = history[k].bind(history);
            history[k] = (...a: Parameters<History['pushState']>) => {
                orig(...a);
                (window as unknown as {seenPath: (p: string) => void}).seenPath(location.pathname);
            };
        }
    });
    await page.reload();
    await expect(page.locator('[data-recovery-bar]')).toBeVisible();
    await expect(page).toHaveURL(/\/recovery$/);
    await page.waitForTimeout(500);
    expect(seen).not.toContain('/system/log');
    // the system back, and the boot watch's reload comes first: the page it interrupted
    m.on = false;
    m.back = true;
    await page.reload();
    await expect(page).toHaveURL(/\/system\/log$/, {timeout: 8000});
    await expect(page.locator('[data-recovery-bar]')).toHaveCount(0);
    // the way back is used up: the view opened by hand afterwards stays
    await page.goto('/recovery');
    await expect(page.locator('[data-recovery-view]')).toContainText('The system is not in its recovery system.');
    await expect(page).toHaveURL(/\/recovery$/);
});

// B-187: the boot watch's answers end the recovery state at once, not only the shell's own slower poll
test('the update boot watch\'s first answer of the system ends the recovery state', async ({page}) => {
    const m = await recoveryMock(page);
    await page.goto('/system/updates');
    await expect(page.locator('h2').first()).toBeVisible();
    m.on = true;
    await page.evaluate(() => window.dispatchEvent(new CustomEvent('ol:recovery', {detail: {status: 503, body: {error: 'updating', recovery: true}}})));
    await expect(page).toHaveURL(/\/recovery$/);
    m.on = false;
    m.back = true;
    // what lib/bootwatch.ts now announces for any answer: the system's own health
    await page.evaluate(() => window.dispatchEvent(new CustomEvent('ol:recovery', {detail: {status: 200, body: {ok: true, uptime_s: 4}}})));
    await expect(page).toHaveURL(/\/system\/updates$/, {timeout: 1500});
});

test('a recovery entered for anything else: the bar and the view without an install', async ({page}) => {
    const m = await recoveryMock(page);
    await page.goto('/system/updates');
    await expect(page.locator('h2').first()).toBeVisible();
    m.on = true;
    await page.locator('.ol-nav a').first().click();
    await expect(page.locator('[data-recovery-bar]')).toContainText('Its pages are not available until it has restarted.');
    await expect(page.locator('[data-install-progress]')).toHaveCount(0);
    await expect(page.locator('[data-recovery-view] h1')).toHaveText('The system is in its recovery system');
});

test('an older recovery (no log here): Details says so instead of showing the system page', async ({page}) => {
    const m = await recoveryMock(page);
    await page.route('**/fwinstall.log', (r) => r.fulfill({status: 200, contentType: 'text/html', body: '<html>menu</html>'}));
    m.on = true;
    m.update = {running: true, phase: 'prepare', step: 6, steps: 7};
    await page.goto('/system/updates');
    await expect(page).toHaveURL(/\/recovery$/);
    await page.getByText('Details: the output of the installation').click();
    await expect(page.locator('[data-install-log-missing]')).toBeVisible();
    await expect(page.locator('[data-install-log]')).toHaveCount(0);
});

test('the bar is the attention colour in both themes, unlike the page', async ({page}, info) => {
    const m = await recoveryMock(page);
    m.on = true;
    m.update = PREPARE;
    await page.goto('/system/updates');
    const bar = page.locator('[data-recovery-bar]');
    await expect(bar).toBeVisible();
    const [bg, fg, page_] = await bar.evaluate((e) => [getComputedStyle(e).backgroundColor, getComputedStyle(e).color, getComputedStyle(document.body).backgroundColor]);
    expect(bg).not.toBe(page_);
    if (info.project.name === 'desktop-dark') {
        expect(bg).toBe('rgb(204, 167, 0)');
        expect(fg).toBe('rgb(27, 31, 38)');
    } else {
        expect(bg).toBe('rgb(160, 104, 0)');
        expect(fg).toBe('rgb(255, 255, 255)');
    }
});
