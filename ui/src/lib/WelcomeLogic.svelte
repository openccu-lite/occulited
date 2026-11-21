<script lang="ts" module>
    /** lite-addon-logic's catalogue entry, by its repository (openccu-lite-addons task 79, OQ-20) */
    export const LOGIC_GIT = /^https:\/\/github\.com\/openccu-lite\/lite-addon-logic\/?$/i;
    /** its manifest id, for a catalogue whose entry carries none yet */
    export const LOGIC_ID = 'logic';
</script>

<script lang="ts">
    /*
     * occulited task 168 (the maintainer, 2026-11-21): beside Homematic Manager's one-click install
     * (task 143), the programs and system variables - lite-addon-logic - in one click. Shown only
     * when the catalogue lists the addon. Right after its install, when the CCU's ReGa database was
     * kept from the switch (task 167), the page offers the import of the programs and system
     * variables: the addon's own page, which its manifest names (ui.regadom_import_url); an addon
     * that names none opens its frontend.
     *
     * The flow is Homematic Manager's: a fresh system holds only the bundled list, so the click runs
     * the catalogue check first (the user's own request to GitHub, D-90), then the catalogue's
     * install route and its progress.
     */
    import {onMount} from 'svelte';
    import {api, ApiError} from './api';
    import {t} from './i18n.svelte';
    import {installError} from './install';
    import {addonsChanged} from './addonsync';
    import InstallProgress, {type InstallBusy, type Progress} from './InstallProgress.svelte';

    interface Entry { git: string; id?: string; latest?: {version: string}; requires?: {architectures?: string[]; lite?: string}; lite_unmet?: boolean }
    interface View {
        catalog: {addons: Entry[]; check_error?: {message: string}; releases_error?: {message: string}};
        installed: Record<string, string>;
        arch: string;
    }
    interface Props {
        step: number;
        onopen: (path: string) => void;
        blocked?: boolean;
    }
    let {step, onopen, blocked = false}: Props = $props();

    type State = 'loading' | 'hidden' | 'ready' | 'checking' | 'installing' | 'installed' | 'failed' | 'unreachable' | 'unavailable';
    let st = $state<State>('loading');
    let reason = $state('');
    let id = $state(LOGIC_ID);
    let slotBusy = $state(false);
    let job = $state<InstallBusy | null>(null);
    let progressView: InstallProgress | undefined = $state();
    // the kept ReGa database (task 167) and the addon's import page (its manifest's ui.regadom_import_url)
    let regadom = $state(false);
    let importURL = $state('');

    const archOk = (e: Entry, arch: string) => {
        const a = e.requires?.architectures ?? [];
        return a.length === 0 || a.includes(arch) || a.includes('any');
    };

    function judge(v: View, checked: boolean): State {
        const e = v.catalog.addons.find((x) => LOGIC_GIT.test(x.git));
        if (e?.id) id = e.id;
        if (v.installed[id]) return 'installed';
        if (!e) return 'hidden';
        if (!e.id || !e.latest) {
            if (!checked) return 'ready';
            reason = v.catalog.check_error?.message ?? v.catalog.releases_error?.message ?? t('The catalogue has no release of the programs and system variables.');
            return v.catalog.check_error || !e.id ? 'unreachable' : 'failed';
        }
        if (!archOk(e, v.arch)) {
            reason = t('The programs and system variables are not available for {arch}.', {arch: v.arch});
            return 'unavailable';
        }
        if (e.lite_unmet) {
            reason = t('The programs and system variables {version} need openccu-lite {lite} or later; update the system first.', {version: e.latest.version, lite: e.requires?.lite ?? '?'});
            return 'unavailable';
        }
        return 'ready';
    }

    async function loadImport() {
        try { regadom = (await api.get<{present: boolean}>('/api/system/v1/regadom')).present; } catch { regadom = false; }
        try {
            const a = (await api.get<{addons: {id: string; regadom_import_url?: string}[]}>('/api/system/v1/addons')).addons.find((x) => x.id === id);
            importURL = a?.regadom_import_url ?? '';
        } catch { /* no link: the frontend opens */ }
    }

    onMount(async () => {
        try {
            st = judge(await api.get<View>('/api/system/v1/catalog'), false);
        } catch {
            st = 'hidden'; // no catalogue on this system: Homematic Manager's step says so
        }
        if (st === 'installed') await loadImport();
    });

    $effect(() => {
        if (job?.kind === 'catalog' && job.addon_id === id && st !== 'installing' && st !== 'installed') st = 'installing';
    });

    async function install() {
        reason = '';
        if (st !== 'ready' && st !== 'failed' && st !== 'unreachable') return;
        let v: View | null = null;
        try {
            v = await api.get<View>('/api/system/v1/catalog');
        } catch { /* the check below asks again */ }
        let s = v ? judge(v, false) : 'ready';
        const e = v?.catalog.addons.find((x) => LOGIC_GIT.test(x.git));
        if (s === 'ready' && (!e?.id || !e.latest)) {
            st = 'checking';
            try {
                s = judge(await api.get<View>('/api/system/v1/catalog?refresh=1'), true);
            } catch (err) {
                reason = err instanceof ApiError ? err.message : String(err);
                st = 'unreachable';
                return;
            }
        }
        if (s !== 'ready') {
            st = s === 'hidden' ? 'failed' : s;
            return;
        }
        st = 'installing';
        try {
            await api.post(`/api/system/v1/catalog/${encodeURIComponent(id)}/install`);
        } catch (err) {
            reason = installError(err);
            st = 'failed';
            return;
        }
        await progressView?.poll();
    }

    async function finished(p: Progress) {
        if (p.addon_id !== id) return;
        let there = false;
        try {
            there = !!(await api.get<View>('/api/system/v1/catalog')).installed[id];
        } catch { /* not known: said as failed */ }
        if (there) {
            st = 'installed';
            addonsChanged();
            await loadImport();
        } else {
            st = 'failed';
        }
    }

    const busy = $derived(st === 'loading' || st === 'checking' || st === 'installing');
    const otherRun = $derived(slotBusy && st !== 'installing' && !(job?.kind === 'catalog' && job.addon_id === id));
    const label = $derived(
        st === 'installed' ? t('Open the programs and system variables')
        : st === 'checking' ? t('Loading the catalogue…')
        : st === 'installing' ? t('Installing the programs and system variables…')
        : st === 'failed' || st === 'unreachable' ? t('Try again')
        : t('Install the programs and system variables'),
    );
    // every label the button can carry, so its box is the widest of them in every state
    const LABELS = ['Install the programs and system variables', 'Open the programs and system variables', 'Loading the catalogue…', 'Installing the programs and system variables…', 'Try again'];
    const disabled = $derived(st === 'installed' ? blocked : busy || otherRun || st === 'unavailable');

    function main() {
        if (st === 'installed') onopen(`/nav/${encodeURIComponent(id)}`);
        else void install();
    }
</script>

{#if st !== 'hidden' && st !== 'loading'}
    <section class="wl" data-welcome-logic data-state={st} aria-labelledby="welcome-logic">
        <h2 id="welcome-logic">{step} · {t('Programs and system variables')}</h2>
        <p>{t('The CCU\'s programs and system variables are an addon on openccu-lite: it runs programs, keeps the system variables and imports both from a CCU or OpenCCU.')}</p>
        <div class="ol-actions wl-row">
            <button class="hmm-button wl-main" {disabled} aria-busy={busy} onclick={main} data-action="welcome-logic">
                {#each LABELS as l (l)}<span class="wl-sizer" aria-hidden="true">{t(l)}</span>{/each}
                <span class="wl-live">{label}</span>
            </button>
            {#if st === 'installed' && regadom}
                <!-- task 168: the import right after the install, while the ReGa database is kept (task 167) -->
                {#if importURL}
                    <a class="hmm-button primary" href={importURL} target="_blank" rel="noopener" data-action="welcome-logic-import">{t('Import programs and system variables')}</a>
                {:else}
                    <button type="button" class="hmm-button primary" disabled={blocked} onclick={() => onopen(`/nav/${encodeURIComponent(id)}`)} data-action="welcome-logic-import">{t('Import programs and system variables')}</button>
                {/if}
            {/if}
        </div>
        <InstallProgress bind:this={progressView} bind:busy={slotBusy} bind:job showEnded={false} onfinished={finished} addon={id} />
        <p class="ol-muted wl-status" data-welcome-logic-status role="status">
            {#if st === 'installed' && regadom}
                {t('The database of the CCU this system was switched from is kept: its programs and system variables can be imported now.')}
            {:else if st === 'installed'}
                {t('The programs and system variables are installed. They open from the menu as well.')}
            {:else if st === 'unreachable'}
                <span class="ol-warn">{t('The catalogue could not be loaded from GitHub: {e}', {e: reason})}</span>
            {:else if st === 'failed'}
                <span class="ol-warn">{reason || t('The programs and system variables were not installed; the notice above says why.')}</span>
            {:else if st === 'installing'}
                {t('The installation runs on the system: it goes on when this page is left.')}
            {:else if st === 'unavailable'}
                <span class="ol-warn">{reason}</span>
            {:else if otherRun}
                {t('Another install is still running. Wait until it has finished.')}
            {:else}
                {t('Optional. One click installs the newest release from GitHub (github.com).')}
            {/if}
        </p>
    </section>
{/if}

<style>
    .wl { margin-top: 8px; }
    .wl-row { display: flex; align-items: stretch; flex-wrap: wrap; gap: 8px; }
    .wl-main { display: inline-grid; justify-items: center; align-items: center; }
    .wl-sizer, .wl-live { grid-area: 1 / 1; white-space: nowrap; }
    .wl-sizer { visibility: hidden; pointer-events: none; }
    .wl-status { min-height: 2.8em; margin: 8px 0 0; }
    @media (max-width: 480px) {
        .wl-main, .wl-row > a, .wl-row > button { width: 100%; }
        .wl-sizer, .wl-live { white-space: normal; }
    }
</style>
