import {afterEach, describe, expect, it, vi} from 'vitest';
import {backupFailure, elapsed, prepareBackup, preparedUrl, type BackupJob} from './backupjob';
import {api} from './api';

afterEach(() => vi.restoreAllMocks());

describe('prepareBackup (B-90)', () => {
    it('starts, polls until ready, and reports every state', async () => {
        const states: string[] = [];
        vi.spyOn(api, 'post').mockResolvedValue({id: 'j1', state: 'running', started: ''} as BackupJob);
        const get = vi.spyOn(api, 'get').mockResolvedValueOnce({id: 'j1', state: 'running', started: ''} as BackupJob).mockResolvedValueOnce({id: 'j1', state: 'ready', started: '', size: 9} as BackupJob);
        const job = await prepareBackup((j) => states.push(j.state), 1, () => Promise.resolve());
        expect(job.size).toBe(9);
        expect(states).toEqual(['running', 'running', 'ready']);
        expect(get).toHaveBeenCalledWith('/api/system/v1/backup/prepare/j1');
    });
    it('joins a ready one without polling', async () => {
        vi.spyOn(api, 'post').mockResolvedValue({id: 'j2', state: 'ready', started: ''} as BackupJob);
        const get = vi.spyOn(api, 'get');
        expect((await prepareBackup(undefined, 1, () => Promise.resolve())).id).toBe('j2');
        expect(get).not.toHaveBeenCalled();
    });
    it('throws the reason of a failed one', async () => {
        vi.spyOn(api, 'post').mockResolvedValue({id: 'j3', state: 'running', started: ''} as BackupJob);
        vi.spyOn(api, 'get').mockResolvedValue({id: 'j3', state: 'failed', started: '', error: 'createBackup.sh: exit status 1'} as BackupJob);
        await expect(prepareBackup(undefined, 1, () => Promise.resolve())).rejects.toThrow('createBackup.sh: exit status 1');
    });
});

describe('preparedUrl and elapsed', () => {
    it('builds the download URL', () => {
        expect(preparedUrl('ab')).toBe('/api/system/v1/backup?prepared=ab');
        expect(preparedUrl('ab', {encrypted: 'false', confirm: 'c 1'})).toBe('/api/system/v1/backup?prepared=ab&encrypted=false&confirm=c+1');
    });
    it('formats m:ss', () => {
        expect(elapsed(0)).toBe('0:00');
        expect(elapsed(75.9)).toBe('1:15');
        expect(elapsed(-3)).toBe('0:00');
    });
});

// occulited B-146: a backup the system has no room for is refused with the sizes and the largest
// directories, in the page's words
describe('backupFailure', () => {
    const t = (key: string, params?: Record<string, string | number>) => key.replace(/\{(\w+)\}/g, (_, k) => String(params?.[k] ?? ''));
    it('names the sizes and the largest directories of a no-space job', async () => {
        vi.spyOn(api, 'post').mockResolvedValue({
            id: 'j4',
            state: 'failed',
            started: '',
            error: 'not enough free space',
            code: 'no-space',
            detail: {backup_bytes: 9e9, need_bytes: 18.1e9, free_bytes: 13e9, reserve_bytes: 128 * 2 ** 20, largest: [{path: '/usr/local/sdcard/backup', bytes: 6e9}]},
        } as BackupJob);
        const err = await prepareBackup(undefined, 1, () => Promise.resolve()).catch((e) => e);
        const m = backupFailure(err, 'en', t);
        expect(m).toContain('it holds 9.0 GB, and making it needs 18 GB');
        expect(m).toContain('13 GB are free');
        expect(m).toContain('The largest directories and files: /usr/local/sdcard/backup (6.0 GB).');
        expect(m).toContain('.nobackup');
    });
    it('passes any other failure through', () => {
        expect(backupFailure(new Error('createBackup.sh: exit status 1'), 'en', t)).toBe('createBackup.sh: exit status 1');
    });
});
