import {type Locator, type Page} from '@playwright/test';
import {expect, test} from './fixtures';

// occulited task 154: an addon's own timer on the Services page. The addon made it through
// /addon-timers; here it is an own timer with the addon named, whose schedule is the user's to
// edit and whose service is the system's rendering - shown, never changed. A link with
// ?edit=local-<id>-<name>.timer (the addon's page links its jobs that way) opens its two files.

async function press(target: Locator) {
    if (test.info().project.use.isMobile) await target.tap();
    else await target.click();
}
const UNIT = 'local-sync-fotos.timer';
// the shape the Services page's form writes (Description= the own timer's name), so the file follows the form
const TIMER = '[Unit]\nDescription=sync-fotos\n\n[Timer]\nOnCalendar=*-*-* 03:00:00\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n';
const SERVICE =
    "# openccu-lite: the service of an addon's timer, rendered by the system.\n[Unit]\nDescription=Sync: Fotos\n\n[Service]\nType=oneshot\nExecStart=/usr/local/addons/sync/bin/lite-sync run fotos\nUser=addon-sync\nGroup=addon-sync\n";

async function mock(page: Page) {
    const stored = {name: 'sync-fotos', unit: UNIT, service: 'local-sync-fotos.service', timer_file: TIMER, service_file: SERVICE, enabled: true, addon: 'sync'};
    const puts: {timer: string; service: string}[] = [];
    await page.route('**/api/system/v1/timers', async (route) => {
        const res = await route.fetch();
        const body = await res.json();
        body.timers.push({unit: UNIT, activates: 'local-sync-fotos.service', next: new Date(Date.now() + 3600000).toISOString(), left: '1h', active: true, own: true, addon: 'sync', enabled: true, unit_file_state: 'enabled-runtime'});
        await route.fulfill({response: res, json: body});
    });
    await page.route(`**/api/system/v1/timers/own/${UNIT}`, async (route) => {
        if (route.request().method() === 'PUT') {
            const b = route.request().postDataJSON();
            puts.push(b);
            if (b.service.trim() !== stored.service_file.trim()) {
                return route.fulfill({status: 422, json: {error: 'addon-service', message: 'the service of this timer is rendered by the system for the addon sync and cannot be changed; the timer file can'}});
            }
            stored.timer_file = b.timer;
        }
        return route.fulfill({json: stored});
    });
    await page.route('**/api/system/v1/timers/own/sync-fotos', async (route) => {
        const b = route.request().postDataJSON();
        puts.push(b);
        stored.timer_file = b.timer;
        return route.fulfill({json: stored});
    });
    return puts;
}

test("an addon's timer: named in the table, the schedule editable, the service read-only and sent back unchanged", async ({page}) => {
    const puts = await mock(page);
    await page.goto('/system/services');
    const row = page.locator('table.ol-timers tbody tr').filter({has: page.locator('td:first-child', {hasText: UNIT})});
    await expect(row.locator('.ol-badge').first()).toHaveText('addon sync');
    await expect(row.locator('.ol-badge').first()).toHaveAttribute('title', /Made by the addon sync/);
    await press(row.getByRole('button', {name: 'More actions'}));
    await expect(row.getByRole('menuitem', {name: 'Delete', exact: true})).toBeVisible();
    await press(row.getByRole('menuitem', {name: 'Edit…', exact: true}));
    const ed = page.getByRole('dialog', {name: `Edit timer ${UNIT}`});
    await expect(ed).toBeVisible();
    await expect(ed.locator('.te-addon')).toContainText('This timer belongs to the addon sync.');
    // what it runs is not a form here, and the file cannot be typed into
    await expect(ed.getByLabel('Command (ExecStart=)')).toBeHidden();
    await expect(ed.getByLabel('Runs as (User=)')).toBeHidden();
    const service = ed.getByRole('textbox', {name: 'local-sync-fotos.service'});
    await expect(service).toHaveValue(SERVICE);
    await expect(service).toHaveAttribute('readonly', '');
    await expect(ed.locator('.te-file .te-lbl', {hasText: 'set by the system'})).toBeVisible();
    // the schedule reads as the form's daily 03:00 and follows the form
    await expect(ed.getByLabel('When')).toHaveValue('daily');
    await ed.getByLabel('Time', {exact: true}).fill('04:30');
    await expect(ed.getByRole('textbox', {name: UNIT})).toHaveValue(/OnCalendar=\*-\*-\* 04:30:00/);
    await expect(service).toHaveValue(SERVICE);
    await press(ed.locator('.ol-modal-foot').getByRole('button', {name: 'Save', exact: true}));
    await expect(ed.getByRole('status')).toHaveText('Saved.');
    expect(puts).toHaveLength(1);
    expect(puts[0]!.service).toBe(SERVICE);
    expect(puts[0]!.timer).toContain('OnCalendar=*-*-* 04:30:00');
});

test("a link with ?edit= on an addon's timer opens its two files, not an override", async ({page}) => {
    await mock(page);
    await page.goto(`/system/services?edit=${UNIT}`);
    const ed = page.getByRole('dialog', {name: `Edit timer ${UNIT}`});
    await expect(ed).toBeVisible();
    await expect(ed.getByRole('textbox', {name: UNIT})).toHaveValue(TIMER);
    await expect(ed.locator('.te-addon')).toBeVisible();
});

test('in German: the badge and the note', async ({page}) => {
    await mock(page);
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await page.goto('/system/services');
    const row = page.locator('table.ol-timers tbody tr').filter({has: page.locator('td:first-child', {hasText: UNIT})});
    await expect(row.locator('.ol-badge').first()).toHaveText('Addon sync');
    await press(row.getByRole('button').last());
    await press(row.getByRole('menuitem').first());
    const ed = page.getByRole('dialog').filter({hasText: UNIT});
    await expect(ed.locator('.te-addon')).toContainText('Dieser Timer gehört zum Addon sync.');
    await expect(ed.locator('.te-file .te-lbl', {hasText: 'vom System festgelegt'})).toBeVisible();
});
