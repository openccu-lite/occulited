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
    /** how long it lasted; absent while it has not normalised; -1 ended, its length unknown */
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
    /** occulited task 177: the processor's clock now */
    clock?: CPUClock;
    /** occulited task 177: when each sticky bit first held this boot */
    causes?: ThrottleCause[];
    /** occulited B-186: the power supply's way in - through the RPI-RF-MOD on the header, or the Pi's own socket; absent while unknown */
    supply?: 'rpi-rf-mod' | 'pi';
}
/** occulited task 177: the processor's clock, MHz; firmware = measured, cpufreq = requested */
export interface CPUClock {
    cur_mhz: number;
    max_mhz?: number;
    source: 'firmware' | 'cpufreq';
}
/** occulited task 177: the first read that showed a sticky bit this boot */
export interface ThrottleCause {
    flag: ThrottleFlag;
    at: string;
    /** already set at occulited's first read: it appeared before `at` */
    before?: boolean;
    uptime_s: number;
    celsius?: number;
    clock_mhz?: number;
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

/**
 * occulited task 173: the throttle state's level for the panel's edge - undervoltage now red, a
 * throttling now or since the boot beyond a boot dip amber, a boot dip and normal nothing
 */
export function throttleLevel(th?: Throttle): TempLevel {
    if (!th) return 'ok';
    const s = throttleState(th);
    return s === 'undervoltage' ? 'error' : s === 'throttled' || s === 'earlier' ? 'warn' : 'ok';
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

/** occulited task 172: a moment or stretch the chart marks (an undervoltage episode): a thin line, or a light band with its edges */
export interface ChartEvent {
    /** start and end in ms since the epoch; equal for a moment */
    start: number;
    end: number;
    /** the tooltip */
    label: string;
}

/**
 * occulited task 172 (the maintainer, 2026-11-23): the kernel's undervoltage episodes as the
 * temperature chart's marks - each from its start for its duration, one of unknown length a moment;
 * an episode without a wall-clock start cannot be placed. A boot dip's say so in their tooltip.
 * B-186: one the log leaves open runs up to now only while the undervoltage holds now - the
 * production Pi's episode from the evening before, with no end in the log and no undervoltage
 * now, painted the whole 24 h chart; otherwise it has ended at a moment nobody knows: a moment.
 */
export function episodeMarks(
    th: Throttle | undefined,
    words: {t: (key: string, params?: Record<string, string | number>) => string; clock: (d: Date) => string; seconds: (v: number) => string},
    now = Date.now(),
): ChartEvent[] {
    const {t} = words;
    const dip = th?.state === 'boot_dip';
    const out: ChartEvent[] = [];
    for (const e of th?.episodes ?? []) {
        const start = e.start ? Date.parse(e.start) : NaN;
        if (!Number.isFinite(start)) continue;
        const at = words.clock(new Date(start));
        const open = e.duration_s === undefined && th?.now.under_voltage === true;
        const known = e.duration_s !== undefined && e.duration_s >= 0;
        const end = open ? Math.max(start, now) : known ? start + e.duration_s! * 1000 : start;
        let label = open
            ? t('Undervoltage since {time}', {time: at})
            : known
              ? t('Undervoltage {time}, {duration}', {time: at, duration: words.seconds(e.duration_s!)})
              : t('Undervoltage {time}', {time: at});
        if (dip) label += ' ' + t('(at start-up, normalised)');
        out.push({start, end, label});
    }
    return out;
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

/** occulited task 177: MHz as GHz with one decimal in the language's mark ("1,2 GHz" without the unit: "1,2") */
export function formatGHz(mhz: number, lang: string): string {
    return new Intl.NumberFormat(lang === 'de' ? 'de-DE' : 'en-US', {minimumFractionDigits: 1, maximumFractionDigits: 1}).format(mhz / 1000);
}

/** occulited task 177: the panel's clock value, "1,2 / 1,5 GHz", or "1,2 GHz" without a maximum */
export function clockText(c: CPUClock, lang: string): string {
    return c.max_mhz ? `${formatGHz(c.cur_mhz, lang)} / ${formatGHz(c.max_mhz, lang)} GHz` : `${formatGHz(c.cur_mhz, lang)} GHz`;
}

/**
 * occulited B-186: the processor runs at its full clock now - the clock's current value within 2 %
 * of its maximum (the pill "throttled earlier" says so, as the maintainer read "throttled" beside
 * "1,2 / 1,2 GHz" as now)
 */
export function fullClock(c?: CPUClock): boolean {
    return !!c?.max_mhz && c.cur_mhz >= c.max_mhz * 0.98;
}

/** occulited task 177: the record of a flag, if any */
export function causeOf(th: Throttle | undefined, flag: ThrottleFlag): ThrottleCause | undefined {
    return th?.causes?.find((c) => c.flag === flag);
}

/**
 * occulited task 177: how a cause says when - "start-up" for a bit already set at a first read in
 * the boot phase, "before" for one already set later (it appeared earlier), else "at"; the moment
 * on the clock, with the day in front when it is not today
 */
export function causeWhen(c: ThrottleCause, lang: string, now = new Date()): {kind: 'startup' | 'before' | 'at'; time: string} {
    const d = new Date(c.at);
    const t = d.toLocaleTimeString(lang, {hour: '2-digit', minute: '2-digit', hourCycle: 'h23'});
    const sameDay = d.toDateString() === now.toDateString();
    const time = sameDay ? t : `${d.toLocaleDateString(lang, {day: 'numeric', month: 'numeric'})} ${t}`;
    if (c.before && c.uptime_s < 120) return {kind: 'startup', time};
    return {kind: c.before ? 'before' : 'at', time};
}
