// occulited task 148 (the maintainer, 2026-11-15): the Status page's temperature panel - a ring per
// reading as the storage panel draws its partitions (every thermal zone, the hwmon sensors no zone
// covers, every disk's SMART temperature), the Raspberry Pi firmware's throttling and
// undervoltage state, and the processor's temperature over time. A system without a sensor (the
// VM) sends no `thermal` and has no panel.

/** a reading of GET /status's `thermal` */
export interface TempReading {
    id: string;
    kind: 'soc' | 'disk' | 'sensor';
    name: string;
    device?: string;
    disk_kind?: string;
    celsius: number;
    /** hot from here (a Pi's firmware throttles from 80 °C) */
    warn_c: number;
    /** the ring's full scale and the error level (a Pi's 85 °C) */
    max_c: number;
    /** a disk's SMART read, at most once an hour */
    read_at?: string;
}
export interface ThrottleFlags {
    under_voltage: boolean;
    freq_capped: boolean;
    throttled: boolean;
    soft_temp_limit: boolean;
}
/** an undervoltage episode of this boot by rpi_volt's kernel lines (occulited B-162) */
export interface VoltageEpisode {
    /** when it began, on the wall clock */
    start?: string;
    /** when it began, seconds since the boot; -1 unknown */
    start_s: number;
    /** how long it lasted; absent while it has not normalised */
    duration_s?: number;
}
export interface Throttle {
    /** firmware: the whole word; hwmon: the undervoltage now alone */
    source: 'firmware' | 'hwmon';
    raw?: number;
    now: ThrottleFlags;
    since_boot: ThrottleFlags;
    /** the server's verdict (B-162); absent from an older occulited */
    state?: ThrottleState | 'boot_dip';
    /** the kernel's undervoltage episodes this boot; absent when the kernel log was not read */
    events?: number;
    undervoltage_s?: number;
    last?: VoltageEpisode;
    episodes?: VoltageEpisode[];
}
export interface Thermal {
    readings: TempReading[];
    throttle?: Throttle;
}

export type TempLevel = 'ok' | 'warn' | 'error';

/** the reading's level against its own limits */
export function tempLevel(r: Pick<TempReading, 'celsius' | 'warn_c' | 'max_c'>): TempLevel {
    if (r.max_c > 0 && r.celsius >= r.max_c) return 'error';
    if (r.warn_c > 0 && r.celsius >= r.warn_c) return 'warn';
    return 'ok';
}

/** the worst level of the panel */
export function panelLevel(rs: TempReading[]): TempLevel {
    const ls = rs.map(tempLevel);
    return ls.includes('error') ? 'error' : ls.includes('warn') ? 'warn' : 'ok';
}

/** how much of the ring a reading fills: 0 °C to its max_c, clamped */
export function ringFraction(r: Pick<TempReading, 'celsius' | 'max_c'>): number {
    if (!(r.max_c > 0)) return 0;
    return Math.max(0, Math.min(1, r.celsius / r.max_c));
}

/** the rings drawn, the outermost first; the legend lists every reading */
export const MAX_TEMP_RINGS = 4;

/** each reading's colour token, the storage panel's family in order */
export function tempColours(rs: TempReading[]): string[] {
    return rs.map((_, i) => `var(--ol-series-${(i % MAX_TEMP_RINGS) + 1})`);
}

/** a temperature as the page writes it: one place, the language's decimal mark ("54,8 °C") */
export function formatCelsius(v: number, lang: string, digits = 1): string {
    return `${new Intl.NumberFormat(lang === 'de' ? 'de-DE' : 'en-US', {minimumFractionDigits: digits, maximumFractionDigits: digits}).format(v)} °C`;
}

/** a limit, whole when it is one ("80 °C") */
export function formatLimit(v: number, lang: string): string {
    return formatCelsius(v, lang, Number.isInteger(v) ? 0 : 1);
}

/** the throttle's state in one word for the pill: undervoltage before throttling before normal; a flag since the boot alone is "earlier" */
export type ThrottleState = 'normal' | 'earlier' | 'throttled' | 'undervoltage';
export function throttleState(th: Throttle): ThrottleState {
    // B-162: a short undervoltage in the boot phase that normalised is no warning - the pill says normal
    if (th.state === 'boot_dip') return 'normal';
    if (th.state) return th.state;
    if (th.now.under_voltage) return 'undervoltage';
    if (th.now.throttled || th.now.freq_capped || th.now.soft_temp_limit) return 'throttled';
    const s = th.since_boot;
    if (th.source === 'firmware' && (s.under_voltage || s.throttled || s.freq_capped || s.soft_temp_limit)) return 'earlier';
    return 'normal';
}

/** the conditions behind the pill, now and since the boot, in the firmware's order */
export type ThrottleFlag = keyof ThrottleFlags;
export const FLAG_ORDER: ThrottleFlag[] = ['under_voltage', 'throttled', 'freq_capped', 'soft_temp_limit'];
export function throttleFacts(th: Throttle): {flag: ThrottleFlag; now: boolean}[] {
    const out: {flag: ThrottleFlag; now: boolean}[] = [];
    // a boot dip's sticky bits are its own info line (bootDip), not conditions
    const since = th.state !== 'boot_dip';
    for (const f of FLAG_ORDER) {
        if (th.now[f]) out.push({flag: f, now: true});
        else if (since && ((th.source === 'firmware' && th.since_boot[f]) || (f === 'under_voltage' && (th.events ?? 0) > 0))) out.push({flag: f, now: false});
    }
    return out;
}

/** B-162: the boot dip's info line - how many episodes and how long together; null when it is none */
export function bootDip(th: Throttle): {n: number; seconds: number} | null {
    if (th.state !== 'boot_dip') return null;
    return {n: Math.max(1, th.events ?? 1), seconds: th.undervoltage_s ?? 0};
}

/** a duration as the panel writes it: "2 s", "1,5 s", "2 min 5 s" */
export function formatSeconds(v: number, lang: string): string {
    const nf = new Intl.NumberFormat(lang === 'de' ? 'de-DE' : 'en-US', {maximumFractionDigits: v < 10 ? 1 : 0});
    if (v < 60) return `${nf.format(v)} s`;
    const m = Math.floor(v / 60);
    const s = Math.round(v - m * 60);
    return s ? `${m} min ${s} s` : `${m} min`;
}

/** a system sample of GET /status/history with the processor's temperature (task 148) */
export interface SocSample {
    t: string;
    soc_c?: number;
}

/**
 * The processor's temperature as the chart draws it: a sample without one breaks the line (a
 * minute the sensor could not be read, the samples kept before task 148). None at all: no chart.
 */
export function socSeries(h: SocSample[] | undefined): {t: string; v: number; up?: boolean}[] {
    const list = h ?? [];
    if (!list.some((s) => typeof s.soc_c === 'number')) return [];
    return list.map((s) => (typeof s.soc_c === 'number' ? {t: s.t, v: s.soc_c} : {t: s.t, v: 0, up: false}));
}

/** the chart's readable tops in °C */
export const TEMP_STEPS = [40, 50, 60, 70, 80, 90, 100, 125];
