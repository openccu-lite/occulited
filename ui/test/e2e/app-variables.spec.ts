import {type Page, type Request} from '@playwright/test';
import {expect, test} from './fixtures';

// occulited B-170 and task 176: an addon interface's system variables and programs in Control (the stub's "Logic":
// test/stub/server.mjs LOGIC, shaped as lite-addon-logic's interface). B-170: they showed among the unassigned
// channels until their descriptions were read, and vanished. Task 176: tiles and sheets of their own - the value with
// its unit, a boolean's texts, a value list as a dropdown, a number with its limits and step, a string; a program run
// and switched; the icon, read-only and the step chosen per object in the metadata's control namespace.

const VARS: Record<string, string> = {
    'Logic.var-regen:1': 'Regen', 'Logic.var-anwesend:1': 'Anwesenheit', 'Logic.var-temperatur:1': 'Solltemperatur Urlaub', 'Logic.var-zaehler:1': 'Zähler',
    'Logic.var-modus:1': 'Modus', 'Logic.var-hinweis:1': 'Hinweis', 'Logic.var-alarm-wasser:1': 'Alarm Wasser', 'Logic.var-duty-cycle:1': 'Duty Cycle',
};
const PROGRAMS: Record<string, string> = {'Logic.prg-markise:1': 'Markise bei Regen einfahren', 'Logic.prg-fest:1': 'Festprogramm'};
const HIDDEN: Record<string, string> = {'Logic.var-versteckt:1': 'Versteckt', 'Logic.prg-versteckt:1': 'Verborgen'};
// the HmIP-SRH's second channel: described, with an empty paramset - nothing Control draws (task 104)
const NO_WIDGET = 'HmIP-RF.000C01:2';
const ROOM = 'rooms/kueche';

type Meta = Record<string, Record<string, unknown>>;
interface Opened {
    /** the control namespaces the page wrote, by ref */
    meta: Meta;
    /** every PATCH body of an object's metadata, in order */
    patches: {ref: string; body: {meta?: {control?: unknown}}}[];
    /** every setValue the page sent to the interface: [address, key, value] */
    writes: unknown[][];
}

async function open(page: Page, baseURL: string, opt: {path?: string; meta?: Meta; rooms?: string[]; before?: () => Promise<void>} = {}): Promise<Opened> {
    const o: Opened = {meta: {...opt.meta}, patches: [], writes: []};
    await page.context().addCookies([{name: 'stub-app', value: `vars-${Math.random().toString(36).slice(2)}`, url: baseURL}]);
    await page.route('**/api/meta/v1/snapshot', async (r) => {
        const res = await r.fetch();
        const json = await res.json();
        for (const [ref, name] of Object.entries({...VARS, ...PROGRAMS, ...HIDDEN})) {
            json.objects[ref] = {name, enums: opt.rooms?.includes(ref) ? [ROOM] : [], meta: o.meta[ref] ? {control: o.meta[ref]} : {}};
            // the device and its channel 0, as the addon names them
            json.objects[ref.replace(/:1$/, '')] = {name, enums: [], meta: {}};
            json.objects[ref.replace(/:1$/, ':0')] = {name: `${name}:0`, enums: [], meta: {}};
        }
        json.objects[NO_WIDGET] = {name: 'Fenstergriff Kanal 2', enums: [], meta: {}};
        await r.fulfill({response: res, json});
    });
    // the metadata writes stay in this test (the stub's snapshot is shared by the projects)
    await page.route('**/api/meta/v1/objects/Logic.*', async (r) => {
        if (r.request().method() !== 'PATCH') return r.fallback();
        const ref = decodeURIComponent(new URL(r.request().url()).pathname.split('/').pop()!);
        const body = r.request().postDataJSON() as {meta?: {control?: Record<string, unknown> | null}};
        o.patches.push({ref, body});
        if (body.meta && 'control' in body.meta) {
            if (body.meta.control === null) delete o.meta[ref];
            else o.meta[ref] = body.meta.control!;
        }
        await r.fulfill({json: {revision: 43 + o.patches.length, changed: true}});
    });
    page.on('request', (req: Request) => {
        if (req.method() !== 'POST' || !req.url().endsWith('/api/rpc/v1/json/Logic')) return;
        const body = req.postDataJSON() as {method: string; params: unknown[]}[];
        for (const c of Array.isArray(body) ? body : [body]) if (c.method === 'setValue') o.writes.push(c.params);
    });
    await opt.before?.();
    await page.goto(opt.path ?? '/app/unassigned');
    return o;
}
const tile = (page: Page, ref: string) => page.locator(`[data-app-tile="${ref}"]`);
/** a tile's size: where it is depends on how far a phone's page scrolled to reach it */
async function sizeOf(page: Page, ref: string) {
    const b = (await tile(page, ref).boundingBox())!;
    return {width: b.width, height: b.height};
}
const state = (page: Page, ref: string) => tile(page, ref).locator('[data-app-state]');
const sheet = (page: Page) => page.locator('.ol-modal.app-sheet:not(.app-assign)');
const assign = (page: Page) => page.locator('.ol-modal.app-assign');
async function openSheet(page: Page, ref: string) {
    await tile(page, ref).locator('.app-tile-name').click();
    await expect(sheet(page)).toBeVisible();
    // the sheet grows out of the tile: its boxes are measured once it stands
    await page.waitForFunction(() => document.getAnimations().every((a) => a.playState !== 'running'));
}
async function closeSheet(page: Page) {
    await page.keyboard.press('Escape');
    await expect(sheet(page)).toHaveCount(0);
}

test('B-170: the variables and programs stay in the unassigned list, and nothing shows before its description is read', async ({page, baseURL}) => {
    // every tile the page ever made, so one that showed and went again is seen
    await page.addInitScript(() => {
        const seen = new Set<string>();
        (window as unknown as {__tiles: Set<string>}).__tiles = seen;
        new MutationObserver(() => document.querySelectorAll('[data-app-tile]').forEach((el) => seen.add(el.getAttribute('data-app-tile')!))).observe(document, {childList: true, subtree: true});
    });
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    await open(page, baseURL!, {before: async () => {
        await page.route('**/api/rpc/v1/paramsets/Logic', async (r) => {
            await held;
            await r.fallback();
        });
    }});
    // the read of the descriptions is under way: the list waits, with the page's loading mark, and says nothing wrong
    await expect(page.locator('[data-app-title]')).toBeVisible();
    await expect(page.locator('.app-content [data-loading]')).toBeVisible();
    await expect(page.locator('[data-app-tile]')).toHaveCount(0);
    await expect(page.locator('[data-app-empty]')).toHaveCount(0);
    release();
    for (const ref of [...Object.keys(VARS), ...Object.keys(PROGRAMS)]) await expect(tile(page, ref)).toBeVisible();
    // not visible in the addon (behind its checkbox, off), or without a widget: no tile here
    for (const ref of [...Object.keys(HIDDEN), NO_WIDGET]) await expect(tile(page, ref)).toHaveCount(0);
    // and they stay: after the next read of the snapshot's time too
    await page.waitForTimeout(600);
    for (const ref of [...Object.keys(VARS), ...Object.keys(PROGRAMS)]) await expect(tile(page, ref)).toBeVisible();
    const shown = await page.locator('[data-app-tile]').evaluateAll((els) => els.map((el) => el.getAttribute('data-app-tile')!).sort());
    const ever = await page.evaluate(() => [...(window as unknown as {__tiles: Set<string>}).__tiles].sort());
    expect(ever).toEqual(shown);
    expect(ever).not.toContain(NO_WIDGET);
});

test('B-170: a read that fails still shows the tiles, as before', async ({page, baseURL}) => {
    await open(page, baseURL!, {before: async () => {
        await page.route('**/api/rpc/v1/json/Logic', (r) => r.fulfill({status: 502, json: {error: 'bad-gateway', message: 'the interface process does not answer'}}));
    }});
    await expect(tile(page, 'Logic.var-regen:1')).toBeVisible();
    await expect(page.locator('.app-content .ol-notice.error')).toBeVisible();
});

test('the tiles: the value in its own words, with its unit, and the picture of its kind', async ({page, baseURL}) => {
    await open(page, baseURL!);
    await expect(state(page, 'Logic.var-regen:1')).toHaveText('false');
    await expect(state(page, 'Logic.var-anwesend:1')).toHaveText('anwesend');
    await expect(state(page, 'Logic.var-temperatur:1')).toHaveText('21.5 °C');
    await expect(state(page, 'Logic.var-zaehler:1')).toHaveText('7');
    await expect(state(page, 'Logic.var-modus:1')).toHaveText('Manuell');
    await expect(state(page, 'Logic.var-hinweis:1')).toHaveText('Müll rausbringen');
    await expect(state(page, 'Logic.var-duty-cycle:1')).toHaveText('12 %');
    await expect(state(page, 'Logic.prg-markise:1')).toHaveText('Active');
    await expect(state(page, 'Logic.prg-fest:1')).toHaveText('Inactive');
    const icon = (ref: string) => tile(page, ref).locator('[data-icon]');
    await expect(icon('Logic.var-regen:1')).toHaveAttribute('data-icon', 'control_on_off');
    await expect(icon('Logic.var-temperatur:1')).toHaveAttribute('data-icon', 'edit_input_numeric');
    await expect(icon('Logic.var-modus:1')).toHaveAttribute('data-icon', 'edit_sort');
    await expect(icon('Logic.var-hinweis:1')).toHaveAttribute('data-icon', 'message_notice');
    await expect(icon('Logic.var-alarm-wasser:1')).toHaveAttribute('data-icon', 'secur_alarm_alarm');
    // a program its tile runs has a play button (the maintainer, 2026-11-24); one that may not be run its own picture
    await expect(icon('Logic.prg-markise:1')).toHaveAttribute('data-icon', 'audio_play');
    await expect(icon('Logic.prg-fest:1')).toHaveAttribute('data-icon', 'edit_settings');
    // a true boolean is lit, an alarm that is on is red, an active program is not lit
    await expect(tile(page, 'Logic.var-anwesend:1')).toHaveAttribute('data-app-tone', 'on');
    await expect(tile(page, 'Logic.var-regen:1')).toHaveAttribute('data-app-tone', '');
    await expect(tile(page, 'Logic.var-alarm-wasser:1')).toHaveAttribute('data-app-tone', 'alarm');
    await expect(tile(page, 'Logic.prg-markise:1')).toHaveAttribute('data-app-tone', '');
    // only a boolean that takes a write and a program that may be run have a disc to press
    for (const ref of ['Logic.var-regen:1', 'Logic.var-anwesend:1', 'Logic.prg-markise:1']) await expect(tile(page, ref).locator('[data-app-act]')).toHaveCount(1);
    for (const ref of ['Logic.var-temperatur:1', 'Logic.var-modus:1', 'Logic.var-hinweis:1', 'Logic.var-alarm-wasser:1', 'Logic.var-duty-cycle:1', 'Logic.prg-fest:1']) await expect(tile(page, ref).locator('[data-app-act]')).toHaveCount(0);
});

test('German: wahr and falsch, the decimal comma, Aktiv', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await open(page, baseURL!);
    await expect(state(page, 'Logic.var-regen:1')).toHaveText('falsch');
    await expect(state(page, 'Logic.var-temperatur:1')).toHaveText('21,5 °C');
    await expect(state(page, 'Logic.prg-markise:1')).toHaveText('Aktiv');
    await openSheet(page, 'Logic.var-temperatur:1');
    await expect(sheet(page).locator('[data-sheet-number]')).toHaveValue('21,5');
    await expect(sheet(page).locator('[data-sheet-number-hint]')).toHaveText('-40 bis 60');
    await closeSheet(page);
    await openSheet(page, 'Logic.prg-markise:1');
    await expect(sheet(page).locator('[data-sheet-run]')).toHaveText('Ausführen');
    await expect(sheet(page).locator('[data-sheet-run-then]')).toHaveText('Ohne Bedingungen ausführen');
    await expect(sheet(page).locator('[data-sheet-inactive]')).toHaveText('Inaktiv');
});

test('a boolean: the disc switches it; the sheet has its two texts as buttons that keep their box', async ({page, baseURL}) => {
    const o = await open(page, baseURL!);
    const ref = 'Logic.var-regen:1';
    const box = await sizeOf(page, ref);
    await tile(page, ref).locator('[data-app-act]').click();
    await expect(state(page, ref)).toHaveText('true');
    await expect(tile(page, ref)).toHaveAttribute('data-app-tone', 'on');
    expect(o.writes).toEqual([['var-regen:1', 'VALUE', true]]);
    expect(await sizeOf(page, ref)).toEqual(box);
    // the variable with texts of its own
    await openSheet(page, 'Logic.var-anwesend:1');
    const off = sheet(page).locator('[data-sheet-false]'), on = sheet(page).locator('[data-sheet-true]');
    await expect(off).toHaveText('abwesend');
    await expect(on).toHaveText('anwesend');
    await expect(on).toHaveAttribute('aria-pressed', 'true');
    const before = [await off.boundingBox(), await on.boundingBox()];
    await off.click();
    await expect(off).toHaveAttribute('aria-pressed', 'true');
    await expect(sheet(page).locator('[data-sheet-state]')).toHaveText('abwesend');
    expect([await off.boundingBox(), await on.boundingBox()]).toEqual(before);
    expect(o.writes.at(-1)).toEqual(['var-anwesend:1', 'VALUE', false]);
    await closeSheet(page);
    await expect(state(page, 'Logic.var-anwesend:1')).toHaveText('abwesend');
});

test('a number: its limits and step, plus and minus, the field, the slider; what is typed wrong is said and not sent', async ({page, baseURL}) => {
    const o = await open(page, baseURL!);
    const ref = 'Logic.var-temperatur:1';
    await openSheet(page, ref);
    const s = sheet(page);
    const field = s.locator('[data-sheet-number]'), hint = s.locator('[data-sheet-number-hint]');
    await expect(field).toHaveValue('21.5');
    await expect(s.locator('.sh-unit')).toHaveText('°C');
    await expect(hint).toHaveText('-40 to 60');
    const range = s.locator('[data-sheet-range]');
    await expect(range).toHaveAttribute('min', '-40');
    await expect(range).toHaveAttribute('max', '60');
    await expect(range).toHaveAttribute('step', '1');
    const hintBox = await hint.boundingBox(), fieldBox = await field.boundingBox();
    await s.locator('[data-sheet-plus]').click();
    await expect(field).toHaveValue('22.5');
    await s.locator('[data-sheet-minus]').click();
    await s.locator('[data-sheet-minus]').click();
    await expect(field).toHaveValue('20.5');
    // a float that is a whole number goes out marked as a double (openccu-lite B-335)
    expect(o.writes).toEqual([['var-temperatur:1', 'VALUE', 22.5], ['var-temperatur:1', 'VALUE', 21.5], ['var-temperatur:1', 'VALUE', 20.5]]);
    // outside the limits, and no number: said in the hint's line, in its box, and nothing is sent
    await field.fill('61');
    await field.press('Enter');
    await expect(hint).toHaveAttribute('data-sheet-number-hint', 'out-of-range');
    await expect(hint).toHaveText('Allowed: -40 to 60');
    await expect(field).toHaveAttribute('aria-invalid', 'true');
    await field.fill('warm');
    await field.press('Enter');
    await expect(hint).toHaveText('This is not a number.');
    expect(await hint.boundingBox()).toEqual(hintBox);
    expect(await field.boundingBox()).toEqual(fieldBox);
    expect(o.writes).toHaveLength(3);
    // a decimal comma is a point; a whole number is a double on the wire
    await field.fill('19,5');
    await field.press('Enter');
    await expect(s.locator('[data-sheet-state]')).toHaveText('19.5 °C');
    await expect(hint).toHaveText('-40 to 60');
    await field.fill('18');
    await field.press('Enter');
    await expect(s.locator('[data-sheet-state]')).toHaveText('18 °C');
    expect(o.writes.slice(3)).toEqual([['var-temperatur:1', 'VALUE', 19.5], ['var-temperatur:1', 'VALUE', {double: 18}]]);
    // the sheet is no wider than the window
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
    await closeSheet(page);
    await expect(state(page, ref)).toHaveText('18 °C');
    // a number without limits: no slider, no limits to name; the hint's line is there all the same
    await openSheet(page, 'Logic.var-zaehler:1');
    await expect(sheet(page).locator('[data-sheet-range]')).toHaveCount(0);
    await expect(sheet(page).locator('[data-sheet-number-hint]')).toHaveText('');
    await expect(sheet(page).locator('[data-sheet-number-hint]')).toHaveCSS('height', /\d+px/);
    await sheet(page).locator('[data-sheet-plus]').click();
    await expect(sheet(page).locator('[data-sheet-number]')).toHaveValue('8');
});

test('a value list is a dropdown; a string a field; a value nobody writes only shown', async ({page, baseURL}) => {
    const o = await open(page, baseURL!);
    await openSheet(page, 'Logic.var-modus:1');
    const select = sheet(page).locator('[data-sheet-select]');
    await expect(select.locator('option')).toHaveText(['Auto', 'Manuell', 'Urlaub']);
    await expect(select).toHaveValue('1');
    await select.selectOption({label: 'Urlaub'});
    await expect(sheet(page).locator('[data-sheet-state]')).toHaveText('Urlaub');
    expect(o.writes).toEqual([['var-modus:1', 'VALUE', 2]]);
    await closeSheet(page);
    await expect(state(page, 'Logic.var-modus:1')).toHaveText('Urlaub');

    await openSheet(page, 'Logic.var-hinweis:1');
    const text = sheet(page).locator('[data-sheet-text]'), set = sheet(page).locator('[data-sheet-text-set]');
    await expect(text).toHaveValue('Müll rausbringen');
    await expect(set).toBeDisabled();
    await text.fill('Fenster schließen');
    await set.click();
    await expect(sheet(page).locator('[data-sheet-state]')).toHaveText('Fenster schließen');
    await expect(set).toBeDisabled();
    await text.fill('');
    await text.press('Enter');
    expect(o.writes.slice(1)).toEqual([['var-hinweis:1', 'VALUE', 'Fenster schließen'], ['var-hinweis:1', 'VALUE', '']]);
    await closeSheet(page);

    // a system value: the sheet shows it and offers nothing to change it with
    await openSheet(page, 'Logic.var-duty-cycle:1');
    await expect(sheet(page).locator('[data-sheet-state]')).toHaveText('12 %');
    await expect(sheet(page).locator('[data-sheet-number], [data-sheet-plus], [data-sheet-range]')).toHaveCount(0);
    await expect(sheet(page).locator('[data-sheet-locked]')).toHaveCount(0);
});

test('an alarm: how often and since when, and its acknowledgement', async ({page, baseURL}) => {
    const o = await open(page, baseURL!);
    const ref = 'Logic.var-alarm-wasser:1';
    await openSheet(page, ref);
    const s = sheet(page);
    await expect(s.locator('[data-reading="COUNT"]')).toHaveText('2');
    await expect(s.locator('[data-reading="FIRST"]')).toContainText('2026');
    const ack = s.locator('[data-sheet-acknowledge]');
    const box = await ack.boundingBox();
    await ack.click();
    await expect(s.locator('[data-sheet-state]')).toHaveText('false');
    await expect(s.locator('[data-reading="COUNT"]')).toHaveText('0');
    // nothing to acknowledge any more: the button stays where it is, switched off
    await expect(ack).toBeDisabled();
    expect(await ack.boundingBox()).toEqual(box);
    expect(o.writes).toEqual([['var-alarm-wasser:1', 'ACKNOWLEDGE', true]]);
    await closeSheet(page);
    await expect(tile(page, ref)).toHaveAttribute('data-app-tone', '');
    await expect(tile(page, ref).locator('[data-icon]')).toHaveAttribute('data-icon', 'secur_alarm');
});

test('a program: the disc runs it; the sheet runs it with or without its conditions and switches it', async ({page, baseURL}) => {
    const o = await open(page, baseURL!);
    const ref = 'Logic.prg-markise:1';
    const box = await sizeOf(page, ref);
    await tile(page, ref).locator('[data-app-act]').click();
    // the run is a moment: said on the tile, which then says its state again, in the same box
    await expect(state(page, ref)).toHaveText('Started');
    expect(await sizeOf(page, ref)).toEqual(box);
    await expect(state(page, ref)).toHaveText('Active');
    expect(o.writes).toEqual([['prg-markise:1', 'RUN', true]]);
    await openSheet(page, ref);
    const s = sheet(page);
    const run = s.locator('[data-sheet-run]'), then = s.locator('[data-sheet-run-then]');
    const boxes = [await run.boundingBox(), await then.boundingBox()];
    await then.click();
    await expect(then).toHaveAttribute('data-sheet-run-then', 'done');
    expect([await run.boundingBox(), await then.boundingBox()]).toEqual(boxes);
    await expect(then).toHaveAttribute('data-sheet-run-then', '');
    await run.click();
    await expect(run).toHaveAttribute('data-sheet-run', 'done');
    expect(o.writes.slice(1)).toEqual([['prg-markise:1', 'RUN_THEN', true], ['prg-markise:1', 'RUN', true]]);
    await expect(s.locator('[data-sheet-active]')).toHaveAttribute('aria-pressed', 'true');
    await s.locator('[data-sheet-inactive]').click();
    await expect(s.locator('[data-sheet-inactive]')).toHaveAttribute('aria-pressed', 'true');
    await expect(s.locator('[data-sheet-state]')).toHaveText('Inactive');
    expect(o.writes.at(-1)).toEqual(['prg-markise:1', 'ACTIVE', false]);
    await closeSheet(page);
    await expect(state(page, ref)).toHaveText('Inactive');
    // a program that is not operable: nothing to run it with, and it is switched all the same
    await openSheet(page, 'Logic.prg-fest:1');
    await expect(sheet(page).locator('[data-sheet-run], [data-sheet-run-then]')).toHaveCount(0);
    await sheet(page).locator('[data-sheet-active]').click();
    await expect(sheet(page).locator('[data-sheet-state]')).toHaveText('Active');
});

test('the settings per object: read-only, the icon and a number\'s step, kept side by side in the metadata', async ({page, baseURL}) => {
    const o = await open(page, baseURL!);
    const ref = 'Logic.var-temperatur:1';
    await openSheet(page, ref);
    await sheet(page).locator('[data-sheet-assign]').click();
    const a = assign(page);
    await expect(a).toBeVisible();
    // the step: 0,25 - a decimal comma is fine - gives two decimals and quarter steps
    const step = a.locator('[data-assign-step]'), save = a.locator('[data-assign-step-save]');
    await expect(step).toHaveValue('');
    await expect(save).toBeDisabled();
    await step.fill('0');
    await expect(step).toHaveAttribute('aria-invalid', 'true');
    await expect(save).toBeDisabled();
    await step.fill('0,25');
    await save.click();
    await expect(a.locator('[data-assign-saved]')).toBeVisible();
    expect(o.patches.at(-1)).toEqual({ref, body: {meta: {control: {step: 0.25}}}});
    // the icon: the suggestions of a variable, and the namespace keeps the step
    await a.locator('[data-assign-icons-fold]').click();
    await expect(a.locator('[data-assign="icon"]')).toHaveAttribute('data-assign-icon-kind', 'object');
    await a.locator('[data-icon-group="suggested"] [data-icon-cell="temp_temperature"]').click();
    await expect.poll(() => o.patches.at(-1)?.body).toEqual({meta: {control: {step: 0.25, icon: 'temp_temperature'}}});
    // read-only beside them
    await a.locator('[data-assign="read-only"] input').check();
    await expect.poll(() => o.patches.at(-1)?.body).toEqual({meta: {control: {step: 0.25, icon: 'temp_temperature', read_only: true}}});
    await a.getByRole('button', {name: 'Close'}).click();
    await expect(a).toHaveCount(0);
    // the sheet under it follows: the value with the step's decimals, no controls, and why
    const s = sheet(page);
    await expect(s.locator('[data-sheet-state]')).toHaveText('21.50 °C');
    await expect(s.locator('[data-sheet-locked]')).toBeVisible();
    await expect(s.locator('[data-sheet-number], [data-sheet-plus], [data-sheet-range]')).toHaveCount(0);
    await closeSheet(page);
    await expect(state(page, ref)).toHaveText('21.50 °C');
    await expect(tile(page, ref).locator('[data-icon]')).toHaveAttribute('data-icon', 'temp_temperature');
    // operated again: the step is the plus button's
    await openSheet(page, ref);
    await sheet(page).locator('[data-sheet-assign]').click();
    await assign(page).locator('[data-assign="read-only"] input').uncheck();
    await expect.poll(() => o.patches.at(-1)?.body).toEqual({meta: {control: {step: 0.25, icon: 'temp_temperature'}}});
    await assign(page).getByRole('button', {name: 'Close'}).click();
    await expect(assign(page)).toHaveCount(0);
    await sheet(page).locator('[data-sheet-plus]').click();
    await expect(sheet(page).locator('[data-sheet-number]')).toHaveValue('21.75');
    await expect(sheet(page).locator('[data-sheet-range]')).toHaveAttribute('step', '0.25');
    expect(o.writes).toEqual([['var-temperatur:1', 'VALUE', 21.75]]);
});

test('read-only: a boolean\'s and a program\'s tile have no disc to press, the sheet nothing to operate', async ({page, baseURL}) => {
    const o = await open(page, baseURL!, {meta: {'Logic.var-regen:1': {read_only: true}, 'Logic.prg-markise:1': {read_only: true, icon: 'scene_scene'}}});
    for (const ref of ['Logic.var-regen:1', 'Logic.prg-markise:1']) {
        await expect(tile(page, ref)).toBeVisible();
        await expect(tile(page, ref).locator('[data-app-act]')).toHaveCount(0);
        await expect(tile(page, ref)).toHaveClass(/app-tile-read/);
    }
    await expect(tile(page, 'Logic.prg-markise:1').locator('[data-icon]')).toHaveAttribute('data-icon', 'scene_scene');
    await expect(state(page, 'Logic.prg-markise:1')).toHaveText('Active');
    await openSheet(page, 'Logic.var-regen:1');
    await expect(sheet(page).locator('[data-sheet-false], [data-sheet-true]')).toHaveCount(0);
    await expect(sheet(page).locator('[data-sheet-locked]')).toBeVisible();
    await closeSheet(page);
    await openSheet(page, 'Logic.prg-markise:1');
    await expect(sheet(page).locator('[data-sheet-run], [data-sheet-run-then], [data-sheet-active], [data-sheet-inactive]')).toHaveCount(0);
    await expect(sheet(page).locator('[data-sheet-locked]')).toBeVisible();
    // the assignment sheet of a program has no step
    await sheet(page).locator('[data-sheet-assign]').click();
    await expect(assign(page).locator('[data-assign="read-only"] input')).toBeChecked();
    await expect(assign(page).locator('[data-assign="step"]')).toHaveCount(0);
    expect(o.writes).toEqual([]);
});

test('in a room: an assigned variable and program are tiles at once, one that is not visible in the addon too', async ({page, baseURL}) => {
    await open(page, baseURL!, {path: '/app/e/rooms/kueche', rooms: ['Logic.var-anwesend:1', 'Logic.prg-markise:1', 'Logic.var-versteckt:1']});
    await expect(tile(page, 'Logic.var-anwesend:1')).toBeVisible();
    await expect(state(page, 'Logic.var-anwesend:1')).toHaveText('anwesend');
    await expect(tile(page, 'Logic.prg-markise:1')).toBeVisible();
    // the maintainer, 2026-11-24: assigned, an invisible object shows like any other
    await expect(tile(page, 'Logic.var-versteckt:1')).toBeVisible();
    await expect(tile(page, 'Logic.var-regen:1')).toHaveCount(0);
    // the unassigned list's checkbox is not here
    await expect(page.locator('[data-app-show-invisible]')).toHaveCount(0);
});

test('the unassigned list: the objects that are not visible in the addon behind their checkbox, off by default and remembered', async ({page, baseURL}) => {
    await open(page, baseURL!);
    await expect(tile(page, 'Logic.var-regen:1')).toBeVisible();
    const box = page.locator('[data-app-show-invisible]');
    await expect(box).not.toBeChecked();
    for (const ref of Object.keys(HIDDEN)) await expect(tile(page, ref)).toHaveCount(0);
    await box.check();
    for (const ref of Object.keys(HIDDEN)) await expect(tile(page, ref)).toBeVisible();
    await expect(tile(page, 'Logic.prg-versteckt:1').locator('[data-icon]')).toHaveAttribute('data-icon', 'audio_play');
    // remembered in this browser, beside the virtual channels' and keys' choices
    expect(JSON.parse((await page.evaluate(() => localStorage.getItem('ol.app.unassigned')))!)).toEqual({channels: false, keys: false, invisible: true});
    await page.reload();
    await expect(page.locator('[data-app-show-invisible]')).toBeChecked();
    for (const ref of Object.keys(HIDDEN)) await expect(tile(page, ref)).toBeVisible();
    await page.locator('[data-app-show-invisible]').uncheck();
    for (const ref of Object.keys(HIDDEN)) await expect(tile(page, ref)).toHaveCount(0);
});

test('a program\'s play button: RUN by default, RUN_THEN where its settings say; the choice beside the icon in the metadata', async ({page, baseURL}) => {
    const o = await open(page, baseURL!, {meta: {'Logic.prg-markise:1': {icon: 'scene_scene'}}});
    const ref = 'Logic.prg-markise:1';
    await expect(tile(page, ref).locator('[data-icon]')).toHaveAttribute('data-icon', 'scene_scene');
    await openSheet(page, ref);
    await sheet(page).locator('[data-sheet-assign]').click();
    const a = assign(page);
    const run = a.locator('[data-assign-run="run"]'), then = a.locator('[data-assign-run="run_then"]');
    await expect(run).toBeChecked();
    await expect(then).not.toBeChecked();
    // a program has no step
    await expect(a.locator('[data-assign="step"]')).toHaveCount(0);
    await then.check();
    await expect.poll(() => o.patches.at(-1)?.body).toEqual({meta: {control: {icon: 'scene_scene', run_then: true}}});
    await a.getByRole('button', {name: 'Close'}).click();
    await expect(a).toHaveCount(0);
    // the sheet's disc follows the choice, its two buttons stay
    await sheet(page).locator('.sh-circle').click();
    await expect.poll(() => o.writes.at(-1)).toEqual(['prg-markise:1', 'RUN_THEN', true]);
    await expect(sheet(page).locator('[data-sheet-run]')).toBeVisible();
    await expect(sheet(page).locator('[data-sheet-run-then]')).toBeVisible();
    await closeSheet(page);
    await tile(page, ref).locator('[data-app-act]').click();
    await expect(state(page, ref)).toHaveText('Started');
    expect(o.writes.at(-1)).toEqual(['prg-markise:1', 'RUN_THEN', true]);
    // back to RUN: the key leaves the namespace, the icon stays
    await openSheet(page, ref);
    await sheet(page).locator('[data-sheet-assign]').click();
    await assign(page).locator('[data-assign-run="run"]').check();
    await expect.poll(() => o.patches.at(-1)?.body).toEqual({meta: {control: {icon: 'scene_scene'}}});
    await assign(page).getByRole('button', {name: 'Close'}).click();
    await closeSheet(page);
    await tile(page, ref).locator('[data-app-act]').click();
    await expect.poll(() => o.writes.at(-1)).toEqual(['prg-markise:1', 'RUN', true]);
    expect(o.writes.length).toBe(3);
});

test('German: the play button\'s choice and the invisible objects\' checkbox', async ({page, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await open(page, baseURL!);
    await expect(page.locator('[data-app-unassigned-show]')).toContainText('Zeige unsichtbare Variablen und Programme');
    await openSheet(page, 'Logic.prg-markise:1');
    await sheet(page).locator('[data-sheet-assign]').click();
    await expect(assign(page).locator('[data-assign="run-mode"]')).toContainText('Die Schaltfläche auf der Kachel');
    await expect(assign(page).locator('[data-assign="run-mode"]')).toContainText('startet es ohne Bedingungen');
});
