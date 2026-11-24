import {describe, expect, it} from 'vitest';
import {classify, isOn, mainAction, shown, stateText, type Description} from './channels';
import {invisible} from './rpc';
import {KNX, stateIcon} from './stateicons';
import {autoIcon, boolLabels, controlSettings, decimalsOf, entryIndex, formatNumber, isProgram, isVariable, limitOf, lockedModel, niceStep, numberProblem, parseNumber, stepOf, stepped, truth, validStep, varKind, variableText, withSetting} from './variables';

const t = (k: string, p?: Record<string, string>) => k.replace(/\{(\w+)\}/g, (_, n) => p?.[n] ?? '');
const MAXF = 3.4028234663852886e38;

// the descriptions as lite-addon-logic's interface gives them
const bool: Description = {VALUE: {TYPE: 'BOOL', OPERATIONS: 7, MIN: 'false', MAX: 'true'}};
const presence: Description = {VALUE: {TYPE: 'BOOL', OPERATIONS: 7, VALUE_LIST: ['abwesend', 'anwesend']}};
const half: Description = {VALUE: {TYPE: 'BOOL', OPERATIONS: 7, VALUE_LIST: ['', 'ja']}};
const temp: Description = {VALUE: {TYPE: 'FLOAT', OPERATIONS: 7, UNIT: '°C', MIN: -40, MAX: 60}};
const free: Description = {VALUE: {TYPE: 'FLOAT', OPERATIONS: 7, UNIT: '', MIN: -MAXF, MAX: MAXF}};
const system: Description = {VALUE: {TYPE: 'FLOAT', OPERATIONS: 5, MIN: -MAXF, MAX: MAXF}};
const mode: Description = {VALUE: {TYPE: 'ENUM', OPERATIONS: 7, MIN: 0, MAX: 2, VALUE_LIST: ['Auto', 'Manuell', 'Urlaub']}};
const note: Description = {VALUE: {TYPE: 'STRING', OPERATIONS: 7}};
const alarm: Description = {VALUE: {TYPE: 'BOOL', OPERATIONS: 7}, ACKNOWLEDGE: {TYPE: 'ACTION', OPERATIONS: 2}, COUNT: {TYPE: 'INTEGER', OPERATIONS: 5}, FIRST: {TYPE: 'INTEGER', OPERATIONS: 5}, LAST: {TYPE: 'INTEGER', OPERATIONS: 5}};
const program: Description = {ACTIVE: {TYPE: 'BOOL', OPERATIONS: 7}, RUN: {TYPE: 'ACTION', OPERATIONS: 2}, RUN_THEN: {TYPE: 'ACTION', OPERATIONS: 2}, LAST_RUN: {TYPE: 'INTEGER', OPERATIONS: 5}};
const fixed: Description = {ACTIVE: {TYPE: 'BOOL', OPERATIONS: 7}, RUN: {TYPE: 'ACTION', OPERATIONS: 0}, RUN_THEN: {TYPE: 'ACTION', OPERATIONS: 0}, LAST_RUN: {TYPE: 'INTEGER', OPERATIONS: 5}};

describe('a variable and a program are known by the channel type', () => {
    it('B-170: they have a widget, so the unassigned list keeps them once their description is read', () => {
        for (const d of [bool, temp, mode, note]) expect(shown({desc: d, model: classify(d, 'VARIABLE')})).toBe(true);
        expect(shown({desc: alarm, model: classify(alarm, 'ALARM')})).toBe(true);
        expect(shown({desc: program, model: classify(program, 'PROGRAM')})).toBe(true);
        // the same descriptions on another channel type are what they were: nothing Control draws
        expect(classify(bool, 'SOMETHING').widget).toBe('none');
        expect(classify(program, 'KEY').widget).toBe('none');
        expect(classify(bool).widget).toBe('none');
        expect(isVariable({}, 'VARIABLE')).toBe(false);
        expect(isVariable(undefined, 'VARIABLE')).toBe(false);
        expect(isProgram({}, 'PROGRAM')).toBe(false);
        expect(isProgram(undefined, 'PROGRAM')).toBe(false);
        expect(isProgram({RUN: {OPERATIONS: 2}}, 'PROGRAM')).toBe(true);
    });
    it('a boolean switches from its disc; a number, an entry and a text are set in the sheet', () => {
        expect(classify(bool, 'VARIABLE')).toEqual({widget: 'variable', tile: 'act-open', stateKey: 'VALUE', commandKey: 'VALUE'});
        for (const d of [temp, mode, note]) expect(classify(d, 'VARIABLE')).toEqual({widget: 'variable', tile: 'act-open', stateKey: 'VALUE', commandKey: ''});
        expect(classify(alarm, 'ALARM')).toEqual({widget: 'variable', tile: 'act-open', stateKey: 'VALUE', commandKey: ''});
        // a value nobody writes (a system value, a computed one) is a reading
        expect(classify(system, 'VARIABLE')).toEqual({widget: 'variable', tile: 'read', stateKey: 'VALUE', commandKey: ''});
        expect(mainAction(classify(bool, 'VARIABLE'), {VALUE: false}, bool)).toEqual({key: 'VALUE', value: true});
        expect(mainAction(classify(bool, 'VARIABLE'), {VALUE: true}, bool)).toEqual({key: 'VALUE', value: false});
        expect(mainAction(classify(temp, 'VARIABLE'), {VALUE: 1}, temp)).toBeUndefined();
    });
    it('a program runs from its disc when it may be run, and is a switch in its sheet either way', () => {
        expect(classify(program, 'PROGRAM')).toEqual({widget: 'program', tile: 'act-open', stateKey: 'ACTIVE', commandKey: 'RUN'});
        expect(classify(fixed, 'PROGRAM')).toEqual({widget: 'program', tile: 'act-open', stateKey: 'ACTIVE', commandKey: ''});
        expect(classify({ACTIVE: {TYPE: 'BOOL', OPERATIONS: 5}}, 'PROGRAM').tile).toBe('read');
        expect(mainAction(classify(program, 'PROGRAM'), {ACTIVE: true}, program)).toEqual({key: 'RUN', value: true});
        expect(mainAction(classify(fixed, 'PROGRAM'), {ACTIVE: true}, fixed)).toBeUndefined();
        // an active program is not "on": the tile is lit only for the moment it is started
        expect(isOn(classify(program, 'PROGRAM'), {ACTIVE: true}, program)).toBe(false);
    });
    it('set read-only, the tile has no action and the sheet no controls', () => {
        const m = lockedModel(classify(bool, 'VARIABLE'));
        expect(m).toEqual({widget: 'variable', tile: 'read', stateKey: 'VALUE', commandKey: '', locked: true});
        expect(mainAction(m, {VALUE: true}, bool)).toBeUndefined();
        expect(mainAction(lockedModel(classify(program, 'PROGRAM')), {}, program)).toBeUndefined();
    });
    it('the kind comes from the value type; an alarm from its ACKNOWLEDGE', () => {
        expect([bool, temp, mode, note, alarm, {VALUE: {TYPE: 'INTEGER'}}, {VALUE: {}}, undefined].map(varKind)).toEqual(['bool', 'number', 'list', 'text', 'alarm', 'number', 'text', 'text']);
    });
});

describe('what the tile says', () => {
    const say = (d: Description, value: unknown, opt = {}) => stateText(classify(d, 'VARIABLE'), {VALUE: value}, d, t, opt);
    it('a boolean in its own texts, else the plain words', () => {
        expect(say(bool, true)).toBe('variable true');
        expect(say(bool, false)).toBe('variable false');
        expect(say(presence, true)).toBe('anwesend');
        expect(say(presence, false)).toBe('abwesend');
        // one text only: the other is the plain word
        expect(say(half, true)).toBe('ja');
        expect(say(half, false)).toBe('variable false');
        expect(boolLabels(mode, t)).toEqual({off: 'variable false', on: 'variable true'}); // a list's entries are no boolean texts
        expect([true, 1, 'true', false, 0, 'false', undefined].map(truth)).toEqual([true, true, true, false, false, false, false]);
        expect(isOn(classify(bool, 'VARIABLE'), {VALUE: true}, bool)).toBe(true);
        expect(isOn(classify(temp, 'VARIABLE'), {VALUE: 5}, temp)).toBe(false);
    });
    it('a number with its unit, in the reader\'s notation and with the chosen step\'s decimals', () => {
        expect(say(temp, 21.5)).toBe('21.5 °C');
        expect(say(temp, 21.5, {language: 'de'})).toBe('21,5 °C');
        expect(say(temp, 21, {language: 'de', step: 0.5})).toBe('21,0 °C');
        expect(say(temp, 21.456, {step: 1})).toBe('21 °C');
        expect(say(temp, 21.456)).toBe('21.46 °C');
        expect(say(free, 1234567)).toBe('1234567');
        expect(say(temp, '7.5')).toBe('7.5 °C');
        expect(say(temp, 'x')).toBe('x'); // not a number: as it came
        expect(formatNumber(undefined)).toBe('');
    });
    it('a list by its entry, a string as it is, nothing while the value is unknown', () => {
        expect(say(mode, 1)).toBe('Manuell');
        expect(say(mode, 'Urlaub')).toBe('Urlaub');
        expect(say(mode, 7)).toBe('7');
        expect(entryIndex(mode, 'Urlaub')).toBe(2);
        expect(entryIndex(mode, 'x')).toBe(-1);
        expect(entryIndex(mode, null)).toBe(-1);
        expect(say(note, 'hallo')).toBe('hallo');
        expect(say(note, undefined)).toBe('');
        expect(variableText({VALUE: null}, note, t)).toBe('');
        expect(say(alarm, true)).toBe('variable true');
    });
    it('a program says whether it is active', () => {
        const m = classify(program, 'PROGRAM');
        expect(stateText(m, {ACTIVE: true}, program, t)).toBe('program active');
        expect(stateText(m, {ACTIVE: false}, program, t)).toBe('program inactive');
        expect(stateText(m, {}, program, t)).toBe('');
    });
});

describe('a number\'s limits and step', () => {
    it('a limit at the largest float is none', () => {
        expect([limitOf(-40), limitOf('60'), limitOf(MAXF), limitOf(-MAXF), limitOf(undefined), limitOf(''), limitOf(Infinity)]).toEqual([-40, 60, undefined, undefined, undefined, undefined, undefined]);
    });
    it('the step: the chosen one, else about a hundredth of the range, else 1', () => {
        expect(stepOf(temp, 0.5)).toBe(0.5);
        expect(stepOf(temp)).toBe(1);
        expect(stepOf(free)).toBe(1);
        expect(stepOf({VALUE: {TYPE: 'INTEGER', MIN: 0, MAX: 10}})).toBe(1);
        expect(stepOf({VALUE: {TYPE: 'FLOAT', MIN: 0, MAX: 1}})).toBe(0.01);
        expect(stepOf({VALUE: {TYPE: 'FLOAT', MIN: 5, MAX: 5}})).toBe(1);
        expect([niceStep(0, 1), niceStep(0, 30), niceStep(0, 255), niceStep(0, 600), niceStep(0, 1000), niceStep(0, 0)]).toEqual([0.01, 0.2, 2, 5, 10, 1]);
        expect(niceStep(0, 900)).toBe(10);
        expect([decimalsOf(1), decimalsOf(0.5), decimalsOf(0.25), decimalsOf(10), decimalsOf(0.001), decimalsOf(1 / 3)]).toEqual([0, 1, 2, 0, 3, 6]);
    });
    it('a step up or down stays inside the limits and carries no binary dust', () => {
        expect(stepped(0.1, 0.2)).toBe(0.3);
        expect(stepped(59.8, 0.5, -40, 60)).toBe(60);
        expect(stepped(-39.9, -0.5, -40, 60)).toBe(-40);
        expect(stepped(5, 1)).toBe(6);
    });
    it('what is typed: a comma is a point, and it must be a number inside the limits', () => {
        expect([parseNumber('21,5'), parseNumber(' -3 '), parseNumber('.5'), parseNumber('5.'), parseNumber('+2')]).toEqual([21.5, -3, 0.5, 5, 2]);
        expect([parseNumber(''), parseNumber('abc'), parseNumber('1e3'), parseNumber('1,2,3'), parseNumber('--1')]).toEqual([undefined, undefined, undefined, undefined, undefined]);
        expect(numberProblem('abc')).toBe('not-a-number');
        expect(numberProblem('61', -40, 60)).toBe('out-of-range');
        expect(numberProblem('-41', -40, 60)).toBe('out-of-range');
        expect(numberProblem('60', -40, 60)).toBe('');
        expect(numberProblem('1000000')).toBe('');
    });
});

describe('the choices in the metadata', () => {
    it('read_only is true only when it says true; a step is a number above 0', () => {
        expect(controlSettings(undefined)).toEqual({readOnly: false});
        expect(controlSettings({control: {icon: 'x'}})).toEqual({readOnly: false});
        expect(controlSettings({control: {read_only: true, step: 0.5}})).toEqual({readOnly: true, step: 0.5});
        expect(controlSettings({control: {read_only: 'yes', step: 0}})).toEqual({readOnly: false});
        expect(controlSettings({control: {step: '0.5'}})).toEqual({readOnly: false});
        expect([validStep(-1), validStep(NaN), validStep(Infinity), validStep(2)]).toEqual([undefined, undefined, undefined, 2]);
    });
    it('one key is written with the others kept; nothing left removes the namespace', () => {
        const meta = {control: {icon: 'scene_day', step: 0.5}, other: {x: 1}};
        expect(withSetting(meta, 'read_only', true)).toEqual({icon: 'scene_day', step: 0.5, read_only: true});
        expect(withSetting(meta, 'icon', null)).toEqual({step: 0.5});
        expect(withSetting(meta, 'step', 2)).toEqual({icon: 'scene_day', step: 2});
        expect(withSetting({control: {read_only: true}}, 'read_only', false)).toBeNull();
        expect(withSetting(undefined, 'icon', '')).toBeNull();
        expect(withSetting(undefined, 'icon', 'scene_day')).toEqual({icon: 'scene_day'});
        expect(meta.control).toEqual({icon: 'scene_day', step: 0.5}); // not changed in place
    });
});

describe('the pictures', () => {
    const icon = (d: Description, type: string, values = {}, extra = {}) => stateIcon({model: classify(d, type), values, desc: d, ...extra});
    it('automatic by the kind, all of them in the bundle', () => {
        const names = [icon(bool, 'VARIABLE').name, icon(temp, 'VARIABLE').name, icon(mode, 'VARIABLE').name, icon(note, 'VARIABLE').name, icon(alarm, 'ALARM').name, icon(alarm, 'ALARM', {VALUE: true}).name, icon(program, 'PROGRAM').name];
        expect(names).toEqual(['control_on_off', 'edit_input_numeric', 'edit_sort', 'message_notice', 'secur_alarm', 'secur_alarm_alarm', 'edit_settings']);
        for (const n of names) expect(KNX[n], n).toBeTruthy();
        expect(autoIcon(classify(program, 'PROGRAM'), program)).toBe('edit_settings');
    });
    it('a true boolean is lit, an alarm that is on is red, a program for the moment it is started', () => {
        expect(icon(bool, 'VARIABLE', {VALUE: true}).tone).toBe('on');
        expect(icon(bool, 'VARIABLE', {VALUE: false}).tone).toBe('');
        expect(icon(alarm, 'ALARM', {VALUE: true}).tone).toBe('alarm');
        expect(icon(temp, 'VARIABLE', {VALUE: 5}).tone).toBe('');
        expect(icon(program, 'PROGRAM', {ACTIVE: true}).tone).toBe('');
        expect(icon(program, 'PROGRAM', {ACTIVE: true}, {pressed: true}).tone).toBe('on');
    });
    it('the chosen icon, when the set has it', () => {
        expect(icon(temp, 'VARIABLE', {}, {icon: 'temp_temperature'}).name).toBe('temp_temperature');
        expect(icon(temp, 'VARIABLE', {}, {icon: 'no_such_icon'}).name).toBe('edit_input_numeric');
        expect(icon(program, 'PROGRAM', {}, {icon: 'scene_scene'}).name).toBe('scene_scene');
        expect(icon(alarm, 'ALARM', {VALUE: true}, {icon: 'message_bell'})).toMatchObject({name: 'message_bell', tone: 'alarm'});
    });
});

describe('visible', () => {
    it('a variable or a program without the visible flag is no tile; everything else is', () => {
        const v = classify(bool, 'VARIABLE'), p = classify(program, 'PROGRAM');
        expect(invisible({flags: 0, model: v})).toBe(true);
        expect(invisible({flags: 2, model: p})).toBe(true);
        expect(invisible({flags: 1, model: v})).toBe(false);
        expect(invisible({flags: 3, model: p})).toBe(false);
        expect(invisible({model: v})).toBe(false); // an interface that says no flags
        expect(invisible({flags: 0, model: {widget: 'switch', tile: 'act-open', stateKey: 'STATE', commandKey: 'STATE'}})).toBe(false);
        expect(invisible(undefined)).toBe(false);
    });
});
