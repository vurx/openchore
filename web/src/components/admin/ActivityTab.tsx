import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { Undo2, Calendar, User as UserIcon, ChevronDown, X } from 'lucide-react';
import { api } from '../../api';
import type { User, PointTransaction } from '../../types';
import { Avatar, Icon, type IconName } from '../../design';
import { localDateStr } from '../../utils';
import { personColor } from './pickers';
import ui from './ui.module.css';
import styles from './ActivityTab.module.css';

const REASON_ICON: Partial<Record<PointTransaction['reason'], IconName>> = {
  chore_complete: 'check',
  chore_uncomplete: 'back',
  reward_redeem: 'gift',
  streak_bonus: 'flame',
  admin_adjust: 'star',
  expiry_penalty: 'clock',
  missed_chore: 'clock',
  points_decay: 'moon',
  commit_to_goal: 'rocket',
  goal_break: 'rocket',
};

/** Get the effective date when the task/event occurred (task completion date, not approval date). */
export function getTxnTaskDate(txn: PointTransaction): string {
  if (txn.completion_date) {
    return txn.completion_date;
  }
  if (txn.completed_at) {
    return localDateStr(new Date(txn.completed_at));
  }
  return localDateStr(new Date(txn.created_at));
}

function getTxnSortTime(txn: PointTransaction): number {
  if (txn.completed_at) {
    return new Date(txn.completed_at).getTime();
  }
  return new Date(txn.created_at).getTime();
}

export const ActivityTab: React.FC = () => {
  const { t, i18n } = useTranslation();
  const [users, setUsers] = useState<User[]>([]);
  const [transactions, setTransactions] = useState<PointTransaction[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedUserId, setSelectedUserId] = useState<number | 'all'>('all');
  const [selectedDate, setSelectedDate] = useState<string>('');

  const today = useMemo(() => localDateStr(new Date()), []);
  const yesterday = useMemo(() => {
    const d = new Date();
    d.setDate(d.getDate() - 1);
    return localDateStr(d);
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const usrs = await api.users.list();
      setUsers(usrs);

      const allTxns = await Promise.all(
        usrs.map(async (u: User) => {
          const data = await api.points.getForUser(u.id, 200);
          return data.transactions.map(tx => ({ ...tx, user_id: u.id }));
        })
      );
      // Flatten and sort by task occurrence time descending
      const flat = allTxns.flat().sort((a, b) => getTxnSortTime(b) - getTxnSortTime(a));
      setTransactions(flat);
    } catch (err) {
      console.error(err);
    }
    setLoading(false);
  }, []);

  useEffect(() => { load(); }, [load]);

  const formatTime = (dateStr: string) => {
    const d = new Date(dateStr);
    const diffMs = Date.now() - d.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    const diffHr = Math.floor(diffMs / 3600000);

    if (diffMin < 1) return t('admin.activityTab.justNow');
    if (diffMin < 60) return t('admin.activityTab.minutesAgo', { count: diffMin });
    if (diffHr < 24) return t('admin.activityTab.hoursAgo', { count: diffHr });

    return d.toLocaleDateString(i18n.language, { month: 'short', day: 'numeric' }) + ' ' +
      d.toLocaleTimeString(i18n.language, { hour: 'numeric', minute: '2-digit' });
  };

  const getReasonLabel = (reason: string) => {
    const key = `admin.activityTab.reason.${reason}`;
    return i18n.exists(key) ? t(key) : reason;
  };

  const handleUndo = async (txn: PointTransaction) => {
    if (txn.reason === 'reward_redeem' && txn.reference_id) {
      await api.rewards.undoRedemption(txn.reference_id);
    } else {
      const note = `Undo: ${txn.note || getReasonLabel(txn.reason)}`;
      await api.points.adjust(txn.user_id, -txn.amount, note);
    }
    load();
  };

  // Filtered by selected user and selected task occurrence date
  const filtered = useMemo(() => {
    return transactions.filter(tx => {
      if (selectedUserId !== 'all' && tx.user_id !== selectedUserId) {
        return false;
      }
      if (selectedDate && getTxnTaskDate(tx) !== selectedDate) {
        return false;
      }
      return true;
    });
  }, [transactions, selectedUserId, selectedDate]);

  // Points breakdown for the filtered set
  const stats = useMemo(() => {
    let earned = 0;
    let deducted = 0;
    for (const tx of filtered) {
      if (tx.amount > 0) earned += tx.amount;
      else if (tx.amount < 0) deducted += Math.abs(tx.amount);
    }
    return {
      net: earned - deducted,
      earned,
      deducted,
      count: filtered.length,
    };
  }, [filtered]);

  const renderTimes = (txn: PointTransaction) => {
    const isChore = txn.reason === 'chore_complete' || txn.reason === 'chore_uncomplete' || txn.reason === 'expiry_penalty';

    if (isChore && txn.completed_at && txn.approved_at) {
      const compTime = new Date(txn.completed_at).getTime();
      const appTime = new Date(txn.approved_at).getTime();
      // Show both if completion and approval times differ noticeably (> 60s)
      if (Math.abs(appTime - compTime) > 60000) {
        return (
          <span className={styles.timeCluster}>
            <span className={styles.timePart}>
              <span className={styles.timeLabel}>{t('admin.activityTab.completedAt')}:</span>
              {formatTime(txn.completed_at)}
            </span>
            <span className={styles.timeDot} aria-hidden>·</span>
            <span className={styles.timePart}>
              <span className={styles.timeLabel}>{t('admin.activityTab.approvedAt')}:</span>
              {formatTime(txn.approved_at)}
            </span>
          </span>
        );
      }
      return (
        <span className={styles.timePart}>
          <span className={styles.timeLabel}>{t('admin.activityTab.completedAt')}:</span>
          <span className={styles.time}>{formatTime(txn.completed_at)}</span>
        </span>
      );
    }

    if (isChore && txn.completed_at) {
      return (
        <span className={styles.timePart}>
          <span className={styles.timeLabel}>{t('admin.activityTab.completedAt')}:</span>
          <span className={styles.time}>{formatTime(txn.completed_at)}</span>
        </span>
      );
    }

    return <span className={styles.time}>{formatTime(txn.created_at)}</span>;
  };

  if (loading) return <p className={ui.emptyInline}>{t('admin.activityTab.loading')}</p>;

  return (
    <div className={ui.page}>
      <div className={ui.pageHead}>
        <div>
          <h2 className={ui.pageTitle}>{t('admin.activityTab.title')}</h2>
          <p className={ui.pageSub}>{t('admin.activityTab.eventCount', { count: filtered.length })}</p>
        </div>
      </div>

      {/* Filter Bar */}
      <div className={styles.filterBar}>
        <div className={styles.filterControls}>
          <div className={clsx(styles.filterField, styles.filterFieldUser)}>
            <UserIcon size={15} className={styles.fieldIcon} aria-hidden />
            <select
              className={styles.select}
              value={selectedUserId}
              onChange={e => setSelectedUserId(e.target.value === 'all' ? 'all' : Number(e.target.value))}
              aria-label={t('admin.activityTab.filterUser')}
            >
              <option value="all">{t('admin.activityTab.allUsers')}</option>
              {users.map(u => (
                <option key={u.id} value={u.id}>
                  {u.name}
                </option>
              ))}
            </select>
            <ChevronDown size={14} className={styles.selectCaret} aria-hidden />
          </div>

          <div className={clsx(styles.filterField, styles.filterFieldDate, selectedDate && styles.filterFieldActive)}>
            <Calendar size={15} className={styles.fieldIcon} aria-hidden />
            <span className={clsx(styles.dateValue, !selectedDate && styles.datePlaceholder)}>
              {selectedDate
                ? (i18n.language.startsWith('zh')
                    ? `${selectedDate.split('-')[0]}年${parseInt(selectedDate.split('-')[1], 10)}月${parseInt(selectedDate.split('-')[2], 10)}日`
                    : new Date(`${selectedDate}T00:00:00`).toLocaleDateString(i18n.language, { year: 'numeric', month: 'short', day: 'numeric' }))
                : t('admin.activityTab.filterDate')}
            </span>
            <input
              type="date"
              className={styles.dateInputOverlay}
              value={selectedDate}
              onChange={e => setSelectedDate(e.target.value)}
              aria-label={t('admin.activityTab.filterDate')}
            />
            {selectedDate && (
              <button
                type="button"
                className={styles.clearDateBtn}
                onClick={(e) => {
                  e.stopPropagation();
                  setSelectedDate('');
                }}
                title={t('admin.activityTab.allDates')}
                aria-label={t('admin.activityTab.allDates')}
              >
                <X size={13} aria-hidden />
              </button>
            )}
          </div>
        </div>

        <div className={styles.quickDates} role="group" aria-label={t('admin.activityTab.quickDates')}>
          <button
            type="button"
            className={clsx(styles.chipBtn, !selectedDate && styles.chipBtnActive)}
            onClick={() => setSelectedDate('')}
          >
            {t('admin.activityTab.allDates')}
          </button>
          <button
            type="button"
            className={clsx(styles.chipBtn, selectedDate === today && styles.chipBtnActive)}
            onClick={() => setSelectedDate(today)}
          >
            {t('admin.activityTab.today')}
          </button>
          <button
            type="button"
            className={clsx(styles.chipBtn, selectedDate === yesterday && styles.chipBtnActive)}
            onClick={() => setSelectedDate(yesterday)}
          >
            {t('admin.activityTab.yesterday')}
          </button>
        </div>
      </div>

      {/* Summary card if filtered by date or member */}
      {(selectedDate || selectedUserId !== 'all') && (
        <div className={styles.summaryCard} aria-live="polite">
          <div className={styles.summaryItem}>
            <span className={styles.summaryLabel}>{t('admin.activityTab.netPoints')}</span>
            <span className={clsx(styles.summaryVal, stats.net > 0 && styles.summaryValPos, stats.net < 0 && styles.summaryValNeg)}>
              {stats.net > 0 ? '+' : ''}{stats.net}
            </span>
          </div>
          <div className={styles.summaryItem}>
            <span className={styles.summaryLabel}>{t('admin.activityTab.earnedPoints')}</span>
            <span className={styles.summaryVal}>+{stats.earned}</span>
          </div>
          <div className={styles.summaryItem}>
            <span className={styles.summaryLabel}>{t('admin.activityTab.deductedPoints')}</span>
            <span className={clsx(styles.summaryVal, styles.summaryValNeg)}>−{stats.deducted}</span>
          </div>
          <div className={styles.summaryItem}>
            <span className={styles.summaryLabel}>{t('admin.activityTab.eventsTotal')}</span>
            <span className={styles.summaryVal}>{stats.count}</span>
          </div>
        </div>
      )}

      {filtered.length === 0 ? (
        <div className={ui.empty}>
          <Icon name="clock" />
          <p>{t('admin.activityTab.empty')}</p>
        </div>
      ) : (
        <ul className={clsx(ui.list, styles.list)}>
          {filtered.map(txn => {
            const user = users.find(u => u.id === txn.user_id);
            const name = user?.name ?? `User ${txn.user_id}`;
            return (
              <li key={`${txn.user_id}-${txn.id}`} className={ui.row}>
                <span className={styles.avatar}>
                  <Avatar name={name} color={personColor(user)} size="sm" />
                  <span className={styles.reasonIcon} aria-hidden>
                    <Icon name={REASON_ICON[txn.reason] ?? 'star'} />
                  </span>
                </span>
                <div className={ui.rowMain}>
                  <span className={styles.line}>
                    <span className={styles.user}>{name}</span>
                    <span className={styles.reason}>{getReasonLabel(txn.reason)}</span>
                  </span>
                  {(txn.chore_title || txn.note) && (
                    <span className={ui.rowDesc}>
                      {txn.chore_title ? (
                        <>
                          <strong>{txn.chore_title}</strong>
                          {txn.note ? ` · ${txn.note}` : ''}
                        </>
                      ) : (
                        txn.note
                      )}
                    </span>
                  )}
                  {renderTimes(txn)}
                </div>
                <span className={clsx(styles.amount, txn.amount < 0 && styles.amountNeg)}>
                  {txn.amount > 0 ? '+' : txn.amount < 0 ? '−' : ''}{Math.abs(txn.amount)}
                </span>
                <button
                  type="button"
                  className={ui.iconBtn}
                  title={t('admin.activityTab.undoTitle')}
                  aria-label={t('admin.activityTab.undoTitle')}
                  onClick={() => handleUndo(txn)}
                >
                  <Undo2 aria-hidden />
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
};

