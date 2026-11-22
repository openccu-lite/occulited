import type {Page} from '@playwright/test';
import {expect, test} from './fixtures';
import {pageWidth} from './scroll';

// occulited task 148 (the maintainer, 2026-11-15): the temperature panel beside the disks - a ring
// per reading like the storage panel, the legend in words and figures, a Pi's throttling state as a
// pill that keeps its box, the processor's temperature over time; a Status warning when hot (from
// 80 °C on a Pi) whose button leads to the panel; and no panel where there is no sensor (the VM).

async function thermal(page: Page, baseURL: string, v: string) {
    await page.context().addCookies([{name: 'stub-thermal', value: v, url: baseURL}]);
}
const panel = (page: Page) => page.locator('[data-temp-panel]');

test('the VM has no sensor and no panel', async ({page}) => {
    await page.goto('/');
    await expect(page.locator('[data-storage-device="sda"]')).toBeVisible();
    await expect(panel(page)).toHaveCount(0);
    await expect(page.locator('[data-notice="soc-hot"]')).toHaveCount(0);
});

test('a Pi: one ring, the processor in words and figures, the firmware normal, the history', async ({page, baseURL}) => {
    await thermal(page, baseURL!, 'pi');
    await page.goto('/');
    const p = panel(page);
    await expect(p).toBeVisible();
    // among the Utilisation panels
    await expect(page.locator('.ol-gauges [data-temp-panel]')).toHaveCount(1);
    await expect(p.locator('.ol-card-title')).toHaveText('Temperature');
    await expect(p.locator('circle.arc')).toHaveCount(1);
    await expect(p.locator('li[data-reading]')).toHaveCount(1);
    const soc = p.locator('li[data-reading="thermal_zone0"]');
    await expect(soc).toContainText('Processor');
    await expect(soc.locator('[data-celsius]')).toHaveText('54.8 °C');
    await expect(soc).toHaveAttribute('title', /hot from 80 °C, limit 85 °C/);
    // 54.8 of 85 °C: the arc's share of its circumference
    const share = await p.locator('circle.arc').evaluate((c) => {
        const [a, b] = (c.getAttribute('stroke-dasharray') ?? '').split(' ').map(Number);
        return a! / b!;
    });
    expect(share).toBeCloseTo(54.8 / 85, 2);
    await expect(p.locator('[data-throttle]').first()).toHaveText('normal');
    await expect(p.locator('[data-throttle-facts="0"]')).toContainText('No undervoltage, no throttling since the boot');
    await expect(p).not.toHaveClass(/\b(warn|err)\b/);
    // the processor's history in the foot, with its peak over the span
    await expect(p.locator('.ol-card-foot svg').first()).toBeVisible();
    await expect(p.locator('[data-temp-peak]')).toContainText('Processor: max');
});

// openccu-lite task 490 (the maintainer, 2026-11-22): Memory, Load, Temperature, then the disks and
// the rest as before - in the grid's order on a desktop and on a phone, where the grid is one column
test('the panel order: memory, load, temperature, then the disks', async ({page, baseURL}) => {
    await thermal(page, baseURL!, 'ssd');
    await page.goto('/');
    await expect(panel(page)).toBeVisible();
    const order = await page.locator('.ol-gauges > .ol-card').evaluateAll((cs) =>
        cs.map((c) => (c.hasAttribute('data-load-card') ? 'load' : c.hasAttribute('data-temp-panel') ? 'temp' : c.querySelector('[data-storage-device]') || c.hasAttribute('data-storage-device') ? 'disk' : c.querySelector('.ol-card-title')?.textContent?.trim())),
    );
    expect(order.slice(0, 4)).toEqual(['Memory', 'load', 'temp', 'disk']);
    // what the eye reads: each one after the one before (left to right, then down)
    const boxes = await page.locator('.ol-gauges > .ol-card').evaluateAll((cs) => cs.slice(0, 4).map((c) => c.getBoundingClientRect()).map((r) => ({x: r.x, y: r.y})));
    for (let i = 1; i < boxes.length; i++) {
        const a = boxes[i - 1]!, b = boxes[i]!;
        expect(b.y > a.y + 1 || (Math.abs(b.y - a.y) <= 1 && b.x > a.x)).toBe(true);
    }
    // the anchor still leads to the panel
    await page.goto('/#temperature');
    await expect(panel(page)).toBeInViewport();
});

test('a Pi with a USB SSD: two rings, the disk by its model, undervoltage since the boot', async ({page, baseURL}) => {
    await thermal(page, baseURL!, 'ssd');
    await page.goto('/');
    const p = panel(page);
    await expect(p.locator('circle.arc')).toHaveCount(2);
    const radii = await p.locator('circle.arc').evaluateAll((cs) => cs.map((c) => Number(c.getAttribute('r'))));
    expect(radii[0]! > radii[1]!).toBe(true);
    const strokes = await p.locator('circle.arc').evaluateAll((cs) => cs.map((c) => getComputedStyle(c).stroke));
    expect(new Set(strokes).size).toBe(2);
    const disk = p.locator('li[data-reading="disk:sda"]');
    await expect(disk).toContainText('Samsung PSSD T7');
    await expect(disk.locator('[data-celsius]')).toHaveText('38.0 °C');
    await expect(disk).toHaveAttribute('title', /USB drive Samsung PSSD T7 \(sda\).*read at/);
    await expect(p.locator('.tp-pill')).toHaveText('throttled earlier');
    await expect(p.locator('[data-fact="under_voltage"]')).toHaveText('Undervoltage since the boot');
    await expect(p.locator('[data-fact="throttled"]')).toHaveText('Throttled since the boot');
});

test('hot: the warning above, its button to the panel, the panel amber; the pill and the figures keep their box', async ({page, baseURL}) => {
    await thermal(page, baseURL!, 'pi');
    await page.goto('/');
    const p = panel(page);
    const pill = p.locator('.tp-pill');
    const val = p.locator('li[data-reading="thermal_zone0"] [data-celsius]');
    await expect(pill).toHaveText('normal');
    const before = {pill: (await pill.boundingBox())!, val: (await val.boundingBox())!, panel: (await p.boundingBox())!};
    for (const [v, word, sev] of [['hot', 'throttled', 'warning'], ['crit', 'undervoltage', 'error']] as const) {
        await thermal(page, baseURL!, v);
        await page.goto('/');
        await expect(pill).toHaveText(word);
        const notice = page.locator('[data-warnings] [data-notice="soc-hot"]');
        await expect(notice).toBeVisible();
        await expect(notice).toHaveAttribute('data-severity', sev);
        await expect(p).toHaveClass(sev === 'error' ? /\berr\b/ : /\bwarn\b/);
        const after = {pill: (await pill.boundingBox())!, val: (await val.boundingBox())!, panel: (await p.boundingBox())!};
        expect(after.pill.width).toBeCloseTo(before.pill.width, 0);
        expect(after.pill.height).toBeCloseTo(before.pill.height, 0);
        expect(after.val.width).toBeCloseTo(before.val.width, 0);
        expect(after.panel.height).toBeCloseTo(before.panel.height, 0);
    }
    // crit: the processor at its limit, a hot disk, undervoltage now
    const soc = page.locator('[data-warnings] [data-notice="soc-hot"]');
    await expect(soc).toContainText('The processor is at 85.3 °C, at its limit of 85 °C');
    const disk = page.locator('[data-warnings] [data-notice="disk-hot"]');
    await expect(disk).toHaveAttribute('data-severity', 'warning');
    await expect(disk).toContainText('Samsung PSSD T7 (sda) at 72.0 °C (rated up to 70 °C)');
    await expect(p.locator('[data-fact="under_voltage"]')).toHaveText('Undervoltage now');
    await expect(p.locator('li[data-reading="disk:sda"] [data-celsius]')).toHaveClass(/\bwarn\b/);
    await soc.getByRole('link', {name: 'Temperature', exact: true}).click();
    await expect(page).toHaveURL(/\/#temperature$/);
    await expect(p).toBeInViewport();
});

// occulited B-162 (the maintainer's Pi 3: one dip of 2 s at 9.66 s showed as "Unterspannung seit dem
// Start" for the whole uptime): a short dip at the start that normalised is an info line, not a
// warning; a dip that recurs is the warning with how often and when; undervoltage now is red - and
// neither the pill nor the line changes its size between the three
test('undervoltage: a boot dip is an info line, a recurring one a warning, one now red - each in the same box', async ({page, baseURL}) => {
    const p = panel(page);
    const pill = p.locator('.tp-pill');
    const line = p.locator('[data-throttle-facts]');
    const boxes: {pill: {width: number; height: number}; line: {height: number}; panel: {height: number}}[] = [];
    for (const v of ['bootdip', 'recurring', 'crit'] as const) {
        await thermal(page, baseURL!, v);
        await page.goto('/');
        await expect(p).toBeVisible();
        if (v === 'bootdip') {
            await expect(pill).toHaveText('normal');
            await expect(pill).toHaveClass(/\bgood\b/);
            const dip = p.locator('[data-fact="boot_dip"]');
            await expect(dip).toHaveText('Brief undervoltage at start-up (2 s, normalised)');
            await expect(p.locator('[data-fact="under_voltage"], [data-fact="throttled"]')).toHaveCount(0);
            await expect(line).toHaveAttribute('title', /no warning/);
            await expect(line).toHaveAttribute('title', /Undervoltage episodes since the boot: 1, 2 s together/);
            // quiet: the muted outline, not the warning's colour
            const [c, warn] = await dip.evaluate((e) => [getComputedStyle(e).color, getComputedStyle(e).getPropertyValue('--hmm-warn')]);
            expect(c).not.toBe(warn.trim());
            await expect(p).not.toHaveClass(/\b(warn|err)\b/);
        } else if (v === 'recurring') {
            await expect(pill).toHaveText('throttled earlier');
            await expect(pill).toHaveClass(/\bwarn\b/);
            await expect(p.locator('[data-fact="under_voltage"]')).toHaveText(/^Undervoltage since the boot \(3×, last at \d\d:\d\d\)$/);
            await expect(p.locator('[data-fact="throttled"]')).toHaveText('Throttled since the boot');
            await expect(p.locator('[data-fact="boot_dip"]')).toHaveCount(0);
        } else {
            await expect(pill).toHaveText('undervoltage');
            await expect(pill).toHaveClass(/\bbad\b/);
            await expect(p.locator('[data-fact="under_voltage"]')).toHaveText('Undervoltage now');
        }
        boxes.push({pill: (await pill.boundingBox())!, line: (await line.boundingBox())!, panel: (await p.boundingBox())!});
    }
    for (const b of boxes.slice(1)) {
        expect(b.pill.width).toBeCloseTo(boxes[0]!.pill.width, 0);
        expect(b.pill.height).toBeCloseTo(boxes[0]!.pill.height, 0);
        expect(b.line.height).toBeCloseTo(boxes[0]!.line.height, 0);
    }
    // the two without a disk have the same panel
    expect(boxes[1]!.panel.height).toBeCloseTo(boxes[0]!.panel.height, 0);
});

test('the boot dip in German, in both themes, on a phone', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await thermal(page, baseURL!, 'bootdip');
    await page.setViewportSize({width: 360, height: 780});
    for (const scheme of ['light', 'dark'] as const) {
        await page.emulateMedia({colorScheme: scheme});
        await page.goto('/');
        const p = panel(page);
        await expect(p.locator('.tp-pill')).toHaveText('normal');
        await expect(p.locator('[data-fact="boot_dip"]')).toHaveText('Kurze Unterspannung beim Start (2 s, normalisiert)');
        await expect(p.locator('[data-fact="boot_dip"]')).toBeVisible();
        expect(await pageWidth(page)).toBeLessThanOrEqual(360);
        expect((await p.locator('[data-throttle-facts]').boundingBox())!.height).toBeLessThan(30);
    }
    await thermal(page, baseURL!, 'recurring');
    await page.goto('/');
    await expect(panel(page).locator('[data-fact="under_voltage"]')).toHaveText(/^Unterspannung seit dem Start \(3×, zuletzt um \d\d:\d\d\)$/);
});

test('in German, on a phone, in both themes', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await thermal(page, baseURL!, 'crit');
    for (const scheme of ['light', 'dark'] as const) {
        await page.emulateMedia({colorScheme: scheme});
        await page.setViewportSize({width: 360, height: 780});
        await page.goto('/');
        const p = panel(page);
        await expect(p.locator('.ol-card-title')).toHaveText('Temperatur');
        await expect(p.locator('li[data-reading="thermal_zone0"]')).toContainText('Prozessor');
        await expect(p.locator('li[data-reading="thermal_zone0"] [data-celsius]')).toHaveText('85,3 °C');
        await expect(p.locator('.tp-pill')).toHaveText('Unterspannung');
        await expect(p.locator('[data-fact="under_voltage"]')).toHaveText('Unterspannung jetzt');
        await expect(page.locator('[data-notice="soc-hot"]')).toContainText('Der Prozessor hat 85,3 °C');
        await expect(page.locator('[data-notice="disk-hot"]')).toContainText('mit 72,0 °C (ausgelegt bis 70 °C)');
        expect(await pageWidth(page)).toBeLessThanOrEqual(360);
        // the line of conditions stays one line high on a phone
        const line = (await p.locator('[data-throttle-facts]').boundingBox())!;
        expect(line.height).toBeLessThan(30);
    }
});
