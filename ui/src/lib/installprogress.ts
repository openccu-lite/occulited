/*
 * occulited task 165: which catalogue run the progress notice still shows. The system keeps the
 * last run in /catalog/progress until the next one starts, so without a rule an update of last
 * month stayed a notice with a full bar on every visit of the Addons page.
 *
 * - a run that is still on: always shown;
 * - a run that ended while the page was open: shown until the page is left or reloaded, or its
 *   close button is pressed;
 * - a run that had already ended when the page opened:
 *   - done: only within a minute of its end, so a reload right after the install still says how it
 *     went; older ones are gone;
 *   - failed: shown until its close button is pressed (the user must see why), but not when it is
 *     older than a day - the page's own state (the installed version) is the truth by then.
 *
 * The age is the browser's clock against the system's `finished`. A system whose clock is off
 * (no time sync yet) gives a negative or huge age: a done run is then not shown (the plain
 * "gone after a reload"), a failed one is shown until closed.
 *
 * The close button remembers the run (its addon and start) in this browser, so a reload does not
 * bring a closed failure back; another browser shows it until it is closed there or is a day old.
 */
export interface EndedRun { addon_id: string; phase: string; started: string; finished?: string }

/** how long a done run that ended before the page opened is still shown */
export const DONE_GRACE_MS = 60_000;
/** how long a failed run that ended before the page opened is shown until it is closed */
export const FAILED_MAX_MS = 24 * 3600_000;

const DISMISSED_KEY = 'ol.installProgress.dismissed';

/** the run's identity: one addon, one start */
export const runKey = (p: EndedRun | null): string => (p ? `${p.addon_id}@${p.started}` : '');

/** whether a run that had already ended when the page opened is still shown */
export function shownAtOpen(p: EndedRun, now: number, dismissed: string): boolean {
    if (!p.finished) return true;
    if (dismissed !== '' && dismissed === runKey(p)) return false;
    const end = Date.parse(p.finished);
    const age = Number.isNaN(end) ? Infinity : now - end;
    if (p.phase === 'failed') return !(age > FAILED_MAX_MS);
    return age >= 0 && age <= DONE_GRACE_MS;
}

/** the run this browser closed last ('' when none, or storage is not there) */
export function readDismissed(): string {
    try {
        return localStorage.getItem(DISMISSED_KEY) ?? '';
    } catch {
        return '';
    }
}

export function writeDismissed(key: string): void {
    try {
        localStorage.setItem(DISMISSED_KEY, key);
    } catch {
        /* a private window: closed for this page view only */
    }
}

/**
 * occulited B-178: whether the page still asks for the reboot the installer wanted. A new addon is
 * started after an exit 10 anyway (B-186), so once the system started what the install brought and
 * left nothing stopped, the reboot would only start what already runs.
 */
export function rebootAsked(r: {reboot_required?: boolean; started?: string[]; stopped_addons?: unknown[]} | null | undefined): boolean {
    return !!r?.reboot_required && !(r.started?.length && !r.stopped_addons?.length);
}
