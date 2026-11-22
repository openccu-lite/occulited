<script lang="ts">
    /*
     * occulited task 148 (the maintainer, 2026-11-15: "a panel on the status page that shows
     * temperatures on pi3/4 ... if the hardware gives more than one temperature reading show them
     * in concentric circles like in the disk usage panel"). The storage panel's geometry: a ring per
     * reading, concentric, the processor outermost, each in its own hue of the --ol-series tokens,
     * and beside them a legend that says each reading in words and figures, so nothing depends on
     * a colour. A ring runs from 0 °C to the reading's maximum (a Pi's 85 °C, where its firmware
     * throttles hard - not the 110 °C trip); the edge goes amber from the warning level (80 °C on a
     * Pi), red from the maximum, and the Status warnings soc-hot and disk-hot say the same above.
     *
     * On a Raspberry Pi the head carries the firmware's state as a pill - normal, throttled earlier,
     * throttled, undervoltage - whose box fits the longest word in either language, so a change
     * moves nothing; the line under the rings names the conditions, now and since the boot, in one
     * line of a fixed height. The foot is the processor's temperature over time, the same span as
     * every chart of the page.
     */
    import Icon from './Icon.svelte';
    import HistoryChart from './HistoryChart.svelte';
    import {i18n, t} from './i18n.svelte';
    import {MAX_TEMP_RINGS, TEMP_STEPS, bootDip, formatCelsius, formatSeconds, formatLimit, panelLevel, ringFraction, tempColours, tempLevel, throttleFacts, throttleState, type Thermal, type ThrottleFlag, type ThrottleState, type TempReading} from './thermal';
    import {clock, spanMax, type Sample} from './history';
    import {panelEdge} from './storage';

    interface Props {
        thermal: Thermal;
        /** the processor's temperature over time (GET /status/history's soc_c) */
        history?: Sample[];
        /** the span the page's charts show, in minutes, and its setter */
        span: number;
        setSpan: (m: number) => void;
        /** the warnings' edge (soc-hot, disk-hot) */
        warnAttrs?: Record<string, string | undefined>;
    }
    let {thermal, history = [], span, setSpan, warnAttrs = {}}: Props = $props();

    const lang = $derived(i18n.language);
    const readings = $derived(thermal.readings);
    const colours = $derived(tempColours(readings));
    const level = $derived(panelLevel(readings));
    // the edge: the worse of the readings' level and the warnings that lead here
    const edge = $derived(panelEdge(level, warnAttrs['data-warn-edge']));
    const th = $derived(thermal.throttle);
    const state = $derived(th ? throttleState(th) : undefined);
    const facts = $derived(th ? throttleFacts(th) : []);
    const peak = $derived(spanMax(history, span));
    const soc = $derived(readings.find((r) => r.kind === 'soc'));

    function kindWord(k?: string): string {
        switch (k) {
            case 'usb': return t('USB drive');
            case 'sata': return t('SATA drive');
            case 'nvme': return t('NVMe SSD');
        }
        return t('Disk');
    }
    function nameOf(r: TempReading): string {
        if (r.kind === 'soc') return t('Processor');
        if (r.kind === 'disk') return r.name || kindWord(r.disk_kind);
        return r.name;
    }
    function titleOf(r: TempReading): string {
        const what = r.kind === 'disk' ? [kindWord(r.disk_kind), r.name, r.device ? `(${r.device})` : ''].filter(Boolean).join(' ') : r.kind === 'soc' ? `${t('Processor')} (${r.name})` : r.name;
        const limits = t('hot from {warn}, limit {max}', {warn: formatLimit(r.warn_c, lang), max: formatLimit(r.max_c, lang)});
        const read = r.read_at ? t('read at {time}', {time: clock(new Date(r.read_at), lang)}) : '';
        return [`${what}: ${formatCelsius(r.celsius, lang)}`, limits, read].filter(Boolean).join(' · ');
    }
    const stateWord = (s: ThrottleState) =>
        s === 'undervoltage' ? t('undervoltage') : s === 'throttled' ? t('throttled') : s === 'earlier' ? t('throttled earlier') : t('normal');
    const stateClass = (s: ThrottleState) => (s === 'undervoltage' ? 'bad' : s === 'throttled' || s === 'earlier' ? 'warn' : 'good');
    function flagWord(f: ThrottleFlag): string {
        switch (f) {
            case 'under_voltage': return t('Undervoltage');
            case 'throttled': return t('Throttled');
            case 'freq_capped': return t('Clock capped');
            case 'soft_temp_limit': return t('Soft temperature limit');
        }
    }
    // B-162: the kernel's episodes name how often and when; the last one on the clock
    const lastAt = $derived(th?.last?.start ? clock(new Date(th.last.start), lang) : '');
    function factText(f: {flag: ThrottleFlag; now: boolean}): string {
        if (f.now) return t('{what} now', {what: flagWord(f.flag)});
        if (f.flag === 'under_voltage' && th?.events && lastAt) return t('{what} since the boot ({n}×, last at {time})', {what: flagWord(f.flag), n: th.events, time: lastAt});
        return t('{what} since the boot', {what: flagWord(f.flag)});
    }
    // B-162: a short undervoltage in the boot phase that normalised - an info line, no warning
    const dip = $derived(th ? bootDip(th) : null);
    const dipText = $derived(
        dip
            ? dip.n === 1
                ? t('Brief undervoltage at start-up ({time}, normalised)', {time: formatSeconds(dip.seconds, lang)})
                : t('{n} brief undervoltages at start-up ({time}, normalised)', {n: dip.n, time: formatSeconds(dip.seconds, lang)})
            : '',
    );
    const episodesLine = $derived(
        th?.events
            ? t('Undervoltage episodes since the boot: {n}, {time} together', {n: th.events, time: formatSeconds(th.undervoltage_s ?? 0, lang)}) +
                  (lastAt ? ' · ' + t('the last at {time}', {time: lastAt}) : '')
            : '',
    );
    const quiet = $derived(th?.source === 'hwmon' ? t('No undervoltage') : t('No undervoltage, no throttling since the boot'));
    const throttleTitle = $derived(
        [
            facts.length ? facts.map(factText).join(' · ') : dip ? `${dipText}: ${t('the supply dipped in the load peak of the start and recovered at once; no warning')}` : quiet,
            episodesLine,
            t("The Raspberry Pi's firmware slows the processor down when it gets hot and when the supply voltage drops; both are a common cause of radio trouble."),
        ]
            .filter(Boolean)
            .join('\n'),
    );

    // the rings on a 100 × 100 grid: 8 wide with a 2.5 gap, the outermost at r 45 (the storage panel's)
    const STROKE = 8;
    const radius = (i: number) => 45 - i * (STROKE + 2.5);
    const dash = (r: TempReading, i: number) => {
        const c = 2 * Math.PI * radius(i);
        return `${(ringFraction(r) * c).toFixed(2)} ${c.toFixed(2)}`;
    };
    const fmt = (v: number) => formatCelsius(v, lang);
    // the chart writes its unit after the figure itself
    const fmtNum = (v: number) => new Intl.NumberFormat(lang === 'de' ? 'de-DE' : 'en-US', {maximumFractionDigits: 1}).format(v);
    const spanName = (m: number) => t('{n} h', {n: m / 60});
</script>

<div class="ol-card ol-temp-panel" class:warn={edge === 'warn'} class:err={edge === 'error'} id="temperature" data-temp-panel data-level={level} {...warnAttrs}>
    <div class="ol-card-head">
        <span class="ol-card-icon"><Icon name="thermometer" size={14} /></span>
        <div class="ol-card-titles">
            <div class="ol-card-title">{t('Temperature')}</div>
        </div>
        {#if th && state}
            <!-- the box fits the longer words (in its pseudo-elements, unseen), so a change of the state moves nothing -->
            <span class="ol-badge tp-pill {stateClass(state)}" data-throttle={state} title={throttleTitle} data-w1={stateWord('earlier')} data-w2={stateWord('undervoltage')}><span class="tp-pill-word">{stateWord(state)}</span></span>
        {/if}
    </div>
    <div class="tp-row">
        <svg class="tp-rings" viewBox="0 0 100 100" aria-hidden="true">
            {#each readings.slice(0, MAX_TEMP_RINGS) as r, i (r.id)}
                <circle class="track" cx="50" cy="50" r={radius(i)} style:stroke={colours[i]} />
                <circle class="arc" data-ring={r.id} cx="50" cy="50" r={radius(i)} style:stroke={colours[i]} stroke-dasharray={dash(r, i)} stroke-linecap="round" transform="rotate(-90 50 50)" />
            {/each}
        </svg>
        <ul class="tp-legend">
            {#each readings as r, i (r.id)}
                {@const l = tempLevel(r)}
                <li data-reading={r.id} data-kind={r.kind} title={titleOf(r)}>
                    <span class="sw" style:background={i < MAX_TEMP_RINGS ? colours[i] : 'transparent'} aria-hidden="true"></span>
                    <span class="tp-name">{nameOf(r)}</span>
                    <span class="tp-val" class:warn={l === 'warn'} class:err={l === 'error'} data-celsius>{fmt(r.celsius)}</span>
                </li>
            {/each}
        </ul>
    </div>
    {#if th}
        <!-- one line of a fixed height whatever it holds: the conditions, or that there are none -->
        <div class="tp-throttle" data-throttle-facts={facts.length} title={throttleTitle}>
            {#if facts.length}
                {#each facts as f (f.flag)}
                    <span class="tp-fact" class:now={f.now} class:err={f.now && f.flag === 'under_voltage'} data-fact={f.flag} data-now={f.now}>{factText(f)}</span>
                {/each}
            {:else if dip}
                <span class="tp-fact info" data-fact="boot_dip">{dipText}</span>
            {:else}
                <span class="ol-muted tp-quiet">{quiet}</span>
            {/if}
        </div>
    {/if}
    {#if soc && history.length}
        <div class="ol-card-foot">
            {#if peak !== null}<div class="ol-muted tp-peak" data-temp-peak>{t('Processor: max {temp} ({span})', {temp: fmt(peak), span: spanName(span)})}</div>{/if}
            <HistoryChart samples={history} label={t('processor temperature history')} unit="°C" steps={TEMP_STEPS} format={fmtNum} floor={soc.max_c} threshold={{value: soc.warn_c, label: formatLimit(soc.warn_c, lang), above: t('Throttling from {temp}', {temp: formatLimit(soc.warn_c, lang)})}} bind:span={() => span, setSpan} />
        </div>
    {/if}
</div>

<style>
    .ol-temp-panel { display: flex; flex-direction: column; gap: 10px; scroll-margin-top: 72px; }
    /* the pill: its word and, unseen in the same cell, the longer ones - the widest sets the box */
    .tp-pill { flex: 0 0 auto; margin-left: auto; display: inline-grid; padding: 1px 9px; border-radius: 999px; font-size: var(--hmm-font-size-small); font-weight: 600; text-align: center; }
    .tp-pill::before { content: attr(data-w1); }
    .tp-pill::after { content: attr(data-w2); }
    .tp-pill::before, .tp-pill::after { grid-area: 1 / 1; visibility: hidden; white-space: nowrap; }
    .tp-pill-word { grid-area: 1 / 1; white-space: nowrap; }
    .tp-row { display: flex; flex-wrap: wrap; align-items: center; gap: 10px 14px; }
    .tp-rings { flex: 0 0 auto; width: 88px; height: 88px; display: block; }
    .track { fill: none; stroke-width: 8; opacity: 0.18; }
    .arc { fill: none; stroke-width: 8; }
    .tp-legend { flex: 1 1 150px; min-width: 0; list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 4px 8px; align-items: baseline; font-size: var(--hmm-font-size-small); }
    .tp-legend li { display: contents; }
    .sw { width: 10px; height: 10px; border-radius: 50%; align-self: center; }
    .tp-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--hmm-fg); }
    /* room for "100,0 °C" whatever the figure, so a change of the value never moves the row; the
       level is the colour alone - a bolder weight would widen the figure */
    .tp-val { white-space: nowrap; text-align: right; min-width: 8.5ch; color: var(--hmm-fg); font-variant-numeric: tabular-nums; }
    .tp-val.warn { color: var(--hmm-warn); }
    .tp-val.err { color: var(--hmm-error); }
    /* the throttle's line: always one line high, cut with the tooltip when it holds more */
    .tp-throttle { display: flex; gap: 6px; align-items: center; min-width: 0; height: 1.6em; overflow: hidden; white-space: nowrap; font-size: var(--hmm-font-size-small); }
    .tp-quiet { min-width: 0; overflow: hidden; text-overflow: ellipsis; }
    .tp-fact { flex: 0 0 auto; display: inline-flex; align-items: center; box-sizing: border-box; height: 1.6em; padding: 0 8px; line-height: 1; border-radius: 999px; border: 1px solid var(--hmm-border-muted); color: var(--hmm-fg-muted); }
    /* B-162: the boot dip says what happened and stays quiet - the muted outline, cut like the quiet line */
    .tp-fact.info { flex: 0 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; display: block; line-height: calc(1.6em - 2px); }
    .tp-fact.now { border-color: var(--hmm-warn); color: var(--hmm-warn); }
    .tp-fact.err { border-color: var(--hmm-error); color: var(--hmm-error); font-weight: 600; }
    .tp-peak { font-size: var(--hmm-font-size-small); margin-bottom: 2px; }
</style>
