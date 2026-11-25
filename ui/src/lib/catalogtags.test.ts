import {describe, expect, it} from 'vitest';

import {knownChosen, matchesTags, orderTags, parseChosen, readEntryTags, readTags, shownTags, tagCounts, toggleTag, visibleTags} from './catalogtags';

// occulited task 188: the catalogue's tag filter
const list = readTags([
    {id: 'control', label: {de: 'Bedienung', en: 'Control'}},
    {id: 'automation', label: {de: 'Automation', en: 'Automation'}},
    {id: 'tools', label: 'Tools'},
]);

describe('readTags', () => {
    it('keeps tags with an id and a label, each id once, and drops the rest', () => {
        expect(readTags([...list, {id: 'tools', label: 'again'}, {id: 5, label: 'x'}, {id: 'x'}, null, 'tools']).map((t) => t.id)).toEqual(['control', 'automation', 'tools']);
        expect(readTags('nope')).toEqual([]);
        expect(list[2]?.label).toEqual({en: 'Tools'});
    });
});

describe('readEntryTags', () => {
    it('strings only, each once; none is undefined', () => {
        expect(readEntryTags(['tools', 3, 'tools', '', 'control'])).toEqual(['tools', 'control']);
        expect(readEntryTags([])).toBeUndefined();
        expect(readEntryTags('tools')).toBeUndefined();
    });
});

describe('matchesTags', () => {
    it('no choice lets everything through; a choice any of its tags', () => {
        expect(matchesTags(undefined, [])).toBe(true);
        expect(matchesTags(['tools'], ['control', 'tools'])).toBe(true);
        expect(matchesTags(['automation'], ['control'])).toBe(false);
        // an installed addon the catalogue does not know carries none
        expect(matchesTags(undefined, ['control'])).toBe(false);
    });
});

describe('tagCounts and visibleTags', () => {
    const cards = [{tags: ['control', 'tools']}, {tags: ['tools', 'tools']}, {}, {tags: ['future']}];
    it('counts each card once per tag of the list', () => {
        expect(tagCounts(cards, list)).toEqual({control: 1, automation: 0, tools: 2});
    });
    it('shows the tags some card carries, and a chosen one even without a card', () => {
        expect(visibleTags(list, cards, []).map((t) => t.id)).toEqual(['control', 'tools']);
        expect(visibleTags(list, cards, ['automation']).map((t) => t.id)).toEqual(['control', 'automation', 'tools']);
    });
});

describe('the remembered choice', () => {
    it('parses a JSON list of ids and nothing else', () => {
        expect(parseChosen('["tools","tools","control",4]')).toEqual(['tools', 'control']);
        expect(parseChosen('{')).toEqual([]);
        expect(parseChosen('"tools"')).toEqual([]);
        expect(parseChosen(null)).toEqual([]);
    });
    it('drops a tag the list no longer has, but keeps all while the list is unknown', () => {
        expect(knownChosen(['tools', 'gone'], list)).toEqual(['tools']);
        expect(knownChosen(['tools', 'gone'], [])).toEqual(['tools', 'gone']);
    });
    it('toggles a chip, in the list order', () => {
        expect(toggleTag(['tools'], 'control', list)).toEqual(['control', 'tools']);
        expect(toggleTag(['control', 'tools'], 'control', list)).toEqual(['tools']);
    });
});

describe('many tags: the order and the collapsed row', () => {
    const many = readTags(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k'].map((id) => ({id, label: id.toUpperCase()})));
    const cards = [{tags: ['k', 'c']}, {tags: ['k', 'c', 'b']}, {tags: ['k']}];
    it('orders by the whole catalogue count, the list breaking ties', () => {
        expect(orderTags(many, cards).map((t) => t.id)).toEqual(['k', 'c', 'b', 'a', 'd', 'e', 'f', 'g', 'h', 'i', 'j']);
    });
    it('collapses to the first eight plus every chosen one, and says how many are left out', () => {
        const o = orderTags(many, cards);
        expect(shownTags(o, [], false)).toEqual({tags: o.slice(0, 8), more: 3});
        const r = shownTags(o, ['j'], false);
        expect(r.tags.map((t) => t.id)).toEqual(['k', 'c', 'b', 'a', 'd', 'e', 'f', 'g', 'j']);
        expect(r.more).toBe(2);
        expect(shownTags(o, [], true)).toEqual({tags: o, more: 0});
        // one left out is not worth a button: all show
        expect(shownTags(o.slice(0, 9), [], false).more).toBe(0);
    });
});
