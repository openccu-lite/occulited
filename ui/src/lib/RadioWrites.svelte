<script lang="ts">
    /*
     * occulited task 163 (the maintainer, 2026-11-21: users keep running into a program that sends
     * too many setValue/putParamset calls, often in bursts, and drives the duty cycle up - "can we
     * show how many of them per minute an external client causes on lite-rpc? With a sparkline and
     * a warning on the Status page when it gets too much?"): the Status page's panel "Radio
     * writes". One row per client that sent a radio write in the last hour - its rate now, the 60
     * minutes as bars, the hour's sum and the share of writes that changed nothing - and below
     * them, on the same 60 minutes, the duty cycle of each radio interface, so a burst and the
     * rise it caused stand one above the other.
     *
     * The clients' bars share one scale (the top is written under the table), so a quiet client is
     * low beside a busy one; the duty cycle has its own row and its own scale, never a second axis
     * in the same plot. Hint and warning are a badge with a word in a column of a fixed width, the
     * same words as the Status warnings above.
     *
     * Nothing here changes its size with the figures: the numbers are right-aligned in columns of
     * a fixed width, every sparkline is the same box, and the badge's column is there without one.
     */
    import {onMount} from 'svelte';
    import {api} from './api';
    import {t} from './i18n.svelte';
    import {link} from './router.svelte';
    import {scrollToAnchor} from './anchor';
    import SectionHead from './SectionHead.svelte';
    import Sparkline from './Sparkline.svelte';
    import {clientKey, dutyTop, lastReading, minutesAgo, noopShare, writesTop, type RWClient, type RWDuty, type RWView} from './radiowrites';

    let {active = () => true, warnAttrs = {}}: {active?: () => boolean; warnAttrs?: Record<string, string | undefined>} = $props();

    let view = $state<RWView | null>(null);
    let error = $state('');

    async function load() {
        try {
            view = await api.get<RWView>('/api/system/v1/radio/writes');
            error = '';
        } catch (e) {
            error = (e as Error).message;
        }
    }
    onMount(() => {
        // a warning's link and the token list's badge name the section
        void load().then(() => scrollToAnchor('radio-writes'));
        const poll = setInterval(() => active() && void load(), 10000);
        return () => clearInterval(poll);
    });

    const top = $derived(view ? writesTop(view.clients) : 10);
    const dcTop = $derived(view ? dutyTop(view.duty_cycle) : 25);

    function ago(i: number, n: number): string {
        const m = minutesAgo(i, n);
        return m === 0 ? t('this minute') : m === 1 ? t('1 minute ago') : t('{n} minutes ago', {n: m});
    }
    function kindWord(c: RWClient): string {
        return c.kind === 'token' ? t('program') : c.kind === 'user' ? t('account') : t('system');
    }
    function levelTitle(c: RWClient): string {
        if (!view) return '';
        if (c.level === 'warning' && c.cause)
            return t('The duty cycle of {interface} rose from {from} % to {to} %, and this client sent {share} % of the radio writes to it meanwhile.', {interface: c.cause.interface, from: c.cause.from, to: c.cause.to, share: c.cause.share_percent});
        if (c.hint?.burst) return t('Up to {n} radio writes in one minute.', {n: c.hint.peak_minute});
        if (c.hint) return t('{n} radio writes a minute on average over {m} minutes.', {n: c.hint.peak_average, m: view.thresholds.average_minutes});
        return '';
    }
    /** an interface with several radios (rfd's LAN gateways beside its module): each row names its own */
    const several = (d: RWDuty, all: RWDuty[]) => all.filter((x) => x.interface === d.interface).length > 1;
</script>

{#if view?.available}
    <SectionHead id="radio-writes" title={t('Radio writes')} help={t('Every command a program sends to a radio device through the system (setValue, putParamset on BidCos-RF and HmIP-RF) costs airtime. A program that sends too many, often in bursts, uses up the duty cycle, and the radio module then stops sending for a while. Counted per program for the last 60 minutes, in memory only; wired and virtual devices are not counted. Nothing is slowed down or refused.')} />
    {#if error}<div class="ol-notice error" data-notice="radio-writes-error">{error}</div>{/if}
    <div class="ol-card rw-panel {warnAttrs['data-warn-edge'] ?? ''}" data-radio-writes {...warnAttrs}>
        {#if view.clients.length === 0}
            <p class="ol-muted rw-none" data-radio-writes-none>{t('No program has sent a radio write in the last 60 minutes.')}</p>
        {:else}
            <div class="rw-scroll">
                <table class="ol-table rw-table">
                    <thead>
                        <tr>
                            <th>{t('Client')}</th>
                            <th class="rw-badge"></th>
                            <th class="rw-r">{t('Per minute')}</th>
                            <th class="rw-spark">{t('Last 60 minutes')}</th>
                            <th class="rw-r">{t('In the hour')}</th>
                            <th class="rw-r" title={t('The share of writes whose value was already set: they cost airtime and change nothing.')}>{t('Change nothing')}</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each view.clients as c (clientKey(c))}
                            {@const share = noopShare(c)}
                            <tr data-radio-client={clientKey(c)} data-level={c.level ?? ''}>
                                <td class="rw-name">
                                    <span class="rw-clip" title={c.label ? `${c.name} · ${c.label}` : c.name}><span class="hmm-mono">{c.name}</span>{#if c.label}<span class="ol-muted">{' · '}{c.label}</span>{/if}</span>
                                    <span class="ol-muted rw-kind">{kindWord(c)}{#if c.interfaces.length}{' · '}{c.interfaces.map((i) => i.name).join(', ')}{/if}</span>
                                </td>
                                <td class="rw-badge">
                                    {#if c.level === 'warning'}<span class="ol-badge bad" data-radio-level="warning" title={levelTitle(c)}>{t('Warning')}</span>
                                    {:else if c.level === 'hint'}<span class="ol-badge warn" data-radio-level="hint" title={levelTitle(c)}>{t('Hint')}</span>{/if}
                                </td>
                                <td class="rw-r rw-num" data-radio-rate>{c.per_minute}</td>
                                <td class="rw-spark"><Sparkline values={c.minutes} {top} threshold={view.thresholds.burst_per_minute} label={t('Radio writes of {name} per minute, last 60 minutes', {name: c.name})} tip={(i, v) => `${ago(i, c.minutes.length)}: ${v ?? 0}`} /></td>
                                <td class="rw-r rw-num" data-radio-hour>{c.hour}</td>
                                <td class="rw-r rw-num" data-radio-noop>{share === null ? '–' : `${share} %`}</td>
                            </tr>
                        {/each}
                        {#each view.duty_cycle as d (`${d.interface}/${d.address}`)}
                            {@const now = lastReading(d.minutes)}
                            <tr class="rw-duty" data-radio-duty={d.interface}>
                                <td class="rw-name">
                                    <span class="rw-clip">{t('Duty cycle')} <span class="hmm-mono">{d.interface}</span></span>
                                    {#if several(d, view.duty_cycle)}<span class="ol-muted rw-kind hmm-mono" title={d.address} data-radio-duty-address>{d.address}</span>{/if}
                                </td>
                                <td class="rw-badge"></td>
                                <td class="rw-r rw-num" data-radio-dc>{now === null ? '–' : `${now} %`}</td>
                                <td class="rw-spark"><Sparkline kind="line" values={d.minutes} top={dcTop} label={t('Duty cycle of {name}, last 60 minutes', {name: d.interface})} tip={(i, v) => `${ago(i, d.minutes.length)}: ${v === null ? '–' : `${v} %`}`} /></td>
                                <td class="rw-r"></td>
                                <td class="rw-r"></td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            </div>
            <p class="ol-muted rw-foot" data-radio-writes-foot>
                {t('The bars reach up to {n} writes in a minute; the dotted line marks {limit}.', {n: top, limit: view.thresholds.burst_per_minute})}{' '}
                {#if view.duty_cycle.length}{t('The duty cycle is drawn up to {n} %.', {n: dcTop})}{' '}{/if}
                {t('Now is at the right.')}
            </p>
        {/if}
        {#if view.classic_rpc}
            <p class="ol-muted rw-foot" data-radio-writes-classic>{t('Classic RPC is on: programs that use its ports reach the interfaces directly and are not counted here.')} <a href="/system/remote-access" use:link>{t('Remote access')}</a></p>
        {/if}
    </div>
{/if}

<style>
    .rw-panel { margin: 8px 0 14px; }
    .rw-none { margin: 2px 0 8px; }
    .rw-scroll { overflow-x: auto; max-width: 100%; }
    .rw-table { max-width: 62em; table-layout: fixed; min-width: 640px; }
    .rw-table :is(th, td) { vertical-align: middle; }
    .rw-table :is(th, td).rw-r { text-align: right; width: 7.5em; }
    .rw-table :is(th, td).rw-badge { width: 7.5em; padding-left: 0; }
    .rw-table :is(th, td).rw-spark { width: 196px; }
    .rw-badge .ol-badge { margin-left: 0; }
    .rw-num { font-variant-numeric: tabular-nums; white-space: nowrap; }
    .rw-name { min-width: 0; }
    .rw-clip, .rw-kind { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .rw-kind { font-size: var(--hmm-font-size-small); }
    .rw-duty td { border-bottom-color: transparent; }
    .rw-duty:first-of-type td, tr:not(.rw-duty) + .rw-duty td { border-top: 1px solid var(--hmm-border); }
    .rw-foot { margin: 8px 0 0; font-size: var(--hmm-font-size-small); }
</style>
