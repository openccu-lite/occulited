/*
 * occulited task 162: GET /radio/health's history and rates are a ring of 500 samples per series
 * (8 hours), and the Status page polled the whole of it every 10 s - 160 to 230 KiB an answer. A
 * page that holds the last answer asks with its `history_cursor` and gets only the samples appended
 * since (`history_delta`), which it appends here; every other field of the answer is whole.
 */

/** the part of a /radio/health answer this merge touches */
export interface HistoryAnswer<S = unknown, R = unknown> {
    history?: Record<string, S[]>;
    rates?: Record<string, R[]>;
    history_cursor?: string;
    history_delta?: boolean;
    history_rows?: number;
}

export const HEALTH_PATH = '/api/system/v1/radio/health';

/** the request for the next answer: with the cursor of the one the page holds */
export function healthPath(held: HistoryAnswer | null | undefined): string {
    const c = held?.history_cursor;
    return c ? `${HEALTH_PATH}?after=${encodeURIComponent(c)}` : HEALTH_PATH;
}

function appendSeries<T>(held: Record<string, T[]> | undefined, add: Record<string, T[]> | undefined, rows: number): Record<string, T[]> {
    const out: Record<string, T[]> = {...(held ?? {})};
    for (const [k, v] of Object.entries(add ?? {})) {
        const joined = [...(out[k] ?? []), ...v];
        out[k] = rows > 0 && joined.length > rows ? joined.slice(joined.length - rows) : joined;
    }
    return out;
}

/** the answer to show: a whole one as it came, a delta's samples appended to the held history */
export function mergeHealth<T extends HistoryAnswer>(held: T | null | undefined, next: T): T {
    if (!next.history_delta || !held) return next;
    const rows = next.history_rows ?? 0;
    return {...next, history: appendSeries(held.history, next.history, rows), rates: appendSeries(held.rates, next.rates, rows)};
}
