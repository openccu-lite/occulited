// occulited B-90: a backup is prepared on the system before its download. GET /backup alone writes
// nothing until the .sbk is complete - minutes on a large /usr/local - and lighttpd ends an HTTP/2
// stream that writes nothing for 6 minutes. So the page starts the backup (POST
// /backup/prepare), polls it, and then follows /backup?prepared=<id>, which streams at once.
import {api, ApiError} from './api';
import {formatDecimal} from './storagepanels';

type Translate = (key: string, params?: Record<string, string | number>) => string;

export interface BackupJob {
    id: string;
    state: 'running' | 'ready' | 'downloaded' | 'failed';
    started: string;
    finished?: string;
    size?: number;
    error?: string;
    /** why a failed one failed, where the page words it itself: "no-space" (occulited B-146) */
    code?: string;
    detail?: Record<string, unknown>;
}

export const BACKUP_URL = '/api/system/v1/backup';

const wait = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

/**
 * Starts the backup, or joins the one that runs or waits, and resolves when it is ready to
 * download; a failed one throws its reason. onState sees every answer.
 */
export async function prepareBackup(onState?: (job: BackupJob) => void, interval = 2000, sleep: (ms: number) => Promise<void> = wait): Promise<BackupJob> {
    let job = await api.post<BackupJob>(`${BACKUP_URL}/prepare`);
    onState?.(job);
    while (job.state === 'running') {
        await sleep(interval);
        job = await api.get<BackupJob>(`${BACKUP_URL}/prepare/${encodeURIComponent(job.id)}`);
        onState?.(job);
    }
    if (job.state !== 'ready') throw failure(job);
    return job;
}

/**
 * Waits for a backup started before (B-95: the page went to the identity provider and came back):
 * resolves with it once ready, throws the reason when it failed, was downloaded or is gone (a 404).
 */
export async function awaitBackup(id: string, onState?: (job: BackupJob) => void, interval = 2000, sleep: (ms: number) => Promise<void> = wait): Promise<BackupJob> {
    for (;;) {
        const job = await api.get<BackupJob>(`${BACKUP_URL}/prepare/${encodeURIComponent(id)}`);
        onState?.(job);
        if (job.state === 'ready') return job;
        if (job.state !== 'running') throw failure(job);
        await sleep(interval);
    }
}

/** The download URL of a prepared backup, with further query parameters (encrypted=false, confirm). */
export function preparedUrl(id: string, extra: Record<string, string> = {}): string {
    const q = new URLSearchParams({prepared: id, ...extra});
    return `${BACKUP_URL}?${q.toString()}`;
}

/** Seconds as m:ss - the time the backup has been running. */
export function elapsed(seconds: number): string {
    const s = Math.max(0, Math.floor(seconds));
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

/** a failed job as the error its caller throws: its code and detail kept for backupFailure */
function failure(job: BackupJob): Error {
    return job.code ? new ApiError(0, job.code, job.error || job.state, job.detail) : new Error(job.error || job.state);
}

/**
 * The words for a backup that could not be made (occulited B-146): a backup the system has no room
 * for names the sizes and the largest directories, so the operator can mark them .nobackup; any
 * other failure is its message. Pure: the page hands in its t().
 */
export function backupFailure(err: unknown, lang: string, t: Translate): string {
    const e = err as Error;
    if (!(err instanceof ApiError) || err.code !== 'no-space' || !err.detail || err.detail.backup_bytes === undefined) return e?.message ?? String(err);
    const d = err.detail;
    const f = (n: unknown) => formatDecimal(Number(n), lang);
    const lines = [
        t('Not enough free space on the system for this backup: it holds {size}, and making it needs {need} (the archive twice while it is built, and {reserve} that stay free); {free} are free. Nothing was written.', {
            size: f(d.backup_bytes),
            need: f(d.need_bytes),
            reserve: f(d.reserve_bytes),
            free: f(d.free_bytes),
        }),
    ];
    const largest = Array.isArray(d.largest) ? (d.largest as {path: string; bytes: number}[]) : [];
    if (largest.length) {
        lines.push(t('The largest directories and files: {dirs}.', {dirs: largest.map((x) => `${x.path} (${f(x.bytes)})`).join(', ')}));
        lines.push(t('A directory that holds a file named .nobackup is left out of the backup.'));
    }
    return lines.join(' ');
}
