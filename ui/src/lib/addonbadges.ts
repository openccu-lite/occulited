/**
 * Which declaration mark an addon's card carries (occulited B-163).
 *
 * `undeclared` means the system holds no runtime declaration of the addon. For an addon that came
 * along from before openccu-lite while the manifest in its own directory declares what it needs
 * (`adopted_confinable`), that reads as a contradiction beside the root badge's hint, which says
 * the manifest declares it can run as its own user. So:
 *
 * - adopted, its manifest declares, root: no mark of its own - the root badge's hint says it
 *   (`'in-root-hint'`);
 * - adopted, its manifest declares, not root (switched on the Services page): `'not-from-package'`;
 * - no declaration anywhere: `'undeclared'`;
 * - declared: nothing.
 */
export type DeclarationMark = '' | 'undeclared' | 'not-from-package' | 'in-root-hint';

export function declarationMark(a: {undeclared?: boolean; adopted_confinable?: boolean; policy_mode?: string}): DeclarationMark {
    if (a.adopted_confinable) {
        if (a.policy_mode === 'root') return 'in-root-hint';
        return a.undeclared ? 'not-from-package' : '';
    }
    return a.undeclared ? 'undeclared' : '';
}
