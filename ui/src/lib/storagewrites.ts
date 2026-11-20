// occulited task 159: System → Storage's "Disk writes" - GET /storage/writes and what the panel
// derives from it.

export interface WriteWindow { seconds: number; written_bytes: number }
export interface WritesPartition { name: string; read_bytes: number; written_bytes: number; mount?: string; role?: string }
export interface WritesHint { bytes_per_hour: number; seconds: number; limit_bytes_per_hour: number; top_units: string[] }
export interface WritesDisk {
    name: string;
    kind?: string;
    vendor?: string;
    model?: string;
    read_bytes: number;
    written_bytes: number;
    hour: WriteWindow;
    day: WriteWindow;
    partitions: WritesPartition[];
    hint?: WritesHint;
}
export interface UnitDisk { disk: string; read_bytes: number; written_bytes: number; hour: WriteWindow; day: WriteWindow }
export interface WritesUnit {
    unit: string;
    kind: 'unit' | 'kernel' | 'ended';
    running: boolean;
    read_bytes: number;
    written_bytes: number;
    hour: WriteWindow;
    day: WriteWindow;
    disks: UnitDisk[];
}
export interface WritesRecording { supported: boolean; enabled: boolean; active: boolean; since?: string }
export interface WritesView {
    at: string;
    uptime_s: number;
    interval_s: number;
    recording: WritesRecording;
    disks: WritesDisk[];
    units: WritesUnit[];
}

/** the span the table shows: the last hour, the last day, or everything counted */
export type Span = 'hour' | 'day' | 'all';

/** bytes per hour over a window; null while the window covers less than a minute */
export function ratePerHour(w: WriteWindow): number | null {
    return w.seconds >= 60 ? (w.written_bytes * 3600) / w.seconds : null;
}

/** what a row wrote in the span, on one disk ('' = on all of them) */
export function writtenIn(u: WritesUnit, span: Span, disk = ''): number {
    const pick = (x: {written_bytes: number; hour: WriteWindow; day: WriteWindow}) => (span === 'all' ? x.written_bytes : x[span].written_bytes);
    if (!disk) return pick(u);
    const d = u.disks.find((x) => x.disk === disk);
    return d ? pick(d) : 0;
}

/** the window a row's rate is taken over in the span; none for "all" (a unit's start is not known) */
export function windowIn(u: WritesUnit, span: Span, disk = ''): WriteWindow | null {
    if (span === 'all') return null;
    if (!disk) return u[span];
    return u.disks.find((x) => x.disk === disk)?.[span] ?? null;
}

/** the rows that wrote something in the span, the most first; the two rows that are no unit keep their place by amount */
export function rowsIn(units: WritesUnit[], span: Span, disk = ''): WritesUnit[] {
    return units
        .filter((u) => writtenIn(u, span, disk) > 0)
        .sort((a, b) => writtenIn(b, span, disk) - writtenIn(a, span, disk) || a.unit.localeCompare(b.unit));
}

/** a unit's name as the Services page shows it: without .service */
export function unitLabel(unit: string): string {
    return unit.endsWith('.service') ? unit.slice(0, -'.service'.length) : unit;
}

/** the id of the unit's row on the Services page, when it is a service */
export function serviceID(u: WritesUnit): string | null {
    return u.kind === 'unit' && u.unit.endsWith('.service') ? unitLabel(u.unit) : null;
}

/** a span of seconds in the two largest units that say something: 45 s, 12 min, 3 h 20 min, 12 d 4 h */
export function spanParts(seconds: number): {d: number; h: number; min: number; s: number} {
    const s = Math.max(0, Math.floor(seconds));
    return {d: Math.floor(s / 86400), h: Math.floor((s % 86400) / 3600), min: Math.floor((s % 3600) / 60), s: s % 60};
}

/** whether the window covers clearly less than what the span names (the history is younger) */
export function shortOf(w: WriteWindow | null, span: Span): boolean {
    if (!w || span === 'all') return false;
    return w.seconds < (span === 'hour' ? 3600 : 86400) * 0.95;
}
