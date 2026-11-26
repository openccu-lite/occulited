import {describe, expect, it} from 'vitest';
import {bootDip, causeOf, causeWhen, clockText, episodeMarks, fullClock, formatCelsius, formatLimit, formatSeconds, panelLevel, ringFraction, socSeries, tempColours, tempLevel, throttleFacts, throttleLevel, throttleState, type TempReading, type Throttle} from './thermal';

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

    // occulited B-162: the server's verdict - a boot dip is normal with an info line, a dip later is earlier
    it('reads the server\'s verdict and the kernel\'s episodes', () => {
        const prod: Throttle = {source: 'firmware', raw: 0x50000, now: flags(), since_boot: flags({under_voltage: true, throttled: true}), state: 'boot_dip', events: 1, undervoltage_s: 2};
        expect(throttleState(prod)).toBe('normal');
        expect(throttleFacts(prod)).toEqual([]);
        expect(bootDip(prod)).toEqual({n: 1, seconds: 2});
        const later: Throttle = {...prod, state: 'earlier', events: 3, undervoltage_s: 13};
        expect(throttleState(later)).toBe('earlier');
        expect(bootDip(later)).toBeNull();
        expect(throttleFacts(later)).toEqual([
            {flag: 'under_voltage', now: false},
            {flag: 'throttled', now: false},
        ]);
        // hwmon's alarm with the kernel's episodes: the undervoltage since the boot is known
        const hw: Throttle = {source: 'hwmon', now: flags(), since_boot: flags(), state: 'earlier', events: 2};
        expect(throttleFacts(hw)).toEqual([{flag: 'under_voltage', now: false}]);
        expect(throttleState({...prod, state: 'undervoltage', now: flags({under_voltage: true})})).toBe('undervoltage');
        // occulited task 173: the panel's edge from the state - a boot dip and normal none
        expect(throttleLevel(undefined)).toBe('ok');
        expect(throttleLevel(prod)).toBe('ok');
        expect(throttleLevel(later)).toBe('warn');
        expect(throttleLevel({...prod, state: 'throttled', now: flags({throttled: true})})).toBe('warn');
        expect(throttleLevel({...prod, state: 'undervoltage', now: flags({under_voltage: true})})).toBe('error');
        expect(throttleLevel({source: 'firmware', now: flags(), since_boot: flags()})).toBe('ok');
        expect(formatSeconds(2, 'de')).toBe('2 s');
        expect(formatSeconds(1.5, 'de')).toBe('1,5 s');
        expect(formatSeconds(17, 'en')).toBe('17 s');
        expect(formatSeconds(125, 'en')).toBe('2 min 5 s');
        expect(formatSeconds(120, 'en')).toBe('2 min');
    });

    // occulited task 172: the episodes as the chart's red lines
    it('marks the undervoltage episodes for the chart', () => {
        const words = {t: (k: string, p?: Record<string, string | number>) => Object.entries(p ?? {}).reduce((s, [a, b]) => s.replaceAll(`{${a}}`, String(b)), k), clock: (d: Date) => d.toISOString().slice(11, 16), seconds: (v: number) => `${v} s`};
        const base: Throttle = {source: 'firmware', now: flags(), since_boot: flags({under_voltage: true}), state: 'earlier', events: 4};
        const now = Date.parse('2026-11-22T12:00:00Z');
        // the open one runs up to now while the undervoltage holds now (B-186)
        const marks = episodeMarks({...base, now: flags({under_voltage: true}), episodes: [
            {start: '2026-11-22T08:00:08Z', start_s: 8, duration_s: 2},
            {start: '2026-11-22T10:00:00Z', start_s: 7200, duration_s: -1},
            {start_s: 9000, duration_s: 1},
            {start: '2026-11-22T11:30:00Z', start_s: 12600},
        ]}, words, now);
        expect(marks).toEqual([
            {start: Date.parse('2026-11-22T08:00:08Z'), end: Date.parse('2026-11-22T08:00:10Z'), label: 'Undervoltage 08:00, 2 s'},
            {start: Date.parse('2026-11-22T10:00:00Z'), end: Date.parse('2026-11-22T10:00:00Z'), label: 'Undervoltage 10:00'},
            {start: Date.parse('2026-11-22T11:30:00Z'), end: now, label: 'Undervoltage since 11:30'},
        ]);
        const dip = episodeMarks({...base, state: 'boot_dip', episodes: [{start: '2026-11-22T08:00:09Z', start_s: 9.7, duration_s: 2}]}, words, now);
        expect(dip[0]!.label).toBe('Undervoltage 08:00, 2 s (at start-up, normalised)');
        expect(episodeMarks(undefined, words)).toEqual([]);
        expect(episodeMarks(base, words)).toEqual([]);
    });

    // occulited B-186: the production Pi's episode from 19:52 the evening before, no end in the log,
    // no undervoltage now - it painted the whole 24 h chart; open only while the undervoltage holds
    describe('an episode the log leaves open (B-186)', () => {
        const words = {t: (k: string, p?: Record<string, string | number>) => Object.entries(p ?? {}).reduce((s, [a, b]) => s.replaceAll(`{${a}}`, String(b)), k), clock: (d: Date) => d.toISOString().slice(11, 16), seconds: (v: number) => `${v} s`};
        const start = Date.parse('2026-11-25T19:52:00Z');
        const now = Date.parse('2026-11-26T08:44:00Z');
        const ep = {start: '2026-11-25T19:52:00Z', start_s: 7200};
        it.each([
            {name: 'firmware, undervoltage now: up to now', th: {source: 'firmware', now: flags({under_voltage: true})}, ep, end: now, label: 'Undervoltage since 19:52'},
            {name: 'firmware, fine now: a moment (the older server)', th: {source: 'firmware', now: flags()}, ep, end: start, label: 'Undervoltage 19:52'},
            {name: 'firmware, throttled now only: a moment', th: {source: 'firmware', now: flags({throttled: true})}, ep, end: start, label: 'Undervoltage 19:52'},
            {name: 'hwmon alarm set: up to now', th: {source: 'hwmon', now: flags({under_voltage: true})}, ep, end: now, label: 'Undervoltage since 19:52'},
            {name: 'hwmon alarm clear: a moment', th: {source: 'hwmon', now: flags()}, ep, end: start, label: 'Undervoltage 19:52'},
            {name: 'the server closed it (-1): a moment', th: {source: 'firmware', now: flags()}, ep: {...ep, duration_s: -1}, end: start, label: 'Undervoltage 19:52'},
            {name: 'closed with a length, undervoltage again now: its length', th: {source: 'firmware', now: flags({under_voltage: true})}, ep: {...ep, duration_s: 3}, end: start + 3000, label: 'Undervoltage 19:52, 3 s'},
        ] as const)('$name', ({th, ep, end, label}) => {
            const marks = episodeMarks({...th, since_boot: flags({under_voltage: true}), state: 'earlier', events: 1, episodes: [ep]} as Throttle, words, now);
            expect(marks).toEqual([{start, end, label}]);
        });
    });

    it('knows the full clock', () => {
        expect(fullClock({cur_mhz: 1200, max_mhz: 1200, source: 'firmware'})).toBe(true);
        expect(fullClock({cur_mhz: 1199, max_mhz: 1200, source: 'firmware'})).toBe(true);
        expect(fullClock({cur_mhz: 600, max_mhz: 1500, source: 'cpufreq'})).toBe(false);
        expect(fullClock({cur_mhz: 900, source: 'cpufreq'})).toBe(false);
        expect(fullClock(undefined)).toBe(false);
    });

    it('draws the processor history, breaking the line where a sample has none', () => {
        expect(socSeries(undefined)).toEqual([]);
        expect(socSeries([{t: 'a'}, {t: 'b'}])).toEqual([]);
        expect(socSeries([{t: 'a'}, {t: 'b', soc_c: 51.2}])).toEqual([{t: 'a', v: 0, up: false}, {t: 'b', v: 51.2}]);
    });
});

describe('occulited task 177: the clock and the causes', () => {
    it('writes the clock in GHz with the language mark', () => {
        expect(clockText({cur_mhz: 1200, max_mhz: 1500, source: 'firmware'}, 'en')).toBe('1.2 / 1.5 GHz');
        expect(clockText({cur_mhz: 600, max_mhz: 1500, source: 'cpufreq'}, 'de')).toBe('0,6 / 1,5 GHz');
        expect(clockText({cur_mhz: 900, source: 'cpufreq'}, 'en')).toBe('0.9 GHz');
    });
    it('says start-up, before or at', () => {
        const now = new Date('2026-11-24T20:00:00Z');
        expect(causeWhen({flag: 'freq_capped', at: '2026-11-24T19:00:00Z', uptime_s: 4000}, 'de', now).kind).toBe('at');
        expect(causeWhen({flag: 'freq_capped', at: '2026-11-24T19:00:00Z', before: true, uptime_s: 4000}, 'de', now).kind).toBe('before');
        expect(causeWhen({flag: 'under_voltage', at: '2026-11-24T19:00:00Z', before: true, uptime_s: 30}, 'de', now).kind).toBe('startup');
        // another day: the day in front
        expect(causeWhen({flag: 'freq_capped', at: '2026-11-22T10:00:00Z', uptime_s: 4000}, 'en', now).time).toMatch(/^\d+\/\d+ \d\d:\d\d$/);
        expect(causeOf({source: 'firmware', now: {} as never, since_boot: {} as never, causes: [{flag: 'throttled', at: '', uptime_s: 1}]}, 'throttled')?.flag).toBe('throttled');
    });
});
