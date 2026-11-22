<script lang="ts">
    /*
     * occulited task 163: a sparkline of a fixed size - 60 one-minute values, the newest at the
     * right. Counts are bars from the baseline (a minute is a count, not a point on a curve), a
     * level such as the duty cycle a line that breaks where there was no reading. The scale is
     * the caller's (`top`), so the rows of one panel share it, and what the top means is written
     * by the caller beside the panel - a sparkline carries no axis of its own. A threshold inside
     * the scale is a dotted line. Every minute has its value under the pointer (the title of an
     * invisible column over the whole height, so a low bar is as easy to hit as a high one).
     *
     * The box never changes: the same width and height whatever the values. Without a `width` it
     * takes its container's (openccu-lite task 489: the Status page's panel gives the chart column
     * the width the table leaves), measured, so the bars keep their pixels and the box its height.
     */
    import {lineRuns} from './radiowrites';

    interface Props {
        values: (number | null)[];
        /** the value at the top edge */
        top: number;
        kind?: 'bars' | 'line';
        /** what the sparkline shows, for the screen reader */
        label: string;
        /** the text under the pointer for one entry */
        tip: (index: number, value: number | null) => string;
        /** a threshold drawn as a dotted line when it lies within the scale */
        threshold?: number;
        /** in pixels; unset, the container's width */
        width?: number;
        height?: number;
    }
    let {values, top, kind = 'bars', label, tip, threshold, width: fixed, height = 28}: Props = $props();
    let measured = $state(0);
    const width = $derived(fixed ?? measured);

    const step = $derived(values.length ? width / values.length : width);
    const PAD = 1.5;
    const y = (v: number) => height - (Math.max(0, Math.min(top, v)) / top) * (height - PAD);
    const runs = $derived(kind === 'line' ? lineRuns(values, top, width, height, PAD) : []);
    const thrY = $derived(threshold !== undefined && threshold <= top ? y(threshold) : null);
</script>

<div class="ol-spark-box" class:fluid={fixed === undefined} style:width={fixed === undefined ? undefined : `${fixed}px`} style:height={`${height}px`} bind:clientWidth={measured}>
{#if width > 0}
<svg class="ol-spark" {width} {height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label} data-spark={kind}>
    <line class="base" x1="0" x2={width} y1={height - 0.5} y2={height - 0.5} />
    {#if thrY !== null}<line class="thr" x1="0" x2={width} y1={thrY} y2={thrY} data-spark-threshold />{/if}
    {#if kind === 'bars'}
        {#each values as v, i (i)}
            {#if v}<rect class="bar" x={i * step + 0.5} y={Math.min(y(v), height - 1.5)} width={Math.max(1, step - 1)} height={height - Math.min(y(v), height - 1.5)} rx="1" data-spark-bar={v} />{/if}
        {/each}
    {:else}
        {#each runs as run, i (i)}
            {#if run.length > 1}<polyline class="ln" points={run.join(' ')} />{:else}<circle class="dot" cx={run[0]!.split(',')[0]} cy={run[0]!.split(',')[1]} r="1.5" />{/if}
        {/each}
    {/if}
    {#each values as v, i (i)}
        <rect class="hit" x={i * step} y="0" width={step} {height}><title>{tip(i, v)}</title></rect>
    {/each}
</svg>
{/if}
</div>

<style>
    .ol-spark-box { flex: 0 0 auto; }
    .ol-spark-box.fluid { width: 100%; min-width: 0; }
    .ol-spark { display: block; overflow: visible; }
    .base { stroke: var(--hmm-border-muted); stroke-width: 1; }
    .thr { stroke: var(--hmm-warn); stroke-width: 1; stroke-dasharray: 2 3; }
    .bar { fill: var(--ol-series-1); }
    .ln { fill: none; stroke: var(--ol-series-1); stroke-width: 1.5; stroke-linejoin: round; stroke-linecap: round; }
    .dot { fill: var(--ol-series-1); }
    .hit { fill: transparent; }
    .hit:hover { fill: var(--hmm-fg); fill-opacity: 0.08; }
</style>
