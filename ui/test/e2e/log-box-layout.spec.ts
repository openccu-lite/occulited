import {expect, test, type Locator, type Page} from '@playwright/test';

// The log box's layout: no empty lines at its ends (B-149), no text under the floating jump buttons
// (B-151), and a filter row that keeps its height when "Newest" comes (B-150). The stub serves a boot
// of as many entries as the cookie names (task 178); every seventh line is long enough to wrap.

async function longBoot(page: Page, baseURL: string, entries: number) {
    await page.context().addCookies([{name: 'stub-log-pages', value: String(entries), url: baseURL}]);
}

const box = (page: Page): Locator => page.locator('.lg-box');

async function open(page: Page) {
    await page.goto('/system/log');
    await box(page).waitFor({state: 'attached'});
    await expect(page.locator('.lg-box .ol-line').first()).toBeVisible();
}

async function scrollTo(page: Page, where: 'top' | 'middle' | 'bottom') {
    await box(page).evaluate((el, where) => {
        el.scrollTop = where === 'top' ? 0 : where === 'bottom' ? el.scrollHeight : Math.floor((el.scrollHeight - el.clientHeight) / 2);
    }, where);
}

test('B-149: the first entry sits directly under the start row, the last one at the end of the box', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 600);
    await open(page);
    await scrollTo(page, 'top');
    await expect(page.locator('[data-edge="start"]')).toBeVisible();
    await expect.poll(() => box(page).evaluate((el) => el.scrollTop)).toBe(0);
    const top = await box(page).evaluate((el) => {
        const edge = el.querySelector('[data-edge="start"]')!.getBoundingClientRect();
        const edges = el.querySelector('.lg-edges')!.getBoundingClientRect();
        const first = el.querySelector('.ol-line')!.getBoundingClientRect();
        return {edgeToFirst: first.top - edge.bottom, edgesToFirst: first.top - edges.bottom};
    });
    expect(Math.abs(top.edgeToFirst)).toBeLessThan(1);
    expect(Math.abs(top.edgesToFirst)).toBeLessThan(1);

    await scrollTo(page, 'bottom');
    await expect.poll(() => box(page).evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight)).toBeLessThan(1);
    const below = await box(page).evaluate((el) => {
        const lines = el.querySelectorAll('.ol-line');
        const last = lines[lines.length - 1].getBoundingClientRect();
        const r = el.getBoundingClientRect();
        const pad = parseFloat(getComputedStyle(el).paddingBottom);
        // what lies between the last entry and the box's padding at the very bottom of the list
        return r.top + el.clientTop + el.clientHeight - pad - last.bottom;
    });
    expect(Math.abs(below)).toBeLessThan(1);
});

test('B-151: no entry runs under the floating jump buttons, in any state', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 600);
    await open(page);
    for (const where of ['middle', 'top', 'bottom'] as const) {
        await scrollTo(page, where);
        if (where === 'middle') {
            await expect(page.locator('.lg-jump[data-float="start"]')).toBeVisible();
            await expect(page.locator('.lg-jump[data-float="end"]')).toBeVisible();
        }
        const clash = await page.evaluate(() => {
            const buttons = [...document.querySelectorAll('.lg-jump')].map((b) => b.getBoundingClientRect());
            const left = Math.min(...buttons.map((b) => b.left));
            // every entry's text, line by line: the rectangles of its message's text
            const over: string[] = [];
            let wrapped = 0;
            for (const msg of document.querySelectorAll('.lg-box .ol-line .ol-msg')) {
                const range = document.createRange();
                range.selectNodeContents(msg);
                const rects = [...range.getClientRects()];
                if (new Set(rects.map((r) => Math.round(r.top))).size > 1) wrapped++;
                for (const r of rects) if (r.width > 0 && r.right > left) over.push(`${msg.textContent?.slice(0, 20)} ${r.right} > ${left}`);
            }
            return {over, wrapped};
        });
        expect(clash.wrapped, `long lines wrap (${where})`).toBeGreaterThan(0);
        expect(clash.over, where).toEqual([]);
    }
});

test('B-150: the filter row keeps its height when "Newest" appears, and the log does not move', async ({page, baseURL}) => {
    await longBoot(page, baseURL!, 2500);
    await open(page);
    const fold = page.locator('.lg-filters-toggle');
    if (await fold.isVisible()) await fold.click();
    await expect(page.locator('[data-count]')).toHaveAttribute('data-count', '1000');
    const newest = page.locator('[data-jump="newest"]');
    await expect(newest).toBeHidden();
    const geometry = () => page.evaluate(() => {
        const r = (sel: string) => {
            const b = document.querySelector(sel)!.getBoundingClientRect();
            return [b.left, b.top, b.width, b.height].map((v) => Math.round(v * 10) / 10).join(',');
        };
        return [r('.lg-panel'), r('.lg-box'), r('.lg-jump-top'), r('.lg-jump-bottom'), r('[data-jump="newest"]')].join(' | ');
    });
    // the phone's fold opens with a transition: measured once the panel stands still
    const settled = async () => {
        let last = '';
        await expect.poll(async () => {
            const now = await geometry();
            const same = now === last;
            last = now;
            return same;
        }, {intervals: [150]}).toBe(true);
        return last;
    };
    const before = await settled();
    await page.getByRole('button', {name: 'Start of the boot'}).click();
    await expect(page.locator('[data-edge="more"]')).toHaveText('Newer entries load while scrolling.');
    await expect(newest).toBeVisible();
    expect(await settled()).toBe(before);
    // back at the live end it is hidden again, still in its place
    await newest.click();
    await expect(newest).toBeHidden();
    expect(await settled()).toBe(before);
});
