import {expect, test, type Page} from '@playwright/test';

// occulited task 150: backup targets an addon provides - the hint without a provider, the Add
// target button per provider, the form (account, folder, upload from, no encryption switch), the
// card in every state with a badge that keeps its box, the provider's sentence and action, the
// hand-over from the addon's page, a phone's width, German.

const card = (page: Page, id: string) => page.locator(`[data-target="${id}"]`);

const PROVIDER = {
    addon: 'sync', name: 'Sync', label: {de: 'Cloud-Speicher', en: 'Cloud storage'}, setup: '/addon-settings/sync#accounts', available: true,
    destinations: [
        {id: 'acc1', name: 'Privat', detail: 'OneDrive · max@example.org', state: 'writable'},
        {id: 'acc2', name: 'Haus', detail: 'Google Drive · haus@example.org', state: 'writable'},
    ],
};

function cloud(state: Record<string, unknown> = {}, more: Record<string, unknown> = {}) {
    return {
        id: 'tcloud01', name: 'OneDrive', kind: 'addon', enabled: true, max_backups: 30, subdir: '', encrypt: true, append_only: false, created: '2026-11-01T10:00:00Z',
        addon: {id: 'sync', destination: 'acc1', folder: 'openccu-lite/openccu', source: ''},
        provider: {addon: 'sync', name: 'Sync', label: PROVIDER.label, installed: true, destination: PROVIDER.destinations[0], upload_from: 'USB'},
        state: {state: 'idle', failures: 0, mounted: false, ...state},
        last_backup: {at: '2026-11-17T00:21:00Z', ok: true, state: 'writable', name: 'openccu-2.0.0-2026-11-17-0007.sbk.age', size: 8388608, duration_ms: 38000, encrypted: true, last_ok: '2026-11-17T00:21:00Z', from: 'USB', verified: 'hash'},
        ...more,
    };
}

async function plant(page: Page, change: (v: Record<string, any>) => void) {
    const res = await page.request.get('/api/system/v1/backup/targets');
    const view = await res.json();
    change(view);
    await page.route(/\/api\/system\/v1\/backup\/targets(\?.*)?$/, (r) => (r.request().method() === 'GET' ? r.fulfill({json: view}) : r.fallback()));
    return view;
}

test('without a provider: a muted hint, no button that does nothing', async ({page}) => {
    await page.goto('/system/backup');
    await expect(page.locator('[data-add="sftp"]')).toBeVisible();
    await expect(page.locator('[data-add="addon"]')).toHaveCount(0);
    await expect(page.locator('[data-notice="no-provider"]')).toContainText('install the addon Sync');
    await expect(page.locator('[data-notice="no-provider"] a')).toHaveAttribute('href', '/addons/catalog');
});

test('add a cloud target: the account, the folder, upload from, always encrypted', async ({page}) => {
    const sent: {method: string; body: any}[] = [];
    page.on('request', (r) => {
        if (r.url().endsWith('/backup/targets') && r.method() === 'POST') sent.push({method: r.method(), body: JSON.parse(r.postData() || '{}')});
    });
    await plant(page, (v) => {
        v.providers = [PROVIDER];
        v.encryption = true;
        v.kinds.addon = '';
    });
    await page.goto('/system/backup');
    await expect(page.locator('[data-notice="no-provider"]')).toHaveCount(0);
    const add = page.locator('[data-add="addon"][data-provider="sync"]');
    await expect(add).toHaveText('Cloud storage');
    await add.click();
    await expect(page.locator('[data-new-target="addon"] .ol-card-title')).toHaveText('New target: Cloud storage');
    const form = page.locator('[data-form="addon"]');
    await expect(form.locator('[data-field="destination"]')).toHaveValue('acc1');
    await expect(form.locator('[data-field="destination"] option')).toHaveText(['OneDrive · max@example.org', 'Google Drive · haus@example.org']);
    await expect(form.locator('[data-field="name"]')).toHaveValue('Privat');
    await expect(form.locator('[data-field="folder"]')).toHaveValue('openccu-lite/openccu');
    // the way to the addon's page carries the way back
    await expect(form.locator('[data-action="connect-account"]')).toHaveAttribute('href', '/addon-settings/sync?return=%2Fbackup%23targets#accounts');
    // upload from: automatic names the local target that keeps encrypted backups
    await expect(form.locator('[data-field="source"]')).toHaveValue('');
    await expect(form.locator('[data-source-hint]')).toContainText('from "USB", which already holds each backup');
    await form.locator('[data-field="source"]').selectOption('staging');
    await expect(form.locator('[data-source-hint]')).toContainText("from the system's working copy");
    await form.locator('[data-field="source"]').selectOption('');
    // no encryption switch, no subdirectory
    await expect(form.locator('[data-field="encrypt"]')).toHaveCount(0);
    await expect(form.locator('[data-field="subdir"]')).toHaveCount(0);
    await expect(form.locator('[data-notice="always-encrypted"]')).toBeVisible();
    // a folder that leads out is not saved
    await form.locator('[data-field="folder"]').fill('../up');
    await expect(form.locator('[data-action="save"]')).toBeDisabled();
    await form.locator('[data-field="folder"]').fill('openccu-lite/ccu-keller');
    await form.locator('[data-field="destination"]').selectOption('acc2');
    await form.locator('[data-action="save"]').click();
    await expect.poll(() => sent.length).toBe(1);
    expect(sent[0].body).toMatchObject({kind: 'addon', encrypt: true, subdir: '', max_backups: 30, enabled: true, addon: {id: 'sync', destination: 'acc2', folder: 'openccu-lite/ccu-keller', source: ''}});
});

test('no recovery key: the form says so and cannot be saved', async ({page}) => {
    await plant(page, (v) => {
        v.providers = [PROVIDER];
        v.encryption = false;
    });
    await page.goto('/system/backup');
    await page.locator('[data-add="addon"]').click();
    const form = page.locator('[data-form="addon"]');
    await expect(form.locator('[data-notice="needs-encryption"]')).toContainText('encrypted backups only');
    await expect(form.locator('[data-notice="needs-encryption"] a')).toHaveAttribute('href', '#encryption');
    await expect(form.locator('[data-action="save"]')).toBeDisabled();
});

test('a provider that is not running: its button is there and disabled', async ({page}) => {
    await plant(page, (v) => {
        v.providers = [{...PROVIDER, available: false, destinations: []}];
    });
    await page.goto('/system/backup');
    await expect(page.locator('[data-add="addon"]')).toBeDisabled();
});

for (const lang of ['en', 'de'] as const) {
    test(`the card keeps its badge's box from state to state (${lang})`, async ({page}) => {
        await page.addInitScript((l) => localStorage.setItem('ol.language', l), lang);
        const action = {label: {de: 'Erneut anmelden', en: 'Log in again'}, href: '/addon-settings/sync#accounts'};
        const states: Record<string, unknown>[] = [
            {state: 'idle'},
            {state: 'writable', free_bytes: 4100000000, total_bytes: 5000000000},
            {state: 'running', progress: {name: 'x.sbk.age', size: 1000, sent: 420}},
            {state: 'running'},
            {state: 'auth-failed', message: {de: 'OneDrive nimmt die Anmeldung dieses Systems nicht mehr an.', en: "OneDrive no longer accepts this system's login."}, action},
            {state: 'unreachable', detail: 'dial tcp: i/o timeout'},
            {state: 'full'},
            {state: 'addon-missing'},
            {state: 'not-encrypted'},
            {state: 'error', gone: true},
        ];
        let current = states[0];
        const res = await page.request.get('/api/system/v1/backup/targets');
        const view = await res.json();
        view.providers = [PROVIDER];
        view.encryption = true;
        await page.route('**/api/system/v1/backup/targets', (r) => (r.request().method() === 'GET' ? r.fulfill({json: {...view, targets: [...view.targets, cloud(current)]}}) : r.fallback()));
        await page.goto('/system/backup');
        const c = card(page, 'tcloud01');
        await expect(c).toContainText(lang === 'de' ? 'Cloud-Speicher' : 'Cloud storage');
        await expect(c.locator('.ol-card-sub')).toHaveText('OneDrive · max@example.org · openccu-lite/openccu');
        await expect(c.locator('[data-upload-from]')).toContainText(lang === 'de' ? 'Hochgeladen von „USB“' : 'Uploaded from "USB"');
        const badge = c.locator('.ol-badge-sized');
        const first = await badge.boundingBox();
        const texts: string[] = [];
        for (const s of states) {
            current = s;
            await page.reload();
            await expect(badge).toHaveAttribute('data-state', String(s.state));
            const box = await badge.boundingBox();
            expect(box?.width, `the badge's width in state ${JSON.stringify(s)}`).toBe(first?.width);
            expect(box?.height).toBe(first?.height);
            texts.push((await badge.locator('.ol-badge-now').textContent()) ?? '');
        }
        expect(texts[2]).toMatch(/42 %$/);
        expect(texts[3]).not.toMatch(/%/);
        // the lapsed login: the provider's sentence and its action in front of the row
        current = states[4];
        await page.reload();
        await expect(c.locator('[data-detail]')).toHaveText(lang === 'de' ? 'OneDrive nimmt die Anmeldung dieses Systems nicht mehr an.' : "OneDrive no longer accepts this system's login.");
        const act = c.locator('[data-action="provider-action"]');
        await expect(act).toHaveText(lang === 'de' ? 'Erneut anmelden' : 'Log in again');
        await expect(act).toHaveAttribute('href', '/addon-settings/sync#accounts');
        expect(await c.locator('.ol-card-foot > *').first().getAttribute('data-action')).toBe('provider-action');
        // the addon gone: what it needs, and the way to the Addons page
        current = states[7];
        await page.reload();
        await expect(c.locator('[data-detail]')).toContainText('Sync');
        await expect(c.locator('[data-action="addons"]')).toHaveAttribute('href', '/addons');
        await expect(c.locator('[data-action="provider-action"]')).toHaveCount(0);
        // the account removed in the addon
        current = states[9];
        await page.reload();
        await expect(c.locator('[data-detail]')).toContainText(lang === 'de' ? 'gibt es nicht mehr' : 'no longer exists');
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    });
}

test('the only target says what that means; the hand-over from the addon opens the form', async ({page}) => {
    await plant(page, (v) => {
        v.providers = [PROVIDER];
        v.encryption = true;
        v.targets = [cloud({}, {provider: {addon: 'sync', name: 'Sync', label: PROVIDER.label, installed: true, destination: PROVIDER.destinations[0]}, last_backup: undefined})];
    });
    // the account was made a moment ago: the page asks the provider now, not for a cached answer
    const asked: string[] = [];
    page.on('request', (r) => {
        if (/\/backup\/targets(\?|$)/.test(r.url())) asked.push(new URL(r.url()).search);
    });
    await page.goto('/system/backup?add-target=sync&destination=acc2#targets');
    const c = card(page, 'tcloud01');
    await expect(c.locator('[data-notice="only-target"]')).toContainText('only target');
    await expect(c.locator('[data-upload-from]')).toContainText("working copy");
    const form = page.locator('[data-form="addon"]');
    await expect(form.locator('[data-field="destination"]')).toHaveValue('acc2');
    await expect(form.locator('[data-field="name"]')).toHaveValue('Haus');
    await expect(form.locator('[data-source-hint]')).toContainText("working copy");
    expect(new URL(page.url()).search).toBe('');
    expect(asked).toContain('?providers=fresh');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('editing keeps the account and the folder; a test that fails shows the provider\'s sentence', async ({page}) => {
    await plant(page, (v) => {
        v.providers = [PROVIDER];
        v.encryption = true;
        v.targets.push(cloud());
    });
    await page.route('**/api/system/v1/backup/targets/tcloud01/test', (r) => r.fulfill({json: {ok: false, state: 'auth-failed', step: 'test', error: 'x', free_bytes: 0, total_bytes: 0, needed_bytes: 0, at: '2026-11-17T10:00:00Z', message: {en: 'The login has lapsed.', de: 'Die Anmeldung ist abgelaufen.'}}}));
    await page.goto('/system/backup');
    const c = card(page, 'tcloud01');
    await c.locator('[data-action="edit"]').click();
    const form = c.locator('[data-form="addon"]');
    await expect(form.locator('[data-field="destination"]')).toHaveValue('acc1');
    await expect(form.locator('[data-field="folder"]')).toHaveValue('openccu-lite/openccu');
    await form.locator('[data-action="cancel"]').click();
    await c.locator('[data-action="test"]').click();
    await expect(c.locator('[data-test-result="auth-failed"]')).toHaveText('login refused: The login has lapsed.');
});
