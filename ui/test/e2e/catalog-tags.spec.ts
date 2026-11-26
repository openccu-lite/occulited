import {type Page} from '@playwright/test';
import {expect, test} from './fixtures';

// occulited task 188 (the maintainer, 2026-11-25): the catalogue's cards show the manifest's
// one-line summary, else the description cut to two lines, and the whole text in a details
// dialog; the catalogue's tags filter the cards (task 189: a list of checkboxes, the checked ones
// as chips, remembered per browser), and each card shows its tags as icons; one, two or three cards
// side by side by the window's width.
const card = (page: Page, id: string) => page.locator(`#addon-row-${id}`);
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
    // occulited task 188 (D-90): the technical account below the note - the addon's words, then
    // what the system derives: how an install would run it, the port (closed until opened), the scopes
    const tech = dialog.locator('[data-detail-technical]');
    await expect(tech.getByRole('heading', {name: 'Technical'})).toBeVisible();
    await expect(tech.locator('[data-detail-technical-text]')).toHaveText('Stub: reads the device descriptions, contacts nothing outside.');
    await expect(tech.locator('[data-detail-facts] li')).toHaveText(['Runs as its own user (confined)', 'Port 8099/tcp · Stub port: closed until opened', 'API token: meta:read']);
    // the order: note, then technical
    const order = await dialog.locator('[data-detail-note], [data-detail-technical]').evaluateAll((els) => els.map((el) => el.hasAttribute('data-detail-note') ? 'note' : 'technical'));
    expect(order).toEqual(['note', 'technical']);
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

test('the runtime note and the technical account are in the install question', async ({page}) => {
    await page.goto('/addons');
    await card(page, 'tm-devices').getByRole('button', {name: 'Install', exact: true}).click();
    const q = page.getByRole('dialog');
    await expect(q).toContainText('Install TM Devices?');
    await expect(q).toContainText('Not yet tried on openccu-lite.');
    // occulited task 188 (D-90): and the technical account with the derived lines
    await expect(q).toContainText('Stub: reads the device descriptions, contacts nothing outside.');
    await expect(q).toContainText('• Port 8099/tcp · Stub port: closed until opened');
    await expect(q).toContainText('• API token: meta:read');
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

// occulited task 189 (variant B, the maintainer 2026-11-26): the tags as a list of checkboxes behind
// a button beside the search, the checked ones as chips with × under it, the card's tags as icons
const button = (page: Page) => page.locator('[data-tag-filter] .tf-button');
const panel = (page: Page) => page.locator('[data-tag-filter-panel]');
const row = (page: Page, id: string) => panel(page).locator(`[data-tag="${id}"]`);
const chips = (page: Page) => page.locator('[data-tag-chip]');
// a row of the list as its name and its count, e.g. "MQTT3"
const rowTexts = (page: Page) => panel(page).locator('[data-tag]').evaluateAll((els) => els.map((el) => `${el.querySelector('.tf-name')?.textContent}${el.querySelector('.tf-count')?.textContent}`));
const rowText = async (page: Page, id: string) => row(page, id).evaluate((el) => `${el.querySelector('.tf-name')?.textContent}${el.querySelector('.tf-count')?.textContent}`);
const chipNames = (page: Page) => chips(page).evaluateAll((els) => els.map((el) => el.querySelector('.ad-chip-name')?.textContent));
const cardsTop = async (page: Page) => Math.round((await page.locator('.ad-cards').boundingBox())!.y);
const ROWS = ['Integration4', 'MQTT3', 'Control2', 'Device management2', 'Matter2', 'Home Assistant2', 'Automation1', 'Devices1', 'HomeKit1', 'MCP1', 'Alarm system1'];

test('the tag filter: the list, All and None, OR, the chips, the reset, remembered, and nothing moves', async ({page}) => {
    await page.goto('/addons');
    await expect(names(page)).toHaveCount(8);
    const btn = page.getByRole('button', {name: 'Tags: all'});
    await expect(btn).toHaveAttribute('aria-expanded', 'false');
    const width = async () => Math.round((await button(page).boundingBox())!.width);
    const w0 = await width();
    const top0 = await cardsTop(page);
    // all checked: no chips, the line is there all the same
    await expect(page.locator('[data-tag-chips]')).toHaveAttribute('data-tag-chips', 'all');
    await expect(page.getByText('Filtered:')).toHaveCount(0);
    // the list: the tags some entry carries, the most used first, each with its count, its icon in its colour
    await btn.click();
    await expect(btn).toHaveAttribute('aria-expanded', 'true');
    await expect.poll(() => rowTexts(page)).toEqual(ROWS);
    // the list stays inside the window, on a phone too
    await expect.poll(async () => Math.round((await panel(page).boundingBox())!.x + (await panel(page).boundingBox())!.width) <= page.viewportSize()!.width).toBe(true);
    await expect(panel(page).getByRole('checkbox').first()).toBeFocused();
    for (const box of await panel(page).getByRole('checkbox').all()) await expect(box).toBeChecked();
    await expect(row(page, 'mqtt').locator('.tf-icon')).toHaveCSS('color', /rgb/);
    expect(await row(page, 'mqtt').locator('.tf-icon').evaluate((el) => getComputedStyle(el).color)).toBe(await page.evaluate(() => {
        const d = document.createElement('span');
        d.style.color = 'var(--ol-tag-mqtt)';
        document.body.append(d);
        const c = getComputedStyle(d).color;
        d.remove();
        return c;
    }));
    await expect(panel(page).getByRole('button', {name: 'All'})).toBeDisabled();
    // one unchecked from all: the cards with any of the others (OR); XML-API carries only
    // Integration, and the two installed addons the catalogue does not know carry none
    await row(page, 'integration').getByRole('checkbox').uncheck();
    await expect(names(page)).toHaveText(['RedMatic', 'Homematic-Manager', 'Mosquitto', 'SukramJ/openccu-loom', 'TM Devices']);
    await expect(page.locator('[data-tag-filter-value]')).toHaveText('10');
    await expect(chips(page)).toHaveCount(10);
    expect(await width()).toBe(w0); // the button keeps its width
    // None: nothing, and the line says so
    await panel(page).getByRole('button', {name: 'None'}).click();
    await expect(names(page)).toHaveCount(0);
    await expect(page.getByText('Nothing matches the filter.')).toBeVisible();
    await expect(page.locator('[data-tag-chip-none]')).toHaveText('no tag checked');
    await expect(page.locator('[data-tag-filter-value]')).toHaveText('0');
    // two checked: the cards with either
    await row(page, 'devices').getByRole('checkbox').check();
    await row(page, 'device-management').getByRole('checkbox').check();
    await expect(names(page)).toHaveText(['Homematic-Manager', 'SukramJ/openccu-loom', 'TM Devices']);
    await expect(page.locator('[data-tag-filter-value]')).toHaveText('2');
    expect(await width()).toBe(w0);
    // Escape closes the list and gives the focus back to the button
    await page.keyboard.press('Escape');
    await expect(panel(page)).toHaveCount(0);
    await expect(button(page)).toBeFocused();
    // the chips, in the list's order, each with its icon
    await expect(page.getByText('Filtered:', {exact: true})).toBeVisible();
    await expect.poll(() => chipNames(page)).toEqual(['Device management', 'Devices']);
    await expect(chips(page).first().locator('svg')).toHaveCount(1);
    // the cards begin where they began without a filter
    expect(await cardsTop(page)).toBe(top0);
    // remembered across a reload
    await page.reload();
    await expect.poll(() => chipNames(page)).toEqual(['Device management', 'Devices']);
    await expect(names(page)).toHaveText(['Homematic-Manager', 'SukramJ/openccu-loom', 'TM Devices']);
    // a chip's × takes its tag out of the filter
    await page.getByRole('button', {name: 'Remove Devices from the filter'}).click();
    await expect.poll(() => chipNames(page)).toEqual(['Device management']);
    await expect(names(page)).toHaveText(['Homematic-Manager', 'SukramJ/openccu-loom']);
    // with the text filter: the counts follow it
    await page.getByPlaceholder('Filter').fill('manager');
    await expect(names(page)).toHaveText(['Homematic-Manager']);
    await button(page).click();
    await expect.poll(() => rowText(page, 'device-management')).toBe('Device management1');
    expect(await rowText(page, 'integration')).toBe('Integration0');
    // a click outside closes the list
    await page.locator('h1, .ad-meta').first().click();
    await expect(panel(page)).toHaveCount(0);
    await page.getByPlaceholder('Filter').fill('');
    // Reset filter: all again, the chips gone, the line still there
    await page.getByRole('button', {name: 'Reset filter'}).click();
    await expect(names(page)).toHaveCount(8);
    await expect(page.getByRole('button', {name: 'Tags: all'})).toBeVisible();
    await expect(chips(page)).toHaveCount(0);
    expect(await cardsTop(page)).toBe(top0);
    expect(await width()).toBe(w0);
    // the keyboard: Enter opens, Space checks, Tab leaves and closes
    await button(page).focus();
    await page.keyboard.press('Enter');
    await expect(panel(page).getByRole('checkbox').first()).toBeFocused();
    await page.keyboard.press('Space');
    await expect(page.locator('[data-tag-filter-value]')).toHaveText('10');
    // checking it again is all again
    await page.keyboard.press('Space');
    await expect(page.locator('[data-tag-filter-value]')).toHaveText('all');
});

test('many checked tags stay on one line: the chips scroll sideways, the cards do not move', async ({page}) => {
    await page.goto('/addons');
    await expect(names(page)).toHaveCount(8);
    const top0 = await cardsTop(page);
    await button(page).click();
    await row(page, 'alarm').getByRole('checkbox').uncheck();
    await expect(chips(page)).toHaveCount(10);
    const line = page.locator('[data-tag-chips]');
    const h = (await line.boundingBox())!.height;
    expect(h).toBeLessThan(40);
    expect(await cardsTop(page)).toBe(top0);
    // nothing wider than the page
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('the card pills: the tags as icons in their colours, the name on hover and on focus, "+n" for many', async ({page}) => {
    // the catalogue's loom carries every tag here, so its card needs "+n"
    await page.route('**/api/system/v1/catalog', async (r) => {
        const res = await r.fetch();
        const body = await res.json();
        body.catalog.addons = body.catalog.addons.map((e: {git: string}) => (e.git.endsWith('openccu-loom') ? {...e, tags: body.catalog.tags.map((t: {id: string}) => t.id)} : e));
        await r.fulfill({response: res, json: body});
    });
    await page.goto('/addons');
    const mq = card(page, 'mosquitto').locator('[data-addon-pills]');
    await expect(mq.locator('[data-pill]')).toHaveCount(2);
    expect(await mq.locator('[data-pill]').evaluateAll((els) => els.map((el) => el.getAttribute('data-pill')))).toEqual(['integration', 'mqtt']);
    const pill = mq.locator('[data-pill="mqtt"]');
    await expect(pill).toHaveAttribute('aria-label', 'MQTT');
    await expect(pill.locator('svg')).toHaveCount(1);
    await expect(pill).not.toHaveText(/MQTT/); // an icon, the name only in its tooltip
    const tip = (l: typeof pill) => l.evaluate((el) => (getComputedStyle(el, '::after').display === 'none' ? '0' : '1'));
    expect(await tip(pill)).toBe('0');
    await pill.hover();
    await expect.poll(() => tip(pill)).toBe('1');
    expect(await pill.evaluate((el) => getComputedStyle(el, '::after').content)).toBe('"MQTT"');
    await page.mouse.move(0, 0);
    await expect.poll(() => tip(pill)).toBe('0');
    // the keyboard reaches it and shows the same
    await mq.locator('[data-pill="integration"]').focus();
    await page.keyboard.press('Tab');
    await expect(pill).toBeFocused();
    await expect.poll(() => tip(pill)).toBe('1');
    // not a button: a click filters nothing
    await pill.click();
    await expect(names(page)).toHaveCount(8);
    // fifteen tags: nine icons and "+6", the rest named in its tooltip
    const loom = card(page, 'repo-SukramJ-openccu-loom').locator('[data-addon-pills]');
    await expect(loom.locator('[data-pill]')).toHaveCount(9);
    const more = loom.locator('[data-pill-more]');
    await expect(more).toHaveText('+6');
    await expect(more).toHaveAttribute('aria-label', /^6 more: /);
    // every card's row has the same height: none, two or fifteen tags; no row is wider than its card
    const rows = await page.locator('[data-addon-pills]').evaluateAll((els) => els.map((el) => ({h: Math.round(el.getBoundingClientRect().height), over: el.scrollWidth > (el.parentElement as HTMLElement).clientWidth})));
    expect(new Set(rows.map((r) => r.h))).toEqual(new Set([22]));
    expect(rows.some((r) => r.over)).toBe(false);
    await expect(card(page, 'hm2mqtt').locator('[data-addon-pills] [data-pill]')).toHaveCount(0);
});

test('German: the tag filter, the chips, the pills, the summary and the details', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await page.goto('/addons');
    await page.getByRole('button', {name: 'Tags: alle'}).click();
    await expect(panel(page)).toHaveAttribute('aria-label', 'Nach Tags filtern');
    expect(await rowText(page, 'device-management')).toBe('Geräteverwaltung2');
    await expect(panel(page).getByRole('button', {name: 'Alle'})).toBeDisabled();
    await panel(page).getByRole('button', {name: 'Keine'}).click();
    await expect(page.locator('[data-tag-chip-none]')).toHaveText('kein Tag angehakt');
    await row(page, 'mqtt').getByRole('checkbox').check();
    await expect(page.getByRole('button', {name: 'Tags: 1'})).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByText('Gefiltert:', {exact: true})).toBeVisible();
    await expect(page.getByRole('button', {name: 'MQTT aus dem Filter entfernen'})).toBeVisible();
    await page.getByRole('button', {name: 'Filter zurücksetzen'}).click();
    await expect(page.getByRole('button', {name: 'Tags: alle'})).toBeVisible();
    await expect(card(page, 'mosquitto').locator('[data-pill="integration"]')).toHaveAttribute('aria-label', 'Anbindung');
    await expect(card(page, 'mosquitto').locator('[data-addon-summary]')).toHaveText('Stub-Zusammenfassung von Mosquitto');
    await card(page, 'mosquitto').getByRole('button', {name: 'Details zu Mosquitto'}).click();
    await expect(page.getByRole('dialog', {name: 'Mosquitto'}).locator('[data-detail-tags] .ol-badge')).toHaveText(['Anbindung', 'MQTT']);
    await page.keyboard.press('Escape');
    await card(page, 'tm-devices').getByRole('button', {name: 'Details zu TM Devices'}).click();
    const tech = page.getByRole('dialog', {name: 'TM Devices'}).locator('[data-detail-technical]');
    await expect(tech.getByRole('heading', {name: 'Technisch'})).toBeVisible();
    await expect(tech.locator('[data-detail-facts] li')).toHaveText(['Läuft als eigener Benutzer (eingesperrt)', 'Port 8099/tcp · Stub-Port: geschlossen, bis es geöffnet wird', 'API-Token: meta:read']);
});

test('a remembered filter whose tags the list no longer has is no filter', async ({page}) => {
    await page.addInitScript(() => localStorage.setItem('ol.addons.tagFilter', '["gone"]'));
    await page.goto('/addons');
    await expect(page.getByRole('button', {name: 'Tags: all'})).toBeVisible();
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

test("an installed addon's details: its policy's mode and its ports' state in the firewall", async ({page}) => {
    await page.goto('/addons');
    await card(page, 'mosquitto').getByRole('button', {name: 'Details of Mosquitto'}).click();
    const facts = page.getByRole('dialog', {name: 'Mosquitto'}).locator('[data-detail-facts] li');
    await expect(facts).toHaveText(['Runs as its own user (confined)', 'Port 1883/tcp: open', 'Port 8883/tcp · TLS · MQTT over TLS: closed']);
});
