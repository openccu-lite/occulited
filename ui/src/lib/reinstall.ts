/*
 * occulited B-181: "Reinstall from the catalogue" replaced RedMatic 10.0.0-beta.0 with the stable
 * 9.20.0 - a downgrade - after a question that named no version. This is what the question says,
 * as phrases (an i18n key with its values; install.ts translates them, so this module stays pure).
 *
 * It names the version that comes and the installed one. A downgrade - the catalogue's release older
 * by version order, pre-releases included - is said plainly, and the dialog's default becomes *Keep
 * <installed>* (Enter keeps it), with *Downgrade to <version>* in the warning colour. A newer channel
 * is not offered: the catalogue resolves only its own (the stable releases, or pre-releases while an
 * addon has no stable one), so a pre-release installed some other way is not on offer; the text says
 * where a newer version comes from instead.
 */
import type {Phrase} from './installrefusal';
import {compareVersions, prereleaseKind} from './prerelease';

/**
 * `downgrade` when the catalogue's release is older than the installed version, `unknown` when the
 * installed version cannot be read (a disabled addon whose script does not run), else
 * `same-or-newer` - also for two versions that cannot be ordered: no warning is claimed then.
 */
export type ReinstallKind = 'downgrade' | 'unknown' | 'same-or-newer';

export function reinstallKind(version: string | undefined, installed: string | undefined): ReinstallKind {
    if (!installed?.trim()) return 'unknown';
    const c = compareVersions(version, installed);
    return c !== null && c < 0 ? 'downgrade' : 'same-or-newer';
}

export interface ReinstallPlan {
    /** the message's paragraphs; the phrases of one paragraph go on lines of their own */
    paragraphs: Phrase[][];
    confirm: Phrase;
    cancel?: Phrase;
    /** a downgrade: the confirming button in the warning colour, and Cancel (Keep) the default */
    danger?: boolean;
}

export function reinstallPlan(e: {name: string; latest?: {version: string; prerelease?: boolean}}, installed: string | undefined, lead?: Phrase): ReinstallPlan {
    const version = e.latest?.version ?? '?';
    const have = installed?.trim() ?? '';
    const kind = reinstallKind(e.latest?.version, have);
    const paragraphs: Phrase[][] = [
        [lead ?? ['Reinstall {name} from the catalogue? The addon replaces its own installation; its configuration is kept.', {name: e.name}]],
        [have ? ['Installed: {version}', {version: have}] : ['Installed: unknown version'], ['From the catalogue: {version}', {version}]],
    ];
    if (kind === 'downgrade') paragraphs.push([["{version} is older than the installed {installed}: this reinstall is a downgrade, and an older version may not read what the newer one stored. The catalogue offers no newer version; a newer one comes through the addon's own update or an upload of its package.", {version, installed: have}]]);
    if (kind === 'unknown') paragraphs.push([['The installed version is not known; if it is newer than {version}, this reinstall is a downgrade.', {version}]]);
    if (prereleaseKind(e.latest?.version, e.latest?.prerelease)) paragraphs.push([['{version} is a pre-release: not all functions have been tested yet.', {version}]]);
    if (kind === 'downgrade') return {paragraphs, confirm: ['Downgrade to {version}', {version}], cancel: ['Keep {version}', {version: have}], danger: true};
    return {paragraphs, confirm: ['Reinstall']};
}
