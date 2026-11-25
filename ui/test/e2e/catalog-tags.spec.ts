import {type Page} from '@playwright/test';
import {expect, test} from './fixtures';

// occulited task 188 (the maintainer, 2026-11-25): the catalogue's cards show the manifest's
// one-line summary, else the description cut to two lines, and the whole text in a details
// dialog; the catalogue's tags filter the cards (chips with counts, several at a time, remembered
// per browser); one, two or three cards side by side by the window's width.
const card = (page: Page, id: string) => page.locator(`#addon-row-${id}`);
const chip = (page: Page, id: string) => page.locator(`[data-tag-filter] [data-tag="${id}"]`);
const names = (page: Page) => page.locator('.ad-card .ad-name');

test('a card: the summary when there is one, else the description; the note behind Details', async ({page}) => {
    await page.goto('/addons');
    // Mosquitto has a summary: one line, cut with an ellipsis, whole in the tooltip
    const mq = card(page, 'mosquitto');
    const sum = mq.locator('[data-addon-summary]');
    await expect(sum).toHaveText('Stub summary of Mosquitto');
    await expect(sum).toHaveAttribute('title', 'Stub summary of Mosquitto');
    await expect(sum).toHaveCSS('white-space', 'nowrap');
    await expect(sum).toHaveCSS('text-overflow', 'ellipsis');
    await expect(mq.locator('[data-addon-description]')).toHaveCount(0);
    // TM Devices has none: its whole description, as before
    const tm = card(page, 'tm-devices');
    const desc = tm.locator('[data-addon-description]');
    await expect(desc).toContainText('Device support for home-built sensors.');
    const full = (await desc.textContent())!;
    expect(await desc.evaluate((el) => el.scrollHeight <= el.clientHeight + 1)).toBe(true); // not cut
    // the grey runtime note is not on the card; Details shows it
    await expect(tm.getByText('Not yet tried on openccu-lite.')).toHaveCount(0);
    await expect(tm.locator('.ad-note')).toHaveCount(0);
    const open = tm.getByRole('button', {name: 'Details of TM Devices'});
    await expect(open).toHaveAttribute('aria-haspopup', 'dialog');
    // the keyboard opens it
    await open.focus();
    await page.keyboard.press('Enter');
    const dialog = page.getByRole('dialog', {name: 'TM Devices'});
    await expect(dialog.locator('[data-detail-description]')).toHaveText(full);
    await expect(dialog.locator('[data-detail-note]')).toHaveText('Not yet tried on openccu-lite.');
    await expect(dialog.locator('[data-detail-tags] .ol-badge')).toHaveText(['Devices']);
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    await expect(open).toBeFocused(); // the focus goes back to the button that opened it
    // a card with a summary shows it in the details too, above the description
    await mq.getByRole('button', {name: 'Details of Mosquitto'}).click();
    const d2 = page.getByRole('dialog', {name: 'Mosquitto'});
    await expect(d2.locator('[data-detail-summary]')).toHaveText('Stub summary of Mosquitto');
    await expect(d2.locator('[data-detail-description]')).toHaveText('MQTT broker');
    await d2.getByRole('button', {name: 'Close'}).last().click();
    await expect(d2).toHaveCount(0);
    // a card with neither a hidden description nor a note nor a long Info: line has no Details
    await expect(card(page, 'jp-hb-devices-addon').locator('[data-addon-details]')).toHaveCount(0);
    // opening and closing the details changes no card's size (a phone may scroll to the button)
    const size = async () => (await page.locator('.ad-card').evaluateAll((els) => els.map((el) => `${Math.round(el.getBoundingClientRect().width)}x${Math.round(el.getBoundingClientRect().height)}`))).join(' ');
    const before = await size();
    await open.click();
    await expect(page.getByRole('dialog', {name: 'TM Devices'})).toBeVisible();
    expect(await size()).toBe(before);
    await page.keyboard.press('Escape');
    expect(await size()).toBe(before);
});

test('the runtime note is in the install question', async ({page}) => {
    await page.goto('/addons');
    await card(page, 'tm-devices').getByRole('button', {name: 'Install', exact: true}).click();
    const q = page.getByRole('dialog');
    await expect(q).toContainText('Install TM Devices?');
    await expect(q).toContainText('Not yet tried on openccu-lite.');
    await q.getByRole('button', {name: 'Cancel'}).click();
    await expect(q).toHaveCount(0);
});

test("an installed addon's long Info: line is behind Details, a short one stays", async ({page}) => {
    const long = 'RedMatic bundles Node-RED with nodes for the CCU, dashboards, HomeKit, Matter and MQTT; this sentence stands for a long Info: line of an addon. https://github.com/rdmtc/RedMatic';
    await page.route('**/api/system/v1/addons', async (r) => {
        const res = await r.fetch();
        const body = await res.json();
        body.addons = body.addons.map((a: {id: string}) => (a.id === 'redmatic' ? {...a, info: long} : a));
        await r.fulfill({response: res, json: body});
    });
    await page.goto('/addons');
    const red = card(page, 'redmatic');
    await expect(red.locator('.ad-info')).toHaveCount(0);
    await red.getByRole('button', {name: 'Details of RedMatic'}).click();
    const info = page.getByRole('dialog', {name: 'RedMatic'}).locator('[data-detail-info]');
    await expect(info).toContainText('this sentence stands for a long Info: line');
    await expect(info.getByRole('link', {name: 'https://github.com/rdmtc/RedMatic'})).toHaveAttribute('href', 'https://github.com/rdmtc/RedMatic');
    await page.keyboard.press('Escape');
    // JP HB Devices' short line (a name and its link) stays on the card
    await expect(card(page, 'jp-hb-devices-addon').locator('.ad-info')).toContainText('JP HB Devices Support Addon');
});

test('the tag filter: chips with counts by use, the most used first, several at a time, combined, remembered', async ({page}) => {
    await page.goto('/addons');
    const group = page.getByRole('group', {name: 'Filter by tag'});
    // the tags some entry carries, the most used first (the whole catalogue's counts, the list
    // breaking ties); eight of eleven, and a button for the other three
    await expect(group.locator('[data-tag]')).toHaveText(['All', 'Integration4', 'MQTT3', 'Control2', 'Device management2', 'Matter2', 'Home Assistant2', 'Automation1', 'Devices1']);
    await expect(chip(page, '')).toHaveAttribute('aria-pressed', 'true');
    await expect(chip(page, 'device-management')).toHaveAttribute('aria-label', 'Device management: 2');
    const widths = async () => Promise.all(['integration', 'mqtt', 'control', 'device-management'].map(async (id) => (await chip(page, id).boundingBox())!.width));
    const before = await widths();
    // one tag: the cards that carry it (an installed addon without a catalogue entry carries none)
    await chip(page, 'device-management').click();
    await expect(chip(page, 'device-management')).toHaveAttribute('aria-pressed', 'true');
    await expect(chip(page, '')).toHaveAttribute('aria-pressed', 'false');
    await expect(names(page)).toHaveText(['Homematic-Manager', 'SukramJ/openccu-loom']);
    // a second: the cards with either
    await chip(page, 'devices').click();
    await expect(names(page)).toHaveText(['Homematic-Manager', 'SukramJ/openccu-loom', 'TM Devices']);
    // a pressed chip keeps its box
    expect(await widths()).toEqual(before);
    // remembered across a reload
    await page.reload();
    await expect(chip(page, 'device-management')).toHaveAttribute('aria-pressed', 'true');
    await expect(chip(page, 'devices')).toHaveAttribute('aria-pressed', 'true');
    await expect(names(page)).toHaveText(['Homematic-Manager', 'SukramJ/openccu-loom', 'TM Devices']);
    // with the text filter: the counts follow it, the chips keep their place and size
    await page.getByPlaceholder('Filter').fill('manager');
    await expect(names(page)).toHaveText(['Homematic-Manager']);
    await expect(group.locator('[data-tag]')).toHaveText(['All', 'Integration0', 'MQTT0', 'Control0', 'Device management1', 'Matter0', 'Home Assistant0', 'Automation0', 'Devices0']);
    expect(await widths()).toEqual(before);
    // with Installed only
    await page.getByPlaceholder('Filter').fill('');
    await page.getByRole('checkbox', {name: 'Installed only'}).check();
    await expect(names(page)).toHaveText(['Homematic-Manager']);
    await page.getByRole('checkbox', {name: 'Installed only'}).uncheck();
    // nothing left: the notice says the filter
    await page.getByPlaceholder('Filter').fill('mosq');
    await expect(page.getByText('Nothing matches the filter.')).toBeVisible();
    // All clears the choice
    await page.getByPlaceholder('Filter').fill('');
    await chip(page, '').click();
    await expect(names(page)).toHaveCount(8);
    // the chips are buttons: the keyboard reaches and presses them
    await chip(page, '').focus();
    await page.keyboard.press('Tab');
    await expect(chip(page, 'integration')).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(chip(page, 'integration')).toHaveAttribute('aria-pressed', 'true');
    await expect(names(page)).toHaveText(['RedMatic', 'Mosquitto', 'XML-API', 'SukramJ/openccu-loom']);
});

test('many tags: the rest behind "3 more", a chosen one always shown, the choice of the row remembered', async ({page}) => {
    await page.goto('/addons');
    const group = page.getByRole('group', {name: 'Filter by tag'});
    const more = group.locator('[data-tag-more]');
    await expect(more).toHaveText('3 more');
    await expect(more).toHaveAttribute('aria-expanded', 'false');
    await expect(chip(page, 'homekit')).toHaveCount(0);
    await more.click();
    await expect(more).toHaveText('Fewer');
    await expect(more).toHaveAttribute('aria-expanded', 'true');
    await expect(group.locator('[data-tag]')).toHaveText(['All', 'Integration4', 'MQTT3', 'Control2', 'Device management2', 'Matter2', 'Home Assistant2', 'Automation1', 'Devices1', 'HomeKit1', 'MCP1', 'Alarm system1']);
    // a chip of the rest, chosen, stays when the row collapses
    await chip(page, 'mcp').click();
    await expect(names(page)).toHaveText(['SukramJ/openccu-loom']);
    await more.click();
    await expect(chip(page, 'mcp')).toHaveAttribute('aria-pressed', 'true');
    await expect(chip(page, 'homekit')).toHaveCount(0);
    await expect(more).toHaveText('2 more');
    // the open row is remembered
    await more.click();
    await page.reload();
    await expect(group.locator('[data-tag-more]')).toHaveText('Fewer');
    await expect(chip(page, 'homekit')).toBeVisible();
});

test('German: the tags, the summary and the details', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await page.goto('/addons');
    const group = page.getByRole('group', {name: 'Nach Schlagwort filtern'});
    await expect(group.locator('[data-tag]').first()).toHaveText('Alle');
    await expect(chip(page, 'device-management')).toHaveText('Geräteverwaltung2');
    await expect(group.locator('[data-tag-more]')).toHaveText('3 weitere');
    await expect(card(page, 'mosquitto').locator('[data-addon-summary]')).toHaveText('Stub-Zusammenfassung von Mosquitto');
    await card(page, 'mosquitto').getByRole('button', {name: 'Details zu Mosquitto'}).click();
    await expect(page.getByRole('dialog', {name: 'Mosquitto'}).locator('[data-detail-tags] .ol-badge')).toHaveText(['Anbindung', 'MQTT']);
});

test('a remembered tag the list no longer has is no choice', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.addons.tags', '["gone"]'));
    await page.goto('/addons');
    await expect(chip(page, '')).toHaveAttribute('aria-pressed', 'true');
    await expect(names(page)).toHaveCount(8);
});

test('the grid: three cards side by side on a large screen, two above 1100 px, one up to it', async ({page}, info) => {
    test.skip(info.project.name === 'phone', 'the widths are set here; once is enough');
    const columns = () => page.locator('.ad-cards').evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(' ').length);
    for (const [width, n] of [[1600, 3], [1400, 3], [1280, 2], [1101, 2], [1100, 1], [900, 1]] as const) {
        await page.setViewportSize({width, height: 900});
        await page.goto('/addons');
        await expect(card(page, 'mosquitto')).toBeVisible();
        expect(await columns(), `${width} px`).toBe(n);
        // the cards of a row stand side by side
        const tops = await page.locator('.ad-card').evaluateAll((els) => els.slice(0, 3).map((el) => Math.round(el.getBoundingClientRect().top)));
        expect(new Set(tops.slice(0, n)).size, `${width} px`).toBe(1);
    }
});
