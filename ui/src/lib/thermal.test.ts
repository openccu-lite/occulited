import {describe, expect, it} from 'vitest';
import {formatCelsius, formatLimit, panelLevel, ringFraction, socSeries, tempColours, tempLevel, throttleFacts, throttleState, type TempReading, type Throttle} from './thermal';

// occulited task 148: the temperature panel's figures and words
const soc = (celsius: number): TempReading => ({id: 'thermal_zone0', kind: 'soc', name: 'cpu-thermal', celsius, warn_c: 80, max_c: 85});
const flags = (o: Partial<Throttle['now']> = {}) => ({under_voltage: false, freq_capped: false, throttled: false, soft_temp_limit: false, ...o});

describe('thermal', () => {
    it('levels against the reading\'s own limits', () => {
        expect(tempLevel(soc(54.8))).toBe('ok');
        expect(tempLevel(soc(80))).toBe('warn');
        expect(tempLevel(soc(85))).toBe('error');
        expect(panelLevel([soc(40), {...soc(71), kind: 'disk', warn_c: 70, max_c: 80}])).toBe('warn');
        expect(panelLevel([soc(40)])).toBe('ok');
        expect(panelLevel([soc(40), soc(90)])).toBe('error');
    });

    it('fills the ring from 0 °C to the maximum, clamped', () => {
        expect(ringFraction(soc(42.5))).toBe(0.5);
        expect(ringFraction(soc(100))).toBe(1);
        expect(ringFraction(soc(-5))).toBe(0);
        expect(ringFraction({celsius: 40, max_c: 0})).toBe(0);
    });

    it('colours the rings in the series order', () => {
        expect(tempColours([soc(1), soc(2)])).toEqual(['var(--ol-series-1)', 'var(--ol-series-2)']);
    });

    it('writes degrees in the language', () => {
        expect(formatCelsius(54.8, 'de')).toBe('54,8 °C');
        expect(formatCelsius(54.8, 'en')).toBe('54.8 °C');
        expect(formatCelsius(38, 'en')).toBe('38.0 °C');
        expect(formatLimit(80, 'de')).toBe('80 °C');
        expect(formatLimit(84.5, 'de')).toBe('84,5 °C');
    });

    it('says the throttle in one word, undervoltage first', () => {
        const th = (now: Partial<Throttle['now']>, since: Partial<Throttle['now']> = {}, source: Throttle['source'] = 'firmware'): Throttle => ({source, now: flags(now), since_boot: flags(since)});
        expect(throttleState(th({}))).toBe('normal');
        expect(throttleState(th({}, {under_voltage: true}))).toBe('earlier');
        expect(throttleState(th({throttled: true}, {throttled: true}))).toBe('throttled');
        expect(throttleState(th({soft_temp_limit: true}))).toBe('throttled');
        expect(throttleState(th({under_voltage: true, throttled: true}))).toBe('undervoltage');
        expect(throttleState(th({}, {under_voltage: true}, 'hwmon'))).toBe('normal');
        expect(throttleFacts(th({throttled: true}, {under_voltage: true, throttled: true}))).toEqual([
            {flag: 'under_voltage', now: false},
            {flag: 'throttled', now: true},
        ]);
        expect(throttleFacts(th({}))).toEqual([]);
    });

    it('draws the processor history, breaking the line where a sample has none', () => {
        expect(socSeries(undefined)).toEqual([]);
        expect(socSeries([{t: 'a'}, {t: 'b'}])).toEqual([]);
        expect(socSeries([{t: 'a'}, {t: 'b', soc_c: 51.2}])).toEqual([{t: 'a', v: 0, up: false}, {t: 'b', v: 51.2}]);
    });
});
