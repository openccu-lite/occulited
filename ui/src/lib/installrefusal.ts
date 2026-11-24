// The system's refusals of a package before any of its code ran: a package for a newer
// openccu-lite (occulited task 134), a package for another architecture (occulited B-167). The
// daemon says them in English; this takes such a message apart into the catalogue's phrases and
// their values, so the page can say it in the user's language (install.ts: liteRefusal). Pure,
// without the i18n state, so a unit test can load it.

/** one phrase of the catalogue (i18n.svelte.ts) and its values */
export type Phrase = [key: string, params?: Record<string, string>];

/** The phrases a refusal is made of, or null for any other message. */
export function refusalPhrases(message: string): Phrase[] | null {
    const m = /^(\S+) requires openccu-lite (\S+) or later; this system runs (\S+)\./.exec(message);
    if (m) return [['{id} requires openccu-lite {version} or later; this system runs {running}. Nothing was installed.', {id: m[1]!, version: m[2]!, running: m[3]!}]];
    const a = /^This package(?: of (\S+))? is for (.+?); this system is (\S+?)\. (?:Take the package for \S+: (\S+)\.|(The addon has no package for) \S+\.|Take the package for \S+, where the addon has one\.) Nothing was installed\.$/.exec(message);
    if (!a) return null;
    const [, id, pkg, system, file, none] = a;
    return [
        id ? ['This package of {id} is for {package}; this system is {system}.', {id, package: pkg!, system: system!}] : ['This package is for {package}; this system is {system}.', {package: pkg!, system: system!}],
        file ? ['Take the package for {system}: {file}.', {system: system!, file}] : none ? ['The addon has no package for {system}.', {system: system!}] : ['Take the package for {system}, where the addon has one.', {system: system!}],
        ['Nothing was installed.'],
    ];
}
