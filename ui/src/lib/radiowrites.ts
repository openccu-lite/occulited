// occulited task 163: the radio writes per lite-rpc client - GET /radio/writes and what the
// Status page's panel and the API tokens' column derive from it.

export type RWKind = 'token' | 'user' | 'system';
export type RWLevel = 'hint' | 'warning';

export interface RWHint { burst: boolean; average: boolean; peak_minute: number; peak_average: number }
export interface RWCause { interface: string; address: string; from: number; to: number; writes: number; total: number; share_percent: number }
export interface RWClient {
    kind: RWKind;
    name: string;
    label?: string;
    per_minute: number;
    hour: number;
    noop: number;
    /** the writes per minute, oldest first; the last is the running minute */
    minutes: number[];
    /** openccu-lite task 489: the writes per bucket of the view's span, oldest first; their sums */
    buckets: number[];
    writes: number;
    writes_noop: number;
    interfaces: {name: string; hour: number; noop: number; writes: number}[];
    level?: RWLevel;
    hint?: RWHint;
    cause?: RWCause;
}
/** one physical radio (openccu-lite task 489): both stacks of a module shared through multimacd are one */
export interface RWDuty { interface: string; address: string; interfaces: string[]; radio?: string; name?: string; minutes: (number | null)[]; buckets: (number | null)[] }
export interface RWThresholds {
    average_per_minute: number;
    average_minutes: number;
    burst_per_minute: number;
    hold_minutes: number;
    duty_cycle_rise: number;
    duty_cycle_minutes: number;
    share: number;
    min_writes: number;
}
export interface RWView {
    at: string;
    /** the minutes the buckets cover (60, 360, 1440) and one bucket's */
    span: number;
    bucket_minutes: number;
    available: boolean;
    classic_rpc: boolean;
    thresholds: RWThresholds;
    clients: RWClient[];
    duty_cycle: RWDuty[];
}

/** the share of a client's writes that changed nothing, in whole percent; null without a write */
export function noopShare(c: {hour: number; noop: number}): number | null {
    return c.hour > 0 ? Math.round((c.noop * 100) / c.hour) : null;
}

/** the key a client is listed under: kind and name */
export function clientKey(c: {kind: string; name: string}): string {
    return `${c.kind}:${c.name}`;
}

/** a token's client by the token's name */
export function tokenClient(view: RWView | null, token: string): RWClient | undefined {
    return view?.clients.find((c) => c.kind === 'token' && c.name === token);
}

const TOPS = [10, 20, 50, 100, 200, 500, 1000, 2000, 5000];

/**
 * The top of the clients' sparklines: one scale for all of them, so a quiet client's row is low
 * beside a busy one's - the next readable step at or above the busiest minute of any client.
 */
export function writesTop(clients: {minutes: number[]}[]): number {
    const peak = Math.max(0, ...clients.flatMap((c) => c.minutes));
    return TOPS.find((s) => peak <= s) ?? Math.ceil(peak / 1000) * 1000;
}

/** the top of a duty cycle's sparkline: 25 % (where the radio-load warning begins) or the next step the series reaches */
export function dutyTop(series: {buckets: (number | null)[]}[]): number {
    const peak = Math.max(0, ...series.flatMap((s) => s.buckets.filter((v): v is number => v !== null)));
    return peak <= 25 ? 25 : peak <= 50 ? 50 : 100;
}

/** a duty-cycle series' last reading; null without one */
export function lastReading(minutes: (number | null)[]): number | null {
    for (let i = minutes.length - 1; i >= 0; i--) if (minutes[i] !== null) return minutes[i]!;
    return null;
}

/** how many minutes ago a sparkline's entry is: the last is the running minute, 0 */
export function minutesAgo(index: number, length: number): number {
    return length - 1 - index;
}

/**
 * A line's points from a series with holes: the runs of readings, each as "x,y x,y"; x in
 * [0, width] over the series' length, y from the bottom. A lone reading is a run of one.
 */
export function lineRuns(minutes: (number | null)[], top: number, width: number, height: number, pad = 1.5): string[][] {
    const n = minutes.length;
    const step = n > 0 ? width / n : width;
    const runs: string[][] = [];
    let run: string[] = [];
    minutes.forEach((v, i) => {
        if (v === null) {
            if (run.length) runs.push(run);
            run = [];
            return;
        }
        const y = height - pad - (Math.max(0, Math.min(top, v)) / top) * (height - 2 * pad);
        run.push(`${(i * step + step / 2).toFixed(1)},${y.toFixed(1)}`);
    });
    if (run.length) runs.push(run);
    return runs;
}

/** the share of a client's writes in the span that changed nothing, in whole percent; null without a write */
export function spanNoopShare(c: {writes: number; writes_noop: number}): number | null {
    return c.writes > 0 ? Math.round((c.writes_noop * 100) / c.writes) : null;
}

/**
 * openccu-lite task 489: when a bucket of the view begins and ends - the last bucket holds the
 * view's `at`, buckets start at a multiple of their length since the epoch (as the system counts
 * them).
 */
export function bucketTimes(at: string, bucketMinutes: number, index: number, length: number): {start: Date; end: Date} {
    const ms = bucketMinutes * 60_000;
    const last = Math.floor(Date.parse(at) / ms) * ms;
    const start = last - (length - 1 - index) * ms;
    return {start: new Date(start), end: new Date(start + ms)};
}
