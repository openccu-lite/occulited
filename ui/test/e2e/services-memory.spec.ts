import {expect, test} from './fixtures';

// occulited task 178 (the maintainer, 2026-11-24): occulited showed over 200 MB on his Pi 3, of
// which 159 MB were the reclaimable page cache. The Memory column shows what a unit uses -
// memory.current less inactive_file - and its tooltip breaks it down; a unit without the figures
// (an older system) shows memory.current as before.
test('memory without the page cache, broken down in the tooltip', async ({page}) => {
    await page.goto('/system/services');
    const hmip = page.locator('tr[data-service="hmipserver"] [data-memory]');
    await expect(hmip).toHaveText('40.0 MB');
    await expect(hmip).toHaveAttribute('data-memory', 'used');
    await expect(hmip).toHaveAttribute('title', '40.0 MB in use, 52.0 MB page cache that the kernel frees when memory is needed; 92.0 MB together');
    const rfd = page.locator('tr[data-service="rfd"] [data-memory]');
    await expect(rfd).toHaveText('23.0 MB');
    await expect(rfd).toHaveAttribute('data-memory', 'current');
    await expect(rfd).not.toHaveAttribute('title', /.+/);

    await page.evaluate(() => localStorage.setItem('ol.language', 'de'));
    await page.reload();
    await expect(page.locator('tr[data-service="hmipserver"] [data-memory]')).toHaveAttribute('title', /Seitencache, den der Kernel freigibt/);
});
