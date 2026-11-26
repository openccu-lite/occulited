import {describe, expect, it} from 'vitest';

import {cardPills, checkedCount, isChecked, matchesFilter, normalizeFilter, orderTags, parseFilter, readEntryTags, readTags, serializeFilter, tagCounts, toggleFilter, usedTags} from './catalogtags';
import {TAG_ICONS, tagColor, tagIcon} from './tagstyle';

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

describe('tagCounts and visibleTags', () => {
    const cards = [{tags: ['control', 'tools']}, {tags: ['tools', 'tools']}, {}, {tags: ['future']}];
    it('counts each card once per tag of the list', () => {
        expect(tagCounts(cards, list)).toEqual({control: 1, automation: 0, tools: 2});
    });
    it('the rows are the tags some card carries', () => {
        expect(usedTags(list, cards).map((t) => t.id)).toEqual(['control', 'tools']);
    });
});

describe('many tags: the order', () => {
    const many = readTags(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k'].map((id) => ({id, label: id.toUpperCase()})));
    const cards = [{tags: ['k', 'c']}, {tags: ['k', 'c', 'b']}, {tags: ['k']}];
    it('orders by the whole catalogue count, the list breaking ties', () => {
        expect(orderTags(many, cards).map((t) => t.id)).toEqual(['k', 'c', 'b', 'a', 'd', 'e', 'f', 'g', 'h', 'i', 'j']);
    });
});

// occulited task 189: the checkbox filter (variant B)
describe('the tag filter', () => {
    const ids = ['mqtt', 'integration', 'control'];
    it('all checked is no filter: every card passes, one without tags too', () => {
        expect(matchesFilter(undefined, null)).toBe(true);
        expect(matchesFilter(['x'], null)).toBe(true);
        expect(isChecked(null, 'mqtt')).toBe(true);
        expect(checkedCount(null, ids)).toBeNull();
    });
    it('OR: a card passes with any checked tag; one without tags does not', () => {
        expect(matchesFilter(['control', 'tools'], ['mqtt', 'control'])).toBe(true);
        expect(matchesFilter(['tools'], ['mqtt'])).toBe(false);
        expect(matchesFilter(undefined, ['mqtt'])).toBe(false);
        expect(matchesFilter(['mqtt'], [])).toBe(false);
    });
    it('a box unchecked from all leaves the others; checked again it is all', () => {
        const f = toggleFilter(null, 'integration', ids);
        expect(f).toEqual(['mqtt', 'control']);
        expect(isChecked(f, 'integration')).toBe(false);
        expect(checkedCount(f, ids)).toBe(2);
        expect(toggleFilter(f, 'integration', ids)).toBeNull();
    });
    it('keeps the rows order and drops a tag no row has; all rows checked is null', () => {
        expect(toggleFilter(['control'], 'mqtt', ids)).toEqual(['mqtt', 'control']);
        expect(normalizeFilter(['gone', 'control'], ids)).toEqual(['control']);
        expect(normalizeFilter(['gone'], ids)).toBeNull();
        expect(normalizeFilter(['control', 'mqtt', 'integration', 'gone'], ids)).toBeNull();
        expect(normalizeFilter([], ids)).toEqual([]);
        expect(checkedCount([], ids)).toBe(0);
        // while the rows are unknown, the remembered filter stays as it is
        expect(normalizeFilter(['gone'], [])).toEqual(['gone']);
    });
    it('is remembered as "all" or a JSON list', () => {
        expect(serializeFilter(null)).toBe('all');
        expect(serializeFilter(['mqtt'])).toBe('["mqtt"]');
        expect(parseFilter('all')).toBeNull();
        expect(parseFilter(null)).toBeNull();
        expect(parseFilter('[]')).toEqual([]);
        expect(parseFilter('["mqtt","mqtt",3,""]')).toEqual(['mqtt']);
        expect(parseFilter('{')).toBeNull();
        expect(parseFilter('"mqtt"')).toBeNull();
    });
});

describe('the card pills', () => {
    const many = readTags(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l'].map((id) => ({id, label: id})));
    it('the entry order, the known tags only', () => {
        expect(cardPills(['c', 'zz', 'a'], many).shown.map((t) => t.id)).toEqual(['c', 'a']);
        expect(cardPills(undefined, many)).toEqual({shown: [], rest: []});
    });
    it('up to the limit, the rest behind +n - never "+1"', () => {
        const ids = many.map((t) => t.id);
        expect(cardPills(ids.slice(0, 10), many, 9).rest).toEqual([]);
        const r = cardPills(ids, many, 9);
        expect(r.shown.map((t) => t.id)).toEqual(ids.slice(0, 9));
        expect(r.rest.map((t) => t.id)).toEqual(['j', 'k', 'l']);
    });
});

describe('the tag faces', () => {
    it('every tag of the fixed list has an icon and a colour token; an unknown one the plain tag', () => {
        expect(Object.keys(TAG_ICONS)).toHaveLength(15);
        expect(tagIcon('mqtt')).toBe('radio');
        expect(tagColor('mqtt')).toBe('var(--ol-tag-mqtt)');
        expect(tagIcon('future')).toBe('tag');
        expect(tagColor('future')).toBe('var(--hmm-fg-muted)');
        expect(new Set(Object.values(TAG_ICONS)).size).toBe(15);
    });
});
