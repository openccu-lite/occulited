<script lang="ts" module>
    // occulited task 167: the CCU's ReGa database kept after the switch (GET /regadom)
    export interface KeptRegadomView { present: boolean; bytes?: number; modified?: string }
</script>

<script lang="ts">
    /*
     * occulited task 167 (the maintainer, 2026-11-21): the ReGa database of the CCU is no longer
     * removed after the switch - lite-addon-logic imports programs and system variables from it. The
     * panel is shown while it is there: what it is, that backups carry it, and its removal, which is
     * the user's choice and asks first.
     */
    import {onMount} from 'svelte';
    import {api, ApiError} from './api';
    import {t} from './i18n.svelte';
    import {dateOnly} from './datefmt';
    import {formatBytes} from './netpanels';
    import {ask} from './dialog.svelte';
    import Help from './Help.svelte';

    let view = $state<KeptRegadomView | null>(null);
    let busy = $state(false);
    let error = $state('');
    let removed = $state('');

    // occulited task 168: the import page of an installed addon that names one (ui.regadom_import_url)
    let importURL = $state('');
    async function load() {
        try { view = await api.get<KeptRegadomView>('/api/system/v1/regadom'); } catch { /* the panel stays hidden */ }
        if (!view?.present) return;
        try {
            importURL = (await api.get<{addons: {regadom_import_url?: string}[]}>('/api/system/v1/addons')).addons.find((a) => a.regadom_import_url)?.regadom_import_url ?? '';
        } catch { /* no link */ }
    }
    onMount(() => { void load(); });

    async function remove() {
        if (!(await ask({
            title: t('Remove the ReGa database?'),
            message: t('The CCU\'s programs and system variables can then no longer be imported on this system - only from a backup made before the removal, or from a backup of the CCU.'),
            confirm: t('Remove'),
            danger: true,
        }))) return;
        busy = true;
        error = '';
        try {
            const r = await api.del<{removed: boolean; freed_bytes: number}>('/api/system/v1/regadom');
            removed = t('The ReGa database was removed ({size} freed).', {size: formatBytes(r.freed_bytes)});
            view = {present: false};
        } catch (e) {
            error = t('The ReGa database could not be removed: {error}', {error: e instanceof ApiError ? e.message : String(e)});
        } finally {
            busy = false;
        }
    }
</script>

{#if view?.present || removed}
    <h2 id="regadom">{t('ReGa database of the CCU')}<Help>{t('The database of the CCU this system was switched from (homematic.regadom). openccu-lite took the names, rooms and functions from it at its first start; the addon Logic imports the programs and system variables from it. It is kept until you remove it here, and every backup carries it.')}</Help></h2>
    <div class="ol-panel" data-section="regadom">
        {#if error}<div class="ol-notice err">{error}</div>{/if}
        {#if view?.present}
            <p data-regadom="present">{t('Kept from the switch: {size}, last saved on the CCU on {date}.', {size: formatBytes(view.bytes ?? 0), date: view.modified ? dateOnly(view.modified) : '?'})}</p>
            <p class="ol-muted">{t('The addon Logic can import the CCU\'s programs and system variables from it. Remove it when you no longer need it.')}</p>
            <div class="ol-actions">
                {#if importURL}<a class="hmm-button" href={importURL} target="_blank" rel="noopener" data-action="regadom-import">{t('Import programs and system variables')}</a>{/if}
                <button type="button" class="hmm-button danger" onclick={remove} disabled={busy} data-action="regadom-remove">{busy ? t('Removing…') : t('Remove the ReGa database…')}</button>
            </div>
        {:else}
            <p class="ol-muted" data-regadom="removed">{removed}</p>
        {/if}
    </div>
{/if}
