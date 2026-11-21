import {expect, test, type Page} from '@playwright/test';
import {fitsWindow} from './scroll';

// occulited task 163: the radio writes per lite-rpc client - the Status page's panel (a row per
// client with its rate, the last 60 minutes as bars, the hour's sum and the share of writes that
// changed nothing; the duty cycle of each radio interface on the same minutes below), and the API
// tokens' column with each token's rate and bars, marked when it is behind a hint or a warning.

async function open(page: Page, baseURL: string | undefined, path: string, variant = '') {
    await page.context().addCookies([{name: 'stub-radio-writes', value: variant, url: baseURL!}]);
    await page.goto(path);
}
const client = (page: Page, key: string) => page.locator(`[data-radio-client="${key}"]`);

test('the Status page lists each client with its rate, its hour and what changed nothing', async ({page, baseURL}) => {
    await open(page, baseURL, '/', 'busy');
    await expect(page.getByRole('heading', {level: 2, name: 'Radio writes'})).toBeVisible();
    const panel = page.locator('[data-radio-writes]');
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

    const ci = client(page, 'token:ci-nightly');
    await expect(ci.locator('[data-radio-level="hint"]')).toHaveText('Hint');
    await expect(ci.locator('[data-radio-level="hint"]')).toHaveAttribute('title', 'Up to 84 radio writes in one minute.');
    await expect(ci.locator('[data-radio-noop]')).toHaveText('0 %');
    // a minute is a bar: the burst of 84 and the 12 after it, nothing else
    await expect(ci.locator('[data-spark-bar]')).toHaveCount(2);
    await expect(ci.locator('[data-spark-bar="84"]')).toHaveCount(1);
    // the busiest minute is 84, so every row is drawn to 100, and the 60 of the hint is marked
    await expect(ci.locator('[data-spark-threshold]')).toHaveCount(1);
    await expect(panel.locator('[data-radio-writes-foot]')).toContainText('The bars reach up to 100 writes in a minute; the dotted line marks 60.');
    // under the pointer: the minute and its count
    await expect(ci.locator('svg rect.hit').nth(50).locator('title')).toHaveText('9 minutes ago: 84');
    await expect(ci.locator('svg rect.hit').nth(59).locator('title')).toHaveText('this minute: 0');

    const admin = client(page, 'user:admin');
    await expect(admin).toHaveAttribute('data-level', '');
    await expect(admin.locator('.rw-kind')).toHaveText('account · HmIP-RF');
    await expect(admin.locator('[data-radio-level]')).toHaveCount(0);
    await expect(admin.locator('[data-radio-noop]')).toHaveText('50 %');

    // the duty cycle of each radio interface on the same minutes, a line with its last reading
    const duty = panel.locator('[data-radio-duty="HmIP-RF"]');
    await expect(duty.locator('.rw-clip')).toHaveText('Duty cycle HmIP-RF');
    await expect(duty.locator('[data-radio-dc]')).toHaveText('24 %');
    await expect(duty.locator('svg[data-spark="line"]')).toBeVisible();
    // a minute without a reading breaks the line
    expect(await duty.locator('svg polyline').count()).toBe(2);
    await expect(duty.locator('svg rect.hit').nth(10).locator('title')).toHaveText('49 minutes ago: –');
    await expect(panel.locator('[data-radio-duty="BidCos-RF"] [data-radio-dc]')).toHaveText('2 %');
    // one radio per interface: no address under the name
    await expect(panel.locator('[data-radio-duty-address]')).toHaveCount(0);
    await expect(panel.locator('[data-radio-writes-foot]')).toContainText('The duty cycle is drawn up to 25 %.');
    await expect(panel.locator('[data-radio-writes-classic]')).toHaveCount(0);
    expect(await fitsWindow(page)).toBe(true);
});

test('nothing changes its box with the level: every sparkline and every badge column is the same', async ({page, baseURL}) => {
    await open(page, baseURL, '/', 'busy');
    await expect(client(page, 'token:hm2mqtt')).toBeVisible();
    const boxes = async (sel: string) => page.locator(`[data-radio-writes] tbody tr ${sel}`).evaluateAll((els) => els.map((e) => {
        const r = e.getBoundingClientRect();
        return `${Math.round(r.x)} ${Math.round(r.width)} ${Math.round(r.height)}`;
    }));
    const sparks = await boxes('svg');
    expect(sparks).toHaveLength(5);
    expect(new Set(sparks).size).toBe(1);
    expect(new Set(await boxes('td.rw-badge')).size).toBe(1);
    const heights = await page.locator('[data-radio-client]').evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().height)));
    expect(new Set(heights).size).toBe(1);
});

test('the panel says so when nobody wrote, and is not there on a system that does not count', async ({page, baseURL}) => {
    await open(page, baseURL, '/');
    await expect(page.getByRole('heading', {level: 2, name: 'Radio writes'})).toBeVisible();
    await expect(page.locator('[data-radio-writes-none]')).toHaveText('No program has sent a radio write in the last 60 minutes.');
    await expect(page.locator('[data-radio-writes] table')).toHaveCount(0);

    await open(page, baseURL, '/', 'off');
    await expect(page.getByRole('heading', {level: 2, name: 'Components'})).toBeVisible();
    await expect(page.getByRole('heading', {level: 2, name: 'Radio writes'})).toHaveCount(0);
});

test('with classic RPC on the panel says whom it does not count', async ({page, baseURL}) => {
    await open(page, baseURL, '/', 'busy-classic');
    const note = page.locator('[data-radio-writes-classic]');
    await expect(note).toContainText('Classic RPC is on: programs that use its ports reach the interfaces directly and are not counted here.');
    // an interface with several radios (a LAN gateway beside the module): each row names its own, in full
    const addresses = page.locator('[data-radio-duty="BidCos-RF"] [data-radio-duty-address]');
    await expect(addresses).toHaveText(['JEQ0000001', 'JEQ9000001']);
    expect(await addresses.first().evaluate((e) => e.scrollWidth <= e.clientWidth)).toBe(true);
    await expect(page.locator('[data-radio-duty="HmIP-RF"] [data-radio-duty-address]')).toHaveCount(0);
    await note.getByRole('link', {name: 'Remote access'}).click();
    await expect(page).toHaveURL(/\/system\/remote-access$/);
});

test('the panel in German', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await open(page, baseURL, '/', 'busy');
    await expect(page.getByRole('heading', {level: 2, name: 'Schreibbefehle per Funk'})).toBeVisible();
    await expect(client(page, 'token:hm2mqtt').locator('[data-radio-level="warning"]')).toHaveText('Warnung');
    await expect(client(page, 'token:ci-nightly').locator('[data-radio-level="hint"]')).toHaveText('Hinweis');
    await expect(page.locator('[data-radio-writes] thead')).toContainText('Ändern nichts');
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
