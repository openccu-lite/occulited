<script lang="ts">
    /*
     * The assignment sheet (task 193; the maintainer, 2026-09-22): a very-long press on a card
     * opens it - the channel's name, and its rooms, functions and favorite as large check rows,
     * written to the metadata store at once. Same look as the channel sheet, grown out of the
     * card; everything sized for a thumb. occulited task 73: Control does not rename the channel -
     * the metadata name is shown read-only, beside it the account's own display name, shown only in
     * Control and only to this account.
     */
    import {onMount} from 'svelte';
    import ModalDialog from '../ModalDialog.svelte';
    import Icon from '../Icon.svelte';
    import {api, type MetaSnapshot, type MetaNode} from '../api';
    import {t} from '../i18n.svelte';
    import Help from '../Help.svelte';
    import {displayNames, setDisplayName} from './displaynames.svelte';
    import {chosenIcon, chosenIcons, CONTACT_ICONS, contactDefault, CONTROL_NS, KEY_DEFAULT, KEY_ICONS, SWITCH_ICONS, SWITCH_STATES, switchIconId, switchStateDefault, type ContactIcons, type ContactState, type IconState} from './stateicons';
    import {humanName} from './knxall';
    import {knxSVG} from './stateicons';
    import IconPicker from './IconPicker.svelte';
    import {controlSettings, formatNumber, parseNumber, withSetting} from './variables';
    import {i18n} from '../i18n.svelte';

    // layer: above the device sheet when opened from its gear (occulited task 96), so closing it returns there
    // iconOf (occulited task 99, 105, 106): the channel kinds with the "Symbol" picker - a switch (its shown name and
    // device model decide the automatic icon), a key, or a contact (one icon per state: closed, open, and tilted for
    // a three-state sensor; `window` and `opener` decide the automatic ones); other kinds have none.
    // occulited task 176: a system variable or a program (kind 'object') - one icon, any of the set, the automatic one
    // by its kind; and its settings: read-only in Control, and a number's step
    let {ref, snap, favoritesPath, from = null, layer = 90, iconOf, onclose, onchanged}: {ref: string; snap: MetaSnapshot; favoritesPath: string; from?: DOMRect | null; layer?: number; iconOf?: {kind: 'switch' | 'key'; name: string; parent: string} | {kind: 'contact'; states: readonly ContactState[]; window: boolean; opener: boolean} | {kind: 'object'; auto: string; suggested: readonly string[]; program: boolean; number: boolean}; onclose: () => void; onchanged: () => void} = $props();
    const FAVORITE_ENUM = 'favorite';
    const object = $derived(snap.objects[ref]);
    let display = $state('');
    let enums = $state<string[]>([]);
    let busy = $state(false);
    let error = $state('');
    let saved = $state(false);
    // the display-name field follows the stored one only when that changes (a reload after a
    // toggle must not clobber what is being typed); the memberships always follow
    const storedDisplay = $derived(displayNames.names[ref] ?? '');
    let seenDisplay: string | null = null;
    $effect(() => {
        const d = storedDisplay;
        if (d !== seenDisplay) {
            seenDisplay = d;
            display = d;
        }
    });
    $effect(() => {
        enums = [...(object?.enums ?? [])];
    });
    const realName = $derived(object?.name || ref);
    const shown = $derived(storedDisplay || realName);
    const url = $derived(`/api/meta/v1/objects/${encodeURIComponent(ref)}`);

    // every enum but the favorites, flattened with its depth
    type Row = {path: string; label: string; depth: number};
    function flatten(enumId: string, nodes: MetaNode[], parent: string, depth: number, out: Row[]) {
        for (const n of nodes) {
            const path = parent ? `${parent}/${n.id}` : n.id;
            out.push({path: `${enumId}/${path}`, label: n.name || n.id, depth});
            if (n.children?.length) flatten(enumId, n.children, path, depth + 1, out);
        }
    }
    const sections = $derived(Object.entries(snap.enums).filter(([id, e]) => id !== FAVORITE_ENUM && e.tree.length > 0).map(([id, e]) => {
        const rows: Row[] = [];
        flatten(id, e.tree, '', 0, rows);
        const lang = document.documentElement.lang === 'de' ? 'de' : 'en';
        return {id, label: e.name[lang] ?? e.name.en ?? id, rows};
    }));
    const inFavorites = $derived(!!favoritesPath && enums.includes(favoritesPath));

    function flashSaved() {
        saved = true;
        setTimeout(() => (saved = false), 1200);
    }
    // ---- occulited task 99: the switch's icon, in the metadata's control namespace (null: automatic)
    const chosen = $derived(chosenIcon(object?.meta));
    const ICON_LABELS: Record<string, string> = {
        light_light_dim_00: 'Light', light_ceiling_light: 'Ceiling light', light_pendant_light: 'Pendant light', light_pendant_light_round: 'Pendant light, round',
        light_wall_1: 'Wall light', light_floor_lamp: 'Floor lamp', light_table_lamp: 'Table lamp', light_bedside: 'Bedside lamp', light_ceiling_spots: 'Spotlights',
        light_downlight: 'Downlight', light_led_stripe: 'LED strip', light_mirror: 'Mirror light', light_stairs: 'Stair light', light_outdoor: 'Outdoor light',
        light_garage: 'Garage light', light_fairy_lights: 'Fairy lights', 'light_x-mas_tree': 'Christmas tree', 'light_x-mas_star_1': 'Christmas star',
        message_socket: 'Socket', vent_ventilation: 'Fan', sani_pump: 'Pump', sani_garden_pump: 'Garden pump', sani_pool_filter_pump: 'Pool pump',
        sani_heating: 'Heater', sani_towel_rad: 'Towel radiator', sani_irrigation: 'Irrigation', scene_coffee_maker_automatic: 'Coffee machine',
        it_television: 'TV', it_media_player: 'Media player', it_router: 'Router', it_pc: 'Computer', scene_washing_machine: 'Washing machine', scene_dishwasher: 'Dishwasher',
        // task 106: the key suggestions
        control_switch_1: 'Light switch', control_switch_3: 'Three-way switch', control_switch_m_4: 'Four keys', control_switch_m_8: 'Eight keys', control_on_off: 'On/off',
        control_all_on_off: 'All on/off', control_standby: 'Standby', control_home: 'Home', control_ok: 'OK', control_plus: 'Plus', control_minus: 'Minus',
        control_reload: 'Reload', control_reset: 'Reset', it_remote: 'Remote control', message_bell: 'Bell', message_bell_door: 'Doorbell',
        scene_scene: 'Scene', scene_scene_teach_in: 'Scene, teach-in', scene_party: 'Party', scene_day: 'Day', scene_night: 'Night', scene_sleeping: 'Sleeping',
        scene_cinema: 'Cinema', scene_dinner: 'Dinner', scene_visit_guests: 'Guests', scene_cleaning: 'Cleaning', scene_garden: 'Garden', scene_living: 'Living room',
        // task 105: the contact suggestions
        fts_window_1w: 'Window', fts_window_1w_open: 'Window, open', fts_window_1w_tilt: 'Window, tilted', fts_window_2w: 'Double window', fts_window_2w_open: 'Double window, open',
        fts_window_2w_tilt: 'Double window, tilted', fts_window_roof: 'Roof window', fts_window_roof_open_1: 'Roof window, open', fts_window_louvre: 'Louvre window',
        fts_window_louvre_open: 'Louvre window, open', fts_light_dome: 'Light dome', fts_light_dome_open: 'Light dome, open', fts_door: 'Door', fts_door_open: 'Door, open',
        fts_door_tilt: 'Door, ajar', fts_frontdoor: 'Front door', fts_door_slide: 'Sliding door', fts_door_slide_open: 'Sliding door, open', fts_garage: 'Garage',
        fts_garage_door_100: 'Garage door, open', fts_sliding_gate_closed: 'Sliding gate', fts_sliding_gate_open: 'Sliding gate, open', fts_yard_gate_2w: 'Yard gate', fts_yard_gate_2w_open: 'Yard gate, open',
        // task 176: the variable and program suggestions
        edit_input_numeric: 'Number', edit_sort: 'Value list', message_notice: 'Text', edit_settings: 'Program', audio_play: 'Start', secur_alarm: 'Alarm', message_presence: 'Presence',
        temp_temperature: 'Temperature', measure_power_meter: 'Meter', weather_station_generic: 'Reading',
    };
    // the suggested icons have translated labels; any other icon of the set its readable name
    const iconLabel = (id: string) => (ICON_LABELS[id] ? t(ICON_LABELS[id]) : humanName(id));
    const autoIcon = $derived(iconOf ? (iconOf.kind === 'switch' ? switchIconId(undefined, iconOf.name, iconOf.parent) : iconOf.kind === 'key' ? KEY_DEFAULT : iconOf.kind === 'object' ? iconOf.auto : '') : '');
    const suggested = $derived(iconOf?.kind === 'key' ? KEY_ICONS : iconOf?.kind === 'contact' ? CONTACT_ICONS : iconOf?.kind === 'object' ? iconOf.suggested : SWITCH_ICONS);
    // ---- occulited task 105: a contact's icons per state - one picker, the state chosen above it; the namespace is
    // written whole (a PATCH replaces it), with the icon key kept and the namespace removed when nothing is left
    // task 110: a switch has two, off and on (off left, as the switch sheet's buttons); key and input channels keep one
    const STATE_LABELS: Record<IconState, string> = {closed: 'Closed', open: 'Open', tilted: 'Tilted', off: 'Off', on: 'On'};
    const iconStates = $derived<readonly IconState[]>(iconOf?.kind === 'contact' ? iconOf.states : iconOf?.kind === 'switch' ? SWITCH_STATES : []);
    let picked = $state<IconState | null>(null);
    const pickState = $derived<IconState>(picked && iconStates.includes(picked) ? picked : (iconStates[0] ?? 'closed'));
    const chosenPerState = $derived(chosenIcons(object?.meta));
    const stateAuto = (st: IconState) =>
        iconOf?.kind === 'contact' && (st === 'closed' || st === 'open' || st === 'tilted') ? contactDefault(st, iconOf.window, iconOf.opener)
        : iconOf?.kind === 'switch' && (st === 'on' || st === 'off') ? switchStateDefault(st, chosen, iconOf.name, iconOf.parent) : '';
    const stateIconName = (st: IconState) => chosenPerState[st] ?? stateAuto(st);
    // task 110: the icon section starts folded - one row with the current icon(s) small - so the rooms and functions
    // are in view on landing; it opens with a click and starts folded again each time the sheet opens
    let iconsOpen = $state(false);
    const currentIcons = $derived(iconStates.length ? iconStates.map(stateIconName) : [chosen && knxSVG(chosen) !== undefined ? chosen : autoIcon]);
    function pickStateIcon(id: string | null) {
        const next: ContactIcons = {...chosenPerState};
        if (id) next[pickState] = id;
        else delete next[pickState];
        const ns = {...((object?.meta?.[CONTROL_NS] as Record<string, unknown> | undefined) ?? {})};
        if (Object.keys(next).length) ns.icons = next;
        else delete ns.icons;
        void write({meta: {[CONTROL_NS]: Object.keys(ns).length ? ns : null}});
    }
    function pickIcon(id: string | null) {
        if ((id ?? undefined) === chosen) return;
        // the namespace's other keys stay (task 176: a variable's read-only and step sit beside its icon)
        void write({meta: {[CONTROL_NS]: withSetting(object?.meta, 'icon', id)}});
    }
    // ---- occulited task 176: a variable's or a program's settings in Control, beside the icon in the same namespace
    const settings = $derived(controlSettings(object?.meta));
    function setReadOnly(on: boolean) {
        void write({meta: {[CONTROL_NS]: withSetting(object?.meta, 'read_only', on)}});
    }
    // the rework (maintainer, 2026-11-24): what a program's play button on the tile runs - RUN or RUN_THEN
    function setRunThen(on: boolean) {
        void write({meta: {[CONTROL_NS]: withSetting(object?.meta, 'run_then', on)}});
    }
    // the step's field follows the stored one only when that changes, like the display name
    const storedStep = $derived(settings.step === undefined ? '' : formatNumber(settings.step, i18n.language, settings.step));
    let stepText = $state('');
    let seenStep: string | null = null;
    $effect(() => {
        const st = storedStep;
        if (st !== seenStep) {
            seenStep = st;
            stepText = st;
        }
    });
    const stepValue = $derived(stepText.trim() === '' ? null : parseNumber(stepText));
    const stepBad = $derived(stepValue === undefined || (stepValue !== null && !(stepValue > 0)));
    const stepDirty = $derived(stepText.trim() !== storedStep);
    function saveStep() {
        if (!stepDirty || stepBad || busy) return;
        void write({meta: {[CONTROL_NS]: withSetting(object?.meta, 'step', stepValue ?? null)}});
    }
    async function write(body: {enums: string[]} | {meta: Record<string, unknown>}) {
        busy = true;
        error = '';
        try {
            await api.patch(url, body);
            flashSaved();
            onchanged();
        } catch (e) {
            error = (e as Error).message;
        } finally {
            busy = false;
        }
    }
    function toggle(path: string) {
        const next = enums.includes(path) ? enums.filter((p) => p !== path) : [...enums, path];
        enums = next;
        void write({enums: next});
    }
    const displayDirty = $derived(display.trim() !== storedDisplay);
    async function saveDisplay() {
        if (!displayDirty || busy) return;
        busy = true;
        error = '';
        try {
            await setDisplayName(ref, display);
            flashSaved();
        } catch (e) {
            error = (e as Error).message;
        } finally {
            busy = false;
        }
    }

    // the same growing out of the card as the channel sheet
    function frames(el: HTMLElement): [Keyframe, Keyframe] | null {
        if (!from || typeof el.animate !== 'function' || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return null;
        const to = el.getBoundingClientRect();
        if (to.width === 0 || to.height === 0) return null;
        el.style.transformOrigin = 'top left';
        return [{transform: `translate(${from.left - to.left}px, ${from.top - to.top}px) scale(${from.width / to.width}, ${from.height / to.height})`, opacity: 0.4}, {transform: 'none', opacity: 1}];
    }
    const sheetEl = () => document.querySelector<HTMLElement>('.ol-modal.app-assign');
    onMount(() => {
        const el = sheetEl();
        const f = el && frames(el);
        if (!el || !f) return;
        el.animate(f, {duration: 240, easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)'});
        el.parentElement?.animate([{backgroundColor: 'transparent', backdropFilter: 'blur(0px)'}, {}], {duration: 240, easing: 'ease-out'});
    });
    let closing = false;
    function close() {
        if (closing) return;
        closing = true;
        const el = sheetEl();
        const f = el && frames(el);
        if (!el || !f) {
            onclose();
            return;
        }
        el.style.pointerEvents = 'none';
        const a = el.animate([f[1], f[0]], {duration: 200, easing: 'cubic-bezier(0.4, 0, 1, 1)', fill: 'forwards'});
        el.parentElement?.animate([{}, {backgroundColor: 'transparent', backdropFilter: 'blur(0px)'}], {duration: 200, easing: 'ease-in', fill: 'forwards'});
        a.finished.then(onclose, onclose);
    }
</script>

<ModalDialog open={true} title={t('Assign {name}', {name: shown})} size="medium" onclose={close} class="app-sheet app-assign" {layer}>
    <div class="as-head">
        <span class="as-circle" aria-hidden="true"><Icon name="edit" size={26} /></span>
        <div class="as-titles">
            <div class="as-name" title={storedDisplay && storedDisplay !== realName ? t('actually: {name}', {name: realName}) : undefined}>{shown}</div>
            <div class="ol-muted hmm-mono as-ref">{ref}</div>
        </div>
    </div>
    {#if error}<div class="ol-notice error">{error}</div>{/if}
    <div class="as-field">
        <span class="as-label">{t('Name')}<Help>{t("The channel's name in the metadata, as every view and app shows it. It is changed for everybody where the metadata is edited - in homematic-manager, say - by an account that may configure, not here.")}</Help></span>
        <input class="hmm-input as-input" type="text" value={object?.name ?? ''} aria-label={t('Name')} readonly data-assign-name />
        <span></span>
    </div>
    <div class="as-field">
        <label class="as-label" for="as-display">{t('Display name in Control')}<Help>{t('Your own name for this channel, shown only in Control and only to you. Empty: the name above.')}</Help></label>
        <input id="as-display" class="hmm-input as-input" type="text" bind:value={display} maxlength="200" placeholder={object?.name ?? ''} disabled={busy} onblur={() => void saveDisplay()} onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void saveDisplay(); } }} data-assign-display />
        <button type="button" class="hmm-button as-btn" disabled={busy || !displayDirty} onclick={() => void saveDisplay()} data-assign-display-save>{t('Save')}</button>
    </div>
    {#if iconOf}
        <!-- task 110: folded by default; the row shows the icon(s) in use, the triangle (task 98's) at its right end -->
        <button type="button" class="as-fold" aria-expanded={iconsOpen} onclick={() => (iconsOpen = !iconsOpen)} data-assign-icons-fold={iconsOpen ? 'open' : 'closed'}>
            <span class="as-fold-label">{iconStates.length ? t('Icons') : t('Symbol')}</span>
            <span class="as-fold-icons" aria-hidden="true">{#each currentIcons as id, i (i)}<span class="as-fold-svg" data-assign-current-icon={id}>{@html knxSVG(id) ?? ''}</span>{/each}</span>
            <svg class="as-caret" class:open={iconsOpen} viewBox="0 0 12 12" aria-hidden="true"><path d="M4.2 2.6 L9 6 L4.2 9.4 Z" /></svg>
        </button>
        {#if iconsOpen}
            {#if iconStates.length}
                <!-- task 105 (contacts), task 110 (switches): one picture per state; the state is chosen above the picker, each button showing its current icon -->
                <div data-assign="icon" data-assign-icon-kind={iconOf.kind}>
                    <div class="as-states" role="tablist" aria-label={t('State')} data-assign-states={iconStates.length} style={`--as-n:${iconStates.length}`}>
                        {#each iconStates as st (st)}
                            <button type="button" class="as-state" role="tab" aria-selected={pickState === st} class:chosen={!!chosenPerState[st]} onclick={() => (picked = st)} data-assign-state={st} data-assign-state-icon={stateIconName(st)}>
                                <span class="as-state-svg" aria-hidden="true">{@html knxSVG(stateIconName(st)) ?? ''}</span><span class="as-state-label">{t(STATE_LABELS[st])}</span>
                            </button>
                        {/each}
                    </div>
                    <IconPicker value={chosenPerState[pickState] ?? null} auto={stateAuto(pickState)} {suggested} labelOf={iconLabel} disabled={busy} onchange={pickStateIcon} />
                </div>
            {:else}
                <!-- task 99: the switch's icon; "Automatisch" is the keyword or type default. task 102: the picker with
                     the search and the whole set. task 106: a key's icon too, with its own suggestions -->
                <div data-assign="icon" data-assign-icon-kind={iconOf.kind}>
                    <IconPicker value={chosen ?? null} auto={autoIcon} {suggested} labelOf={iconLabel} disabled={busy} onchange={pickIcon} />
                </div>
            {/if}
        {/if}
    {/if}
    {#if iconOf?.kind === 'object'}
        <!-- task 176: shown only - the tile and the sheet show the value (a program: its state) and change nothing -->
        <label class="as-row" data-assign="read-only">
            <input type="checkbox" checked={settings.readOnly} disabled={busy} onchange={(e) => setReadOnly(e.currentTarget.checked)} />
            <span>{t('Show only (read-only in Control)')}</span>
            <Help>{iconOf.program ? t('Control then only shows whether this program is active: no run, no switch. Other software and the addon itself can still run and switch it.') : t('Control then only shows the value of this variable. Programs, other software and the addon itself can still change it.')}</Help>
        </label>
        {#if iconOf.program}
            <div class="as-runmode" role="radiogroup" aria-label={t('The button on the tile')} data-assign="run-mode">
                <span class="as-label">{t('The button on the tile')}<Help>{t('With its conditions the program checks them now and runs what applies; without them it runs its first "then" at once, as the CCU\'s "Start" does. The card keeps both.')}</Help></span>
                <label class="as-row"><input type="radio" name="as-run-mode" checked={!settings.runThen} disabled={busy || settings.readOnly} onchange={() => setRunThen(false)} data-assign-run="run" /> <span>{t('runs the program (with its conditions)')}</span></label>
                <label class="as-row"><input type="radio" name="as-run-mode" checked={settings.runThen} disabled={busy || settings.readOnly} onchange={() => setRunThen(true)} data-assign-run="run_then" /> <span>{t('starts it without its conditions')}</span></label>
            </div>
        {/if}
        {#if iconOf.number}
            <div class="as-field" data-assign="step">
                <label class="as-label" for="as-step">{t('Step')}<Help>{t("The step of the plus and minus buttons and of the slider in this number's sheet; it also decides how many decimals Control shows. Empty: chosen by the limits.")}</Help></label>
                <input id="as-step" class="hmm-input as-input" type="text" inputmode="decimal" autocomplete="off" bind:value={stepText} placeholder={t('automatic')} aria-invalid={stepBad ? 'true' : undefined} title={stepBad ? t('A number above 0, or empty.') : undefined} disabled={busy} onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); saveStep(); } }} data-assign-step />
                <button type="button" class="hmm-button as-btn" disabled={busy || !stepDirty || stepBad} onclick={saveStep} data-assign-step-save>{t('Save')}</button>
            </div>
        {/if}
    {/if}
    {#if favoritesPath}
        <label class="as-row as-fav" data-assign="favorite">
            <input type="checkbox" checked={inFavorites} disabled={busy} onchange={() => toggle(favoritesPath)} />
            <Icon name="pin" size={18} /> <span>{t('In my favorites')}</span>
        </label>
    {/if}
    {#each sections as s (s.id)}
        <h3 class="as-section">{s.label}</h3>
        <div class="as-rows" data-assign={s.id}>
            {#each s.rows as r (r.path)}
                <label class="as-row" style={`--depth:${r.depth}`} data-assign-node={r.path}>
                    <input type="checkbox" checked={enums.includes(r.path)} disabled={busy} onchange={() => toggle(r.path)} />
                    <span>{r.label}</span>
                </label>
            {/each}
        </div>
    {/each}
    {#if saved}<div class="ol-muted as-saved" data-assign-saved>{t('Saved')}</div>{/if}
</ModalDialog>

<style>
    .as-head { display: flex; align-items: center; gap: 16px; margin: 0 0 16px; }
    .as-circle { width: 56px; height: 56px; border-radius: 50%; border: 2px solid var(--hmm-border-muted); display: inline-flex; align-items: center; justify-content: center; color: var(--hmm-fg-muted); flex: 0 0 auto; }
    .as-titles { min-width: 0; }
    .as-name { font-size: 1.25em; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .as-ref { font-size: var(--hmm-font-size-small); }
    .as-field { display: grid; grid-template-columns: 11em 1fr 6.5em; align-items: center; gap: 10px; margin: 0 0 14px; font-size: 1.05em; }
    .as-label { display: inline-flex; align-items: center; gap: 4px; min-width: 0; }
    /* task 176: a program's play button - RUN or RUN_THEN, two radio rows under their heading */
    .as-runmode { margin: 0 0 14px; }
    .as-runmode > .as-label { padding: 0 8px; font-size: 1.05em; min-height: 32px; }
    .as-input[readonly] { color: var(--hmm-fg-muted); background: transparent; }
    @media (max-width: 520px) {
        .as-field { grid-template-columns: 1fr auto; }
        .as-label { grid-column: 1 / -1; }
        .as-field > span:empty { display: none; }
    }
    .as-input { min-height: 44px; font-size: 1.05em; }
    .as-input[aria-invalid='true'] { border-color: var(--hmm-error); }
    .as-btn { min-height: 44px; padding: 0 16px; }
    .as-section { margin: 14px 0 6px; font-size: 1em; color: var(--hmm-fg-muted); font-weight: 600; }
    .as-rows { display: flex; flex-direction: column; }
    .as-row { display: flex; align-items: center; gap: 12px; min-height: 48px; padding: 0 8px 0 calc(8px + var(--depth, 0) * 18px); border-radius: 8px; font-size: 1.1em; cursor: pointer; }
    .as-row:hover { background: var(--hmm-bg-hover, rgba(127, 127, 127, 0.12)); }
    .as-row input { width: 24px; height: 24px; flex: 0 0 auto; }
    .as-fav { margin-bottom: 4px; }
    .as-saved { margin-top: 10px; text-align: right; }
    /* task 105: the state buttons above the picker - one fixed box each, the current one in the accent's ring, a dot
       below the label where an icon is chosen */
    .as-states { display: grid; grid-template-columns: repeat(var(--as-n, 3), 1fr); gap: 6px; margin-bottom: 8px; }
    .as-state { height: 64px; padding: 4px; border: 2px solid transparent; border-radius: 10px; background: color-mix(in srgb, var(--hmm-border-muted) 45%, transparent); color: var(--hmm-fg-muted); cursor: pointer; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2px; font: inherit; }
    .as-state[aria-selected='true'] { border-color: var(--hmm-accent); color: var(--hmm-fg); }
    .as-state.chosen .as-state-label::after { content: ' ·'; color: var(--hmm-accent); }
    .as-state:focus-visible { outline: 2px solid var(--hmm-accent); outline-offset: 2px; }
    .as-state-svg { display: block; width: 30px; height: 30px; }
    .as-state-svg :global(svg) { width: 100%; height: 100%; display: block; }
    .as-state-label { font-size: 12px; white-space: nowrap; overflow: hidden; max-width: 100%; height: 16px; line-height: 16px; }
    /* task 110: the folded icon section - one row of one height whatever it holds; the triangle at the right end */
    .as-fold { display: flex; align-items: center; gap: 10px; width: 100%; min-height: 48px; margin: 8px 0 4px; padding: 0 8px; border: 0; border-radius: 8px; background: transparent; color: inherit; font: inherit; cursor: pointer; text-align: left; }
    .as-fold:hover { background: var(--hmm-bg-hover, rgba(127, 127, 127, 0.12)); }
    .as-fold:focus-visible { outline: 2px solid var(--hmm-accent); outline-offset: 2px; }
    .as-fold-label { font-weight: 600; color: var(--hmm-fg-muted); }
    .as-fold-icons { display: inline-flex; gap: 6px; margin-left: auto; height: 24px; }
    .as-fold-svg { display: block; width: 24px; height: 24px; }
    .as-fold-svg :global(svg) { width: 100%; height: 100%; display: block; }
    .as-caret { display: block; width: 14px; height: 14px; flex: 0 0 14px; fill: currentColor; stroke: currentColor; stroke-width: 1.6; stroke-linejoin: round; color: var(--hmm-fg-muted); transition: transform 150ms ease-out; }
    .as-caret.open { transform: rotate(90deg); }
    @media (prefers-reduced-motion: reduce) { .as-caret { transition: none; } }
</style>
