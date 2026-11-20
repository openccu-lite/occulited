<script lang="ts" module>
    // openccu-lite task 86: the Backup page's targets - the USB directory, NFS and SMB shares
    // (mounted on demand by the system) and SSH servers (an SFTP upload). One card per target
    // with its state, the last backup, the test, Back up now, the mount, the backups there and
    // Restore this; the SSH key and the server's key; the nightly switch on top.
    // occulited task 150: targets an addon provides - its provider, the destinations it offers, and
    // the provider's own sentence and link for a state
    export type ProviderText = Partial<Record<'de' | 'en', string>>;
    export interface ProviderAction { label: ProviderText; href: string }
    export interface Destination { id: string; name: string; detail?: string; state: string; message?: ProviderText; action?: ProviderAction }
    export interface Provider { addon: string; name: string; label: ProviderText; setup: string; available: boolean; destinations: Destination[] }
    export interface TargetState { state: string; detail?: string; checked_at?: string; failures: number; next_retry_at?: string; mounted: boolean; source?: string; options?: string; free_bytes?: number; total_bytes?: number; unsupported?: string; message?: ProviderText; action?: ProviderAction; progress?: {name: string; size: number; sent: number}; gone?: boolean }
    export interface LastBackup { at: string; ok: boolean; state: string; step?: string; error?: string; name?: string; size?: number; duration_ms?: number; encrypted: boolean; last_ok?: string; removed?: string[]; from?: string; caught_up?: string[]; message?: ProviderText; action?: ProviderAction }
    export interface Target {
        id: string; name: string; kind: 'directory' | 'share' | 'nfs' | 'cifs' | 'sftp' | 'addon'; enabled: boolean; max_backups: number; subdir: string; encrypt: boolean; append_only: boolean; created?: string;
        directory?: {path: string; location?: string};
        share?: {id: string; folder: string};
        nfs?: {server: string; export: string; version: string};
        cifs?: {server: string; share: string; version: string; seal: boolean; user: string; domain?: string; has_password: boolean};
        sftp?: {host: string; port: number; user: string; path: string; public_key?: string; host_key?: {type: string; fingerprint: string}};
        addon?: {id: string; destination: string; folder: string; source?: string};
        provider?: {addon: string; name?: string; label?: ProviderText; installed: boolean; destination?: Destination; upload_from?: string};
        state: TargetState; last_backup?: LastBackup;
    }
    export interface TargetsView { nightly: {enabled: boolean; time: string}; container: string; kinds: Record<string, string>; encryption: boolean; hostname: string; needed_bytes: number; targets: Target[]; providers?: Provider[] }
    export interface TargetBackup { name: string; size: number; time: string; encrypted: boolean; recovery_fingerprint?: string; known?: string }
</script>

<script lang="ts">
    import {dateTime} from './datefmt';
    import {link} from './router.svelte';
    import {onMount} from 'svelte';
    import {api, ApiError} from './api';
    import {ask} from './dialog.svelte';
    import {i18n, t} from './i18n.svelte';
    import Help from './Help.svelte';
    import CopyButton from './CopyButton.svelte';
    import {warnEdge, warnFor} from './systemmenu.svelte';
    // the Status page's warnings that lead to this section (occulited task 12)
    const TARGET_WARNINGS = ['backup-delivery', 'backup-target', 'backup-userfs'];
    import Disclosure from './Disclosure.svelte';
    import LocationPicker from './LocationPicker.svelte';
    import {folderError, formatLocation, parseLocation} from './locations';

    let {onrestore}: {onrestore?: (target: string, name: string) => void} = $props();

    let view = $state<TargetsView | null>(null);
    let busy = $state('');
    let msg = $state<Record<string, string>>({});
    let err = $state('');
    let open = $state<Record<string, TargetBackup[] | null>>({});
    // task 268: a target's Backups here becomes the panel of its backups, and its Edit is gone
    // while its form is open (the panel's Close / the form's Cancel bring them back)
    let backupsButtons = $state<Record<string, HTMLButtonElement>>({});
    let hostkeys = $state<Record<string, {type: string; fingerprint: string; trusted: boolean; changed: boolean; trusted_fingerprint?: string}>>({});
    let tests = $state<Record<string, {ok: boolean; state: string; step: string; error?: string; free_bytes: number; needed_bytes: number; write_mbps?: number; message?: ProviderText; action?: ProviderAction}>>({});

    // the directory target's own checks (upstream's markers): a path that is not there, or one on
    // the system itself
    interface Schedule { path: string; path_exists: boolean; on_userfs?: boolean; real_path?: string; max_backups: number }
    let sched = $state<Schedule | null>(null);
    // occulited task 150: an account an addon's page hands over (Use for backups), until the form is open
    let handover = $state<{addon: string; destination: string} | null>(null);
    async function load() {
        try {
            // a hand-over from an addon's page names an account made a moment ago: ask its provider now
            view = await api.get<TargetsView>('/api/system/v1/backup/targets' + (handover ? '?providers=fresh' : ''));
            err = '';
        } catch (e) {
            err = (e as Error).message;
            return;
        }
        if (view.targets.some((x) => x.kind === 'directory')) {
            try { sched = await api.get<Schedule>('/api/system/v1/backup/schedule'); } catch { sched = null; }
        } else {
            sched = null;
        }
    }
    let timer: ReturnType<typeof setTimeout> | undefined;
    // a run in progress: look again until it is done
    $effect(() => {
        const running = view?.targets.some((x) => x.state.state === 'running');
        clearTimeout(timer);
        if (running) timer = setTimeout(() => void load(), 3000);
        return () => clearTimeout(timer);
    });
    // occulited task 150: the addon's page hands an account over (Use for backups): the form
    // opens with it once the providers are known, and the parameters leave the address
    $effect(() => {
        if (!handover || !view) return;
        const h = handover;
        handover = null;
        if (providerOf(h.addon)) addAddon(h.addon, h.destination);
    });
    onMount(() => {
        const q = new URLSearchParams(location.search);
        const addon = q.get('add-target');
        if (addon) {
            handover = {addon, destination: q.get('destination') ?? ''};
            q.delete('add-target');
            q.delete('destination');
            const rest = q.toString();
            history.replaceState(history.state, '', location.pathname + (rest ? '?' + rest : '') + location.hash);
        }
        void load();
    });

    const KIND: Record<Target['kind'], () => string> = {directory: () => t('USB / directory'), share: () => t('Network share'), nfs: () => t('NFS share'), cifs: () => t('SMB share'), sftp: () => t('SSH server (SFTP)'), addon: () => t('Addon')};
    // occulited task 150: a provider's texts come in its own languages; the kind's word is its label
    const text = (x?: ProviderText) => x?.[i18n.language as 'de' | 'en'] ?? x?.en ?? x?.de ?? '';
    const providers = $derived(view?.providers ?? []);
    const providerOf = (addon?: string) => providers.find((p) => p.addon === addon);
    const kindText = (x: {kind: Target['kind']; provider?: Target['provider']; addon?: {id: string}}) =>
        x.kind === 'addon' ? text(x.provider?.label) || text(providerOf(x.addon?.id)?.label) || KIND.addon() : KIND[x.kind]();
    // the states an addon's target can show: its badge reserves the widest of them, so the card
    // keeps its shape from state to state
    const ADDON_STATES = ['idle', 'writable', 'read-only', 'full', 'unreachable', 'auth-failed', 'addon-missing', 'not-encrypted', 'error'];
    const percent = (x: Target) => (x.state.state === 'running' && x.state.progress && x.state.progress.size > 0 ? Math.min(100, Math.floor((100 * x.state.progress.sent) / x.state.progress.size)) : -1);
    const badgeText = (x: Target) => (percent(x) >= 0 ? `${stateText('running')} ${percent(x)} %` : stateText(x.state.state));
    const STATE: Record<string, () => string> = {
        'not-configured': () => t('not set up'),
        unsupported: () => t('not available here'),
        idle: () => t('ready'),
        connecting: () => t('connecting'),
        writable: () => t('writable'),
        'read-only': () => t('not writable'),
        full: () => t('full'),
        unreachable: () => t('unreachable'),
        'auth-failed': () => t('login refused'),
        'host-key-unknown': () => t("server's key not confirmed"),
        'host-key-changed': () => t("server's key changed"),
        'no-sftp': () => t('no SFTP on the server'),
        stale: () => t('not answering'),
        'no-medium': () => t('no USB stick'),
        'update-room': () => t('skipped: room for an update'),
        'addon-missing': () => t('addon missing'),
        'not-encrypted': () => t('encryption off'),
        running: () => t('backing up…'),
        error: () => t('error'),
    };
    const stateText = (s: string) => (STATE[s] ?? (() => s))();
    const stateClass = (s: string) => (s === 'writable' || s === 'idle' ? 'good' : s === 'running' || s === 'connecting' || s === 'unsupported' || s === 'host-key-unknown' ? '' : 'bad');
    const failed = (s: string) => stateClass(s) === 'bad';
    const size = (n?: number) => (n === undefined ? '' : n >= 1e9 ? `${(n / 1e9).toFixed(1)} GB` : `${Math.max(1, Math.round(n / 1e6))} MB`);
    const when = (iso?: string) => (iso ? dateTime(iso) : '');
    const where = (x: Target) =>
        x.kind === 'directory' ? (x.directory?.location ? locationText(x.directory.location, x.directory.path) : x.directory?.path ?? '') :
        x.kind === 'share' ? `${t('share {name}', {name: x.share?.id ?? ''})}${x.share?.folder ? ' · ' + x.share.folder : ''}` :
        x.kind === 'nfs' ? `${x.nfs?.server}:${x.nfs?.export}${x.subdir ? ' · ' + x.subdir : ''}` :
        x.kind === 'cifs' ? `//${x.cifs?.server}/${x.cifs?.share}${x.subdir ? ' · ' + x.subdir : ''}` :
        x.kind === 'addon' ? `${x.provider?.destination?.detail || x.provider?.destination?.name || t('account {id}', {id: x.addon?.destination ?? ''})} · ${x.addon?.folder ?? ''}` :
        `${x.sftp?.user}@${x.sftp?.host}${x.sftp?.port && x.sftp.port !== 22 ? ':' + x.sftp.port : ''}:${x.sftp?.path || '~'}${x.subdir ? '/' + x.subdir : ''}`;
    const unsupportedText = (why?: string) => (why === 'container' ? t('This system is a container. Mount the share on the host and pass it into the container (Docker: a volume; Proxmox: a mount point), then choose it here as a directory target.') : t('The program for this kind of share is not part of this system image yet.'));

    // a directory chosen as a location: the stick by its label or the system storage, and where it is now
    function locationText(loc: string, path: string): string {
        const p = parseLocation(loc);
        if (!p) return path;
        if (p.kind === 'userfs') return `${t('System storage')} · ${p.folder}`;
        const now = path.startsWith('/media/.usb-not-plugged-in') ? t('not plugged in') : path;
        return `${t('USB stick {name}', {name: p.name})} · ${p.folder} · ${now}`;
    }

    function say(id: string, text: string) {
        msg = {...msg, [id]: text};
    }

    async function nightly(on: boolean) {
        busy = 'nightly';
        try {
            await api.put('/api/system/v1/backup/nightly', {enabled: on});
            await load();
        } catch (e) {
            err = (e as Error).message;
        } finally {
            busy = '';
        }
    }

    async function runAll() {
        busy = 'run';
        err = '';
        try {
            await api.post('/api/system/v1/backup/run', {});
            await load();
        } catch (e) {
            err = (e as Error).message;
        } finally {
            busy = '';
        }
    }

    async function act(x: Target, what: 'test' | 'run' | 'mount' | 'unmount') {
        busy = x.id + what;
        say(x.id, '');
        try {
            if (what === 'test') {
                const r = await api.post<(typeof tests)[string]>(`/api/system/v1/backup/targets/${x.id}/test`, {});
                tests = {...tests, [x.id]: r};
            } else {
                await api.post(`/api/system/v1/backup/targets/${x.id}/${what}`, {});
                if (what === 'run') say(x.id, t('The backup is running; this card shows when it is done.'));
            }
            await load();
        } catch (e) {
            say(x.id, (e as Error).message);
        } finally {
            busy = '';
        }
    }

    async function remove(x: Target) {
        const ok = await ask({title: t('Remove target'), message: t('Remove the target "{name}"? The backups already there stay where they are; the system only stops writing to it.', {name: x.name}), confirm: t('Remove'), danger: true});
        if (!ok) return;
        busy = x.id + 'remove';
        try {
            await api.del(`/api/system/v1/backup/targets/${x.id}`);
            await load();
        } catch (e) {
            say(x.id, (e as Error).message);
        } finally {
            busy = '';
        }
    }

    async function toggleBackups(x: Target) {
        if (open[x.id] !== undefined) {
            const {[x.id]: _, ...rest} = open;
            open = rest;
            return;
        }
        open = {...open, [x.id]: null};
        try {
            const r = await api.get<{backups: TargetBackup[]}>(`/api/system/v1/backup/targets/${x.id}/backups`);
            open = {...open, [x.id]: r.backups};
        } catch (e) {
            const {[x.id]: _, ...rest} = open;
            open = rest;
            say(x.id, (e as Error).message);
        }
    }

    async function restore(x: Target, b: TargetBackup) {
        if (!(await ask({title: t('Restore this'), message: t('Check {name} from "{target}" for a restore? It is copied onto this system first; nothing is applied before you confirm below.', {name: b.name, target: x.name}), confirm: t('Check')}))) return;
        onrestore?.(x.id, b.name);
    }

    // the SSH key and the server's key
    async function newKey(x: Target) {
        if (x.sftp?.public_key && !(await ask({title: t('New key'), message: t('Make a new key for this target? The server then has to get the new line in authorized_keys; the old key stops working here.'), confirm: t('New key'), danger: true}))) return;
        busy = x.id + 'key';
        try {
            await api.post(`/api/system/v1/backup/targets/${x.id}/keypair`, {});
            await load();
        } catch (e) {
            say(x.id, (e as Error).message);
        } finally {
            busy = '';
        }
    }
    const keyLine = (x: Target) => (x.sftp?.public_key ? `restrict ${x.sftp.public_key}` : '');
    // B-135: the key lines' elements, selected where the browser cannot copy
    let keyEls = $state<Record<string, HTMLElement | undefined>>({});
    async function scan(x: Target) {
        busy = x.id + 'scan';
        say(x.id, '');
        try {
            const r = await api.get<(typeof hostkeys)[string]>(`/api/system/v1/backup/targets/${x.id}/hostkey`);
            hostkeys = {...hostkeys, [x.id]: r};
        } catch (e) {
            say(x.id, (e as Error).message);
        } finally {
            busy = '';
        }
    }
    async function trust(x: Target) {
        const hk = hostkeys[x.id];
        if (!hk) return;
        if (hk.changed && !(await ask({title: t("The server's key changed"), message: t('The server shows a different key than the one confirmed before ({old}). Trust the new one only when you know why it changed - a reinstalled server, say. Otherwise someone may be in between.', {old: hk.trusted_fingerprint ?? ''}), confirm: t('Trust the new key'), danger: true}))) return;
        busy = x.id + 'trust';
        try {
            await api.put(`/api/system/v1/backup/targets/${x.id}/hostkey`, {fingerprint: hk.fingerprint});
            const {[x.id]: _, ...rest} = hostkeys;
            hostkeys = rest;
            await load();
        } catch (e) {
            say(x.id, (e as Error).message);
        } finally {
            busy = '';
        }
    }

    // the form: adding and editing
    interface Draft { id: string; kind: Target['kind']; name: string; enabled: boolean; max_backups: number; subdir: string; encrypt: boolean; append_only: boolean; path: string; server: string; exportPath: string; share: string; version: string; seal: boolean; user: string; domain: string; password: string; host: string; port: number; sshpath: string; has_password: boolean; loc: string; folder: string; addon: string; destination: string; source: string }
    let draft = $state<Draft | null>(null);
    let formErr = $state('');
    function blank(kind: Target['kind']): Draft {
        const name = {directory: 'USB', share: 'NAS', nfs: 'NAS', cifs: 'NAS', sftp: 'SSH', addon: 'Cloud'}[kind];
        return {id: '', kind, name, enabled: true, max_backups: 30, subdir: kind === 'directory' || kind === 'share' ? '' : view?.hostname ?? '', encrypt: true, append_only: false, path: '', server: '', exportPath: '', share: '', version: '', seal: false, user: '', domain: '', password: '', host: '', port: 22, sshpath: '', has_password: false,
            loc: '', folder: kind === 'share' ? view?.hostname ?? '' : kind === 'addon' ? `openccu-lite/${view?.hostname ?? 'system'}` : 'backup', addon: '', destination: '', source: ''};
    }
    // a target on an addon's destination: the provider's first account preselected, or the one the
    // addon's page handed over (Use for backups)
    function addAddon(addon: string, destination = '') {
        formErr = '';
        const p = providerOf(addon);
        const d = blank('addon');
        d.addon = addon;
        d.destination = destination || p?.destinations[0]?.id || '';
        d.name = p?.destinations.find((x) => x.id === d.destination)?.name || text(p?.label) || d.name;
        draft = d;
    }
    // the addon's page, and the way back to this section
    const setupHref = (p?: Provider) => (p ? `${p.setup.replace(/#.*$/, '')}${p.setup.includes('?') ? '&' : '?'}return=${encodeURIComponent('/backup#targets')}${/#/.test(p.setup) ? p.setup.replace(/^[^#]*/, '') : ''}` : '');
    const localTargets = $derived((view?.targets ?? []).filter((x) => (x.kind === 'directory' || x.kind === 'share' || x.kind === 'nfs' || x.kind === 'cifs') && x.encrypt));
    // what "automatic" means now: the first enabled local target that keeps encrypted backups
    const autoSource = $derived(localTargets.find((x) => x.enabled));
    function edit(x: Target) {
        formErr = '';
        const d: Draft = {...blank(x.kind), id: x.id, name: x.name, enabled: x.enabled, max_backups: x.max_backups, subdir: x.subdir, encrypt: x.encrypt, append_only: x.append_only,
            path: x.directory?.path ?? '', server: x.nfs?.server ?? x.cifs?.server ?? '', exportPath: x.nfs?.export ?? '', share: x.cifs?.share ?? '', version: x.nfs?.version ?? x.cifs?.version ?? '',
            seal: x.cifs?.seal ?? false, user: x.cifs?.user ?? x.sftp?.user ?? '', domain: x.cifs?.domain ?? '', host: x.sftp?.host ?? '', port: x.sftp?.port ?? 22, sshpath: x.sftp?.path ?? '', has_password: x.cifs?.has_password ?? false,
            loc: '', folder: 'backup', addon: x.addon?.id ?? '', destination: x.addon?.destination ?? '', source: x.addon?.source ?? ''};
        if (x.kind === 'addon') d.folder = x.addon?.folder ?? '';
        const p = x.directory?.location ? parseLocation(x.directory.location) : null;
        if (p) {
            d.loc = p.id;
            d.folder = p.folder;
        }
        if (x.kind === 'share' && x.share) {
            d.loc = `share:${x.share.id}`;
            d.folder = x.share.folder;
        }
        draft = d;
    }
    function add(kind: Target['kind']) {
        formErr = '';
        draft = blank(kind);
    }
    // a location picked with a usable folder (the directory target may keep an older path)
    const draftReady = $derived(
        !draft ? false :
        draft.kind === 'share' ? !!draft.loc && !folderError(draft.folder, true) :
        draft.kind === 'directory' ? (draft.loc ? !folderError(draft.folder, false, draft.loc === 'userfs' ? 'backup' : undefined) : !!draft.path) :
        draft.kind === 'addon' ? !!view?.encryption && !!draft.destination && /^[A-Za-z0-9_][A-Za-z0-9._ -]*(\/[A-Za-z0-9_][A-Za-z0-9._ -]*){0,5}$/.test(draft.folder.trim()) :
        true,
    );
    const hasDirectory = $derived(!!view?.targets.some((x) => x.kind === 'directory'));
    async function encryptChange(e: Event) {
        const box = e.currentTarget as HTMLInputElement;
        if (!draft || box.checked) {
            if (draft) draft.encrypt = true;
            return;
        }
        const nfs = draft.kind === 'nfs' ? ' ' + t('An NFS share checks no password at all: any computer with an allowed address reads every file on it.') : '';
        const ok = await ask({title: t('Back up without encryption?'), message: t('Unencrypted backups carry every key of this system in plain - radio keys, password hashes, certificates.') + nfs, confirm: t('Without encryption'), danger: true});
        draft.encrypt = !ok;
        box.checked = !ok;
    }
    function body(d: Draft): Record<string, unknown> {
        const b: Record<string, unknown> = {kind: d.kind, name: d.name.trim(), enabled: d.enabled, max_backups: Number(d.max_backups), subdir: d.subdir.trim(), encrypt: d.encrypt, append_only: d.append_only};
        // task 228: chosen as a location (the stick by its label, the system storage); an older
        // setting's path is kept only while no location is picked
        if (d.kind === 'directory') b.directory = d.loc ? {path: '', location: formatLocation(d.loc, d.folder)} : {path: d.path.trim()};
        if (d.kind === 'share') b.share = {id: d.loc.replace(/^share:/, ''), folder: d.folder.replace(/^\/+|\/+$/g, '')};
        if (d.kind === 'nfs') b.nfs = {server: d.server.trim(), export: d.exportPath.trim(), version: d.version};
        if (d.kind === 'cifs') b.cifs = {server: d.server.trim(), share: d.share.trim().replace(/^\/+|\/+$/g, ''), version: d.version, seal: d.seal, user: d.user.trim(), domain: d.domain.trim(), ...(d.password ? {password: d.password} : {})};
        if (d.kind === 'addon') {
            b.addon = {id: d.addon, destination: d.destination, folder: d.folder.trim().replace(/^\/+|\/+$/g, ''), source: d.source};
            b.encrypt = true;
            b.subdir = '';
        }
        if (d.kind === 'sftp') b.sftp = {host: d.host.trim(), port: Number(d.port) || 22, user: d.user.trim(), path: d.sshpath.trim()};
        return b;
    }
    async function save() {
        if (!draft) return;
        busy = 'save';
        formErr = '';
        try {
            const r = draft.id
                ? await api.put<{target: Target; apply_error?: string}>(`/api/system/v1/backup/targets/${draft.id}`, body(draft))
                : await api.post<{target: Target; apply_error?: string}>('/api/system/v1/backup/targets', body(draft));
            if (r.apply_error) say(r.target.id, t('Saved, but the mount could not be prepared: {error}', {error: r.apply_error}));
            draft = null;
            await load();
        } catch (e) {
            formErr = e instanceof ApiError ? e.message : (e as Error).message;
        } finally {
            busy = '';
        }
    }
</script>

<h2 id="targets">{t('Backup targets')}<Help>{t('Where the nightly backup goes: a USB stick, an NFS or SMB share on a NAS, an SSH server. The backup is made once and copied to every enabled target, checked after writing, and only this system\'s older files are removed beyond the number to keep. A share is mounted only while it is used.')}</Help></h2>
<!-- occulited task 59: the section's panel, with the edge of the warnings that lead here -->
<div class="ol-panel ol-targets-panel {warnEdge(TARGET_WARNINGS)}" {...warnFor(TARGET_WARNINGS)}>
{#if err}<div class="ol-notice err">{err}</div>{/if}
{#if view}
    <div class="ol-toolbar ol-targets-bar" data-section="targets">
        <label><input type="checkbox" checked={view.nightly.enabled} disabled={busy !== ''} onchange={(e) => void nightly((e.currentTarget as HTMLInputElement).checked)} data-action="nightly" /> {t('Every night at {time} to every enabled target', {time: view.nightly.time})}</label>
        <button class="hmm-button" onclick={runAll} disabled={busy !== '' || !view.targets.some((x) => x.enabled)} data-action="run-all">{t('Back up now')}</button>
    </div>
    {#if !view.targets.length}
        <p class="ol-muted" data-notice="no-targets">{t('No target yet: the backups exist only when you download one.')}</p>
    {/if}
    <div class="ol-targets">
        {#each view.targets as x (x.id)}
            <div class="ol-card" class:err={x.enabled && failed(x.state.state)} data-target={x.id} data-kind={x.kind}>
                <div class="ol-card-head">
                    <div class="ol-card-titles">
                        <div class="ol-card-title">{x.name} <span class="ol-muted">· {kindText(x)}</span>{#if !x.enabled}{' '}<span class="ol-badge">{t('off')}</span>{/if}</div>
                        <div class="ol-card-sub">{where(x)}</div>
                    </div>
                    {#if x.kind === 'addon'}
                        <!-- the badge keeps its box: every state's label lies under the shown one, unseen -->
                        <span class="ol-badge ol-badge-sized {stateClass(x.state.state)}" data-state={x.state.state} data-progress={percent(x) >= 0 ? percent(x) : undefined}><span class="ol-badge-now">{badgeText(x)}</span>{#each ADDON_STATES as st (st)}<span class="ol-badge-size" aria-hidden="true">{stateText(st)}</span>{/each}<span class="ol-badge-size" aria-hidden="true">{stateText('running')} 100 %</span></span>
                    {:else}
                        <span class="ol-badge {stateClass(x.state.state)}" data-state={x.state.state}>{stateText(x.state.state)}</span>
                    {/if}
                </div>
                <div class="ol-card-detail">
                    {#if x.kind === 'directory' && sched && !sched.path_exists}
                        <div class="ol-warn" data-notice="directory-missing">{t('{path} cannot be read: plug in the USB stick, or change the directory.', {path: sched.path})}</div>
                    {:else if x.kind === 'directory' && sched?.on_userfs}
                        <div class="ol-warn" data-notice="directory-userfs">{t('{path} is on the system itself ({real}), so a backup there is lost with the system - and {n} of them fill the storage the system runs on. Use a USB stick or a share.', {path: sched.path, real: sched.real_path ?? sched.path, n: sched.max_backups || 30})}</div>
                    {/if}
                    {#if x.state.state === 'unsupported'}
                        <div data-notice="unsupported">{unsupportedText(x.state.unsupported)}</div>
                    {:else}
                        {#if x.kind === 'addon' && x.state.state === 'addon-missing'}
                            <div class="ol-warn" data-detail>{t('Needs the addon {addon}, which is not installed or not running.', {addon: x.provider?.name || x.addon?.id || ''})}</div>
                        {:else if x.kind === 'addon' && x.state.state === 'not-encrypted'}
                            <div class="ol-warn" data-detail>{t('Encryption is switched off: nothing is sent to this target.')} <a href="#encryption">{t('Set up encryption')}</a></div>
                        {:else if x.kind === 'addon' && x.state.gone}
                            <div class="ol-warn" data-detail>{t('The account no longer exists. Edit the target and choose another one.')}</div>
                        {:else if x.kind === 'addon' && failed(x.state.state) && text(x.state.message)}
                            <div class="ol-warn" data-detail>{text(x.state.message)}</div>
                        {:else if x.state.detail && failed(x.state.state)}<div class="ol-warn" data-detail>{x.state.detail}</div>{/if}
                        {#if x.last_backup}
                            <div data-last>
                                {#if x.last_backup.ok}
                                    {t('Last backup {when} · {size} · {secs} s', {when: when(x.last_backup.at), size: size(x.last_backup.size), secs: Math.round((x.last_backup.duration_ms ?? 0) / 1000)})}{#if x.last_backup.encrypted}{' · '}{t('encrypted')}{/if}
                                {:else}
                                    {t('Last attempt {when} failed', {when: when(x.last_backup.at)})}{#if x.last_backup.last_ok}{' · '}{t('last success {when}', {when: when(x.last_backup.last_ok)})}{/if}
                                {/if}
                            </div>
                        {:else}
                            <div>{t('No backup here yet.')}</div>
                        {/if}
                        {#if x.state.free_bytes}<div>{t('{free} free of {total}', {free: size(x.state.free_bytes), total: size(x.state.total_bytes)})}</div>{/if}
                        {#if x.kind === 'addon'}
                            <div data-upload-from>{#if x.last_backup?.ok && x.last_backup.from}{t('Uploaded from "{name}".', {name: x.last_backup.from})}{:else if x.provider?.upload_from}{t('Uploads from "{name}", which already holds each backup.', {name: x.provider.upload_from})}{:else}{t("Uploads from the system's working copy (kept only during the backup).")}{/if}{#if x.last_backup?.ok && x.last_backup.caught_up?.length}{' '}{t('{n} older backups caught up.', {n: x.last_backup.caught_up.length})}{/if}</div>
                            {#if x.enabled && view.targets.filter((y) => y.enabled).length === 1}<div class="ol-muted" data-notice="only-target">{t('This is the only target: without an internet connection no backup is made. A USB stick or a share as a second target covers that.')}</div>{/if}
                        {/if}
                        {#if x.state.mounted}<div data-mounted>{t('Mounted: {source}', {source: x.state.source ?? ''})}</div>{/if}
                        {#if x.state.next_retry_at && failed(x.state.state)}<div>{t('Next check {when}', {when: when(x.state.next_retry_at)})}</div>{/if}
                        {#if x.append_only}<div>{t('Append-only: this system never deletes here; prune old backups on the server.')}</div>{/if}
                        {#if !x.encrypt}<div class="ol-warn" data-notice="target-plain">{t('Backups to this target are not encrypted.')}</div>{/if}
                    {/if}
                </div>
                {#if tests[x.id]}
                    {@const r = tests[x.id]!}
                    <div class="ol-notice" class:err={!r.ok || r.state === 'full'} data-test-result={r.state}>
                        {#if r.ok && r.state !== 'full'}
                            {x.kind === 'addon' ? t('Writable: a test file written, read back and deleted.') : t('Writable: 1 MB written, read back and deleted.')}{#if r.write_mbps}{' ' + t('About {mbps} MB/s.', {mbps: r.write_mbps.toFixed(1)})}{/if}
                        {:else if r.state === 'full'}
                            {t('Writable, but {free} free is less than the next backup needs ({needed}).', {free: size(r.free_bytes), needed: size(r.needed_bytes)})}
                        {:else}
                            {stateText(r.state)}{#if text(r.message)}: {text(r.message)}{:else}{' '}({r.step}){#if r.error}: {r.error}{/if}{/if}
                            {#if r.state === 'read-only' && (x.kind === 'nfs' || x.kind === 'share')}{' '}{t('An export that maps root to nobody (root_squash) needs a directory that user may write, or "map all users" to one account.')}{/if}
                        {/if}
                    </div>
                {/if}
                {#if x.kind === 'sftp' && x.state.state !== 'unsupported'}
                    <div class="ol-sshkey" data-ssh>
                        {#if x.sftp?.public_key}
                            <div class="ol-muted">{t("This system's key, for the server's authorized_keys (restrict allows file transfer only):")}</div>
                            <code class="hmm-mono ol-keyline" data-public-key bind:this={keyEls[x.id]}>{keyLine(x)}</code>
                            <div class="ol-actions"><CopyButton text={() => keyLine(x)} shown={() => keyEls[x.id]} place="row-end" action="copy-key" /><button class="hmm-button" onclick={() => void newKey(x)} disabled={busy !== ''} data-action="new-key">{t('New key')}</button></div>
                        {:else}
                            <button class="hmm-button" onclick={() => void newKey(x)} disabled={busy !== ''} data-action="new-key">{t('Make a key')}</button>
                        {/if}
                        <div class="ol-muted" data-host-key>{#if x.sftp?.host_key}{t("Server's key: {type} {fp}", {type: x.sftp.host_key.type, fp: x.sftp.host_key.fingerprint})}{:else}{t("The server's key is not confirmed yet.")}{/if}</div>
                        {#if hostkeys[x.id]}
                            {@const hk = hostkeys[x.id]!}
                            <div class="ol-notice" class:err={hk.changed} data-scan={hk.trusted ? 'trusted' : hk.changed ? 'changed' : 'new'}>
                                {#if hk.trusted}{t('The server shows the confirmed key.')}{:else}
                                    {t('The server shows {type} {fp}. Compare it with the server (ssh-keygen -lf on its host key) and trust it only if it matches.', {type: hk.type, fp: hk.fingerprint})}
                                    <button class="hmm-button primary" onclick={() => void trust(x)} disabled={busy !== ''} data-action="trust-key">{t('Trust this key')}</button>
                                {/if}
                            </div>
                        {:else}
                            <button class="hmm-button" onclick={() => void scan(x)} disabled={busy !== ''} data-action="scan-key">{x.sftp?.host_key ? t("Check the server's key") : t("Get the server's key")}</button>
                        {/if}
                    </div>
                {/if}
                {#if msg[x.id]}<div class="ol-notice" data-msg>{msg[x.id]}</div>{/if}
                <div class="ol-actions ol-card-foot">
                    {#if x.kind === 'addon' && x.state.state === 'addon-missing'}
                        <a class="hmm-button" href="/addons" use:link data-action="addons">{t('Addons')}</a>
                    {:else if x.kind === 'addon' && failed(x.state.state) && x.state.action}
                        <a class="hmm-button primary ol-ellipsis" href={x.state.action.href} use:link title={text(x.state.action.label)} data-action="provider-action">{text(x.state.action.label)}</a>
                    {/if}
                    {#if x.state.state !== 'unsupported'}
                        <button class="hmm-button" onclick={() => void act(x, 'test')} disabled={busy !== '' || x.state.state === 'running'} data-action="test">{busy === x.id + 'test' ? t('Testing…') : t('Test')}</button>
                        <button class="hmm-button" onclick={() => void act(x, 'run')} disabled={busy !== '' || x.state.state === 'running'} data-action="run">{t('Back up now')}</button>
                        {#if x.kind === 'nfs' || x.kind === 'cifs' || x.kind === 'share'}
                            {#if x.state.mounted}<button class="hmm-button" onclick={() => void act(x, 'unmount')} disabled={busy !== ''} data-action="unmount">{t('Unmount')}</button>
                            {:else}<button class="hmm-button" onclick={() => void act(x, 'mount')} disabled={busy !== ''} data-action="mount">{t('Mount')}</button>{/if}
                        {/if}
                        <button class="hmm-button" aria-expanded={open[x.id] !== undefined} onclick={() => void toggleBackups(x)} bind:this={backupsButtons[x.id]} data-action="backups">{t('Backups here')}</button>
                    {/if}
                    {#if !(draft && draft.id === x.id)}<button class="hmm-button" onclick={() => edit(x)} disabled={busy !== ''} data-action="edit">{t('Edit')}</button>{/if}
                    <button class="hmm-button" onclick={() => void remove(x)} disabled={busy !== ''} data-action="remove">{t('Remove')}</button>
                </div>
                <Disclosure title={t('Backups here')} readOnly trigger={backupsButtons[x.id]} bind:open={() => open[x.id] !== undefined, (v) => { if (!v && open[x.id] !== undefined) void toggleBackups(x); }}>
                {#if open[x.id] === null}
                    <p class="ol-muted">{t('Loading…')}</p>
                {:else if open[x.id]}
                    {@const list = open[x.id] ?? []}
                    {#if !list.length}<p class="ol-muted" data-backups-empty>{t('No backups there.')}</p>{:else}
                        <table class="ol-table" data-backups>
                            <thead><tr><th>{t('File')}</th><th>{t('Size')}</th><th>{t('Time')}</th><th></th></tr></thead>
                            <tbody>
                                {#each list as b (b.name)}
                                    <tr>
                                        <td class="hmm-mono">{b.name}{#if b.encrypted && b.known === 'previous'}{' '}<span class="ol-badge warn">{t('earlier key')}</span>{:else if b.encrypted && b.known === 'unknown'}{' '}<span class="ol-badge bad">{t('unknown key')}</span>{/if}</td>
                                        <td>{size(b.size)}</td>
                                        <td>{when(b.time)}</td>
                                        <td><button class="hmm-button" onclick={() => void restore(x, b)} data-action="restore-this">{t('Restore this')}</button></td>
                                    </tr>
                                {/each}
                            </tbody>
                        </table>
                    {/if}
                {/if}
                </Disclosure>
                {#if draft && draft.id === x.id}{@render form()}{/if}
            </div>
        {/each}
    </div>
    {#if draft && !draft.id}
        <div class="ol-card ol-newtarget" data-new-target={draft.kind}>
            <div class="ol-card-title">{t('New target')}: {kindText({kind: draft.kind, addon: {id: draft.addon}})}</div>
            {@render form()}
        </div>
    {:else if !draft}
        <div class="ol-actions ol-addtarget" data-section="add-target">
            <span class="ol-muted">{t('Add target')}:</span>
            {#if !hasDirectory}<button class="hmm-button" onclick={() => add('directory')} data-add="directory">{KIND.directory()}</button>{/if}
            <!-- task 228: NFS and SMB are shares of System → Storage; a target picks one and a folder -->
            <button class="hmm-button" onclick={() => add('share')} disabled={!!view.kinds.share} title={view.kinds.share ? unsupportedText(view.kinds.share) : ''} data-add="share">{KIND.share()}</button>
            <button class="hmm-button" onclick={() => add('sftp')} data-add="sftp">{KIND.sftp()}</button>
            <!-- occulited task 150: one button per installed addon that provides targets -->
            {#each providers as p (p.addon)}
                <button class="hmm-button ol-ellipsis" onclick={() => addAddon(p.addon)} title={p.available ? text(p.label) : t('The addon {addon} is not running.', {addon: p.name})} disabled={!p.available} data-add="addon" data-provider={p.addon}>{text(p.label)}</button>
            {/each}
        </div>
        {#if !providers.length}
            <p class="ol-muted ol-provider-hint" data-notice="no-provider">{t('Cloud storage (OneDrive, Google Drive, Dropbox, S3 …): install the addon Sync.')} <a href="/addons/catalog" use:link>{t('Addons')}</a></p>
        {/if}
        {#if view.container}<div class="ol-notice" data-notice="container">{unsupportedText('container')}</div>{/if}
    {/if}
{/if}
</div>

{#snippet form()}
    {#if draft}
        <form class="ol-form ol-targetform" onsubmit={(e) => { e.preventDefault(); void save(); }} data-form={draft.kind}>
            <label>{t('Name')} <input class="hmm-input" bind:value={draft.name} maxlength="64" required data-field="name" /></label>
            {#if draft.kind === 'directory'}
                <!-- task 228: the location picker - a USB stick by its label (in any port), or the system's own storage -->
                <LocationPicker use="backup" kinds={['usb', 'userfs']} bind:location={draft.loc} bind:folder={draft.folder} label={t('Where')} />
                {#if draft.loc === 'userfs' || (!draft.loc && (draft.path + '/').startsWith('/usr/local/backup/'))}
                    <!-- occulited B-153: allowed, but no help when the system's own medium fails -->
                    <p class="ol-muted" data-notice="userfs-hint">{t('A folder on the system storage is on the same SD card or disk as the system: when that medium fails, the backups there are lost with it. A USB stick or a network share is safer.')}</p>
                {/if}
                {#if !draft.loc && draft.path}
                    <p class="ol-muted" data-legacy-path>{t('Chosen before as the path {path}. Pick the stick above, so the backups find it by its label in any USB port.', {path: draft.path})}</p>
                {/if}
            {:else if draft.kind === 'share'}
                <LocationPicker use="backup" kinds={['share']} bind:location={draft.loc} bind:folder={draft.folder} label={t('Share')} />
                <p class="ol-muted">{t('The folder is this system\'s own on the share (the host name by default), so several systems can share it.')}</p>
            {:else if draft.kind === 'addon'}
                {@const p = providerOf(draft.addon)}
                <label>{t('Account')}
                    <select class="hmm-select ol-account" bind:value={draft.destination} required data-field="destination">
                        {#if draft.destination && !p?.destinations.some((d) => d.id === draft?.destination)}<option value={draft.destination}>{t('account {id}', {id: draft.destination})}</option>{/if}
                        {#each p?.destinations ?? [] as d (d.id)}<option value={d.id}>{d.detail || d.name}</option>{/each}
                    </select>
                    {#if p}<a href={setupHref(p)} use:link data-action="connect-account">{t('Connect an account…')}</a>{/if}
                </label>
                {#if p && !p.destinations.length}<p class="ol-muted" data-notice="no-account">{t('No account yet: connect one on the page of the addon {addon}, then come back.', {addon: p.name})}</p>{/if}
                <label>{t('Folder')} <input class="hmm-input hmm-mono" bind:value={draft.folder} maxlength="255" required data-field="folder" /> <span class="ol-muted">{t('one per system, so several can share it')}</span></label>
                <label>{t('Upload from')}
                    <select class="hmm-select" bind:value={draft.source} data-field="source">
                        <option value="">{t('automatic')}</option>
                        <option value="staging">{t("the system's working copy")}</option>
                        {#each localTargets as lt (lt.id)}<option value={lt.id}>{lt.name}</option>{/each}
                    </select>
                </label>
                <p class="ol-muted" data-source-hint>
                    {#if draft.source === '' && autoSource}{t('from "{name}", which already holds each backup', {name: autoSource.name})}
                    {:else if draft.source === '' || draft.source === 'staging'}{t("from the system's working copy (kept only during the backup)")}
                    {:else}{t('from "{name}", which already holds each backup', {name: localTargets.find((x) => x.id === draft?.source)?.name ?? draft.source})}{/if}
                </p>
            {:else if draft.kind === 'nfs'}
                <label>{t('Server')} <input class="hmm-input hmm-mono" bind:value={draft.server} placeholder="nas.local" required data-field="server" /></label>
                <label>{t('Export')} <input class="hmm-input hmm-mono" bind:value={draft.exportPath} placeholder="/volume1/backup" required data-field="export" /></label>
                <label>{t('NFS version')} <select class="hmm-select" bind:value={draft.version} data-field="version"><option value="">{t('automatic (4.2, 4.1, 4, then 3)')}</option><option value="4.2">4.2</option><option value="4.1">4.1</option><option value="4">4.0</option><option value="3">3</option></select></label>
                <p class="ol-muted">{t('NFS checks no password: limit the export to this system\'s address. The system writes as root; an export that maps root to nobody needs "map all users" to one account that owns the directory.')}</p>
            {:else if draft.kind === 'cifs'}
                <label>{t('Server')} <input class="hmm-input hmm-mono" bind:value={draft.server} placeholder="nas.local" required data-field="server" /></label>
                <label>{t('Share')} <input class="hmm-input hmm-mono" bind:value={draft.share} placeholder="backup" required data-field="share" /></label>
                <label>{t('User')} <input class="hmm-input" bind:value={draft.user} autocomplete="off" required data-field="user" /></label>
                <label>{t('Password')} <input class="hmm-input" type="password" bind:value={draft.password} autocomplete="new-password" placeholder={draft.has_password ? t('(unchanged)') : ''} data-field="password" /></label>
                <label>{t('Domain')} <input class="hmm-input" bind:value={draft.domain} placeholder={t('(optional)')} data-field="domain" /></label>
                <label>{t('SMB version')} <select class="hmm-select" bind:value={draft.version} data-field="version"><option value="">{t('3.0 or newer')}</option><option value="3.1.1">3.1.1</option><option value="3.0">3.0</option></select></label>
                <label><input type="checkbox" bind:checked={draft.seal} data-field="seal" /> {t('Encrypt the transfer (SMB3)')}</label>
                <p class="ol-muted">{t('Use an account of its own on the NAS for this: its password travels in every backup of this system, so a restored system reconnects without it.')}</p>
            {:else}
                <label>{t('Server')} <input class="hmm-input hmm-mono" bind:value={draft.host} placeholder="nas.local" required data-field="host" /></label>
                <label>{t('Port')} <input class="hmm-input" type="number" min="1" max="65535" bind:value={draft.port} style="width:7em" data-field="port" /></label>
                <label>{t('User')} <input class="hmm-input" bind:value={draft.user} autocomplete="off" required data-field="user" /></label>
                <label>{t('Directory on the server')} <input class="hmm-input hmm-mono" bind:value={draft.sshpath} placeholder={t('(home directory)')} data-field="sshpath" /></label>
                <p class="ol-muted">{t('The system makes a key of its own when the target is saved; you then add its line to the server\'s authorized_keys and confirm the server\'s key. Passwords are not used.')}</p>
            {/if}
            {#if draft.kind !== 'directory' && draft.kind !== 'share' && draft.kind !== 'addon'}
                <label>{t('Subdirectory')} <input class="hmm-input hmm-mono" bind:value={draft.subdir} placeholder={view?.hostname ?? ''} data-field="subdir" /> <span class="ol-muted">{t('one per system, so several can share it')}</span></label>
            {/if}
            <label>{t('Keep')} <input class="hmm-input" type="number" min="0" max="1000" bind:value={draft.max_backups} style="width:6em" data-field="max" /> <span class="ol-muted">{t('backups of this system (0 = all)')}</span></label>
            {#if draft.kind === 'addon'}
                <!-- never a switch: an addon and its destination only ever hold encrypted backups -->
                {#if view?.encryption}
                    <p class="ol-muted" data-notice="always-encrypted">{t('Backups to this kind of target are always encrypted with the recovery key.')}</p>
                {:else}
                    <div class="ol-notice err" data-notice="needs-encryption">{t('This kind of target takes encrypted backups only. Set up encryption first.')} <a href="#encryption">{t('Set up encryption')}</a></div>
                {/if}
            {:else}
                <label><input type="checkbox" checked={draft.encrypt} onchange={encryptChange} data-field="encrypt" /> {t('Encrypt the backups (with the recovery key)')}</label>
            {/if}
            <label><input type="checkbox" bind:checked={draft.append_only} data-field="append-only" /> {t('Append-only: never delete anything here')}</label>
            <label><input type="checkbox" bind:checked={draft.enabled} data-field="enabled" /> {t('Include in the nightly backup')}</label>
            {#if formErr}<div class="ol-notice err" data-form-error>{formErr}</div>{/if}
            <div class="ol-actions">
                <button class="hmm-button primary" type="submit" disabled={busy !== '' || !draftReady} data-action="save">{t('Save')}</button>
                <button class="hmm-button" type="button" onclick={() => (draft = null)} data-action="cancel">{t('Cancel')}</button>
            </div>
        </form>
    {/if}
{/snippet}

<style>
    /* task 161: the mounted sticks, one button each; the text wraps on a phone */
    .ol-sticks { display: flex; flex-wrap: wrap; gap: 6px; }
    .ol-stick { white-space: normal; text-align: left; overflow-wrap: anywhere; max-width: 100%; }
    .ol-targets { display: flex; flex-direction: column; gap: 12px; margin: 10px 0; }
    /* occulited task 59: inside the section's panel a target is a bordered box, not a second raised card */
    .ol-targets .ol-card, .ol-newtarget { box-shadow: none; }
    .ol-targets-bar { flex-wrap: wrap; gap: 12px; align-items: center; }
    .ol-targets-bar label { display: inline-flex; gap: 6px; align-items: center; }
    .ol-card-head .ol-badge { flex: 0 0 auto; }
    .ol-card-detail > div { margin-top: 2px; }
    .ol-sshkey { margin-top: 10px; display: flex; flex-direction: column; gap: 6px; min-width: 0; }
    /* a fingerprint is one run without a break: it breaks anywhere, so a phone keeps its width */
    .ol-sshkey > div, .ol-card-detail, [data-test-result] { overflow-wrap: anywhere; min-width: 0; }
    .ol-keyline { display: block; overflow-wrap: anywhere; font-size: var(--hmm-font-size-small); padding: 6px 8px; border: 1px solid var(--hmm-border-muted); border-radius: 4px; }
    .ol-card-foot { flex-wrap: wrap; gap: 6px; }
    .ol-addtarget { flex-wrap: wrap; gap: 6px; align-items: center; margin: 8px 0; }
    .ol-targetform { margin-top: 10px; max-width: 640px; display: flex; flex-direction: column; gap: 8px; }
    .ol-targetform label { display: block; }
    .ol-table td.hmm-mono { overflow-wrap: anywhere; }
    .ol-notice.err { border-left: 3px solid var(--hmm-error); }
    /* occulited task 150: an addon target's badge reserves its widest label; the shown one is centred in it */
    .ol-badge-sized { display: inline-grid; justify-items: center; }
    .ol-badge-sized > span { grid-area: 1 / 1; }
    .ol-badge-size { visibility: hidden; pointer-events: none; }
    /* a provider's label or an account's name is as long as its author made it */
    .ol-ellipsis { max-width: 16em; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .ol-account { max-width: min(24em, 100%); text-overflow: ellipsis; }
    .ol-provider-hint { margin: 4px 0 0; }
    /* the way to the Addons page and a provider's action are links in the row of buttons */
    .ol-card-foot > a.hmm-button { text-decoration: none; white-space: nowrap; }
    .ol-card-foot > a.hmm-button:not(.primary) { color: var(--hmm-fg); }
</style>
