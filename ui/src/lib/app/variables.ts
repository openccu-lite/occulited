/*
 * occulited task 176: system variables and programs in Control. An interface may offer them as channels - the
 * addon Logic does: one device per object, its values on channel 1 - and Control gives them tiles and sheets of
 * their own. They are recognised by the channel's type and datapoints, not by the interface's name:
 *
 *   VARIABLE or ALARM with VALUE   a variable: BOOL (its texts in VALUE_LIST: false, true), FLOAT or INTEGER
 *                                  (UNIT, MIN, MAX), ENUM (VALUE_LIST, the value is the index), STRING; an alarm
 *                                  also has ACKNOWLEDGE, COUNT and FIRST
 *   PROGRAM with ACTIVE or RUN     a program: ACTIVE (a switch), RUN (with its conditions) and RUN_THEN (the first
 *                                  "then" without them) where it may be run
 *
 * What a user chooses per object lives in the metadata, namespace `control` (meta-format.md): the icon (as for a
 * switch), `read_only` (the tile and the sheet only show) and a number's `step`.
 */
import type {Description, Model, Values} from './channels';

export type VarKind = 'bool' | 'alarm' | 'number' | 'list' | 'text';

/** OPERATIONS bit 2: the datapoint takes a setValue. */
function writable(desc: Description | undefined, key: string): boolean {
    const op = desc?.[key]?.OPERATIONS;
    return typeof op === 'number' && (op & 2) !== 0;
}

/** Whether a channel of this type and description is a variable. */
export function isVariable(desc: Description | undefined, type: string | undefined): boolean {
    return (type === 'VARIABLE' || type === 'ALARM') && !!desc && 'VALUE' in desc;
}

/** Whether a channel of this type and description is a program. */
export function isProgram(desc: Description | undefined, type: string | undefined): boolean {
    return type === 'PROGRAM' && !!desc && ('ACTIVE' in desc || 'RUN' in desc);
}

/** What kind of variable a description is: an alarm by its ACKNOWLEDGE, the others by VALUE's type. */
export function varKind(desc: Description | undefined): VarKind {
    if (desc && 'ACKNOWLEDGE' in desc) return 'alarm';
    switch (desc?.VALUE?.TYPE) {
        case 'BOOL': case 'ACTION': return 'bool';
        case 'FLOAT': case 'INTEGER': return 'number';
        case 'ENUM': return 'list';
    }
    return 'text';
}

/** The variable's model: a boolean that takes a write switches from its disc; a number, an entry and a text are set
 *  in the sheet, an alarm acknowledged there. One that takes no write is a reading. */
export function variableModel(desc: Description): Model {
    const write = writable(desc, 'VALUE');
    const act = varKind(desc) === 'bool' && write;
    return {widget: 'variable', tile: write || writable(desc, 'ACKNOWLEDGE') ? 'act-open' : 'read', stateKey: 'VALUE', commandKey: act ? 'VALUE' : ''};
}

/** The program's model: its disc runs it where it may be run; its sheet also switches it. */
export function programModel(desc: Description): Model {
    const run = writable(desc, 'RUN');
    return {widget: 'program', tile: run || writable(desc, 'ACTIVE') ? 'act-open' : 'read', stateKey: 'ACTIVE', commandKey: run ? 'RUN' : ''};
}

/** The model of a tile set read-only: no action, and the sheet shows without controls. */
export function lockedModel(m: Model): Model {
    return {...m, tile: 'read', commandKey: '', locked: true};
}

// ---- the choices in the metadata

/** What Control keeps per variable or program beside the icon. */
export interface ControlSettings {
    readOnly: boolean;
    /** a number's step, when one is chosen */
    step?: number;
}

/** The largest step's decimals Control keeps and shows. */
const MAX_DECIMALS = 6;

/** A step as the metadata may hold it: a positive, finite number. */
export function validStep(x: unknown): number | undefined {
    return typeof x === 'number' && isFinite(x) && x > 0 ? x : undefined;
}

/** The settings in an object's metadata; anything that is not what it should be counts as not set. */
export function controlSettings(meta: Record<string, unknown> | undefined): ControlSettings {
    const ns = meta?.control as {read_only?: unknown; step?: unknown} | undefined;
    const step = validStep(ns?.step);
    return {readOnly: ns?.read_only === true, ...(step === undefined ? {} : {step})};
}

/**
 * The `control` namespace with one setting changed, the other keys kept (a PATCH replaces a namespace whole);
 * null when nothing is left, which removes the namespace.
 */
export function withSetting(meta: Record<string, unknown> | undefined, key: 'icon' | 'read_only' | 'step', value: string | number | boolean | null): Record<string, unknown> | null {
    const ns = {...((meta?.control as Record<string, unknown> | undefined) ?? {})};
    if (value === null || value === false || value === '') delete ns[key];
    else ns[key] = value;
    return Object.keys(ns).length ? ns : null;
}

// ---- numbers

function num(v: unknown): number | undefined {
    return typeof v === 'number' ? v : typeof v === 'string' && v.trim() !== '' && !isNaN(Number(v)) ? Number(v) : undefined;
}

/** A limit of the description, or undefined where there is none: an interface without one says the largest float. */
export function limitOf(x: unknown): number | undefined {
    const n = num(x);
    return n === undefined || !isFinite(n) || Math.abs(n) >= 3.4e38 ? undefined : n;
}

/** The decimals of a step: 0.5 has one, 0.25 two, 10 none. */
export function decimalsOf(step: number): number {
    for (let d = 0; d < MAX_DECIMALS; d++) {
        const x = step * 10 ** d;
        if (Math.abs(x - Math.round(x)) < 1e-9) return d;
    }
    return MAX_DECIMALS;
}

/** A step for a range nobody chose one for: about a hundredth of it, as 1, 2 or 5 times a power of ten. */
export function niceStep(min: number, max: number): number {
    const raw = (max - min) / 100;
    if (!(raw > 0) || !isFinite(raw)) return 1;
    const pow = 10 ** Math.floor(Math.log10(raw));
    const f = raw / pow;
    const step = (f < 1.5 ? 1 : f < 3.5 ? 2 : f < 7.5 ? 5 : 10) * pow;
    return Number(step.toPrecision(6));
}

/** The step the sheet's controls use: the chosen one, else by the limits, else 1. */
export function stepOf(desc: Description | undefined, chosen?: number): number {
    if (chosen !== undefined) return chosen;
    if (desc?.VALUE?.TYPE === 'INTEGER') return 1;
    const min = limitOf(desc?.VALUE?.MIN), max = limitOf(desc?.VALUE?.MAX);
    return min !== undefined && max !== undefined && max > min ? niceStep(min, max) : 1;
}

/** A number in the reader's notation: the chosen step's decimals, else as many as it has, two at most. */
export function formatNumber(value: unknown, language = 'en', step?: number): string {
    const n = num(value);
    if (n === undefined) return '';
    const d = step === undefined ? undefined : decimalsOf(step);
    return new Intl.NumberFormat(language === 'de' ? 'de-DE' : 'en-US', {minimumFractionDigits: d ?? 0, maximumFractionDigits: d ?? 2, useGrouping: false}).format(n);
}

/** A value moved by one step and kept inside the limits, without the dust of binary fractions. */
export function stepped(value: number, by: number, min?: number, max?: number): number {
    let x = Number((value + by).toFixed(Math.min(MAX_DECIMALS, decimalsOf(Math.abs(by)) + 2)));
    if (min !== undefined) x = Math.max(min, x);
    if (max !== undefined) x = Math.min(max, x);
    return x;
}

/** Why a typed number is not taken ('' when it is): the texts are translation keys, with min and max as given. */
export function numberProblem(text: string, min?: number, max?: number): '' | 'not-a-number' | 'out-of-range' {
    const n = parseNumber(text);
    if (n === undefined) return 'not-a-number';
    if ((min !== undefined && n < min) || (max !== undefined && n > max)) return 'out-of-range';
    return '';
}

/** A typed number: a decimal comma is a point. */
export function parseNumber(text: string): number | undefined {
    const s = text.trim().replace(',', '.');
    if (s === '' || !/^[-+]?(\d+\.?\d*|\.\d+)$/.test(s)) return undefined;
    return Number(s);
}

// ---- the words

type T = (k: string, p?: Record<string, string>) => string;

/** A boolean's two texts: the description's (VALUE_LIST: false, true) where it has them, else the plain words. */
export function boolLabels(desc: Description | undefined, t: T): {off: string; on: string} {
    const list = desc?.VALUE?.TYPE === 'BOOL' ? desc.VALUE.VALUE_LIST : undefined;
    return {off: list?.[0] || t('variable false'), on: list?.[1] || t('variable true')};
}

/** Whether a boolean's value is true (an interface may say 1). */
export function truth(v: unknown): boolean {
    return v === true || v === 1 || v === 'true';
}

/** A value list's entries. */
export function entries(desc: Description | undefined): string[] {
    return desc?.VALUE?.VALUE_LIST ?? [];
}

/** The index of a list's value: the number, or an entry's text. */
export function entryIndex(desc: Description | undefined, v: unknown): number {
    const n = num(v);
    if (n !== undefined) return n;
    return typeof v === 'string' ? entries(desc).indexOf(v) : -1;
}

/** The variable's value as the tile's state line and the sheet say it; '' while it is not known. */
export function variableText(values: Values, desc: Description | undefined, t: T, opt: {step?: number; language?: string} = {}): string {
    const v = values.VALUE;
    if (v === undefined || v === null) return '';
    switch (varKind(desc)) {
        case 'bool': case 'alarm': {
            const l = boolLabels(desc, t);
            return truth(v) ? l.on : l.off;
        }
        case 'number': {
            const s = formatNumber(v, opt.language, opt.step);
            const unit = desc?.VALUE?.UNIT ?? '';
            return s && unit ? `${s} ${unit}` : s || String(v);
        }
        case 'list': return entries(desc)[entryIndex(desc, v)] ?? String(v);
    }
    return String(v);
}

/** The program's state line. */
export function programText(values: Values, t: T): string {
    const v = values.ACTIVE;
    return v === undefined ? '' : truth(v) ? t('program active') : t('program inactive');
}

// ---- the pictures

/** The automatic icon of a variable (by its kind and, for an alarm, its state) or a program. */
export function autoIcon(m: Model, desc: Description | undefined, on = false): string {
    if (m.widget === 'program') return 'edit_settings';
    switch (varKind(desc)) {
        case 'bool': return 'control_on_off';
        case 'alarm': return on ? 'secur_alarm_alarm' : 'secur_alarm';
        case 'number': return 'edit_input_numeric';
        case 'list': return 'edit_sort';
    }
    return 'message_notice';
}
