<script lang="ts">
    /*
     * The bar under the top bar while the system is in its recovery system (openccu-lite task 505,
     * the maintainer: impossible to miss). It lies over the top of the page, under the header - it
     * takes no room in the layout, so nothing moves when it comes or goes (App.svelte places it in a
     * zero-height anchor). The attention colour of the kit (--hmm-warn) as a solid bar, unlike the
     * tinted notices inside the pages; its text stays one line on a desktop and wraps on a phone,
     * where it still lies over the page.
     */
    import {t} from './i18n.svelte';
    import Icon from './Icon.svelte';
    import {inRecovery} from './inrecovery.svelte';
    import {installView} from './recoverystatus';

    const update = $derived(inRecovery.update);
    const installing = $derived(!!update && update.phase !== 'done' && update.phase !== 'failed');
    const percent = $derived(update ? Math.floor(Math.max(installView(update).fraction, inRecovery.shown) * 100) : 0);
</script>

<div class="ol-recoverybar" role="status" data-recovery-bar>
    <Icon name="alert" size={18} />
    <span class="ol-rb-text">
        {#if installing}
            {t('The system is in its recovery system: an update is being installed. Do not switch it off.')}
        {:else if update?.phase === 'done'}
            {t('The system is in its recovery system: the update is installed, the system restarts.')}
        {:else if update?.phase === 'failed'}
            {t('The system is in its recovery system: the installation failed, the system restarts.')}
        {:else}
            {t('The system is in its recovery system. Its pages are not available until it has restarted.')}
        {/if}
    </span>
    {#if installing}<span class="ol-rb-figure" data-recovery-bar-percent>{percent} %</span>{/if}
</div>

<style>
    .ol-recoverybar {
        position: absolute; left: 0; right: 0; top: 0; z-index: 44;
        display: flex; align-items: center; gap: 10px;
        min-height: 38px; box-sizing: border-box; padding: 7px 16px;
        background: var(--ol-recoverybar-bg); color: var(--ol-recoverybar-fg);
        font-weight: 600; box-shadow: var(--hmm-shadow-menu);
        border-bottom: 2px solid var(--ol-recoverybar-edge);
    }
    .ol-rb-text { flex: 1 1 auto; min-width: 0; }
    .ol-rb-figure { flex: 0 0 auto; font-variant-numeric: tabular-nums; min-width: 4.5ch; text-align: right; }
</style>
