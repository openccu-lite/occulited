<script lang="ts">
    /*
     * occulited task 189 (variant B, the maintainer 2026-11-26): the Addons page's tag filter - a
     * button beside the search, "Tags: all ▾" or "Tags: 3 ▾", that opens a list with a checkbox per
     * tag (its icon in its colour, its name, how many of the cards the other filters let through
     * carry it) and All / None. The button keeps its width whatever it says: both values sit in
     * the same grid cell, the one not shown hidden. Escape closes the list and gives the focus back
     * to the button; a click or the focus outside closes it too. The page shows the checked tags as
     * chips under the search (AddonsPage.svelte), from the same filter.
     */
    import Icon from './Icon.svelte';
    import {t} from './i18n.svelte';
    import {checkedCount, isChecked, toggleFilter, type CatalogTag, type TagFilter} from './catalogtags';
    import {tagColor, tagIcon} from './tagstyle';

    interface Props {
        /** the rows, in their order */
        tags: CatalogTag[];
        filter: TagFilter;
        counts: Record<string, number>;
        label: (tag: CatalogTag) => string;
        onchange: (f: TagFilter) => void;
    }
    let {tags, filter, counts, label, onchange}: Props = $props();

    let open = $state(false);
    let shift = $state(0);
    let root: HTMLDivElement | undefined = $state();
    let button: HTMLButtonElement | undefined = $state();
    let panel: HTMLDivElement | undefined = $state();
    const ids = $derived(tags.map((x) => x.id));
    const n = $derived(checkedCount(filter, ids));
    const value = $derived(n === null ? t('all') : String(n));
    const panelId = `tf-${Math.random().toString(36).slice(2, 8)}`;

    function toggleOpen() {
        open = !open;
        shift = 0;
        if (open)
            queueMicrotask(() => {
                // on a narrow screen the list moves left until its right edge is inside the window
                const r = panel?.getBoundingClientRect();
                if (r) shift = Math.min(0, window.innerWidth - 8 - r.right);
                panel?.querySelector<HTMLElement>('input')?.focus();
            });
    }
    function close(focusButton: boolean) {
        open = false;
        if (focusButton) button?.focus();
    }
    function onKey(e: KeyboardEvent) {
        if (e.key === 'Escape' && open) {
            e.preventDefault();
            e.stopPropagation();
            close(true);
        }
    }
    function onFocusOut(e: FocusEvent) {
        const to = e.relatedTarget as Node | null;
        if (open && to && root && !root.contains(to)) close(false);
    }
    $effect(() => {
        if (!open) return;
        const onDown = (e: PointerEvent) => {
            if (root && !root.contains(e.target as Node)) close(false);
        };
        document.addEventListener('pointerdown', onDown, true);
        return () => document.removeEventListener('pointerdown', onDown, true);
    });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="tf" bind:this={root} onkeydown={onKey} onfocusout={onFocusOut} data-tag-filter>
    <button type="button" class="hmm-button tf-button" bind:this={button} aria-haspopup="true" aria-expanded={open} aria-controls={panelId} onclick={toggleOpen} data-tag-filter-button={n === null ? 'all' : n}>
        <span>{t('Tags:')}</span>
        <span class="tf-val">
            <span class="tf-sizer" aria-hidden="true">{t('all')}</span>
            <span class="tf-sizer" aria-hidden="true">88</span>
            <span class="tf-cur" data-tag-filter-value>{value}</span>
        </span>
        <span class="tf-caret" aria-hidden="true">▾</span>
    </button>
    {#if open}
        <div class="tf-panel" style:left="{shift}px" id={panelId} bind:this={panel} role="group" aria-label={t('Filter by tag')} data-tag-filter-panel>
            <div class="tf-bulk">
                <button type="button" class="tf-link" onclick={() => onchange(null)} disabled={n === null} data-tag-all>{t('All')}</button>
                <button type="button" class="tf-link" onclick={() => onchange([])} disabled={n === 0} data-tag-none>{t('None')}</button>
            </div>
            <ul class="tf-list">
                {#each tags as tg (tg.id)}
                    <li>
                        <label class="tf-row" data-tag={tg.id}>
                            <input type="checkbox" checked={isChecked(filter, tg.id)} onchange={() => onchange(toggleFilter(filter, tg.id, ids))} />
                            <span class="tf-icon" style:color={tagColor(tg.id)}><Icon name={tagIcon(tg.id)} size={14} /></span>
                            <span class="tf-name">{label(tg)}</span>
                            <span class="tf-count" data-tag-count>{counts[tg.id] ?? 0}</span>
                        </label>
                    </li>
                {/each}
            </ul>
        </div>
    {/if}
</div>

<style>
    .tf { position: relative; display: inline-block; }
    .tf-button { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
    /* both values in one cell: the button is as wide as the wider of "all" and two digits, always */
    .tf-val { display: inline-grid; text-align: left; font-variant-numeric: tabular-nums; }
    .tf-val > span { grid-area: 1 / 1; }
    .tf-sizer { visibility: hidden; }
    .tf-caret { font-size: 0.8em; color: var(--hmm-fg-muted); }
    .tf-panel {
        position: absolute; z-index: 40; top: calc(100% + 4px); left: 0; min-width: 240px; max-width: min(320px, calc(100vw - 32px));
        max-height: min(70vh, 600px); overflow-y: auto; padding: 6px 0;
        background: var(--hmm-bg); border: 1px solid var(--hmm-border); border-radius: 8px; box-shadow: var(--hmm-shadow-card-hover);
    }
    .tf-bulk { display: flex; gap: 14px; padding: 4px 12px 6px; border-bottom: 1px solid var(--hmm-border-muted); }
    .tf-link { padding: 0; border: 0; background: none; font: inherit; font-size: var(--hmm-font-size-small); color: var(--hmm-link, var(--hmm-accent)); cursor: pointer; }
    .tf-link:disabled { color: var(--hmm-fg-faint, var(--hmm-fg-muted)); cursor: default; }
    .tf-link:not(:disabled):hover { text-decoration: underline; }
    .tf-link:focus-visible { outline: 2px solid var(--hmm-accent); outline-offset: 2px; }
    .tf-list { list-style: none; margin: 0; padding: 4px 0 0; }
    .tf-row { display: flex; align-items: center; gap: 8px; min-height: 32px; padding: 2px 12px; cursor: pointer; }
    .tf-row:hover { background: var(--hmm-control-bg-hover); }
    .tf-row input { margin: 0; flex: 0 0 auto; }
    .tf-icon { display: inline-flex; flex: 0 0 auto; }
    .tf-name { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .tf-count { min-width: 2ch; text-align: right; font-variant-numeric: tabular-nums; color: var(--hmm-fg-muted); font-size: var(--hmm-font-size-small); }
</style>
