import {describe, expect, it} from 'vitest';
import {periodLabel, silencedLine, silencePath, splitSilenced, warningLink, warningText, type Warning, type Words} from './warnings';

const t = (key: string, params?: Record<string, string | number>) => {
    let s = key;
    for (const [k, v] of Object.entries(params ?? {})) s = s.replaceAll(`{${k}}`, String(v));
    return s;
};
const I18N = (import.meta.glob('./i18n.svelte.ts', {query: '?raw', import: 'default', eager: true}) as Record<string, string>)['./i18n.svelte.ts'] ?? '';
const words: Words = {t, addonName: (a) => a.name || a.id, storage: (w) => `storage ${w.variant}`, when: (iso) => `at ${iso}`};
const w = (id: string, variant: string, params: Warning['params'] = {}, href = ''): Warning => ({id, variant, severity: 'error', params, href});

describe('the Status page warnings (task 81)', () => {
    it('names the addons that get the legacy session, one or several', () => {
        const one = warningText(w('legacy-session', 'mosquitto', {addons: [{id: 'mosquitto', name: 'Mosquitto', enabled: true}]}), words);
        expect(one).toContain('Mosquitto receives your session in its URL');
        expect(one).toContain('Addons page');
        const two = warningText(w('legacy-session', 'hmm,mosquitto', {addons: [{id: 'hmm', name: 'Homematic Manager', enabled: true}, {id: 'mosquitto', name: 'Mosquitto', enabled: true}]}), words);
        expect(two).toContain('2 addons receive your session in their URLs');
        expect(two).toContain('Homematic Manager, Mosquitto');
        expect(warningLink(w('legacy-session', 'mosquitto', {}, '/addons'), t)).toEqual({href: '/addons', label: 'Installed addons'});
    });

    // occulited task 173: the throttle state - the supply's advice, the heat's, and that the firmware keeps it
    it('says the power-throttle states with their cause', () => {
        const p = (variant: string, params: Warning['params']) => warningText(w('power-throttle', variant, params, '#temperature'), words);
        expect(p('undervoltage', {cause: 'supply', count: 3, supply: 'pi'})).toBe('The supply voltage is too low now, and the processor is slowed down. The usual cause is the power supply or its cable: use one that delivers enough current (for a Raspberry Pi the official power supply) and a short cable.');
        expect(p('throttled', {cause: 'supply', count: 2, last: '2026-11-22T16:44:09Z', supply: 'pi'})).toBe('The processor is slowed down now; since the start the supply voltage was too low (2×, the last on at 2026-11-22T16:44:09Z). The usual cause is the power supply or its cable: use one that delivers enough current (for a Raspberry Pi the official power supply) and a short cable.');
        expect(p('throttled', {cause: 'heat'})).toBe('The processor is slowed down now by its temperature limit. Give the system more air, a heat sink or a fan.');
        expect(p('throttled', {cause: 'unknown'})).toBe('The processor is slowed down now.');
        const earlier = p('earlier', {cause: 'supply', count: 3, last: '2026-11-22T10:30:00Z'});
        expect(earlier).toMatch(/^Not now, but since the start the supply voltage was too low \(3×, the last on at 2026-11-22T10:30:00Z\), and the processor was slowed down\. The usual cause is the power supply/);
        expect(earlier).toMatch(/The firmware keeps this until the next start\.$/);
        expect(p('earlier', {cause: 'supply'})).toMatch(/^Not now, but since the start the supply voltage was too low, and/);
        expect(p('earlier', {cause: 'heat'})).toBe('Not now, but since the start the processor was slowed down by its temperature limit. Give the system more air, a heat sink or a fan. The firmware keeps this until the next start.');
        expect(p('earlier', {cause: 'unknown'})).toBe('Not now, but since the start the processor was slowed down at least once. The firmware keeps this until the next start.');
        expect(warningLink(w('power-throttle', 'earlier', {}, '#temperature'), t)).toEqual({href: '#temperature', label: 'Temperature'});
    });

    // occulited B-186 (the maintainer): with an RPI-RF-MOD on the Pi the official ELV power supply goes into the radio
    // module, not into the Pi - the advice follows where the supply goes in; unknown, it sends nobody to the Pi's socket
    it.each([
        {supply: 'rpi-rf-mod', want: 'The usual cause is the power supply or its cable. With the RPI-RF-MOD the system is powered through the radio module: use the ELV power supply meant for it, plugged into the RPI-RF-MOD, not into the Raspberry Pi.'},
        {supply: 'pi', want: 'The usual cause is the power supply or its cable: use one that delivers enough current (for a Raspberry Pi the official power supply) and a short cable.'},
        {supply: undefined, want: 'The usual cause is the power supply or its cable: use one that delivers enough current and a short cable. With an RPI-RF-MOD on the Raspberry Pi, the power supply goes into the radio module, not into the Raspberry Pi.'},
    ])('advises on the power supply by where it goes in ($supply)', ({supply, want}) => {
        for (const variant of ['undervoltage', 'throttled', 'earlier']) {
            const text = warningText(w('power-throttle', variant, {cause: 'supply', count: 1, supply}, '#temperature'), words);
            expect(text).toContain(want);
            if (supply !== 'pi') expect(text).not.toContain('official power supply');
        }
    });

    it('says a newer bootloader is available, or prepared, and links to the Hardware page (openccu-lite task 366)', () => {
        const n = {...w('bootloader-update', '2026-11-20', {running: '2026-10-15', latest: '2026-11-20'}, '/system/hardware#bootloader'), severity: 'info' as const};
        expect(warningText(n, words)).toBe('A newer bootloader is available for this Raspberry Pi: 2026-11-20 (installed: 2026-10-15). The Hardware page installs it.');
        expect(warningText({...n, params: {...n.params, staged: true}}, words)).toBe('The bootloader update of 2026-11-20 is prepared; the Raspberry Pi writes it at the next start.');
        expect(warningLink(n, t)).toEqual({href: '/system/hardware#bootloader', label: 'Hardware'});
    });

    it('names the available system update, its channel off stable, and links to the Updates page (occulited task 123)', () => {
        const u = {...w('system-update-available', '1.0.1', {version: '1.0.1', channel: 'stable', running: '1.0.0'}, '/system/updates#system-update'), severity: 'warning' as const};
        expect(warningText(u, words)).toBe('A system update is available: 1.0.1.');
        expect(warningText({...u, variant: '1.1.0-beta.1', params: {version: '1.1.0-beta.1', channel: 'beta'}}, words)).toBe('A system update is available in the beta channel: 1.1.0-beta.1.');
        expect(warningLink(u, t)).toEqual({href: '/system/updates#system-update', label: 'Updates'});
    });

    it('says which accounts were reset on the console (occulited task 14)', () => {
        const text = warningText(w('console-reset', 'admin@1700000000', {accounts: [{user: 'admin', at: '2026-10-03T12:00:00Z'}]}), words);
        expect(text).toContain('Access reset on the console: admin (at 2026-10-03T12:00:00Z).');
        expect(text).toContain('one-time password');
        expect(warningLink(w('console-reset', 'admin@1700000000', {}, '/system/users'), t)).toEqual({href: '/system/users', label: 'Users'});
    });

    it('names the host and the missing authority of a strict TLS failure (openccu-lite task 231)', () => {
        const plain = warningText(w('trust-ca', 'api.github.com', {hosts: ['api.github.com'], issuer: 'CN=DigiCert Global Root G2', store: 'occulited', candidate: false}), words);
        expect(plain).toBe('api.github.com: the server presents a certificate from CN=DigiCert Global Root G2, which the occulited trust store does not hold. The call fails until the authority is added.');
        const fix = warningText(w('trust-ca', 'a,b', {hosts: ['a', 'b'], issuer: 'X', store: 'occulited', candidate: true}), words);
        expect(fix).toContain('a, b: the server presents');
        expect(fix).toContain('one click on the Trust stores page copies it');
        expect(warningLink(w('trust-ca', 'a', {}, '/system/trust#occulited'), t)).toEqual({href: '/system/trust#occulited', label: 'Trust stores'});
    });

    it('names the addons and says disabled once', () => {
        const text = warningText(w('rega', 'email,hm-print', {addons: [{id: 'hm-print', name: 'Print', enabled: false}, {id: 'email', name: 'E-Mail', enabled: true}]}), words);
        expect(text).toBe('Disabled incompatible Addon: Print. Installed, not usable without ReGa, and still enabled: E-Mail.');
        expect(warningText(w('arch', 'cuxd', {addons: [{id: 'cuxd', enabled: false}]}), words)).toBe('Built for another architecture, and therefore disabled: cuxd. The repair is a release built for this system.');
    });

    it('names the client that sends many radio writes, and the one behind a duty cycle\'s rise (occulited task 163)', () => {
        const rest = 'Every radio write costs airtime, and once the duty cycle is used up the radio module stops sending for a while. Nothing is slowed down here: the program has to send less.';
        const burst = w('radio-writes', 'token:nodered-1', {client: 'nodered-1', kind: 'token', burst: true, average: false, peak_minute: 84, peak_average: 19.2, average_minutes: 5}, '#radio-writes');
        expect(warningText(burst, words)).toBe(`The program nodered-1 sends many radio writes: up to 84 in one minute. ${rest}`);
        const avg = w('radio-writes', 'token:hm2mqtt', {client: 'hm2mqtt', label: 'hm2mqtt.js', kind: 'token', burst: false, average: true, peak_minute: 30, peak_average: 24.4, average_minutes: 5}, '#radio-writes');
        expect(warningText(avg, words)).toBe(`The program hm2mqtt (hm2mqtt.js) sends many radio writes: 24.4 a minute on average over 5 minutes. ${rest}`);
        expect(warningText(w('radio-writes', 'user:admin', {client: 'admin', kind: 'user', burst: true, peak_minute: 70}), words)).toContain('The account admin sends many radio writes: up to 70 in one minute.');
        expect(warningText(w('radio-writes', 'system:occulited', {client: 'occulited', kind: 'system', burst: true, peak_minute: 70}), words)).toContain('The system itself sends many radio writes');
        const red = w('radio-writes-duty-cycle', 'token:nodered-1@HmIP-RF', {client: 'nodered-1', kind: 'token', interface: 'HmIP-RF', dc_from: 3, dc_to: 24, writes: 560, share_percent: 85, minutes: 15}, '#radio-writes');
        expect(warningText(red, words)).toBe(`The program nodered-1 sent 85 % of the radio writes to HmIP-RF in the last 15 minutes (560), and the duty cycle there rose from 3 % to 24 % in that time. ${rest}`);
        expect(warningLink(red, t)).toEqual({href: '#radio-writes', label: 'Radio writes'});
        expect(warningLink(burst, t)).toEqual({href: '#radio-writes', label: 'Radio writes'});
    });

    it('says an HB-RF-ETH whose link is lost is reconnected, not retried (B-218)', () => {
        expect(warningText(w('hb-rf-eth', '192.0.2.209', {address: '192.0.2.209', reconnecting: true}), words)).toBe('The HB-RF-ETH at 192.0.2.209 lost its connection: the radio module on it is not available until it is back. The system reconnects it on its own.');
        expect(warningText(w('hb-rf-eth', '192.0.2.209', {address: '192.0.2.209', reconnecting: false}), words)).toContain('tries again every 30 seconds');
        // openccu-lite task 316: the radio's load
        expect(warningText(w('radio-load', 'dc', {interface: 'HmIP-RF', duty_cycle: 63}), words)).toBe('The radio is busy: HmIP-RF reports a duty cycle of 63 %.');
        expect(warningText(w('radio-load', 'dc,cs', {interface: 'HmIP-RF', duty_cycle: 63, carrier_sense: 12}), words)).toContain('a duty cycle of 63 % and a carrier sense of 12 %');
        // occulited task 61: during a firmware transfer the threshold is 75 %, and the warning says why it is more than usual
        expect(warningText(w('radio-load', 'dc', {interface: 'HmIP-RF', duty_cycle: 78, transfer: true}), words)).toBe('The radio is busy: HmIP-RF reports a duty cycle of 78 %. A device firmware transfer is running, which takes about half of the airtime; this is more.');
        expect(warningText(w('radio-load', 'cs', {interface: 'HmIP-RF', duty_cycle: 60, carrier_sense: 12, transfer: true}), words)).not.toContain('firmware transfer');
    });

    it('names the missing module that holds the HmIP network, with the way to the Interfaces page (openccu-lite task 318)', () => {
        expect(warningText(w('hmip-module-missing', '3014F711A0001F0000000A03', {module: '3014F711A0001F0000000A03'}), words)).toBe('HmIP-RF is down: the radio module 3014F711A0001F0000000A03, which holds the HmIP network, is missing or did not answer. hmipserver cannot start without it, so the virtual devices are down as well, and no other module is taken without asking. Plug it back in, or move HmIP-RF to another module on the Interfaces page.');
        expect(warningLink(w('hmip-module-missing', 'x', {}, '/system/interfaces#connections'), t)).toEqual({href: '/system/interfaces#connections', label: 'Interfaces'});
    });

    it('says HMIPServer.jar has no module, with None as the way to the virtual devices (occulited task 147)', () => {
        expect(warningText(w('hmip-adapter', 'no-module', {}), words)).toBe('HmIP-RF and the virtual devices are down: no HmIP radio module was found, and hmipserver (HMIPServer.jar) cannot start without one. Connect the module, or set HmIP-RF to None on the Interfaces page: hmipserver then runs HMServer.jar with the virtual devices only.');
        expect(warningText(w('hmip-adapter', 'no-module', {adapter: '0000000FFF'}), words)).toContain('the radio module 0000000FFF chosen for HmIP-RF is missing');
    });

    it('names a backup target and its cause (task 86)', () => {
        expect(warningText(w('backup-delivery', 'tabc:auth-failed', {name: 'NAS', cause: 'auth-failed'}), words)).toBe('The backup target NAS refuses the login.');
        expect(warningText(w('backup-delivery', 'directory:too-old', {name: 'USB', cause: 'too-old'}), words)).toBe('No successful backup to USB for more than a day.');
        expect(warningText(w('backup-delivery', 'tabc:last-failed', {name: 'NAS', cause: 'last-failed'}), words)).toBe('The last backup to NAS failed.');
        expect(warningLink(w('backup-delivery', 'tabc:full', {name: 'NAS', cause: 'full'}, '/backup#targets'), t)).toEqual({href: '/backup#targets', label: 'Backup'});
    });

    it('names the age of the newest backup and what it was, or since when there is none (occulited task 140)', () => {
        const S = 'Download a backup, or set up a target for the automatic backup.';
        const at = '2026-11-06T10:00:00Z';
        expect(warningText(w('backup-age', 'warning', {days: 4, since: at, way: 'download', by: 'admin'}), words)).toBe(`No backup for 4 days: the last one was downloaded in the browser by admin on at ${at}. ${S}`);
        expect(warningText(w('backup-age', 'warning', {days: 4, since: at, way: 'download'}), words)).toBe(`No backup for 4 days: the last one was downloaded in the browser on at ${at}. ${S}`);
        expect(warningText(w('backup-age', 'error', {days: 9, since: at, way: 'automatic', targets: 'USB, NAS'}), words)).toBe(`No backup for 9 days: the last one was the automatic backup to USB, NAS on at ${at}. ${S}`);
        expect(warningText(w('backup-age', 'error', {days: 8, since: at, way: 'run', targets: 'USB'}), words)).toBe(`No backup for 8 days: the last one was made with Back up now to USB on at ${at}. ${S}`);
        expect(warningText(w('backup-age', 'warning', {days: 5, since: at, way: 'client', by: 'occulite-backup on nas'}), words)).toBe(`No backup for 5 days: the last one was fetched by occulite-backup on nas on at ${at}. ${S}`);
        expect(warningText(w('backup-age', 'warning', {days: 5, since: at, way: 'client'}), words)).toBe(`No backup for 5 days: the last one was fetched by a program on at ${at}. ${S}`);
        expect(warningText(w('backup-age', 'warning', {days: 6, since: at, way: 'earlier'}), words)).toBe(`No backup for 6 days: the last one was made on at ${at}. ${S}`);
        expect(warningText(w('backup-age', 'warning', {days: 6, since: at, way: 'newer-way'}), words)).toBe(`No backup for 6 days: the last one was made on at ${at}. ${S}`);
        expect(warningText(w('backup-age', 'error', {days: 8, since: at}), words)).toBe(`No backup has been made or downloaded since at ${at}, 8 days ago. ${S}`);
        expect(warningLink(w('backup-age', 'error', {days: 8, since: at}, '/backup'), t)).toEqual({href: '/backup', label: 'Backup'});
    });

    it('says how much the backups on the system itself take (B-99)', () => {
        const base = 'The nightly backup writes to /usr/local/sdcard/backup, which is on the system itself: it is lost with the system, and it fills the storage the system runs on.';
        const u = (params: Warning['params']) => warningText(w('backup-userfs', '/usr/local/sdcard/backup', {path: '/usr/local/sdcard/backup', ...params}), words);
        expect(u({})).toBe(base);
        expect(u({count: 0, bytes: 0})).toBe(base);
        expect(u({count: 1, bytes: 5_400_000_000})).toBe(`${base} The one backup there takes 5.4 GB.`);
        expect(u({count: 3, bytes: 16_200_000_000})).toBe(`${base} The 3 backups there take 16 GB.`);
        expect(warningText(w('backup-userfs', '/x', {path: '/x', count: 3, bytes: 16_200_000_000}), {...words, lang: 'de'})).toContain('16 GB');
    });

    it('says the certificate variants apart', () => {
        expect(warningText(w('certificate', 'renewal-failed', {days: 40}), words)).toContain('renewal failed');
        expect(warningText(w('certificate', 'expired', {days: -1}), words)).toBe('The certificate has expired.');
        expect(warningText(w('certificate', 'expiring', {days: 9}), words)).toBe('The certificate expires in 9 days.');
    });

    it('fills the time, the path and the root-owned files', () => {
        expect(warningText(w('unclean', '1', {at: '2026-09-12T03:00:00Z'}), words)).toContain('(at 2026-09-12T03:00:00Z)');
        expect(warningText(w('backup-target', '/media/usb0/backup', {path: '/media/usb0/backup'}), words)).toContain('/media/usb0/backup cannot be read');
        const own = warningText(w('addon-ownership', 'hm2mqtt', {addons: [{id: 'hm2mqtt', path: '/usr/local/addons/hm2mqtt/var/pid'}]}), words);
        expect(own).toContain('Files of hm2mqtt belong to root');
        expect(own).toContain('(/usr/local/addons/hm2mqtt/var/pid)');
        expect(warningText(w('storage', 'watch'), words)).toBe('storage watch');
        expect(warningText(w('security-key', 'default'), words)).toMatch(/^BidCos-RF uses the default security key\./);
        // a warning this page does not know yet still says which it is
        expect(warningText(w('future', 'x'), words)).toBe('future: x');
    });

    it('names a parent of the addons\' directories others can change, why it matters and the fix (occulited B-145)', () => {
        const one = {...w('addon-parents', '/usr/local', {parents: [{path: '/usr/local', uid: 0, gid: 0, mode: '0777', reason: 'mode'}]}, '/services'), severity: 'warning' as const};
        expect(warningText(one, words)).toBe('/usr/local can be changed by users other than root (owner 0, group 0, mode 0777). While it is so, addons cannot run as their own user; they run as root instead. To fix it, run as root on the system: chown root:root /usr/local; chmod go-w /usr/local. A restart of occulited or of the system also fixes a directory that belongs to root. Then switch the addons to their own user on the Services page.');
        expect(warningLink(one, t)).toEqual({href: '/services', label: 'Services'});
        const two = warningText(w('addon-parents', '/usr/local/addons,/usr/local/etc/config/addons/www', {parents: [
            {path: '/usr/local/addons', uid: 1000, gid: 1000, mode: '0755', reason: 'owner'},
            {path: '/usr/local/etc/config/addons/www', uid: 0, gid: 0, mode: '0777', reason: 'link'},
        ]}), words);
        expect(two).toContain('/usr/local/addons belongs to user 1000, not to root (group 1000, mode 0755).');
        expect(two).toContain('/usr/local/etc/config/addons/www is a link to a directory that users other than root can change (owner 0, group 0, mode 0777).');
        expect(two).toContain('chown root:root /usr/local/addons; chmod go-w /usr/local/addons; chown root:root /usr/local/etc/config/addons/www; chmod go-w /usr/local/etc/config/addons/www.');
        // German: every sentence is in the catalogue (read as text: the catalogue's module needs Svelte's runes)
        const de = (key: string, params?: Record<string, string | number>) => {
            const esc = key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
            const m = new RegExp(`'${esc}': \\{\\s*de: '((?:[^'\\\\]|\\\\.)*)'`).exec(I18N);
            expect(m, key).not.toBeNull();
            return t(m?.[1] ?? key, params);
        };
        const text = warningText(one, {...words, t: de});
        expect(text).toContain('/usr/local kann von anderen Benutzern als root geändert werden (Besitzer 0, Gruppe 0, Modus 0777).');
        expect(text).toContain('können Addons nicht als eigener Benutzer laufen');
        expect(text).toContain('führen Sie als root auf dem System aus: chown root:root /usr/local; chmod go-w /usr/local.');
        expect(warningText(w('addon-parents', 'x', {parents: [{path: '/usr/local', uid: 5, gid: 5, mode: '0755', reason: 'owner'}, {path: '/usr/local/etc', uid: 0, gid: 0, mode: '0777', reason: 'link'}]}), {...words, t: de})).toContain('/usr/local gehört dem Benutzer 5, nicht root');
    });

    it('names an adopted root addon that could run as its own user, and the install that gives it one (openccu-lite #23)', () => {
        const one = {...w('addon-adopted-root', 'loom', {addons: [{id: 'loom', name: 'OpenCCU-Loom', enabled: true, token: true}]}, '/addons'), severity: 'info' as const};
        expect(warningText(one, {...words, addonName: (a) => a.name ?? a.id})).toBe('OpenCCU-Loom runs as root, although its manifest says it can run as its own user: it came along from before openccu-lite, so the system has not stored its manifest. Installing the addon again from its package (a file on the Addons page, or the catalogue) stores its manifest, issues the API token it declares and gives it its own user.');
        expect(warningLink(one, t)).toEqual({href: '/addons', label: 'Addons'});
        // without declared scopes there is no token to promise; one sentence per addon
        const two = warningText(w('addon-adopted-root', 'a,b', {addons: [{id: 'a', token: true}, {id: 'b'}]}), words);
        expect(two).toContain('a runs as root');
        expect(two).toContain('b runs as root');
        expect(two).toContain('stores its manifest and gives it its own user.');
        expect(two).not.toContain('API token');
        const de = (key: string, params?: Record<string, string | number>) => {
            const esc = key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
            const m = new RegExp(`'${esc}': \\{\\s*de: '((?:[^'\\\\]|\\\\.)*)'`).exec(I18N);
            expect(m, key).not.toBeNull();
            return t(m?.[1] ?? key, params);
        };
        const text = warningText(one, {...words, t: de, addonName: (a) => a.name ?? a.id});
        expect(text).toContain('OpenCCU-Loom läuft als root');
        expect(text).toContain('stellt das API-Token aus');
        expect(warningText(w('addon-adopted-root', 'b', {addons: [{id: 'b'}]}), {...words, t: de})).toContain('speichert das System sein Manifest und gibt ihm einen eigenen Benutzer.');
    });

    it('labels the way to where it is handled', () => {
        expect(warningLink(w('security-key', 'default', {}, '/radio#security-key'), t)).toEqual({href: '/radio#security-key', label: 'Set security key'});
        expect(warningLink(w('storage', 'watch', {}, '#storage'), t)).toEqual({href: '#storage', label: 'Storage health'});
        expect(warningLink(w('meta', '1'), t)).toBeNull();
    });

    it('splits off the silenced ones and escapes the variant', () => {
        const silenced = {...w('backup-target', '/media/usb0/backup'), silenced: {id: 'backup-target', variant: '/media/usb0/backup', by: 'admin', at: '', until: ''}};
        const {shown, silenced: off} = splitSilenced([w('meta', '1'), silenced]);
        expect(shown.map((x) => x.id)).toEqual(['meta']);
        expect(off.map((x) => x.id)).toEqual(['backup-target']);
        expect(silencePath(silenced)).toBe('/api/system/v1/warnings/silence/backup-target/%2Fmedia%2Fusb0%2Fbackup');
        expect(silencePath({id: 'rega', variant: 'email,hm-print'})).toBe('/api/system/v1/warnings/silence/rega/email%2Chm-print');
    });

    it('counts and names the periods', () => {
        expect(silencedLine(1, t)).toBe('1 silenced warning');
        expect(silencedLine(2, t)).toBe('2 silenced warnings');
        expect(periodLabel(1, t)).toBe('Silence for 1 day');
        expect(periodLabel(90, t)).toBe('Silence for 90 days');
    });
});

describe('hmip-port-open (B-89)', () => {
    it('names the port and the addresses', () => {
        const text = warningText({id: 'hmip-port-open', variant: '39292', severity: 'warning', params: {port: 39292, addresses: '0.0.0.0, ::'}}, words);
        expect(text).toBe("hmipserver's port 39292 listens on 0.0.0.0, ::, not only on this system: the image's loopback shim did not take. The firewall still closes it.");
    });
});

describe('crash loops (openccu-lite task 283)', () => {
    it('names the units, their restarts and since when', () => {
        const text = warningText(w('crash-loop', 'addon-mosquitto,rfd', {units: [{unit: 'addon-mosquitto', restarts: 3, total: 3, since: '2026-09-27T20:00:00Z', addon: true}, {unit: 'rfd', restarts: 4, total: 9, since: '2026-09-27T20:01:00Z', result: 'exit-code'}]}, '/system/services'), words);
        expect(text).toBe('Keeps failing and restarting: addon-mosquitto (3 restarts since at 2026-09-27T20:00:00Z), rfd (4 restarts since at 2026-09-27T20:01:00Z). The system tries again at longer and longer intervals; the log says why.');
        expect(warningText(w('crash-loop', 'rfd'), words)).toContain('restarting: rfd.');
        expect(warningLink(w('crash-loop', 'rfd', {}, '/system/services'), t)).toEqual({href: '/system/services', label: 'Services'});
    });

    it("says occulited's own loop after it came back", () => {
        const text = warningText(w('occulited-crash-loop', '1790000000', {fails: 3, restarts: 5, first: '2026-09-27T20:00:00Z', last: '2026-09-27T20:05:00Z'}, '/system/log?unit=occulited'), words);
        expect(text).toBe('The system service occulited kept failing: 3 failures in a row between at 2026-09-27T20:00:00Z and at 2026-09-27T20:05:00Z. It runs again; the log of that time says why.');
        expect(warningLink(w('occulited-crash-loop', 'x', {}, '/system/log?unit=occulited'), t)).toEqual({href: '/system/log?unit=occulited', label: 'Log'});
    });
});
