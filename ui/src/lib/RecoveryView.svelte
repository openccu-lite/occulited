<script lang="ts">
    /*
     * The page while the system is in its recovery system (openccu-lite task 505): App.svelte routes
     * every path here as long as the recovery answers (lib/recoveryroute.ts), since no other page
     * can load without occulited. It shows what the recovery says (lib/inrecovery.svelte.ts): the
     * install's step and a bar weighted by the steps' measured lengths, with real percentages while
     * a partition is written; the countdown until the system is back when this page started the
     * install or the reboot; the raw output behind Details; and the recovery's own page - on this
     * origin (over HTTPS since 2.0.0) and by IP address, which works in every browser (D-56).
     *
     * Every line keeps its box when its text changes (the UI's no-resize rule): the step line is one
     * line, cut with an ellipsis and the whole text as its title; the figures use tabular digits.
     */
    import {t} from './i18n.svelte';
    import {link} from './router.svelte';
    import BootBar from './BootBar.svelte';
    import {readEntry} from './bootwatch';
    import {inRecovery, recoveryUrl} from './inrecovery.svelte';
    import {installView, isLogAnswer, logText, type StepKey} from './recoverystatus';

    const STEP_TEXT: Record<StepKey, string> = {
        starting: 'The installation starts',
        p1: 'Checking the uploaded file',
        p2: 'Calculating the checksum',
        p3: 'Checking the free space',
        p4: 'Testing the storage speed',
        p5: 'Unpacking the update',
        p6: 'Verifying the update',
        p7: 'Preparing the installation',
        check: 'Checking the system images',
        bootfs: 'Writing the boot partition',
        rootfs: 'Writing the system partition',
        write: 'Writing the system',
        done: 'Installed – the system restarts',
        failed: 'The installation failed – the system restarts with the version it had',
    };

    let now = $state(Date.now());
    $effect(() => {
        const id = setInterval(() => (now = Date.now()), 1000);
        return () => clearInterval(id);
    });

    const update = $derived(inRecovery.update);
    const view = $derived(update ? installView(update, (now - inRecovery.stepSince) / 1000) : null);
    // the bar never goes back (a reconnect, a counter that lags): the store keeps the highest
    const fraction = $derived(view ? Math.max(view.fraction, inRecovery.shown) : 0);
    const percent = $derived(Math.floor(fraction * 100));
    const stepLine = $derived.by(() => {
        if (!view) return '';
        const what = t(STEP_TEXT[view.key]) + (view.flashPercent !== null ? ` – ${view.flashPercent} %` : '');
        return view.step !== null && !view.done && !view.failed ? t('Step {n} of {m}: {what}', {n: view.step, m: view.steps, what}) : what;
    });
    const silentS = $derived(inRecovery.lastSeen ? Math.floor((now - inRecovery.lastSeen) / 1000) : 0);
    const entry = $derived(inRecovery.active ? readEntry(now) : null);
    const ipUrl = recoveryUrl();

    // ---- Details: the raw output, /fwinstall.log (a link to the install log in the recovery) ----
    let detailsOpen = $state(false);
    let log = $state<string | null>(null);
    let logMissing = $state(false);
    let pre = $state<HTMLPreElement | null>(null);
    async function loadLog() {
        try {
            const r = await fetch('/fwinstall.log', {cache: 'no-store'});
            const body = await r.text();
            if (!isLogAnswer(r.status, r.headers.get('Content-Type'), r.redirected)) {
                if (log === null) logMissing = true;
                return;
            }
            const atEnd = !pre || pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 8;
            log = logText(body);
            logMissing = false;
            if (atEnd) requestAnimationFrame(() => pre && (pre.scrollTop = pre.scrollHeight));
        } catch {
            /* no answer: the recovery restarting; the next round tries again */
        }
    }
    $effect(() => {
        if (!detailsOpen || !inRecovery.active) return;
        void loadLog();
        const id = setInterval(loadLog, 3000);
        return () => clearInterval(id);
    });
</script>

<section class="ol-recoveryview" data-recovery-view>
    {#if inRecovery.active}
        <h1>{t('The system is in its recovery system')}</h1>
        {#if update && update.phase !== 'done' && update.phase !== 'failed'}
            <p class="ol-rv-lead">{t('An update is being installed. Do not switch the system off.')}</p>
        {:else if !update}
            <p class="ol-rv-lead">{t('The pages of the system are not available until it has restarted.')}</p>
        {/if}

        {#if view}
            <div class="ol-rv-install" class:failed={view.failed} data-install-progress>
                <div class="ol-rv-step" title={stepLine} data-install-step>{stepLine}</div>
                <div
                    class="ol-rv-track"
                    role="progressbar"
                    aria-label={t('Installation progress')}
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={percent}
                    aria-valuetext={stepLine}
                >
                    <div class="ol-rv-fill" style:width={`${(fraction * 100).toFixed(1)}%`}></div>
                </div>
                <div class="ol-rv-figures" data-install-percent>{t('{p} % of the installation', {p: percent})}</div>
            </div>
        {/if}

        {#if entry && entry.kind === 'update'}
            <BootBar {entry} hint={false} />
        {/if}

        <!-- reserved: the line is always there, so the page does not move when it appears -->
        <p class="ol-rv-silent ol-muted" aria-live="polite">{silentS >= 12 ? t('No answer from the recovery system for {n} s – it may be restarting.', {n: silentS}) : ' '}</p>

        <details class="ol-rv-details" bind:open={detailsOpen} data-install-details>
            <summary>{t('Details: the output of the installation')}</summary>
            {#if log !== null}
                <pre bind:this={pre} class="hmm-mono" data-install-log>{log}</pre>
            {:else if logMissing}
                <p class="ol-muted" data-install-log-missing>{t('Not available: this recovery system does not provide its output here.')}</p>
            {:else}
                <p class="ol-muted">…</p>
            {/if}
        </details>

        <div class="ol-rv-links">
            <!-- a real navigation, not the router: the recovery's own page is on this origin -->
            <a href="/" data-recovery-page>{t("Open the recovery system's page")}</a>
            {#if ipUrl}<span class="ol-muted">·</span> <a href={ipUrl} class="hmm-mono" data-recovery-ip>{ipUrl}</a> <span class="ol-muted">({t('by IP address')})</span>{/if}
        </div>
        <p class="ol-muted">{t('This page returns to the system by itself when it is back.')}</p>
    {:else}
        <h1>{t('The system is not in its recovery system.')}</h1>
        <p><a href="/" use:link>{t('Status')}</a></p>
    {/if}
</section>

<style>
    /* the recovery bar lies over the top of the page (App.svelte): the view starts below it */
    .ol-recoveryview { max-width: 640px; margin: 72px auto 24px; padding: 0 16px; }
    h1 { font-size: 20px; margin: 0 0 6px; }
    .ol-rv-lead { margin: 0 0 18px; font-weight: 600; color: var(--hmm-warn); }
    .ol-rv-install { margin: 0 0 14px; }
    .ol-rv-step { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-bottom: 6px; min-height: 1.4em; }
    .ol-rv-track { height: 10px; border-radius: 5px; overflow: hidden; background: var(--hmm-accent-bg); }
    .ol-rv-fill { height: 100%; background: var(--hmm-accent); border-radius: inherit; transition: width 0.6s ease-out; }
    .failed .ol-rv-fill { background: var(--hmm-error); }
    .ol-rv-figures { margin-top: 6px; color: var(--hmm-fg-muted); font-size: var(--hmm-font-size-small); font-variant-numeric: tabular-nums; }
    .ol-rv-silent { min-height: 1.4em; margin: 8px 0; font-size: var(--hmm-font-size-small); }
    .ol-rv-details { margin: 12px 0; }
    .ol-rv-details summary { cursor: pointer; color: var(--hmm-link); }
    .ol-rv-details pre {
        margin: 8px 0 0; padding: 10px 12px; max-height: 300px; overflow: auto;
        white-space: pre-wrap; overflow-wrap: anywhere; font-size: 12px;
        background: var(--hmm-bg-sunken); border: 1px solid var(--hmm-border); border-radius: var(--hmm-radius);
    }
    .ol-rv-links { margin: 14px 0 6px; overflow-wrap: anywhere; }
    @media (prefers-reduced-motion: reduce) {
        .ol-rv-fill { transition: none; }
    }
</style>
