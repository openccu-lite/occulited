// The route while the system is in its recovery system (openccu-lite task 505): every page of the
// shell needs occulited's API, which is not there, so the shell shows the recovery's view instead -
// a direct URL, a bookmark, Back and Forward included - and goes back to where it was once the
// system answers again. Pure, so vitest runs it.

export const RECOVERY_PATH = '/recovery';

/** Where the route has to go now, or null when it may stay. `returnTo` is where it came from. */
export function recoveryRoute(path: string, active: boolean, returnTo: string): string | null {
    if (active) return path === RECOVERY_PATH ? null : RECOVERY_PATH;
    // the recovery is over: back to the page it interrupted (Status when it began on this view)
    if (path === RECOVERY_PATH && returnTo) return returnTo === RECOVERY_PATH ? '/' : returnTo;
    return null;
}

/** What to remember as the way back when the guard leaves `path` (with its query). */
export function returnPath(path: string, search: string): string {
    return path === RECOVERY_PATH ? '' : path + search;
}

/** The recovery's address as a link: http://<IPv4 or [IPv6]>/ and nothing else, '' otherwise. */
export function validRecoveryUrl(u: string): string {
    return /^http:\/\/(\d{1,3}(\.\d{1,3}){3}|\[[0-9a-fA-F:.]+\])\/$/.test(u) ? u : '';
}

// ---- the way back across a reload (occulited B-187) -----------------------------------------------
// The update's boot watch reloads the page once the system answers again. When the reload came
// before the shell had left the recovery view - on an OVA the recovery took 105 s and the reload won
// the race - the way back was gone with the page, and the view stayed on "not in its recovery
// system". So the way back is kept in this tab's sessionStorage until it is used.

/** sessionStorage's key for the page the recovery interrupted */
export const RETURN_KEY = 'occulite.recovery-return';

/** A way back worth keeping: a path of the shell (with its query), never the recovery view. */
export function validReturn(p: string): string {
    return /^\/(?!\/)[^\s]{0,500}$/.test(p) && !p.startsWith(RECOVERY_PATH + '?') && p !== RECOVERY_PATH ? p : '';
}

export function loadReturn(): string {
    try {
        return validReturn(sessionStorage.getItem(RETURN_KEY) ?? '');
    } catch {
        return '';
    }
}

export function saveReturn(p: string): void {
    try {
        if (validReturn(p)) sessionStorage.setItem(RETURN_KEY, p);
        else sessionStorage.removeItem(RETURN_KEY);
    } catch {
        /* the way back then lives as long as the page */
    }
}
