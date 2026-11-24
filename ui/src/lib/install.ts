/*
 * The catalogue install, one flow for the two pages that start it (task 56): the Catalogue's
 * Install and Update buttons and the Update button of the Installed addons page. The same
 * question through the shell's own dialog, the same POST; the run itself is the box's, and
 * `InstallProgress.svelte` shows it on whichever page is open.
 */
import {refusalPhrases} from './installrefusal';
import {api, ApiError} from './api';
import {ask} from './dialog.svelte';
import {t} from './i18n.svelte';
import {prereleaseKind} from './prerelease';

/**
 * occulited B-113: the message for a refused start. The system's one install slot answers 409
 * `install-running` to a second install or an uninstall while one runs (B-25); its English message
 * says which - this says it in the reader's language. Any other error is its own message.
 */
export function installError(err: unknown): string {
    if (err instanceof ApiError && err.code === 'install-running') {
        return err.message.startsWith('an uninstall') ? t('An uninstall is still running. Wait until it has finished.') : t('Another install is still running. Wait until it has finished.');
    }
    return liteRefusal((err as Error)?.message ?? String(err));
}

/**
 * The system's refusal of a package before any of its code ran, in the user's language (occulited
 * task 134: a newer openccu-lite; occulited B-167: another architecture). Any other message is
 * returned as it came.
 */
export function liteRefusal(message: string): string {
    const phrases = refusalPhrases(message);
    return phrases ? phrases.map(([key, params]) => t(key, params)).join(' ') : message;
}

/** B-98 (D-67): an addon that ran before an install that failed or asks for a reboot, and is stopped now */
export interface StoppedAddon {
    id: string;
    /** the next boot starts it anyway: enabled, not switched off on the Services page, no safe mode */
    starts_at_boot: boolean;
}

/** what an install answers, as far as the question below needs it (the upload's answer and a catalogue run's result) */
export interface InstallOutcome {
    exit?: number;
    reboot_required?: boolean;
    stopped_addons?: StoppedAddon[];
}

/**
 * B-98, decided in D-67: an install that failed or asks for a reboot starts nothing, so an addon its
 * update script stopped stays stopped. The box names such addons in the result, and this asks, addon
 * by addon, whether to start it again: Start is the Services page's normal unit start, Leave stopped
 * leaves it. After a requested reboot the question says the boot starts the addon anyway when the box
 * says it will (`starts_at_boot`). Resolves to the ids started.
 */
export async function askStartStopped(r: InstallOutcome | null | undefined): Promise<string[]> {
    const list = r?.stopped_addons ?? [];
    if (!list.length) return [];
    let names: Record<string, string> = {};
    try {
        const {addons} = await api.get<{addons: {id: string; name?: string}[]}>('/api/system/v1/addons');
        names = Object.fromEntries(addons.map((a) => [a.id, a.name || a.id]));
    } catch {
        /* the ids stand in for the names */
    }
    const started: string[] = [];
    for (const s of list) {
        const name = names[s.id] ?? s.id;
        let message = t('{name} was running before the update. Start it again?', {name});
        if (r?.reboot_required && s.starts_at_boot) message += `\n\n${t('It is enabled, so the reboot the installation asks for starts it anyway.')}`;
        const ok = await ask({
            message,
            confirm: t('Start'),
            cancel: t('Leave stopped'),
            run: async () => {
                await api.post(`/api/system/v1/services/${encodeURIComponent(`addon-${s.id}`)}/start`);
            },
        });
        if (ok) started.push(s.id);
    }
    return started;
}

export interface Installable {
    id: string;
    name: string;
    status?: string;
    latest?: {version: string; prerelease?: boolean};
    /** occulited B-174: the catalogue's word on how it would run; absent = not known (no manifest read) */
    install_mode?: 'root' | 'confined';
}

/**
 * occulited B-174: the install question says what the system will do - root only when the addon
 * will run as root, its own user when it runs confined, and that it cannot tell yet when no
 * manifest was read (the dialog used to claim root for every addon, Logic's card said it needs no
 * extra right).
 */
export function installQuestion(e: Pick<Installable, 'name' | 'install_mode'>): string {
    if (e.install_mode === 'root') return t('Install {name}? It runs as root and can do anything on this system.', {name: e.name});
    if (e.install_mode === 'confined') return t('Install {name}? It runs as its own user and can write only to its own directories and what its manifest declares.', {name: e.name});
    return t('Install {name}? Its manifest has not been read yet, so whether it runs as its own user or as root is not known before the install.', {name: e.name});
}

/**
 * Asks, then starts the install of `e`. `installed` is the version on the box, if any: an update
 * takes the same route, install_addon on an installed addon runs its update_script.
 * Resolves to `null` when nothing was started (the question was declined), to `''` when the run
 * is on, and to the error message when the box refused it.
 */
export async function installFromCatalog(e: Installable, installed?: string): Promise<string | null> {
    const warn = installed
        ? t('Update {name} from {installed} to {version}? The addon replaces its own installation; its configuration is kept.', {name: e.name, installed, version: e.latest?.version ?? '?'})
        : e.status === 'incompatible'
          ? t('This addon is known not to work without ReGa. Install anyway?')
          : installQuestion(e);
    // occulited task 171: the question says it when what comes is a pre-release
    const pre = prereleaseKind(e.latest?.version, e.latest?.prerelease) ? '\n\n' + t('{version} is a pre-release: not all functions have been tested yet.', {version: e.latest?.version ?? ''}) : '';
    if (!(await ask({message: warn + pre, confirm: installed ? t('Update') : t('Install')}))) return null;
    try {
        await api.post(`/api/system/v1/catalog/${encodeURIComponent(e.id)}/install`);
        return '';
    } catch (err) {
        return installError(err);
    }
}
