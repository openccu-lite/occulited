// The recovery system's state as its 503 tells it (openccu-lite task 505), and how far an install
// has got. Pure logic - no DOM, no fetch - so vitest runs it in node.
//
// While the recovery system runs, its lighttpd answers every /api/ path with
//
//   503 {"error": "updating", "recovery": true,
//        "update": {"running": true, "phase": "prepare|install|starting|done|failed",
//                   "step": 5, "steps": 7, "flashing": "rootfs", "written": n, "total": n,
//                   "log_bytes": n}}
//
// (the fork's lite_unavailable.cgi, which reads upstream fwinstall.sh's lock file and log, a running
// dd and the target partition's sectors-written counter from outside). The object is absent when the
// recovery runs for anything else, and every field may be: an older recovery sends only the error.
//
// fwinstall.sh's steps are [1/7]..[7/7] (prepare) and [1/5]..[5/5] (install); the last writes the
// boot and the system partition. They are few and very unevenly long, so the bar weighs them by
// what one install took on a Raspberry Pi 4 with a 21 MB/s card (the study of task 505, 361 s of
// recovery work): the storage test, the unpacking, the verification and the two partitions are 98 %
// of it. Inside the flash steps the fraction is real (bytes on the card); inside the others it creeps
// with the time the step has run against its weight, never past 90 % of the step, so the bar moves
// without claiming what it does not know.

import {isRecoveryAnswer} from './bootbar';

export type UpdatePhase = 'starting' | 'prepare' | 'install' | 'done' | 'failed';

export interface UpdateStatus {
    running: boolean;
    phase: UpdatePhase;
    /** the step within the phase, 1-based, and how many the phase has */
    step?: number;
    steps?: number;
    /** the partition a dd writes now: "bootfs", "rootfs" or the device's name */
    flashing?: string;
    /** bytes on the card and the source's size, while a partition is written */
    written?: number;
    total?: number;
    /** the size of /fwinstall.log, the raw output */
    logBytes?: number;
}

export interface RecoveryState {
    /** the recovery system answered (503 "updating") */
    recovery: boolean;
    /** the install it runs, when it runs one and says so */
    update: UpdateStatus | null;
}

const PHASES: readonly UpdatePhase[] = ['starting', 'prepare', 'install', 'done', 'failed'];
const nat = (n: unknown): n is number => typeof n === 'number' && Number.isFinite(n) && n >= 0 && Number.isInteger(n);

/** The update object of a 503's body, validated field by field: what does not fit is left out. */
export function parseUpdate(u: unknown): UpdateStatus | null {
    if (!u || typeof u !== 'object') return null;
    const x = u as Record<string, unknown>;
    const phase = PHASES.includes(x.phase as UpdatePhase) ? (x.phase as UpdatePhase) : null;
    if (!phase) return null;
    const out: UpdateStatus = {running: x.running === true, phase};
    if (nat(x.step) && nat(x.steps) && x.step >= 1 && x.step <= x.steps && x.steps <= 99) {
        out.step = x.step;
        out.steps = x.steps;
    }
    if (typeof x.flashing === 'string' && /^[A-Za-z0-9_-]{1,32}$/.test(x.flashing)) out.flashing = x.flashing;
    if (out.flashing && nat(x.written) && nat(x.total) && x.total > 0) {
        out.written = Math.min(x.written, x.total);
        out.total = x.total;
    }
    if (nat(x.log_bytes)) out.logBytes = x.log_bytes;
    return out;
}

/** What a health answer says about the recovery system: null when it is not the recovery's. */
export function recoveryOf(status: number | null, body: unknown): RecoveryState | null {
    if (!isRecoveryAnswer(status, body)) return null;
    return {recovery: true, update: parseUpdate((body as {update?: unknown}).update)};
}

// ---- the weights ------------------------------------------------------------------------------

/** Seconds per step on the measured Pi 4 (task 505's study, 2026-11-25). */
const PREPARE_S = [3, 3, 1, 28, 119, 53, 0.5];
const INSTALL_S = [0.25, 0.25, 0.25, 0.25, 0.5];
const BOOTFS_S = 20;
const ROOTFS_S = 133;
const TOTAL_S = [...PREPARE_S, ...INSTALL_S].reduce((a, b) => a + b, 0) + BOOTFS_S + ROOTFS_S;
/** A step without a real fraction never shows more than this much of itself done. */
export const CREEP_CAP = 0.9;

export type StepKey =
    | 'starting'
    | 'p1' | 'p2' | 'p3' | 'p4' | 'p5' | 'p6' | 'p7'
    | 'check'
    | 'bootfs' | 'rootfs' | 'write'
    | 'done' | 'failed';

export interface InstallView {
    /** 0..1 of the recovery's whole work */
    fraction: number;
    /** the overall step, 1..12, and the count; null before the first step */
    step: number | null;
    steps: number;
    /** which words describe the current step (installText) */
    key: StepKey;
    /** the real percentage inside a flash step, null elsewhere */
    flashPercent: number | null;
    /** the weight-time this step is expected to take, for the creep (seconds) */
    stepSeconds: number;
    done: boolean;
    failed: boolean;
}

/** The step's identity, for telling when a new one began (the creep starts again then). */
export function stepId(u: UpdateStatus | null): string {
    if (!u) return '';
    return `${u.phase}:${u.step ?? 0}/${u.steps ?? 0}:${u.flashing ?? ''}`;
}

/**
 * The install's progress from the update object; `inStepS` is how long the page has seen the
 * current step (for the creep inside a step without real numbers).
 */
export function installView(u: UpdateStatus, inStepS = 0): InstallView {
    const steps = PREPARE_S.length + INSTALL_S.length;
    const base = {steps, flashPercent: null as number | null, done: false, failed: false};
    if (u.phase === 'done') return {...base, fraction: 1, step: steps, key: 'done', stepSeconds: 0, done: true};
    let doneS = 0;
    let stepS = 0;
    let step: number | null = null;
    let key: StepKey = 'starting';
    let inner: number | null = null;
    const s = u.step;
    if (u.steps === 7 && s) {
        doneS = PREPARE_S.slice(0, s - 1).reduce((a, b) => a + b, 0);
        stepS = PREPARE_S[s - 1]!;
        step = s;
        key = `p${s}` as StepKey;
    } else if (u.steps === 5 && s) {
        doneS = PREPARE_S.reduce((a, b) => a + b, 0) + INSTALL_S.slice(0, s - 1).reduce((a, b) => a + b, 0);
        stepS = INSTALL_S[s - 1]!;
        step = PREPARE_S.length + s;
        key = s < 5 ? 'check' : 'write';
        if (s === 5) {
            stepS = INSTALL_S[4]! + BOOTFS_S + ROOTFS_S;
            if (u.flashing) {
                const boot = u.flashing === 'bootfs';
                doneS += INSTALL_S[4]! + (boot ? 0 : BOOTFS_S);
                stepS = boot ? BOOTFS_S : ROOTFS_S;
                key = boot ? 'bootfs' : 'rootfs';
                if (u.written !== undefined && u.total) inner = u.written / u.total;
            }
        }
    }
    const creep = stepS > 0 ? Math.min(CREEP_CAP, Math.max(0, inStepS) / stepS) : 0;
    const within = inner ?? creep;
    const fraction = Math.min(1, Math.max(0, (doneS + stepS * within) / TOTAL_S));
    return {
        ...base,
        fraction,
        step,
        key: u.phase === 'failed' ? 'failed' : key,
        flashPercent: inner === null ? null : Math.floor(inner * 100),
        stepSeconds: stepS,
        failed: u.phase === 'failed',
    };
}

/**
 * The bar never goes back: a new answer that would show less than the page already showed (a
 * reconnect, an answer from a counter that lags) keeps the old fraction.
 */
export function monotonic(previous: number, next: number): number {
    return Math.max(previous, next);
}

// ---- the raw output --------------------------------------------------------------------------

/**
 * fwinstall.sh's log as text: it is written for the recovery's own HTML page, with <br/> for the
 * line ends and nothing else of HTML. Tags become line ends or vanish; the entities it could hold
 * are decoded; runs of the progress dots stay as they are.
 */
export function logText(raw: string): string {
    return raw
        .replace(/<br\s*\/?>\s*\n?/gi, '\n')
        .replace(/<[^>]{0,200}>/g, '')
        .replace(/&lt;/g, '<')
        .replace(/&gt;/g, '>')
        .replace(/&quot;/g, '"')
        .replace(/&#39;/g, "'")
        .replace(/&amp;/g, '&')
        .replace(/\r/g, '')
        .replace(/\n{3,}/g, '\n\n');
}

/** Whether an answer to /fwinstall.log is the log: the recovery serves it as text/plain; the
 *  system's own server answers that path with the app's page (HTML), and a missing log is a
 *  redirect to the recovery's menu - neither is the output. */
export function isLogAnswer(status: number, contentType: string | null, redirected: boolean): boolean {
    return (status === 200 || status === 206) && !redirected && /^text\/plain\b/i.test(contentType ?? '');
}

// ---- the shell's state, answer by answer --------------------------------------------------------

/** What the shell knows about the recovery system (lib/inrecovery.svelte.ts holds one). */
export interface RecoveryTrack {
    active: boolean;
    update: UpdateStatus | null;
    /** when the page first saw the recovery answer (ms) */
    since: number;
    /** when the current step began, as far as the page saw (ms) */
    stepSince: number;
    /** the highest fraction shown, so the bar never goes back */
    shown: number;
    /** the last answer of the recovery (ms) */
    lastSeen: number;
    step: string;
}

export const NOT_IN_RECOVERY: RecoveryTrack = {active: false, update: null, since: 0, stepSince: 0, shown: 0, lastSeen: 0, step: ''};

/**
 * One health answer applied: the recovery's sets and refreshes the state; the system answering
 * again - occulited, its lighttpd while occulited starts (any other 5xx), a login wall - ends it;
 * no answer at all (the recovery rebooting, the network) changes nothing.
 */
export function trackRecovery(prev: RecoveryTrack, status: number | null, body: unknown, now: number): RecoveryTrack {
    const r = recoveryOf(status, body);
    if (r) {
        const fresh = !prev.active;
        const id = stepId(r.update);
        const newStep = fresh || id !== prev.step;
        const stepSince = newStep ? now : prev.stepSince;
        const shown = r.update ? monotonic(fresh ? 0 : prev.shown, installView(r.update, (now - stepSince) / 1000).fraction) : fresh ? 0 : prev.shown;
        return {active: true, update: r.update, since: fresh ? now : prev.since, stepSince, shown, lastSeen: now, step: id};
    }
    if (!prev.active || status === null || status === 404) return prev;
    return NOT_IN_RECOVERY;
}
