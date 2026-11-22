<script lang="ts">
    /*
     * occulited task 163 (the maintainer, 2026-11-21: users keep running into a program that sends
     * too many setValue/putParamset calls, often in bursts, and drives the duty cycle up - "can we
     * show how many of them per minute an external client causes on lite-rpc? With a sparkline and
     * a warning on the Status page when it gets too much?"): the Status page's panel "Radio
     * writes". One row per client that sent a radio write in the span - its rate now, the span as
     * bars, the span's sum and the share of writes that changed nothing - and below them, on the
     * same time axis, the duty cycle of each radio, so a burst and the rise it caused stand one
     * above the other.
     *
     * openccu-lite task 489 (the maintainer, 2026-11-22, from his system on rc.1): the chart column
     * takes the width the other columns leave; the spans 1 h, 6 h and 24 h of the page's other
     * charts (one span for the page, viewstate) - 60 minutes, 72 five-minute or 96 quarter-hour
     * bars; one duty-cycle row per physical radio, as the Duty cycle panel above has it (BidCos-RF
     * and HmIP-RF on one module through multimacd are one row); and no caption under the table:
     * the scale stands in the chart column's head, the time under the charts with the span
     * switch between start and now, the rest in the ? of the heading.
     *
     * The clients' bars share one scale, so a quiet client is low beside a busy one; the duty cycle
     * has its own row and its own scale, never a second axis in the same plot. Hint and warning are
     * a badge with a word in a column of a fixed width, the same words as the Status warnings above.
     *
     * Nothing here changes its size with the figures or the span: the numbers are right-aligned in
     * columns of a fixed width, the table's layout is fixed, every sparkline is the same box, and
     * the badge's column is there without one.
     */
    import {onMount} from 'svelte';
    import {api} from './api';
    import {i18n, t} from './i18n.svelte';
    import {link} from './router.svelte';
    import {scrollToAnchor} from './anchor';
    import {clock, timeLabels} from './history';
    import {SPANS} from './viewspan';
    import SectionHead from './SectionHead.svelte';
    import Sparkline from './Sparkline.svelte';
    import {bucketTimes, clientKey, dutyTop, lastReading, minutesAgo, spanNoopShare, writesTop, type RWClient, type RWDuty, type RWView} from './radiowrites';

    let {active = () => true, span = 60, setSpan = () => {}, warnAttrs = {}}: {active?: () => boolean; span?: number; setSpan?: (m: number) => void; warnAttrs?: Record<string, string | undefined>} = $props();

    let view = $state<RWView | null>(null);
    let error = $state('');

    async function load(s: number) {
        try {
            const v = await api.get<RWView>(`/api/system/v1/radio/writes?span=${s}`);
            // an answer for a span left meanwhile is not shown
            if (s !== span) return;
            view = v;
            error = '';
        } catch (e) {
            error = (e as Error).message;
        }
    }
    let first = true;
    $effect(() => {
        const s = span;
        void load(s).then(() => {
            // a warning's link and the token list's badge name the section
            if (first) scrollToAnchor('radio-writes');
            first = false;
        });
    });
    onMount(() => {
        const poll = setInterval(() => active() && void load(span), 10000);
        return () => clearInterval(poll);
    });

    const bm = $derived(view?.bucket_minutes ?? 1);
    const top = $derived(view ? writesTop(view.clients.map((c) => ({minutes: c.buckets}))) : 10);
    const dcTop = $derived(view ? dutyTop(view.duty_cycle) : 25);
    const spanName = (m: number) => t('{n} h', {n: m / 60});
    // the time under the charts: the first bucket's start, then now
    const axis = $derived.by(() => {
        if (!view) return null;
        const n = view.span / bm;
        return timeLabels(bucketTimes(view.at, bm, 0, n).start, new Date(view.at), i18n.language);
    });

    // under the pointer: a minute as before ("9 minutes ago"), a longer bucket as its times
    function when(i: number, n: number): string {
        if (bm === 1) {
            const m = minutesAgo(i, n);
            return m === 0 ? t('this minute') : m === 1 ? t('1 minute ago') : t('{n} minutes ago', {n: m});
        }
        const b = bucketTimes(view!.at, bm, i, n);
        return `${clock(b.start, i18n.language)}–${clock(b.end, i18n.language)}`;
    }
    const perBucket = $derived(bm === 1 ? t('Writes per minute') : t('Writes per {n} minutes', {n: bm}));
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
    // a radio by its name (the module), else by its interface; the interfaces it serves beneath
    const dutyName = (d: RWDuty) => d.name || d.interface;
    const dutyIfs = (d: RWDuty) => (d.interfaces?.length ? d.interfaces : [d.interface]);
    /** two radios of one name (rfd's LAN gateways beside its module): each row names its address */
    const several = (d: RWDuty, all: RWDuty[]) => all.filter((x) => dutyName(x) === dutyName(d)).length > 1;
</script>

{#snippet spanSwitch()}
    <span class="rw-spans" role="group" aria-label={t('Span')} data-radio-spans>
        {#each SPANS as m (m)}
            <button type="button" class:on={m === span} onclick={() => setSpan(m)} aria-pressed={m === span} data-radio-span={m}>{spanName(m)}</button>
        {/each}
    </span>
{/snippet}

{#if view?.available}
    <SectionHead id="radio-writes" title={t('Radio writes')} help={t('Every command a program sends to a radio device through the system (setValue, putParamset on BidCos-RF and HmIP-RF) costs airtime. A program that sends too many, often in bursts, uses up the duty cycle, and the radio module then stops sending for a while. Counted per program; wired and virtual devices are not counted. Nothing is slowed down or refused. The bars of all programs share one scale, written above them; in the 1 h view a dotted line marks 60 writes in a minute. The duty cycle of each radio is drawn on a scale of its own, 25 % or more. Now is at the right. The last 24 hours are kept in the system\'s history database, the last hour by the minute in memory only.')} />
    {#if error}<div class="ol-notice error" data-notice="radio-writes-error">{error}</div>{/if}
    <div class="ol-card rw-panel {warnAttrs['data-warn-edge'] ?? ''}" data-radio-writes data-span={view.span} {...warnAttrs}>
        <!-- occulited B-165: the table stands in every state - without a write the line that says so
             takes a client row's place and height, the radios' duty cycle stays below it, and the
             span switch keeps its place under the chart column -->
        <div class="rw-scroll">
            <table class="ol-table rw-table">
                <thead>
                    <tr>
                        <th class="rw-name">{t('Client')}</th>
                        <th class="rw-badge"></th>
                        <th class="rw-r">{t('Per minute')}</th>
                        <th class="rw-spark"><span class="rw-spark-head"><span class="rw-clip">{perBucket}</span><span class="ol-muted rw-scale" data-radio-scale title={t('The bars reach up to {n} writes.', {n: top})}>0–{top}</span></span></th>
                        <th class="rw-r" data-radio-sum-head>{t('In {span}', {span: spanName(view.span)})}</th>
                        <th class="rw-r rw-noop" title={t('The share of writes whose value was already set: they cost airtime and change nothing.')}>{t('Change nothing')}</th>
                    </tr>
                </thead>
                <tbody>
                    {#if view.clients.length === 0}
                        <tr class="rw-empty" data-radio-writes-empty>
                            <td colspan="6">
                                <div class="rw-empty-cell">
                                    <p class="ol-muted rw-empty-text" data-radio-writes-none>{t('No program has sent a radio write in the last {span}.', {span: spanName(view.span)})}</p>
                                </div>
                            </td>
                        </tr>
                    {/if}
                    {#each view.clients as c (clientKey(c))}
                        {@const share = spanNoopShare(c)}
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
                            <td class="rw-spark"><Sparkline values={c.buckets} {top} threshold={bm === 1 ? view.thresholds.burst_per_minute : undefined} label={t('Radio writes of {name}, last {span}', {name: c.name, span: spanName(view.span)})} tip={(i, v) => `${when(i, c.buckets.length)}: ${v ?? 0}`} /></td>
                            <td class="rw-r rw-num" data-radio-hour>{c.writes}</td>
                            <td class="rw-r rw-num" data-radio-noop>{share === null ? '–' : `${share} %`}</td>
                        </tr>
                    {/each}
                    {#each view.duty_cycle as d (`${d.radio || d.interface}/${d.address}`)}
                        {@const now = lastReading(d.minutes)}
                        <tr class="rw-duty" data-radio-duty={dutyIfs(d).join(',')}>
                            <td class="rw-name">
                                <span class="rw-clip hmm-mono" title={dutyName(d)} data-radio-duty-name>{dutyName(d)}{#if several(d, view.duty_cycle)}<span class="ol-muted">{' · '}</span><span title={d.address} data-radio-duty-address>{d.address}</span>{/if}</span>
                                <span class="ol-muted rw-kind" title={`${t('Duty cycle')} · ${dutyIfs(d).join(', ')}`}>{t('Duty cycle')}{' · '}<span data-radio-duty-ifs>{dutyIfs(d).join(', ')}</span></span>
                            </td>
                            <td class="rw-badge"></td>
                            <td class="rw-r rw-num" data-radio-dc>{now === null ? '–' : `${now} %`}</td>
                            <td class="rw-spark"><Sparkline kind="line" values={d.buckets} top={dcTop} label={t('Duty cycle of {name}, last {span}', {name: dutyName(d), span: spanName(view.span)})} tip={(i, v) => `${when(i, d.buckets.length)}: ${v === null ? '–' : `${v} %`}`} /></td>
                            <td class="rw-r rw-num ol-muted" data-radio-dc-top title={t('The duty cycle is drawn up to {n} %.', {n: dcTop})}>0–{dcTop} %</td>
                            <td class="rw-r"></td>
                        </tr>
                    {/each}
                </tbody>
                <tfoot>
                    <tr class="rw-axis">
                        <td class="rw-name"></td>
                        <td class="rw-badge"></td>
                        <td class="rw-r"></td>
                        <td class="rw-spark">
                            <div class="rw-axis-row" data-radio-axis>
                                <span class="rw-time">{axis?.start ?? ''}</span>
                                {@render spanSwitch()}
                                <span class="rw-time" data-radio-now>{t('now')}</span>
                            </div>
                        </td>
                        <td class="rw-r"></td>
                        <td class="rw-r"></td>
                    </tr>
                </tfoot>
            </table>
        </div>
        {#if view.classic_rpc}
            <p class="ol-muted rw-foot" data-radio-writes-classic>{t('Classic RPC is on: programs that use its ports reach the interfaces directly and are not counted here.')} <a href="/system/remote-access" use:link>{t('Remote access')}</a></p>
        {/if}
    </div>
{/if}

<style>
    .rw-panel { margin: 8px 0 14px; }
    /* B-165: the line without a write in a client row's place and height; it stays in sight while a
       phone scrolls the wide table sideways */
    /* the height of a client row's tallest cell, the sparkline's box (Sparkline's default height) */
    .rw-empty-cell { display: flex; align-items: center; min-height: 28px; position: sticky; left: 0; width: fit-content; max-width: min(100%, calc(100vw - 64px)); }
    .rw-empty-text { margin: 0; }
    .rw-scroll { overflow-x: auto; max-width: 100%; }
    /* openccu-lite task 489: the whole panel's width; the chart column takes what the others leave */
    .rw-table { width: 100%; table-layout: fixed; min-width: 920px; }
    .rw-table :is(th, td) { vertical-align: middle; }
    .rw-table th { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .rw-table :is(th, td).rw-name { width: 16.5em; }
    .rw-table :is(th, td).rw-r { text-align: right; width: 8em; }
    .rw-table :is(th, td).rw-noop { width: 10em; }
    .rw-table :is(th, td).rw-badge { width: 6.5em; padding-left: 0; }
    .rw-table :is(th, td).rw-spark { width: auto; }
    .rw-spark-head { display: flex; justify-content: space-between; gap: 8px; min-width: 0; }
    .rw-scale { font-weight: normal; font-size: var(--hmm-font-size-small); font-variant-numeric: tabular-nums; }
    .rw-badge .ol-badge { margin-left: 0; }
    .rw-num { font-variant-numeric: tabular-nums; white-space: nowrap; }
    .rw-name { min-width: 0; }
    .rw-clip, .rw-kind { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .rw-kind { font-size: var(--hmm-font-size-small); }
    [data-radio-dc-top] { font-size: var(--hmm-font-size-small); }
    .rw-duty td { border-bottom-color: transparent; }
    .rw-duty:first-of-type td, tr:not(.rw-duty) + .rw-duty td { border-top: 1px solid var(--hmm-border); }
    .rw-axis td { border-bottom: 0; padding-top: 2px; }
    /* B-165: the switch in the middle whatever the start time's width (a weekday in 24 h), so it does not move */
    .rw-axis-row { display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; gap: 8px; font-size: var(--hmm-font-size-small); color: var(--hmm-fg-muted); }
    .rw-axis-row > :last-child { justify-self: end; }
    .rw-time { font-variant-numeric: tabular-nums; white-space: nowrap; }
    /* the span switch as the history charts' (HistoryChart.svelte) */
    .rw-spans { display: inline-flex; gap: 2px; font-size: var(--hmm-font-size-small); }
    .rw-spans button {
        border: 0; background: none; color: var(--hmm-fg-muted); font: inherit; cursor: pointer;
        padding: 0 5px; border-radius: var(--hmm-radius); line-height: 16px;
    }
    .rw-spans button:hover { color: var(--hmm-fg); background: var(--hmm-control-bg-hover); }
    .rw-spans button.on { color: var(--hmm-accent); background: var(--hmm-accent-bg); }
    .rw-foot { margin: 8px 0 0; font-size: var(--hmm-font-size-small); }
</style>
