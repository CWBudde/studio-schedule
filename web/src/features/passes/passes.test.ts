import { describe, expect, it } from 'vitest';
import { covers, type PassFacts, passState, pickPass } from './passes';

const owner = { id: 'p1' };
const pass = (extra: Partial<PassFacts>): PassFacts => ({
  id: 'a',
  status: 'active',
  usable: true,
  kind: 'visits',
  visitsRemaining: 5,
  validFrom: '2026-10-01',
  validUntil: '2026-12-31',
  soldAt: '2026-10-01T08:00:00Z',
  owner,
  ...extra,
});

describe('passState', () => {
  it('names the state of a pass on a school date', () => {
    expect(passState(pass({}), '2026-10-23')).toBe('usable');
    expect(passState(pass({ status: 'voided', usable: false }), '2026-10-23')).toBe('voided');
    expect(passState(pass({ usable: false, validFrom: '2026-11-01' }), '2026-10-23')).toBe(
      'notYetValid',
    );
    expect(passState(pass({ usable: false, visitsRemaining: 0 }), '2026-10-23')).toBe('usedUp');
    expect(passState(pass({ usable: false, validUntil: '2026-10-22' }), '2026-10-23')).toBe(
      'expired',
    );
  });
});

describe('covers', () => {
  it('covers a date within its days while a visit is left', () => {
    expect(covers(pass({}), '2026-10-01')).toBe(true);
    expect(covers(pass({}), '2026-12-31')).toBe(true);
    expect(covers(pass({}), '2027-01-01')).toBe(false);
    expect(covers(pass({}), '2026-09-30')).toBe(false);
    expect(covers(pass({ visitsRemaining: 0 }), '2026-10-23')).toBe(false);
    expect(covers(pass({ kind: 'time', visitsRemaining: 0 }), '2026-10-23')).toBe(true);
    expect(covers(pass({ validUntil: undefined }), '2030-01-01')).toBe(true);
    expect(covers(pass({ status: 'voided' }), '2026-10-23')).toBe(false);
  });
});

describe('pickPass', () => {
  it('picks a time pass first, then the one that expires first, then the oldest', () => {
    const visitsLate = pass({ id: 'late', validUntil: undefined });
    const visitsSoon = pass({ id: 'soon', validUntil: '2026-11-30' });
    const visitsOld = pass({ id: 'old', validUntil: '2026-11-30', soldAt: '2026-09-01T08:00:00Z' });
    const time = pass({ id: 'time', kind: 'time', validUntil: '2027-01-31' });
    const other = pass({ id: 'other', owner: { id: 'p2' }, kind: 'time' });

    expect(pickPass([visitsLate, visitsSoon, visitsOld, time, other], 'p1', '2026-10-23')?.id).toBe(
      'time',
    );
    expect(pickPass([visitsLate, visitsSoon, visitsOld], 'p1', '2026-10-23')?.id).toBe('old');
    expect(pickPass([visitsLate, visitsSoon], 'p1', '2026-10-23')?.id).toBe('soon');
    expect(pickPass([visitsLate, visitsSoon], 'p1', '2026-12-15')?.id).toBe('late');
    expect(pickPass([other], 'p1', '2026-10-23')).toBeUndefined();
  });

  it('compares the sales by their instants, whatever their offset', () => {
    const utc = pass({ id: 'utc', soldAt: '2026-10-01T08:00:00Z' });
    // 07:00 UTC: earlier, although the text sorts later.
    const berlin = pass({ id: 'berlin', soldAt: '2026-10-01T09:00:00+02:00' });

    expect(pickPass([utc, berlin], 'p1', '2026-10-23')?.id).toBe('berlin');
  });
});
