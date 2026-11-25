import {expect, test} from '@playwright/test';

// openccu-lite task 505: the App's service worker and the recovery system on the same origin. The
// stub answers as the recovery's lighttpd with the cookie stub-recovery=1 (its menu for a page, a
// redirect for anything else, no shell mark). A worker that kept that page would show it as the
// App after the recovery: it must pass it on and keep the shell it had.
test.use({serviceWorkers: 'allow'});

test("the worker shows the recovery's page but keeps occulited's shell", async ({page, context}) => {
    await page.goto('/');
    await page.evaluate(async () => {
        await navigator.serviceWorker.register('/sw.js', {scope: '/'});
        await navigator.serviceWorker.ready;
    });
    await page.reload();
    await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller), {timeout: 10_000}).toBe(true);
    const shell = async () => page.evaluate(async () => {
        const r = await caches.match('/');
        return r ? (await r.text()).slice(0, 4000) : '';
    });
    await expect.poll(shell, {timeout: 10_000}).toContain('<div id="app"');

    // the recovery answers the origin: a reload shows its page, the kept shell stays the App's
    await context.addCookies([{name: 'stub-recovery', value: '1', url: page.url()}]);
    await page.reload();
    await expect(page.locator('h1')).toHaveText('CCU Recovery System');
    // an icon and a bundle through the worker: redirected to the menu, not kept
    await page.evaluate(async () => {
        await fetch('/icons/icon-192.png').catch(() => undefined);
        await fetch('/assets/recovery-probe-0000.js').catch(() => undefined);
    });
    await page.waitForTimeout(300);
    expect(await shell()).toContain('<div id="app"');
    expect(await shell()).not.toContain('CCU Recovery System');
    const kept = await page.evaluate(async () => {
        const icon = await caches.match('/icons/icon-192.png');
        const probe = await caches.match('/assets/recovery-probe-0000.js');
        return {icon: icon ? icon.headers.get('Content-Type') : null, probe: !!probe};
    });
    expect(kept.probe).toBe(false);
    expect(kept.icon === null || kept.icon === 'image/png').toBe(true);

    // the system back: the App again
    await context.clearCookies({name: 'stub-recovery'});
    await page.reload();
    await expect(page.locator('header.ol-header')).toBeVisible();
});
