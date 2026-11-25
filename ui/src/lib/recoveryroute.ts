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
