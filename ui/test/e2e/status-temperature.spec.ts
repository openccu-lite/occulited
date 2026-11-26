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
    await expect(p.locator('[data-fact="under_voltage"]')).toHaveText('Supply voltage was too low');
    await expect(p.locator('[data-fact="throttled"]')).toHaveText('Was throttled');
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
            await expect(p.locator('[data-fact="under_voltage"]')).toHaveText(/^Supply voltage was too low \(3×, last at \d\d:\d\d\)$/);
            await expect(p.locator('[data-fact="throttled"]')).toHaveText('Was throttled');
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
        // two lines high (B-168), on a phone too
        expect((await p.locator('[data-throttle-facts]').boundingBox())!.height).toBeGreaterThan(30);
        expect((await p.locator('[data-throttle-facts]').boundingBox())!.height).toBeLessThan(50);
    }
    await thermal(page, baseURL!, 'recurring');
    await page.goto('/');
    await expect(panel(page).locator('[data-fact="under_voltage"]')).toHaveText(/^Spannung war zu niedrig \(3×, zuletzt um \d\d:\d\d\)$/);
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
        // the box of conditions stays two lines high on a phone (B-168)
        const line = (await p.locator('[data-throttle-facts]').boundingBox())!;
        expect(line.height).toBeGreaterThan(30);
        expect(line.height).toBeLessThan(50);
    }
});

// occulited B-168 (the maintainer's screenshot, de, a panel ~570 px wide): "Unterspannung seit dem Start (1×,
// zuletzt um 16:44)" and "Gedrosselt seit dem Start" in one line cut the second chip off mid-word. The box holds two
// lines now, of the same height whatever it holds (nothing, the boot dip, two, three conditions), and every chip is
// whole; the pill's word is "war gedrosselt" (B-186; "seit Start gedrosselt" before), and its tooltip says what that means
// every chip inside the box; whole unless allowed an ellipsis (a phone's 300 px are a few pixels short of the longest
// German chip, which then ends in "…" with its text as its tooltip)
async function chipsWhole(page: Page, ellipsis = false) {
    const line = panel(page).locator('[data-throttle-facts]');
    const lb = (await line.boundingBox())!;
    for (const chip of await line.locator('.tp-fact').all()) {
        const b = (await chip.boundingBox())!;
        expect(b.y + b.height).toBeLessThanOrEqual(lb.y + lb.height + 0.5);
        expect(b.x + b.width).toBeLessThanOrEqual(lb.x + lb.width + 0.5);
        if (!ellipsis) expect(await chip.evaluate((e) => e.scrollWidth <= e.clientWidth)).toBe(true);
        else expect(await chip.evaluate((e) => getComputedStyle(e).textOverflow)).toBe('ellipsis');
        await expect(chip).toHaveAttribute('title', (await chip.textContent())!);
    }
}

test('the facts keep two lines in every state, and no chip is cut', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    const p = panel(page);
    const line = p.locator('[data-throttle-facts]');
    const heights: {line: number; panel: number}[] = [];
    for (const [v, n] of [['pi', 0], ['bootdip', 0], ['recurring', 2], ['capped', 3]] as const) {
        await thermal(page, baseURL!, v);
        await page.goto('/');
        await expect(line).toHaveAttribute('data-throttle-facts', String(n));
        if (v === 'recurring') {
            await expect(p.locator('[data-fact="under_voltage"]')).toHaveText(/^Spannung war zu niedrig \(3×, zuletzt um \d\d:\d\d\)$/);
            await expect(p.locator('[data-fact="throttled"]')).toHaveText('War gedrosselt');
            await expect(p.locator('.tp-pill')).toHaveText('war gedrosselt');
            await expect(p.locator('.tp-pill')).toHaveAttribute('title', /^Seit dem Start wurde der Prozessor mindestens einmal gedrosselt/);
        }
        await chipsWhole(page);
        heights.push({line: (await line.boundingBox())!.height, panel: (await p.boundingBox())!.height});
    }
    for (const h of heights.slice(1)) {
        expect(h.line).toBeCloseTo(heights[0]!.line, 0);
        expect(h.panel).toBeCloseTo(heights[0]!.panel, 0);
    }
    // the maintainer's width, and a phone: the two chips of the screenshot whole
    for (const width of [620, 360]) {
        await page.setViewportSize({width, height: 900});
        await thermal(page, baseURL!, 'recurring');
        await page.goto('/');
        await expect(p.locator('[data-fact="throttled"]')).toBeVisible();
        await chipsWhole(page, width < 400);
        expect((await line.boundingBox())!.height).toBeCloseTo(heights[0]!.line, 0);
    }
});

// occulited task 173 (the maintainer, 2026-11-23: "if my CCU has been throttled since the start, shouldn't the panel
// have the yellow left border and a message on the status page?"): undervoltage now red, a throttling now or since the
// start beyond a boot dip yellow - the panel's edge and the Status warning power-throttle with the supply's advice; a
// boot dip and a quiet firmware nothing; a throttling by the heat alone is soc-hot's
test('the throttle state: the edge and the warning power-throttle', async ({page, baseURL}) => {
    const p = panel(page);
    const notice = page.locator('[data-warnings] [data-notice="power-throttle"]');
    const heights: number[] = [];
    for (const [v, sev] of [['pi', ''], ['bootdip', ''], ['recurring', 'warning'], ['capped', 'warning'], ['crit', 'error'], ['hot', '']] as const) {
        await thermal(page, baseURL!, v);
        await page.goto('/');
        await expect(p).toBeVisible();
        await expect(page.locator('[data-warnings]')).toBeAttached();
        if (!sev) {
            await expect(notice).toHaveCount(0);
            if (v !== 'hot') await expect(p).not.toHaveClass(/\b(warn|err)\b/);
        } else {
            await expect(notice).toBeVisible();
            await expect(notice).toHaveAttribute('data-severity', sev);
            await expect(p).toHaveClass(sev === 'error' ? /\berr\b/ : /\bwarn\b/);
        }
        if (v === 'recurring') {
            await expect(notice).toContainText(/Not now, but since the start the supply voltage was too low \(3×, the last on .+\), and the processor was slowed down\./);
            await expect(notice).toContainText('power supply or its cable');
            await expect(notice).toContainText('The firmware keeps this until the next start.');
        }
        if (v === 'crit') await expect(notice).toContainText('The supply voltage is too low now');
        if (v === 'hot') await expect(page.locator('[data-warnings] [data-notice="soc-hot"]')).toBeVisible();
        heights.push((await p.boundingBox())!.height);
    }
    // the edge moves nothing: the panels without a disk keep their height
    for (const i of [1, 2, 3, 5]) expect(heights[i]).toBeCloseTo(heights[0]!, 0);
    await thermal(page, baseURL!, 'recurring');
    await page.goto('/');
    await notice.getByRole('link', {name: 'Temperature', exact: true}).click();
    await expect(page).toHaveURL(/\/#temperature$/);
    await expect(p).toBeInViewport();
});

test('the warning power-throttle in German', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await thermal(page, baseURL!, 'recurring');
    await page.goto('/');
    const notice = page.locator('[data-warnings] [data-notice="power-throttle"]');
    await expect(notice).toContainText(/Jetzt nicht, aber seit dem Start war die Versorgungsspannung zu niedrig \(3×, zuletzt am .+\), und der Prozessor wurde gebremst\./);
    await expect(notice).toContainText('Meist liegt es am Netzteil oder am Kabel');
    await expect(notice).toContainText('Die Firmware merkt sich das bis zum nächsten Start.');
});

// occulited task 172 (the maintainer, 2026-11-23): the undervoltage episodes as red vertical lines in the temperature
// chart, each with its time and length as the tooltip; a boot dip's says it was at start-up and normalised. The panel
// keeps its box.
test('the undervoltage episodes as red lines in the chart', async ({page, baseURL}) => {
    const p = panel(page);
    const chart = p.locator('.ol-card-foot svg').first();
    const heights: number[] = [];
    for (const [v, n] of [['pi', 0], ['recurring', 3], ['capped', 2], ['bootdip', 1]] as const) {
        await thermal(page, baseURL!, v);
        await page.goto('/');
        await expect(chart).toBeVisible();
        await expect(chart.locator('rect.event')).toHaveCount(n);
        heights.push((await p.boundingBox())!.height);
    }
    for (const h of heights.slice(1)) expect(h).toBeCloseTo(heights[0]!, 0);
    // the boot dip's line (the last state of the loop)
    await expect(chart.locator('rect.event').first()).toHaveAttribute('data-event-label', /^Undervoltage \d\d:\d\d, 2 s \(at start-up, normalised\)$/);
    // in the error colour, and its tooltip under the pointer
    for (const scheme of ['light', 'dark'] as const) {
        await page.emulateMedia({colorScheme: scheme});
        await thermal(page, baseURL!, 'recurring');
        await page.goto('/');
        const line = chart.locator('rect.event').last();
        await expect(line).toHaveAttribute('data-event-label', /^Undervoltage \d\d:\d\d, 6 s$/);
        await expect(line.locator('title')).toHaveText(/^Undervoltage \d\d:\d\d, 6 s$/);
        const [fill, err] = await line.evaluate((e) => {
            const s = document.createElement('span');
            s.style.color = 'var(--hmm-error)';
            document.body.append(s);
            const c = getComputedStyle(s).color;
            s.remove();
            return [getComputedStyle(e).fill, c];
        });
        expect(fill).toBe(err);
        await line.scrollIntoViewIfNeeded();
        const b = (await line.boundingBox())!;
        expect(b.width).toBeGreaterThanOrEqual(1.5);
        // a finger taps where a mouse hovers
        if (test.info().project.name === 'phone') await page.touchscreen.tap(b.x + b.width / 2, b.y + b.height / 2);
        else await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
        await expect(chart.locator('[data-event-hover]')).toHaveText(/^Undervoltage \d\d:\d\d, 6 s$/);
    }
});

// occulited B-186 (the maintainer's production Pi, 2026-11-26): one episode begun the evening before with no end in the
// kernel log and no undervoltage now painted the whole 24 h chart red. An episode runs up to now only while the
// undervoltage holds now; otherwise it is a thin mark at its start. A stretch is a light band under the curve, never a
// saturated block over it, in light and dark.
test('an episode without an end is a thin mark, a stretch a light band under the curve', async ({page, baseURL}) => {
    const p = panel(page);
    const chart = p.locator('.ol-card-foot svg').first();
    for (const v of ['unended', 'ended'] as const) {
        await thermal(page, baseURL!, v);
        await page.goto('/');
        await expect(chart).toBeVisible();
        const ev = chart.locator('rect.event');
        await expect(ev).toHaveCount(1);
        await expect(ev).toHaveAttribute('data-event-label', /^Undervoltage \d\d:\d\d$/);
        await expect(ev).not.toHaveClass(/\bband\b/);
        const b = (await ev.boundingBox())!, c = (await chart.boundingBox())!;
        expect(b.width).toBeLessThanOrEqual(2);
        expect(b.width).toBeLessThan(c.width / 20);
    }
    // undervoltage now (crit): the open episode is a band up to now - a light tint under the line, edges thin
    for (const scheme of ['light', 'dark'] as const) {
        await page.emulateMedia({colorScheme: scheme});
        await thermal(page, baseURL!, 'crit');
        await page.goto('/');
        const band = chart.locator('rect.event.band');
        await expect(band).toHaveCount(1);
        await expect(band).toHaveAttribute('data-event-label', /^Undervoltage since \d\d:\d\d$/);
        const look = await band.evaluate((e) => {
            const s = document.createElement('span');
            s.style.color = 'var(--hmm-error)';
            document.body.append(s);
            const err = getComputedStyle(s).color;
            s.remove();
            const line = e.ownerSVGElement!.querySelector('polyline.line');
            return {fill: getComputedStyle(e).fill, err, opacity: Number(getComputedStyle(e).opacity), under: !!line && !!(e.compareDocumentPosition(line) & Node.DOCUMENT_POSITION_FOLLOWING)};
        });
        expect(look.fill).toBe(look.err);
        expect(look.opacity).toBeLessThanOrEqual(0.2);
        expect(look.under).toBe(true);
        // the moments stay thin lines, muted
        for (const m of await chart.locator('rect.event:not(.band)').all()) {
            expect(Number(await m.evaluate((e) => getComputedStyle(e).opacity))).toBeLessThanOrEqual(0.7);
            expect((await m.boundingBox())!.width).toBeLessThanOrEqual(2);
        }
        await expect(chart.locator('line.event-edge')).toHaveCount(1);
    }
});

// occulited B-186: "seit Start gedrosselt" beside "1,2 / 1,2 GHz" read as throttled now - the pill says "war
// gedrosselt", its tooltip that the clock is full now; the pill keeps its box; the warning's advice names the RPI-RF-MOD
// where the power supply goes into it, and sends nobody to the Pi's socket where that is not known
test('throttled earlier at full clock, and the power supply through the RPI-RF-MOD', async ({page, baseURL}) => {
    const p = panel(page);
    const pill = p.locator('.tp-pill');
    const notice = page.locator('[data-warnings] [data-notice="power-throttle"]');
    const widths: number[] = [];
    for (const lang of ['en', 'de'] as const) {
        await page.addInitScript((l) => localStorage.setItem('ol.language', l), lang);
        await thermal(page, baseURL!, 'unended');
        await page.goto('/');
        await expect(pill).toHaveText(lang === 'de' ? 'war gedrosselt' : 'throttled earlier');
        // the facts in the past tense too, each chip's tooltip its text (B-168)
        await expect(p.locator('[data-fact="under_voltage"]')).toHaveText(lang === 'de' ? /^Spannung war zu niedrig \(1×, zuletzt um \d\d:\d\d\)$/ : /^Supply voltage was too low \(1×, last at \d\d:\d\d\)$/);
        await expect(p.locator('[data-fact="throttled"]')).toHaveText(lang === 'de' ? 'War gedrosselt' : 'Was throttled');
        await chipsWhole(page);
        await expect(pill).toHaveAttribute('title', lang === 'de' ? /\nDer Prozessor läuft jetzt mit vollem Takt \(1,2 \/ 1,2 GHz\)\.\n/ : /\nThe processor runs at its full clock now \(1\.2 \/ 1\.2 GHz\)\.\n/);
        await expect(notice).toContainText(lang === 'de'
            ? 'Mit dem RPI-RF-MOD wird das System über das Funkmodul versorgt: Verwenden Sie das dafür vorgesehene Netzteil von ELV und schließen Sie es am RPI-RF-MOD an, nicht am Raspberry Pi.'
            : 'With the RPI-RF-MOD the system is powered through the radio module: use the ELV power supply meant for it, plugged into the RPI-RF-MOD, not into the Raspberry Pi.');
        await expect(notice).not.toContainText(lang === 'de' ? 'offizielle Netzteil' : 'official power supply');
        widths.push((await pill.boundingBox())!.width);
        // the pill keeps its box: normal is as wide as "throttled earlier" and "undervoltage"
        await thermal(page, baseURL!, 'pi');
        await page.goto('/');
        await expect(pill).toHaveText('normal');
        expect((await pill.boundingBox())!.width).toBeCloseTo(widths[widths.length - 1]!, 0);
        await thermal(page, baseURL!, 'crit');
        await page.goto('/');
        expect((await pill.boundingBox())!.width).toBeCloseTo(widths[widths.length - 1]!, 0);
        // the supply not known: generic, and an RPI-RF-MOD's goes into the module
        await thermal(page, baseURL!, 'ended');
        await page.goto('/');
        await expect(notice).toContainText(lang === 'de' ? 'Mit einem RPI-RF-MOD auf dem Raspberry Pi wird das Netzteil am Funkmodul angeschlossen, nicht am Raspberry Pi.' : 'With an RPI-RF-MOD on the Raspberry Pi, the power supply goes into the radio module, not into the Raspberry Pi.');
    }
});

test('the chart lines in German', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await thermal(page, baseURL!, 'bootdip');
    await page.goto('/');
    await expect(panel(page).locator('.ol-card-foot svg rect.event')).toHaveAttribute('data-event-label', /^Unterspannung \d\d:\d\d, 2 s \(beim Start, normalisiert\)$/);
});

// occulited task 177 (the maintainer, 2026-11-24): the processor's clock, current / maximum, and
// when each throttling bit first held - with the temperature then - instead of "since the boot"
test('the clock, and when and how hot the clock was capped', async ({page, baseURL}) => {
    await thermal(page, baseURL!, 'clockcap');
    await page.goto('/');
    const p = panel(page);
    const clk = p.locator('li[data-cpu-clock="firmware"]');
    await expect(clk).toContainText('Processor clock');
    await expect(clk.locator('.tp-clock')).toHaveText('1.2 / 1.2 GHz');
    const capped = p.locator('[data-fact="freq_capped"]');
    await expect(capped).toHaveText(/^Clock capped at (\d+\/\d+ )?\d\d:\d\d at 79\.2 °C$/);
    await expect(capped).toHaveAttribute('title', /Processor clock then: 1\.0 GHz/);
    // set at the first read within the boot phase: since the start-up, no time
    await expect(p.locator('[data-fact="under_voltage"]')).toHaveText('Supply voltage was too low at start-up');
    // the facts' box keeps its two lines
    const h = await p.locator('.tp-throttle').evaluate((e) => e.getBoundingClientRect().height);
    await thermal(page, baseURL!, 'pi');
    await page.reload();
    await expect(p.locator('.tp-clock')).toHaveText('0.6 / 1.5 GHz');
    expect(await p.locator('.tp-throttle').evaluate((e) => e.getBoundingClientRect().height)).toBeCloseTo(h, 0);

    await page.evaluate(() => localStorage.setItem('ol.language', 'de'));
    await thermal(page, baseURL!, 'clockcap');
    await page.reload();
    await expect(p.locator('.tp-clock')).toHaveText('1,2 / 1,2 GHz');
    await expect(p.locator('li[data-cpu-clock]')).toContainText('Prozessortakt');
    await expect(p.locator('[data-fact="freq_capped"]')).toHaveText(/^Takt begrenzt um (\d+\.\d+\. )?\d\d:\d\d bei 79,2 °C$/);
});
