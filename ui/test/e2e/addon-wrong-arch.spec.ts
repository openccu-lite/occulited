import {expect, test, type BrowserContext, type Page} from '@playwright/test';

// occulited B-167: a package for another architecture is refused before any of its code runs, and
// the page says which architectures and which file to take - in the user's language. A package
// only its update_script refuses (exit 13) shows the script's last lines, not the exit code alone.

async function upload(page: Page, context: BrowserContext, baseURL: string | undefined, variant: string, install: string) {
    await context.addCookies([{name: 'stub-install', value: variant, url: baseURL!}]);
    await page.goto('/addons');
    await page.locator('input[type="file"]').setInputFiles({name: 'battery-aarch64-1.5.5.tar.gz', mimeType: 'application/gzip', buffer: Buffer.from('x')});
    await page.locator('form').getByRole('button', {name: install, exact: true}).click();
}

test('refused before the installer: both architectures and the file to take', async ({page, context, baseURL}) => {
    await upload(page, context, baseURL, 'wrong-arch', 'Install');
    await expect(page.locator('.ol-notice').filter({hasText: 'This package of battery'})).toHaveText('This package of battery is for aarch64; this system is x86_64. Take the package for x86_64: battery-x86_64-1.5.5.tar.gz. Nothing was installed.');
    await expect(page.locator('.ol-notice').filter({hasText: 'Install result'})).toHaveCount(0);
});

test('in German', async ({page, context, baseURL}) => {
    await page.addInitScript(() => localStorage.setItem('ol.language', 'de'));
    await upload(page, context, baseURL, 'wrong-arch', 'Installieren');
    await expect(page.locator('.ol-notice').filter({hasText: 'Dieses Paket von battery'})).toHaveText('Dieses Paket von battery ist für aarch64; dieses System ist x86_64. Das Paket für x86_64 ist battery-x86_64-1.5.5.tar.gz. Es wurde nichts installiert.');
});

test('refused by its update_script: the script\'s own words under the exit code', async ({page, context, baseURL}) => {
    await upload(page, context, baseURL, 'exit13', 'Install');
    const result = page.locator('.ol-notice.error').filter({hasText: 'Install result'});
    await expect(result).toContainText('unsupported platform');
    await expect(result).toContainText('exit 13');
    await expect(result.locator('pre')).toContainText('this package is for aarch64, this system is x86_64: take battery-x86_64-1.5.5.tar.gz');
});
