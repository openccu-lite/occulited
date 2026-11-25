// Whether the system is in its recovery system right now, for the whole shell (openccu-lite task
// 505): the bar under the top bar, the locked menu, the route that shows the install.
//
// The shell learns it from the recovery's own answer, 503 {"error": "updating"} on any /api/ path
// (lib/recoverystatus.ts): an API call of any page (lib/api.ts) and every health poll of a boot
// watch (lib/bootwatch.ts) announce such an answer with the window event `ol:recovery`. From then on
// this module polls the health route itself, every POLL_MS, and applies each answer
// (trackRecovery): the update object, when the current step began (for the bar's creep), the
// highest fraction shown. The recovery is over when the system answers again; then the state is
// cleared and the polling stops.

import {probeHealth} from './bootwatch';
import {NOT_IN_RECOVERY, trackRecovery, type RecoveryTrack} from './recoverystatus';
import {validRecoveryUrl} from './recoveryroute';

export const POLL_MS = 3000;
/** where the recovery's own page is by IP address (D-56), when a page of the shell learnt it */
export const RECOVERY_URL_KEY = 'occulite.recovery-url';

class InRecovery {
    track = $state<RecoveryTrack>(NOT_IN_RECOVERY);
    get active() {
        return this.track.active;
    }
    get update() {
        return this.track.update;
    }
    get stepSince() {
        return this.track.stepSince;
    }
    get shown() {
        return this.track.shown;
    }
    get lastSeen() {
        return this.track.lastSeen;
    }

    note(status: number | null, body: unknown, now = Date.now()): void {
        const next = trackRecovery(this.track, status, body, now);
        if (next === this.track) return;
        this.track = next;
        if (next.active) ensurePolling();
        else stopPolling();
    }

    clear(): void {
        this.track = NOT_IN_RECOVERY;
        stopPolling();
    }
}

export const inRecovery = new InRecovery();

let timer: ReturnType<typeof setTimeout> | undefined;
let polling = false;

function ensurePolling(): void {
    if (polling) return;
    polling = true;
    const tick = async () => {
        if (!polling) return;
        const {status, body} = await probeHealth();
        if (!polling) return;
        inRecovery.note(status, body);
        if (polling) timer = setTimeout(tick, POLL_MS);
    };
    timer = setTimeout(tick, POLL_MS);
}

function stopPolling(): void {
    polling = false;
    clearTimeout(timer);
}

/** The shell listens once: lib/api.ts and lib/bootwatch.ts announce the recovery's answers. */
export function listenForRecovery(): () => void {
    const on = (ev: Event) => {
        const d = (ev as CustomEvent<{status: number; body: unknown}>).detail;
        inRecovery.note(d?.status ?? 503, d?.body);
    };
    window.addEventListener('ol:recovery', on);
    return () => window.removeEventListener('ol:recovery', on);
}

/** The recovery's page by IP address, as the page that started the install or the reboot left it. */
export function recoveryUrl(): string {
    try {
        return validRecoveryUrl(localStorage.getItem(RECOVERY_URL_KEY) ?? '');
    } catch {
        return '';
    }
}

export function rememberRecoveryUrl(url: string): void {
    try {
        if (validRecoveryUrl(url)) localStorage.setItem(RECOVERY_URL_KEY, url);
    } catch {
        /* the view then links the same origin only */
    }
}
