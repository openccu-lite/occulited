<script lang="ts">
    /*
     * occulited task 160 (the maintainer, 2026-11-20, on the userfs's mount options: "this stuff
     * should be configurable in system-storage"): System → Storage's "Write behaviour of the system
     * storage". The two ext4 mount options that decide how often the SD card is written to without
     * anybody asking for it - lazytime and the journal's commit interval - each with what it costs.
     * A change applies at once (a remount) and is applied again at every start of the system.
     *
     * Nothing here changes its size with the state: the interval is a row of buttons of a fixed
     * width, the chosen one marked by colour, and the line with the mounted options is always there.
     */
    import {onMount} from 'svelte';
    import {api} from './api';
    import {auth} from './auth.svelte';
    import {t} from './i18n.svelte';
    import SectionHead from './SectionHead.svelte';
    import Help from './Help.svelte';

    interface Options {
        lazytime: boolean;
        commit_s: number;
    }
    interface View {
        mount: string;
        mounted: boolean;
        fstype?: string;
        options?: string;
        supported: boolean;
        active: Options;
        setting: Options;
        default: Options;
        custom: boolean;
        commit_choices: number[];
    }

    let {active = () => true}: {active?: () => boolean} = $props();

    let view = $state<View | null>(null);
    let error = $state('');
    let busy = $state(false);
    const admin = $derived(auth.role === 'admin');

    async function load() {
        try {
            view = await api.get<View>('/api/system/v1/storage/mount-options');
        } catch {
            view = null; // an older daemon: no section
        }
    }
    onMount(() => {
        void load();
        const poll = setInterval(() => active() && !busy && void load(), 30000);
        return () => clearInterval(poll);
    });

    async function set(o: Options) {
        busy = true;
        error = '';
        try {
            view = await api.put<View>('/api/system/v1/storage/mount-options', o);
        } catch (e) {
            error = (e as Error).message;
            await load();
        } finally {
            busy = false;
        }
    }
</script>

{#if view}
    <SectionHead id="write-behaviour" title={t('Write behaviour of the system storage')} help={t('How often the system storage - on most systems an SD card, which wears with every write - is written to without a program asking for it. Both settings change how the file system is mounted, not what is stored on it. They apply at once and stay across a restart of the system.')} />
    <div class="wb" data-write-behaviour>
        {#if !view.supported}
            <p class="ol-muted" data-wb-state="unsupported">{t('Not available: the system storage of this system is not an ext4 file system mounted on its own.')}</p>
        {:else}
            <div class="wb-line" data-setting="lazytime">
                <label for="wb-lazytime"><input id="wb-lazytime" type="checkbox" checked={view.setting.lazytime} disabled={!admin || busy} onchange={(e) => void set({lazytime: e.currentTarget.checked, commit_s: view!.setting.commit_s})} /> {t('Write file timestamps later (lazytime)')}</label><Help>{t('The times at which a file was last changed or read are kept in memory and written with the next change of that file, when everything is synced, or within a day at the latest - instead of at once. That saves many small writes for files that are rewritten in place, such as databases. No data is at risk: after a power cut a file can at most carry an older timestamp.')}</Help>
            </div>
            <div class="wb-line" data-setting="commit">
                <span class="wb-label" id="wb-commit-label">{t('Write changes at the latest after')}</span>
                <div class="ol-seg wb-seg" role="group" aria-labelledby="wb-commit-label">
                    {#each view.commit_choices as c (c)}
                        <button type="button" aria-pressed={view.setting.commit_s === c} disabled={!admin || busy} onclick={() => view!.setting.commit_s !== c && void set({lazytime: view!.setting.lazytime, commit_s: c})} data-commit={c}>{t('{n} s', {n: c})}</button>
                    {/each}
                </div><Help>{t('The commit interval of the file system\'s journal: how long changes that no program asked to be written at once may wait in memory. A longer interval means fewer writes; after a power cut, the changes of up to that many seconds are lost. What a program has written out itself - settings, the names of the devices, the databases of the radio processes - is on the storage at once in every case.')}</Help>
            </div>
            <p class="ol-muted wb-note" data-wb-risk>{t('A power cut can take up to {n} seconds of changes that were not yet written.', {n: view.setting.commit_s})} {t('The system\'s default: {n} seconds, timestamps at once.', {n: view.default.commit_s})}</p>
        {/if}
        {#if view.mounted}
            <p class="ol-muted wb-note" data-wb-mounted>{t('Mounted now:')} <span class="hmm-mono">{view.mount} · {view.fstype} · {view.options}</span></p>
        {/if}
        {#if error}<div class="ol-notice error" role="alert" data-notice="wb-error">{error}</div>{/if}
    </div>
{/if}

<style>
    .wb { margin: 8px 0 14px; max-width: 60em; }
    .wb-line { display: flex; gap: 6px 12px; align-items: center; flex-wrap: wrap; margin: 10px 0; }
    .wb-seg > button { min-width: 4.5em; justify-content: center; text-align: center; }
    /* the chosen interval in the accent colour, not in bold: the buttons keep their width */
    .wb-seg > button[aria-pressed='true'] { color: var(--hmm-accent); background: var(--hmm-accent-bg); }
    .wb-note { margin: 6px 0; overflow-wrap: anywhere; }
</style>
