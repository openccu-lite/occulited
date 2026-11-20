<script lang="ts">
    /*
     * occulited task 159 (the maintainer, 2026-11-20: "would be nice if users could enable that on
     * the system-services page and would also be nice to have a viewer for that data, possibly
     * under system-storage"): System → Storage's "Disk writes". Per disk what was written since the
     * system started and in the last hour and day - always known - and, while the Services page's
     * switch "record disk writes per service" is on, which service wrote it. An SD card or eMMC
     * that is written to faster than the daemon's limit carries a hint with its top writers.
     *
     * Nothing here changes its size with the figures: the numbers are right-aligned in columns of
     * a fixed width, and the span switch keeps its three buttons.
     */
    import {onMount} from 'svelte';
    import {api} from './api';
    import {t} from './i18n.svelte';
    import {link} from './router.svelte';
    import {scrollToAnchor} from './anchor';
    import SectionHead from './SectionHead.svelte';
    import Help from './Help.svelte';
    import Icon from './Icon.svelte';
    import {formatBytes} from './netpanels';
    import {deviceModel} from './storagepanels';
    import {ratePerHour, rowsIn, serviceID, shortOf, spanParts, unitLabel, windowIn, writtenIn, type Span, type WritesDisk, type WritesUnit, type WritesView} from './storagewrites';

    let {active = () => true}: {active?: () => boolean} = $props();

    let view = $state<WritesView | null>(null);
    let error = $state('');
    let span = $state<Span>('day');
    /** the disk the table is narrowed to; '' = all */
    let disk = $state('');

    async function load() {
        try {
            view = await api.get<WritesView>('/api/system/v1/storage/writes');
            error = '';
            if (disk && !view.disks.some((d) => d.name === disk)) disk = '';
        } catch (e) {
            error = (e as Error).message;
        }
    }
    onMount(() => {
        // a link from the Services page's switch names the section
        void load().then(() => scrollToAnchor('writes'));
        const poll = setInterval(() => active() && void load(), 10000);
        return () => clearInterval(poll);
    });

    function kindWord(d: WritesDisk): string {
        switch (d.kind) {
            case 'sd': return t('SD card');
            case 'emmc': return t('eMMC');
            case 'usb': return t('USB drive');
            case 'sata': return t('SATA drive');
            case 'nvme': return t('NVMe SSD');
            case 'virtio': return t('Virtual disk');
        }
        return t('Disk');
    }
    function spanText(seconds: number): string {
        const p = spanParts(seconds);
        if (p.d > 0) return t('{d} d {h} h', {d: p.d, h: p.h});
        if (p.h > 0) return p.min > 0 ? t('{h} h {m} min', {h: p.h, m: p.min}) : t('{h} h', {h: p.h});
        if (p.min > 0) return t('{n} min', {n: p.min});
        return t('{n} s', {n: p.s});
    }
    const rate = (r: number | null) => (r === null ? '—' : t('{amount}/h', {amount: formatBytes(Math.round(r))}));
    function rowName(u: WritesUnit): string {
        return u.kind === 'kernel' ? t('Kernel and file system journal') : u.kind === 'ended' ? t('Ended runs, not attributable') : unitLabel(u.unit);
    }
    const SPANS: {id: Span; label: () => string}[] = [
        {id: 'hour', label: () => t('Last hour')},
        {id: 'day', label: () => t('Last 24 hours')},
        {id: 'all', label: () => t('Everything recorded')},
    ];
    const rows = $derived(view ? rowsIn(view.units, span, disk) : []);
    const silent = $derived(view ? view.units.filter((u) => u.kind === 'unit').length - rows.filter((u) => u.kind === 'unit').length : 0);
    /** the window the table's figures cover, where it is shorter than the span names */
    const covered = $derived.by(() => {
        if (!view || span === 'all') return null;
        const w = rows.map((u) => windowIn(u, span, disk)).reduce((m, x) => (x && (!m || x.seconds > m.seconds) ? x : m), null as ReturnType<typeof windowIn>);
        return shortOf(w, span) && w ? w.seconds : null;
    });
</script>

<SectionHead id="writes" title={t('Disk writes')} help={t('How much is written to each disk since the system started, and how fast. An SD card and an eMMC wear with every write, so a service that writes more than it needs to shortens their life. Which service writes is recorded once it is switched on under System → Services.')} />
{#if error}<div class="ol-notice error" data-notice="writes-error">{error}</div>{/if}
{#if view}
    {#if view.disks.length === 0}
        <p class="ol-muted" data-writes-empty>{t('No disk is being counted.')}</p>
    {:else}
        <div class="sw-disks">
            {#each view.disks as d (d.name)}
                <section class="ol-card sw-disk" data-writes-disk={d.name}>
                    <div class="ol-card-head">
                        <span class="ol-card-icon"><Icon name="disk" size={14} /></span>
                        <div class="ol-card-titles">
                            <h3 class="ol-card-title">{kindWord(d)}</h3>
                            <div class="ol-card-sub">{[deviceModel(d), d.name].filter(Boolean).join(' · ')}</div>
                        </div>
                    </div>
                    {#if d.hint}
                        <div class="ol-notice warn sw-hint" data-writes-hint>
                            {t('On average {rate} are written here - a lot for a medium that wears with every write.', {rate: rate(d.hint.bytes_per_hour)})}
                            {#if d.hint.top_units.length}
                                {' '}{t('Most of it by: {units}.', {units: d.hint.top_units.map((n) => rowName(view!.units.find((u) => u.unit === n) ?? {unit: n, kind: 'unit'} as WritesUnit)).join(', ')})}
                            {:else if view.recording.supported && !view.recording.enabled}
                                {' '}{t('Which service writes it is shown once the recording is switched on:')} <a href="/system/services#io-accounting" use:link>{t('System → Services')}</a>
                            {/if}
                        </div>
                    {/if}
                    <dl class="ol-fields sw-fields">
                        <dt class="ol-field-label">{t('Since the start')}</dt>
                        <dd class="ol-field-value" data-writes-total><span class="sw-num">{formatBytes(d.written_bytes)}</span> <span class="ol-muted">{t('in {span}', {span: spanText(view.uptime_s)})}</span></dd>
                        <dt class="ol-field-label">{t('Last 24 hours')}</dt>
                        <dd class="ol-field-value" data-writes-day><span class="sw-num">{formatBytes(d.day.written_bytes)}</span> <span class="ol-muted">{rate(ratePerHour(d.day))}{shortOf(d.day, 'day') && d.day.seconds >= 60 ? ` · ${t('over {span}', {span: spanText(d.day.seconds)})}` : ''}</span></dd>
                        <dt class="ol-field-label">{t('Last hour')}</dt>
                        <dd class="ol-field-value" data-writes-hour><span class="sw-num">{formatBytes(d.hour.written_bytes)}</span> <span class="ol-muted">{rate(ratePerHour(d.hour))}{shortOf(d.hour, 'hour') && d.hour.seconds >= 60 ? ` · ${t('over {span}', {span: spanText(d.hour.seconds)})}` : ''}</span></dd>
                        {#each d.partitions.filter((p) => p.written_bytes > 0) as p (p.name)}
                            <dt class="ol-field-label hmm-mono">{p.name}</dt>
                            <dd class="ol-field-value" data-writes-part={p.name}><span class="sw-num">{formatBytes(p.written_bytes)}</span> <span class="ol-muted">{p.mount ? t('mounted at {path}', {path: p.mount}) : t('not mounted')}</span></dd>
                        {/each}
                    </dl>
                </section>
            {/each}
        </div>
    {/if}

    <SectionHead level={3} id="writes-services" title={t('By service')} help={t('What each service wrote, counted by the kernel per service. A service counts from its start, or from the moment the recording was switched on; a restart does not reset its sum. Runs that ended between two readings (every {n} seconds) cannot be attributed and are listed as ended runs; the kernel\'s own writes are mostly the file systems\' journals.', {n: view.interval_s || 10})} />
    {#if !view.recording.supported}
        <p class="ol-muted" data-writes-state="unsupported">{t('This system cannot record disk writes per service: its kernel does not offer the io controller.')}</p>
    {:else if !view.recording.enabled}
        <p class="ol-muted" data-writes-state="off">{t('Disk writes per service are not recorded.')} <a href="/system/services#io-accounting" use:link>{t('Switch it on under System → Services')}</a></p>
    {:else if !view.recording.active}
        <p class="ol-muted" data-writes-state="starting">{t('The recording is switched on; the first figures follow in a moment.')}</p>
    {:else}
        <div class="ol-toolbar sw-bar">
            <div class="ol-seg" role="group" aria-label={t('Time span')}>
                {#each SPANS as s (s.id)}
                    <button type="button" aria-pressed={span === s.id} onclick={() => (span = s.id)} data-writes-span={s.id}>{s.label()}</button>
                {/each}
            </div>
            {#if view.disks.length > 1}
                <div class="ol-seg" role="group" aria-label={t('Disk')}>
                    <button type="button" aria-pressed={disk === ''} onclick={() => (disk = '')} data-writes-diskpick="">{t('All disks')}</button>
                    {#each view.disks as d (d.name)}
                        <button type="button" aria-pressed={disk === d.name} onclick={() => (disk = d.name)} data-writes-diskpick={d.name}>{d.name}</button>
                    {/each}
                </div>
            {/if}
        </div>
        {#if rows.length === 0}
            <p class="ol-muted" data-writes-none>{t('Nothing was written in this time.')}</p>
        {:else}
            <table class="ol-table ol-stack sw-table" data-writes-table>
                <thead><tr><th>{t('Service')}</th><th class="sw-r">{t('Written')}</th><th class="sw-r">{t('Per hour')}</th>{#if !disk && view.disks.length > 1}<th>{t('Disks')}</th>{/if}</tr></thead>
                <tbody>
                    {#each rows as u (u.unit)}
                        {@const id = serviceID(u)}
                        {@const w = windowIn(u, span, disk)}
                        <tr data-writes-unit={u.unit}>
                            <td class="sw-name ol-stack-line">
                                {#if id && u.running}<a href={`/system/services#service-${id}`} use:link title={u.unit}>{rowName(u)}</a>{:else}<span title={u.kind === 'unit' ? u.unit : undefined}>{rowName(u)}</span>{/if}
                                {#if u.kind === 'unit' && !u.running}<span class="ol-muted">{' · '}{t('not running')}</span>{/if}
                            </td>
                            <td class="sw-r sw-num" data-label={t('Written')}>{formatBytes(writtenIn(u, span, disk))}</td>
                            <td class="sw-r sw-num" data-label={t('Per hour')}>{rate(w ? ratePerHour(w) : null)}</td>
                            {#if !disk && view.disks.length > 1}
                                <td class="sw-split ol-muted" data-label={t('Disks')}>{u.disks.filter((x) => writtenIn(u, span, x.disk) > 0).map((x) => `${x.disk} ${formatBytes(writtenIn(u, span, x.disk))}`).join(' · ')}</td>
                            {/if}
                        </tr>
                    {/each}
                </tbody>
            </table>
        {/if}
        <p class="ol-muted sw-foot" data-writes-foot>
            {#if covered !== null}{t('The history covers {span} so far.', {span: spanText(covered)})}{' '}{/if}
            {#if silent > 0}{t('{n} more services wrote nothing in this time.', {n: silent})}{' '}{/if}
            {#if view.recording.since}{t('Recorded for {span}.', {span: spanText((Date.parse(view.at) - Date.parse(view.recording.since)) / 1000)})}{/if}
        </p>
    {/if}
{/if}

<style>
    .sw-disks { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(360px, 100%), 1fr)); gap: 12px; margin: 8px 0 14px; }
    .sw-disk { display: flex; flex-direction: column; min-width: 0; }
    .sw-hint { border-left: 3px solid var(--hmm-warn); margin: 8px 0; }
    .sw-fields { margin: 4px 0 0; }
    .sw-num { font-variant-numeric: tabular-nums; white-space: nowrap; }
    .sw-fields .sw-num { display: inline-block; min-width: 6.5em; }
    .sw-bar { margin-top: 6px; }
    /* the chosen span in the accent colour, not in bold: the buttons keep their width */
    .sw-bar .ol-seg > button[aria-pressed='true'] { color: var(--hmm-accent); background: var(--hmm-accent-bg); }
    .sw-table { max-width: 60em; }
    .sw-table :is(th, td).sw-r { text-align: right; width: 8em; }
    .sw-name { overflow-wrap: anywhere; }
    .sw-split { overflow-wrap: anywhere; }
    .sw-foot { margin-top: 8px; }
    @media (max-width: 700px) {
        .sw-table :is(th, td).sw-r { text-align: left; width: auto; }
        .ol-seg { flex-wrap: wrap; }
    }
</style>
