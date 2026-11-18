import {expect, test, type Locator, type Page} from '@playwright/test';

// occulited task 156: two buttons float over the log, at its top right and its bottom right, and
// jump to the log's start and its end. At an end the button to that end is hidden, in between both
// show - also when lines arrive below a reader in the history - and showing or hiding one moves
// nothing in the page. "Start" is the start of the log itself: where older pages are still to
// come, the oldest page is loaded. The stub serves a boot of as many entries as the cookie names
// (task 178), and with the churn cookie its stream emits a line every 200 ms (B-59).

async function longBoot(page: Page, baseURL: string, entries: number) {
    await page.context().addCookies([{name: 'stub-log-pages', value: String(entries), url: baseURL}]);
}

const box = (page: Page): Locator => page.locator('.lg-box');
const toStart = (page: Page): Locator => page.locator('.lg-jump[data-float="start"]');
const toEnd = (page: Page): Locator => page.locator('.lg-jump[data-float="end"]');

async function open(page: Page) {
    await page.goto('/system/log');
    await box(page).waitFor({state: 'attached'});
}

/** the rendered rows' numbers, in DOM order */
async function rowNumbers(page: Page): Promise<number[]> {
    return page.locator('.lg-box .ol-line .ol-msg').evaluateAll((els) => els.map((e) => Number(/Boot line (\d+)/.exec(e.textContent ?? '')?.[1])));
}

/** how far the view is from the list's two ends */
async function away(page: Page): Promise<{top: number; bottom: number}> {
    return box(page).evaluate((el) => ({top: el.scrollTop, bottom: el.scrollHeight - el.scrollTop - el.clientHeight}));
}

async function scrollTo(page: Page, where: 'top' | 'middle' | 'bottom') {
    await box(page).evaluate((el, where) => {
        el.scrollTop = where === 'top' ? 0 : where === 'bottom' ? el.scrollHeight : Math.floor((el.scrollHeight - el.clientHeight) / 2);
    }, where);
}

/** the page's geometry a button's coming and going must not touch: both buttons' boxes and the log's */
async function geometry(page: Page): Promise<string> {
    return page.evaluate(() => {
        const r = (sel: string) => {
            const b = document.querySelector(sel)!.getBoundingClientRect();
            return [b.left, b.top, b.width, b.height].map((v) => Math.round(v * 10) / 10).join(',');
        };
        return [r('.lg-jump-top'), r('.lg-jump-bottom'), r('.lg-box'), r('.lg-panel')].join(' | ');
    });
}

test('at the end only the start button shows, in between both, at the start only the end button', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 600); // one page: the run is the whole log
    await open(page);
    // the page opens at the newest line
    await expect.poll(async () => (await rowNumbers(page)).at(-1)).toBe(599);
    await expect(toEnd(page)).toBeHidden();
    await expect(toStart(page)).toBeVisible();
    const atEnd = await geometry(page);

    await scrollTo(page, 'middle');
    await expect(toStart(page)).toBeVisible();
    await expect(toEnd(page)).toBeVisible();
    expect(await geometry(page)).toBe(atEnd);

    await scrollTo(page, 'top');
    await expect(toStart(page)).toBeHidden();
    await expect(toEnd(page)).toBeVisible();
    expect(await geometry(page)).toBe(atEnd);

    // a few pixels from an end are the end
    await box(page).evaluate((el) => (el.scrollTop = 30));
    await expect(toStart(page)).toBeHidden();
    await box(page).evaluate((el) => (el.scrollTop = 120));
    await expect(toStart(page)).toBeVisible();
});

test('the buttons float over the log: top right and bottom right, beside the scroll bar, with a name', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 600);
    await open(page);
    await scrollTo(page, 'middle');
    await expect(toStart(page)).toBeVisible();
    await expect(toEnd(page)).toBeVisible();
    const g = await page.evaluate(() => {
        const rect = (sel: string) => document.querySelector(sel)!.getBoundingClientRect();
        const el = document.querySelector('.lg-box') as HTMLElement;
        return {box: rect('.lg-box'), top: rect('.lg-jump-top'), bottom: rect('.lg-jump-bottom'), bar: el.offsetWidth - el.clientWidth};
    });
    for (const b of [g.top, g.bottom]) {
        // inside the log, in its right half, and clear of the scroll bar
        expect(b.left).toBeGreaterThan(g.box.left + g.box.width / 2);
        expect(b.right).toBeLessThanOrEqual(g.box.right - g.bar - 4);
        expect(b.width).toBeGreaterThanOrEqual(32);
        expect(b.height).toBeGreaterThanOrEqual(32);
    }
    expect(g.top.top - g.box.top).toBeLessThan(24);
    expect(g.top.top).toBeGreaterThanOrEqual(g.box.top);
    expect(g.box.bottom - g.bottom.bottom).toBeLessThan(24);
    expect(g.bottom.bottom).toBeLessThanOrEqual(g.box.bottom);
    await expect(toStart(page)).toHaveAccessibleName('Jump to the oldest entry');
    await expect(toEnd(page)).toHaveAccessibleName('Jump to the newest entry');
    await expect(toStart(page)).toHaveAttribute('title', 'Jump to the oldest entry');
    // they do not scroll with the lines
    const before = await geometry(page);
    await box(page).evaluate((el) => (el.scrollTop += 300));
    await expect.poll(() => geometry(page)).toBe(before);
});

test('a click jumps: to the start, and back to the end', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 600);
    await open(page);
    await expect.poll(async () => (await rowNumbers(page)).at(-1)).toBe(599);
    await scrollTo(page, 'middle');
    await expect(toStart(page)).toBeVisible();

    // the whole log is loaded: the jump scrolls, nothing is fetched
    const fetched: string[] = [];
    page.on('request', (r) => {
        if (/\/api\/system\/v1\/log\?/.test(r.url())) fetched.push(r.url());
    });
    await toStart(page).click();
    await expect.poll(async () => (await away(page)).top).toBe(0);
    await expect.poll(async () => (await rowNumbers(page))[0]).toBe(0);
    await expect(page.locator('[data-edge="start"]')).toBeVisible();
    await expect(toStart(page)).toBeHidden();
    await expect(toEnd(page)).toBeVisible();

    await toEnd(page).click();
    await expect.poll(async () => (await rowNumbers(page)).at(-1)).toBe(599);
    await expect.poll(async () => (await away(page)).bottom).toBeLessThanOrEqual(1);
    await expect(toEnd(page)).toBeHidden();
    await expect(toStart(page)).toBeVisible();
    expect(fetched).toEqual([]);
});

test('the start is the log\'s start, not the oldest line loaded: the oldest page is fetched, the boot stays', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 3500);
    await open(page);
    // the newest 1000 of 3500
    await expect.poll(async () => (await rowNumbers(page)).at(-1)).toBe(3499);
    await expect(toEnd(page)).toBeHidden();
    await expect(toStart(page)).toBeVisible();

    const head = page.waitForRequest((r) => /\/api\/system\/v1\/log\?/.test(r.url()) && new URL(r.url()).searchParams.get('head') === '1');
    await toStart(page).click();
    // every boot stays on screen: the request names no boot, and the route does not move
    expect(new URL((await head).url()).searchParams.get('boot')).toBeNull();
    await expect.poll(async () => (await rowNumbers(page))[0]).toBe(0);
    await expect(page.locator('[data-edge="start"]')).toHaveText('Start of the log');
    expect(new URL(page.url()).search).toBe('');
    expect((await away(page)).top).toBe(0);
    await expect(toStart(page)).toBeHidden();
    // the run is the oldest page now, detached from the newest line: the end button brings the tail back
    await expect(toEnd(page)).toBeVisible();
    await toEnd(page).click();
    await expect.poll(async () => (await rowNumbers(page)).at(-1)).toBe(3499);
    await expect.poll(async () => (await away(page)).bottom).toBeLessThanOrEqual(1);
    await expect(toEnd(page)).toBeHidden();
    await expect(toStart(page)).toBeVisible();
});

test('at the top of a loaded run with older pages to come, the start button stays', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 3500);
    await open(page);
    await expect.poll(async () => (await rowNumbers(page)).at(-1)).toBe(3499);
    // the older page is held back, so the view stands at the top of the run of 1000
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    const olderPage = /\/api\/system\/v1\/log\?.*before=/;
    await page.route(olderPage, async (route) => {
        await held;
        await route.continue();
    });
    await scrollTo(page, 'top');
    await expect(page.locator('[data-edge="loading"]')).toBeVisible();
    expect((await away(page)).top).toBe(0);
    await expect(toStart(page)).toBeVisible();
    await expect(toEnd(page)).toBeVisible();
    // the page lands above the view, which keeps its place in the middle of the longer run
    release();
    await expect(page.locator('[data-edge="loading"]')).toHaveCount(0);
    await expect.poll(async () => (await away(page)).top).toBeGreaterThan(1000);
    await page.unroute(olderPage);
    await expect(toStart(page)).toBeVisible();
    await expect(toEnd(page)).toBeVisible();
});

test('a log that fits its box shows neither button', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 3);
    await open(page);
    await expect(page.locator('.lg-box .ol-line')).toHaveCount(3);
    await expect(page.locator('.lg-jump')).toHaveCount(2);
    await expect(toStart(page)).toBeHidden();
    await expect(toEnd(page)).toBeHidden();
    // hidden is out of the tab order, not only out of sight
    expect(await page.locator('.lg-jump').evaluateAll((els) => els.map((e) => getComputedStyle(e).visibility))).toEqual(['hidden', 'hidden']);
});

test('lines that arrive: the view at the end follows without the button, a reader in the history gets it', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 600);
    await page.context().addCookies([{name: 'stub-log-churn', value: '1', url: baseURL!}]);
    await open(page);
    const count = async () => Number(await page.locator('[data-count]').getAttribute('data-count'));
    const lastLine = () => page.locator('.lg-box .ol-line .ol-msg').last();
    if (await page.locator('.lg-filters-toggle').isVisible()) await page.locator('.lg-filters-toggle').click();
    await expect(lastLine()).toHaveText(/churn line/);
    // following: lines arrive, the view stays at the end, the end button stays hidden
    for (let i = 0; i < 3; i++) {
        const n = await count();
        await expect.poll(count).toBeGreaterThan(n);
        await expect(toEnd(page)).toBeHidden();
    }
    await expect(toStart(page)).toBeVisible();

    // at the start: the end button shows and stays while lines arrive below, the start button stays hidden
    await scrollTo(page, 'top');
    await expect(toStart(page)).toBeHidden();
    await expect(toEnd(page)).toBeVisible();
    const n = await count();
    await expect.poll(count).toBeGreaterThan(n + 2);
    expect((await away(page)).top).toBe(0);
    await expect(toStart(page)).toBeHidden();
    await expect(toEnd(page)).toBeVisible();

    // the end button takes the view back to the newest line, and it follows again
    await toEnd(page).click();
    await expect(toEnd(page)).toBeHidden();
    const m = await count();
    await expect.poll(count).toBeGreaterThan(m + 2);
    await expect.poll(async () => (await away(page)).bottom).toBeLessThanOrEqual(40);
    await expect(lastLine()).toHaveText(/churn line/);
    await expect(toEnd(page)).toBeHidden();
    await expect(toStart(page)).toBeVisible();
});

test('newest first: the top button goes to the newest entry, the bottom one to the oldest', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.logOrder', 'newest'));
    await longBoot(page, baseURL!, 3500);
    await open(page);
    await expect.poll(async () => (await rowNumbers(page))[0]).toBe(3499);
    // the view is at the top, where the newest line is
    await expect(page.locator('.lg-jump-top')).toHaveAttribute('data-float', 'end');
    await expect(page.locator('.lg-jump-bottom')).toHaveAttribute('data-float', 'start');
    await expect(page.locator('.lg-jump-top')).toBeHidden();
    await expect(page.locator('.lg-jump-bottom')).toBeVisible();
    await expect(page.locator('.lg-jump-bottom')).toHaveAccessibleName('Jump to the oldest entry');

    // the start is the bottom here, and the view stays on it while the oldest page's rows are measured
    await page.locator('.lg-jump-bottom').click();
    await expect.poll(async () => (await rowNumbers(page)).at(-1)).toBe(0);
    await expect.poll(async () => (await away(page)).bottom).toBeLessThanOrEqual(1);
    await expect(page.locator('.lg-jump-bottom')).toBeHidden();
    await expect(page.locator('.lg-jump-top')).toBeVisible();
    await expect(page.locator('.lg-jump-top')).toHaveAccessibleName('Jump to the newest entry');

    await page.locator('.lg-jump-top').click();
    await expect.poll(async () => (await rowNumbers(page))[0]).toBe(3499);
    await expect.poll(async () => (await away(page)).top).toBe(0);
    await expect(page.locator('.lg-jump-top')).toBeHidden();
    await expect(page.locator('.lg-jump-bottom')).toBeVisible();
});

test('the buttons are named in German too', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await longBoot(page, baseURL!, 600);
    await open(page);
    await scrollTo(page, 'middle');
    await expect(toStart(page)).toHaveAccessibleName('Zum ältesten Eintrag springen');
    await expect(toEnd(page)).toHaveAccessibleName('Zum neuesten Eintrag springen');
    await expect(toEnd(page)).toHaveAttribute('title', 'Zum neuesten Eintrag springen');
});
