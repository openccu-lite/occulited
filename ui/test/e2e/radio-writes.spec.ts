import {expect, test, type Page} from '@playwright/test';
import {fitsWindow} from './scroll';

// occulited task 163: the radio writes per lite-rpc client - the Status page's panel (a row per
// client with its rate, the span as bars, the span's sum and the share of writes that changed
// nothing; the duty cycle of each radio on the same time axis below), and the API tokens' column
// with each token's rate and bars, marked when it is behind a hint or a warning. openccu-lite task
// 489: the spans 1 h, 6 h and 24 h, one duty-cycle row per physical radio, the panel's width used.

async function open(page: Page, baseURL: string | undefined, path: string, variant = '', span?: number) {
    await page.context().addCookies([{name: 'stub-radio-writes', value: variant, url: baseURL!}]);
    // the page's one span for every chart (24 h by default); a test of the minutes asks for 1 h
    if (span) await page.addInitScript((s) => localStorage.setItem('ol.charts.span', String(s)), span);
    await page.goto(path);
}
const client = (page: Page, key: string) => page.locator(`[data-radio-client="${key}"]`);

test('the Status page lists each client with its rate, its hour and what changed nothing', async ({page, baseURL}) => {
    await open(page, baseURL, '/', 'busy', 60);
    await expect(page.getByRole('heading', {level: 2, name: 'Radio writes'})).toBeVisible();
    const panel = page.locator('[data-radio-writes]');
    await expect(panel).toHaveAttribute('data-span', '60');
    await expect(panel.locator('[data-radio-client]')).toHaveCount(3);

    // the client behind the duty cycle's rise first, with the paired program's label
    const mqtt = client(page, 'token:hm2mqtt');
    await expect(mqtt).toHaveAttribute('data-level', 'warning');
    await expect(mqtt.locator('.rw-clip')).toHaveText('hm2mqtt · hm2mqtt.js');
    await expect(mqtt.locator('.rw-kind')).toHaveText('program · BidCos-RF, HmIP-RF');
    await expect(mqtt.locator('[data-radio-level="warning"]')).toHaveText('Warning');
    await expect(mqtt.locator('[data-radio-level="warning"]')).toHaveAttribute('title', 'The duty cycle of HmIP-RF rose from 3 % to 24 %, and this client sent 85 % of the radio writes to it meanwhile.');
    await expect(mqtt.locator('[data-radio-rate]')).toHaveText('6');
    await expect(mqtt.locator('[data-radio-hour]')).toHaveText('808');
    await expect(mqtt.locator('[data-radio-noop]')).toHaveText('62 %');
    await expect(panel.locator('[data-radio-sum-head]')).toHaveText('In 1 h');

    const ci = client(page, 'token:ci-nightly');
    await expect(ci.locator('[data-radio-level="hint"]')).toHaveText('Hint');
    await expect(ci.locator('[data-radio-level="hint"]')).toHaveAttribute('title', 'Up to 84 radio writes in one minute.');
    await expect(ci.locator('[data-radio-noop]')).toHaveText('0 %');
    // a minute is a bar: the burst of 84 and the 12 after it, nothing else
    await expect(ci.locator('[data-spark-bar]')).toHaveCount(2);
    await expect(ci.locator('[data-spark-bar="84"]')).toHaveCount(1);
    // the busiest minute is 84, so every row is drawn to 100 - the scale in the column's head, no
    // caption under the table (openccu-lite task 489) - and the 60 of the hint is marked
    await expect(ci.locator('[data-spark-threshold]')).toHaveCount(1);
    await expect(panel.locator('thead')).toContainText('Writes per minute');
    await expect(panel.locator('[data-radio-scale]')).toHaveText('0–100');
    await expect(panel.locator('.rw-foot')).toHaveCount(0);
    await expect(panel).not.toContainText('Now is at the right');
    // under the pointer: the minute and its count; under the charts the time, ending now
    await expect(ci.locator('svg rect.hit').nth(50).locator('title')).toHaveText('9 minutes ago: 84');
    await expect(ci.locator('svg rect.hit').nth(59).locator('title')).toHaveText('this minute: 0');
    await expect(panel.locator('[data-radio-now]')).toHaveText('now');

    const admin = client(page, 'user:admin');
    await expect(admin).toHaveAttribute('data-level', '');
    await expect(admin.locator('.rw-kind')).toHaveText('account · HmIP-RF');
    await expect(admin.locator('[data-radio-level]')).toHaveCount(0);
    await expect(admin.locator('[data-radio-noop]')).toHaveText('50 %');

    // one radio module shared by BidCos-RF and HmIP-RF (multimacd): one duty-cycle row, named as the
    // radio with both interfaces under it, a line with its last reading and its own scale
    await expect(panel.locator('tr.rw-duty')).toHaveCount(1);
    const duty = panel.locator('[data-radio-duty="BidCos-RF,HmIP-RF"]');
    await expect(duty.locator('.rw-clip')).toHaveText('RPI-RF-MOD 1F0000000A03');
    await expect(duty.locator('.rw-kind')).toHaveText('Duty cycle · BidCos-RF, HmIP-RF');
    expect(await duty.locator('.rw-kind').evaluate((e) => e.scrollWidth <= e.clientWidth)).toBe(true);
    await expect(duty.locator('[data-radio-duty-ifs]')).toHaveText('BidCos-RF, HmIP-RF');
    await expect(duty.locator('[data-radio-dc]')).toHaveText('24 %');
    await expect(duty.locator('[data-radio-dc-top]')).toHaveText('0–25 %');
    await expect(duty.locator('svg[data-spark="line"]')).toBeVisible();
    // a minute without a reading breaks the line
    expect(await duty.locator('svg polyline').count()).toBe(2);
    await expect(duty.locator('svg rect.hit').nth(10).locator('title')).toHaveText('49 minutes ago: –');
    await expect(panel.locator('[data-radio-duty-address]')).toHaveCount(0);
    await expect(panel.locator('[data-radio-writes-classic]')).toHaveCount(0);
    expect(await fitsWindow(page)).toBe(true);
});

// openccu-lite task 489: the spans of the page's other charts - 6 h by five minutes, 24 h by a
// quarter hour - with the span's sums, and the bar's time under the pointer
test('the spans 1 h, 6 h and 24 h: the bars, the sums and the clients of each', async ({page, baseURL}) => {
    await open(page, baseURL, '/', 'busy', 60);
    const panel = page.locator('[data-radio-writes]');
    const ci = client(page, 'token:ci-nightly');
    await expect(ci.locator('svg rect.hit')).toHaveCount(60);

    await panel.locator('[data-radio-span="360"]').click();
    await expect(panel).toHaveAttribute('data-span', '360');
    await expect(panel.locator('[data-radio-span="360"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(ci.locator('svg rect.hit')).toHaveCount(72);
    await expect(ci.locator('[data-spark-bar="96"]')).toHaveCount(1);
    // the dotted line is a minute's: not in five-minute bars
    await expect(ci.locator('[data-spark-threshold]')).toHaveCount(0);
    await expect(panel.locator('thead')).toContainText('Writes per 5 minutes');
    await expect(panel.locator('[data-radio-sum-head]')).toHaveText('In 6 h');
    await expect(panel.locator('[data-radio-scale]')).toHaveText('0–500');
    await expect(ci.locator('[data-radio-hour]')).toHaveText('96');
    await expect(client(page, 'user:admin').locator('[data-radio-hour]')).toHaveText('2');
    await expect(ci.locator('svg rect.hit').nth(70).locator('title')).toHaveText(/^\d\d:\d\d–\d\d:\d\d: 96$/);
    await expect(client(page, 'token:nightly-backup')).toHaveCount(0);
    // the duty cycle on the same five minutes
    await expect(panel.locator('tr.rw-duty svg rect.hit')).toHaveCount(72);

    await panel.locator('[data-radio-span="1440"]').click();
    await expect(panel).toHaveAttribute('data-span', '1440');
    await expect(ci.locator('svg rect.hit')).toHaveCount(96);
    await expect(panel.locator('thead')).toContainText('Writes per 15 minutes');
    await expect(panel.locator('[data-radio-sum-head]')).toHaveText('In 24 h');
    // a program that wrote only in the morning is there in 24 h, with nothing in the hour
    const backup = client(page, 'token:nightly-backup');
    await expect(backup.locator('[data-radio-hour]')).toHaveText('300');
    await expect(backup.locator('[data-radio-rate]')).toHaveText('0');
    await expect(backup.locator('.rw-kind')).toHaveText('program · BidCos-RF');
    // the page's one span: the history charts above follow, and it is kept for the next visit
    await expect(page.locator('.chart .spans button[aria-pressed="true"]').first()).toHaveText('24 h');
    expect(await page.evaluate(() => localStorage.getItem('ol.charts.span'))).toBe('1440');
    // and a chart above switches the panel too
    await page.locator('.chart .spans button', {hasText: '1 h'}).first().click();
    await expect(page.locator('[data-radio-writes]')).toHaveAttribute('data-span', '60');
});

test('nothing changes its box with the level or the span: every sparkline, every column the same', async ({page, baseURL}) => {
    await open(page, baseURL, '/', 'busy', 60);
    await expect(client(page, 'token:hm2mqtt')).toBeVisible();
    const panel = page.locator('[data-radio-writes]');
    const boxes = async (sel: string) => page.locator(`[data-radio-writes] tbody tr ${sel}`).evaluateAll((els) => els.map((e) => {
        const r = e.getBoundingClientRect();
        return `${Math.round(r.x)} ${Math.round(r.width)} ${Math.round(r.height)}`;
    }));
    // a click on the phone scrolls the table sideways to the switch: measured from its start
    const cols = async () => {
        await page.locator('[data-radio-writes] .rw-scroll').evaluate((e) => (e.scrollLeft = 0));
        return page.locator('[data-radio-writes] thead th').evaluateAll((els) => els.map((e) => `${Math.round(e.getBoundingClientRect().x)} ${Math.round(e.getBoundingClientRect().width)} ${Math.round(e.getBoundingClientRect().height)}`).join(' | '));
    };
    const sparks = await boxes('svg');
    expect(sparks).toHaveLength(4);
    expect(new Set(sparks).size).toBe(1);
    expect(new Set(await boxes('td.rw-badge')).size).toBe(1);
    const heights = await page.locator('[data-radio-client]').evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().height)));
    expect(new Set(heights).size).toBe(1);
    const before = await cols();
    const axisBox = async () => {
        const b = (await page.locator('[data-radio-axis]').boundingBox())!;
        return `${Math.round(b.x)} ${Math.round(b.width)} ${Math.round(b.height)}`;
    };
    const axis = await axisBox();
    for (const span of ['360', '1440', '60']) {
        await panel.locator(`[data-radio-span="${span}"]`).click();
        await expect(panel).toHaveAttribute('data-span', span);
        expect(await cols()).toBe(before);
        expect(new Set([...(await boxes('svg')), sparks[0]]).size).toBe(1);
        expect(await axisBox()).toBe(axis);
    }
});

// openccu-lite task 489: the table fills the panel, the chart column takes what the others leave,
// and no header wraps
for (const width of [1280, 1920]) {
    test(`the panel uses its width at ${width} px`, async ({page, baseURL}) => {
        await page.setViewportSize({width, height: 900});
        await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
        await open(page, baseURL, '/', 'busy', 360);
        const panel = page.locator('[data-radio-writes]');
        await expect(client(page, 'token:hm2mqtt')).toBeVisible();
        const box = (await panel.boundingBox())!;
        const table = (await panel.locator('table').boundingBox())!;
        expect(table.width).toBeGreaterThan(box.width - 40);
        const spark = (await client(page, 'token:hm2mqtt').locator('svg').boundingBox())!;
        expect(spark.width).toBeGreaterThan(table.width * (width === 1280 ? 0.3 : 0.45));
        // every header on one line, none cut: "Ändern nichts" among them
        const heads = await panel.locator('thead th').evaluateAll((els) => els.map((e) => ({text: e.textContent, cut: e.scrollWidth > e.clientWidth, h: Math.round(e.getBoundingClientRect().height)})));
        expect(heads.filter((h) => h.cut)).toEqual([]);
        expect(new Set(heads.map((h) => h.h)).size).toBe(1);
        await expect(panel.locator('thead')).toContainText('Ändern nichts');
    });
}

test('the panel says so when nobody wrote, and is not there on a system that does not count', async ({page, baseURL}) => {
    await open(page, baseURL, '/', '', 60);
    await expect(page.getByRole('heading', {level: 2, name: 'Radio writes'})).toBeVisible();
    await expect(page.locator('[data-radio-writes-none]')).toHaveText('No program has sent a radio write in the last 1 h.');
    await expect(page.locator('[data-radio-writes] table')).toHaveCount(0);
    // the span switch is there without a table as well
    await page.locator('[data-radio-writes] [data-radio-span="1440"]').click();
    await expect(page.locator('[data-radio-writes-none]')).toHaveText('No program has sent a radio write in the last 24 h.');

    await open(page, baseURL, '/', 'off');
    await expect(page.getByRole('heading', {level: 2, name: 'Components'})).toBeVisible();
    await expect(page.getByRole('heading', {level: 2, name: 'Radio writes'})).toHaveCount(0);
});

test('with classic RPC on the panel says whom it does not count; separate radios are separate rows', async ({page, baseURL}) => {
    await open(page, baseURL, '/', 'busy-classic', 60);
    const note = page.locator('[data-radio-writes-classic]');
    await expect(note).toContainText('Classic RPC is on: programs that use its ports reach the interfaces directly and are not counted here.');
    // rfd's two LAN gateways and HmIP's own stick: three radios, three rows
    await expect(page.locator('[data-radio-writes] tr.rw-duty')).toHaveCount(3);
    // two radios of one name: each row names its own address, in full
    const addresses = page.locator('[data-radio-duty="BidCos-RF"] [data-radio-duty-address]');
    await expect(addresses).toHaveText(['JEQ0000001', 'JEQ9000001']);
    expect(await page.locator('[data-radio-duty="BidCos-RF"] .rw-clip').first().evaluate((e) => e.scrollWidth <= e.clientWidth)).toBe(true);
    const hmip = page.locator('[data-radio-duty="HmIP-RF"]');
    await expect(hmip.locator('.rw-clip')).toHaveText('HmIP-RFUSB 1F0000000A03');
    await expect(hmip.locator('[data-radio-duty-ifs]')).toHaveText('HmIP-RF');
    await expect(hmip.locator('[data-radio-duty-address]')).toHaveCount(0);
    await note.getByRole('link', {name: 'Remote access'}).click();
    await expect(page).toHaveURL(/\/system\/remote-access$/);
});

test('the panel in German', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await open(page, baseURL, '/', 'busy', 360);
    await expect(page.getByRole('heading', {level: 2, name: 'Schreibbefehle per Funk'})).toBeVisible();
    await expect(client(page, 'token:hm2mqtt').locator('[data-radio-level="warning"]')).toHaveText('Warnung');
    await expect(client(page, 'token:ci-nightly').locator('[data-radio-level="hint"]')).toHaveText('Hinweis');
    const head = page.locator('[data-radio-writes] thead');
    await expect(head).toContainText('Ändern nichts');
    await expect(head).toContainText('Befehle je 5 Minuten');
    await expect(page.locator('[data-radio-sum-head]')).toHaveText('In 6 h');
    await expect(page.locator('[data-radio-now]')).toHaveText('jetzt');
});

test('the API tokens show each token\'s rate and bars, and lead to the panel', async ({page, baseURL}) => {
    await open(page, baseURL, '/system/remote-access', 'busy');
    const cell = (name: string) => page.locator(`[data-token-writes="${name}"]`);
    await expect(page.locator('.ol-tokens thead')).toContainText('Radio writes');
    await expect(cell('hm2mqtt')).toHaveAttribute('data-level', 'warning');
    await expect(cell('hm2mqtt').locator('.ol-tok-num')).toHaveText('6/min');
    await expect(cell('hm2mqtt').locator('svg[data-spark="bars"]')).toBeVisible();
    await expect(cell('ci-nightly').locator('[data-radio-level="hint"]')).toHaveText('Hint');
    // the cells are the same box with a badge, another badge
    const w = await page.locator('[data-token-writes]').evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().width)));
    expect(new Set(w).size).toBe(1);
    await cell('hm2mqtt').locator('[data-radio-level="warning"]').click();
    await expect(page).toHaveURL(/\/#radio-writes$/);
    await expect(page.getByRole('heading', {level: 2, name: 'Radio writes'})).toBeInViewport();
});

test('a token that sent nothing has a dash, and a system that does not count has no column', async ({page, baseURL}) => {
    await open(page, baseURL, '/system/remote-access');
    await expect(page.locator('[data-token-writes="hm2mqtt"]')).toHaveText('–');
    await open(page, baseURL, '/system/remote-access', 'off');
    await expect(page.locator('.ol-tokens tbody tr').first()).toBeVisible();
    await expect(page.locator('[data-token-writes]')).toHaveCount(0);
    await expect(page.locator('.ol-tokens thead')).not.toContainText('Radio writes');
});
