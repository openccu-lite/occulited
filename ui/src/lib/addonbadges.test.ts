import {describe, expect, it} from 'vitest';

import {declarationMark} from './addonbadges';

// occulited B-163: "undeclared" beside an adopted addon whose own manifest declares what it needs
// read as a contradiction; such an addon says "not installed from its package", or nothing beside
// the root badge whose hint says it
describe('declarationMark', () => {
    it('an adopted addon whose manifest declares, as root: the root badge says it', () => {
        expect(declarationMark({undeclared: true, adopted_confinable: true, policy_mode: 'root'})).toBe('in-root-hint');
    });
    it('the same addon confined on the Services page: not installed from its package', () => {
        expect(declarationMark({undeclared: true, adopted_confinable: true, policy_mode: 'confined'})).toBe('not-from-package');
        expect(declarationMark({undeclared: false, adopted_confinable: true, policy_mode: 'confined'})).toBe('');
    });
    it('an adopted addon without any manifest: undeclared', () => {
        expect(declarationMark({undeclared: true, policy_mode: 'root'})).toBe('undeclared');
    });
    it('an addon installed from the catalogue: nothing', () => {
        expect(declarationMark({policy_mode: 'confined'})).toBe('');
        expect(declarationMark({})).toBe('');
    });
});
