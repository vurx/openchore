import { describe, it, expect } from 'vitest';
import { getTxnTaskDate } from './ActivityTab';
import type { PointTransaction } from '../../types';

describe('ActivityTab - getTxnTaskDate', () => {
  it('uses completion_date when available for a chore approved days later', () => {
    const txn: PointTransaction = {
      id: 1,
      user_id: 1,
      amount: 5,
      reason: 'chore_complete',
      chore_title: 'Brush Teeth',
      created_at: '2026-10-12T10:00:00Z', // Parent approved on Oct 12
      completed_at: '2026-10-10T20:00:00Z', // Kid completed on Oct 10
      completion_date: '2026-10-10', // Scheduled completion date
    };

    expect(getTxnTaskDate(txn)).toBe('2026-10-10');
  });

  it('uses completed_at date when completion_date is missing', () => {
    const txn: PointTransaction = {
      id: 2,
      user_id: 1,
      amount: 5,
      reason: 'chore_complete',
      chore_title: 'Fold Laundry',
      created_at: '2026-10-12T10:00:00Z',
      completed_at: '2026-10-10T14:30:00Z',
    };

    expect(getTxnTaskDate(txn)).toBe('2026-10-10');
  });

  it('falls back to created_at for non-chore events (like admin adjustment)', () => {
    const txn: PointTransaction = {
      id: 3,
      user_id: 1,
      amount: 10,
      reason: 'admin_adjust',
      created_at: '2026-10-12T10:00:00Z',
    };

    expect(getTxnTaskDate(txn)).toBe('2026-10-12');
  });
});
