import React, { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { Pencil, Trash2, X } from 'lucide-react';
import { api } from '../../api';
import type { User, ChoreSchedule } from '../../types';
import { DAY_NAMES } from '../../types';
import { toggleInArray } from '../../utils';
import { Icon } from '../../design';
import { PersonName, PersonToggle } from './pickers';
import ui from './ui.module.css';
import styles from './managers.module.css';

const ALL_DAYS = [0, 1, 2, 3, 4, 5, 6];
const WEEKDAYS = [1, 2, 3, 4, 5];
const WEEKENDS = [0, 6];

export const ScheduleManager: React.FC<{
  choreId: number;
  users: User[];
}> = ({ choreId, users }) => {
  const { t } = useTranslation();
  const [schedules, setSchedules] = useState<ChoreSchedule[]>([]);
  const [adding, setAdding] = useState(false);
  const [selectedUsers, setSelectedUsers] = useState<number[]>(users[0] ? [users[0].id] : []);
  const [schedType, setSchedType] = useState<'recurring' | 'oneoff' | 'interval'>('recurring');
  const [selectedDays, setSelectedDays] = useState<number[]>([]);
  const [specificDate, setSpecificDate] = useState('');
  const [availableAt, setAvailableAt] = useState('');
  const [dueBy, setDueBy] = useState('');
  const [expiryPenalty, setExpiryPenalty] = useState<'block' | 'no_points' | 'penalty'>('block');
  const [expiryPenaltyValue, setExpiryPenaltyValue] = useState('5');
  const [pointsMultiplier, setPointsMultiplier] = useState('1');
  const [intervalDays, setIntervalDays] = useState('2');
  const [intervalStart, setIntervalStart] = useState(() => new Date().toISOString().slice(0, 10));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editType, setEditType] = useState<'recurring' | 'oneoff' | 'interval'>('recurring');
  const [editUserId, setEditUserId] = useState(0);
  const [editDays, setEditDays] = useState<number[]>([]);
  const [editDate, setEditDate] = useState('');
  const [editInterval, setEditInterval] = useState('2');
  const [editIntervalStart, setEditIntervalStart] = useState('');
  const [editAvailableAt, setEditAvailableAt] = useState('');
  const [editDueBy, setEditDueBy] = useState('');
  const [editExpiryPenalty, setEditExpiryPenalty] = useState<'block' | 'no_points' | 'penalty'>('block');
  const [editExpiryPenaltyValue, setEditExpiryPenaltyValue] = useState('5');
  const [editPointsMultiplier, setEditPointsMultiplier] = useState('1');

  const load = useCallback(async () => {
    try {
      const s = await api.chores.listSchedules(choreId);
      setSchedules(s);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('admin.scheduleManager.loadFailed'));
    }
  }, [choreId, t]);

  useEffect(() => { load(); }, [load]);

  const toggleDay = (d: number) => {
    setSelectedDays(prev => toggleInArray(prev, d));
  };

  const toggleUser = (id: number) => {
    setSelectedUsers(prev => toggleInArray(prev, id));
  };

  const setDayPreset = (days: number[]) => {
    setSelectedDays(prev => {
      const same = prev.length === days.length && days.every(d => prev.includes(d));
      return same ? [] : days;
    });
  };

  const handleAdd = async () => {
    if (selectedUsers.length === 0) return;
    setSaving(true);
    setError('');

    try {
      const penaltyFields = dueBy ? {
        expiry_penalty: expiryPenalty,
        ...(expiryPenalty === 'penalty' ? { expiry_penalty_value: parseInt(expiryPenaltyValue) || 0 } : {}),
      } : {};
      const common = {
        available_at: availableAt || undefined,
        due_by: dueBy || undefined,
        points_multiplier: parseFloat(pointsMultiplier) || 1,
        ...penaltyFields,
      };

      const promises: Promise<unknown>[] = [];
      for (const userId of selectedUsers) {
        if (schedType === 'recurring') {
          for (const day of selectedDays) {
            promises.push(api.chores.createSchedule(choreId, { assigned_to: userId, day_of_week: day, ...common }));
          }
        } else if (schedType === 'interval') {
          const interval = parseInt(intervalDays);
          if (!interval || interval < 1 || !intervalStart) continue;
          promises.push(api.chores.createSchedule(choreId, { assigned_to: userId, recurrence_interval: interval, recurrence_start: intervalStart, ...common }));
        } else {
          if (!specificDate) continue;
          promises.push(api.chores.createSchedule(choreId, { assigned_to: userId, specific_date: specificDate, ...common }));
        }
      }
      const results = await Promise.allSettled(promises);
      const failures = results.filter(r => r.status === 'rejected');
      if (failures.length > 0) {
        const reason = failures[0].status === 'rejected' ? failures[0].reason : null;
        setError(reason instanceof Error ? reason.message : t('admin.scheduleManager.createFailed'));
        return;
      }

      setAdding(false);
      setSelectedDays([]);
      setSpecificDate('');
      setAvailableAt('');
      setDueBy('');
      setExpiryPenalty('block');
      setExpiryPenaltyValue('5');
      setPointsMultiplier('1');
      setSelectedUsers(users[0] ? [users[0].id] : []);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t('admin.scheduleManager.createFailed'));
    } finally {
      setSaving(false);
    }
  };

  const canAdd = selectedUsers.length > 0 && (
    schedType === 'recurring' ? selectedDays.length > 0 :
    schedType === 'oneoff' ? !!specificDate :
    parseInt(intervalDays) >= 1 && !!intervalStart
  ) && parseFloat(pointsMultiplier) > 0 && (!dueBy || expiryPenalty !== 'penalty' || parseInt(expiryPenaltyValue) > 0);

  type Group = { key: string; userId: number; assignmentType: string; availableAt?: string; dueBy?: string; expiryPenalty?: string; expiryPenaltyValue?: number; pointsMultiplier: number; scheduleIds: number[]; } & (
    | { type: 'recurring'; days: number[] }
    | { type: 'interval'; interval: number; start: string }
    | { type: 'oneoff'; date: string }
  );
  const groups: Group[] = [];
  for (const s of schedules) {
    const time = s.available_at || '';
    const base = { userId: s.assigned_to, assignmentType: s.assignment_type || 'individual', availableAt: s.available_at ?? undefined, dueBy: s.due_by ?? undefined, expiryPenalty: s.expiry_penalty, expiryPenaltyValue: s.expiry_penalty_value, pointsMultiplier: s.points_multiplier || 1 };
    if (s.recurrence_interval) {
      groups.push({ key: `${s.id}`, ...base, scheduleIds: [s.id], type: 'interval', interval: s.recurrence_interval, start: s.recurrence_start || '' });
    } else if (s.day_of_week != null) {
      // Only combine weekly rows when every editable common property matches.
      // Otherwise editing one visual group could silently overwrite a different
      // deadline, late policy, or multiplier.
      const gKey = [s.assigned_to, s.assignment_type, 'weekly', time, s.due_by || '', s.expiry_penalty, s.expiry_penalty_value, s.points_multiplier].join('-');
      const existing = groups.find(g => g.key === gKey && g.type === 'recurring');
      if (existing && existing.type === 'recurring') {
        existing.days.push(s.day_of_week);
        existing.scheduleIds.push(s.id);
      } else {
        groups.push({ key: gKey, ...base, scheduleIds: [s.id], type: 'recurring', days: [s.day_of_week] });
      }
    } else if (s.specific_date) {
      groups.push({ key: `${s.id}`, ...base, scheduleIds: [s.id], type: 'oneoff', date: s.specific_date });
    }
  }

  const handleDeleteGroup = async (ids: number[]) => {
    setError('');
    const results = await Promise.allSettled(ids.map(id => api.chores.deleteSchedule(choreId, id)));
    const failures = results.filter(r => r.status === 'rejected');
    if (failures.length > 0) {
      const reason = failures[0].status === 'rejected' ? failures[0].reason : null;
      setError(reason instanceof Error ? reason.message : t('admin.scheduleManager.deleteFailed'));
    }
    await load();
  };

  const startEdit = (g: Group) => {
    setAdding(false);
    setError('');
    setEditingKey(g.key);
    setEditType(g.type);
    setEditUserId(g.userId);
    setEditDays(g.type === 'recurring' ? [...g.days] : []);
    setEditDate(g.type === 'oneoff' ? g.date : '');
    setEditInterval(g.type === 'interval' ? String(g.interval) : '2');
    setEditIntervalStart(g.type === 'interval' ? g.start : '');
    setEditAvailableAt(g.availableAt || '');
    setEditDueBy(g.dueBy || '');
    setEditExpiryPenalty((g.expiryPenalty as 'block' | 'no_points' | 'penalty') || 'block');
    setEditExpiryPenaltyValue(String(g.expiryPenaltyValue || 5));
    setEditPointsMultiplier(String(g.pointsMultiplier || 1));
  };

  const handleSaveGroup = async (g: Group) => {
    setSaving(true);
    setError('');
    try {
      const common = {
        assigned_to: editUserId,
        assignment_type: g.assignmentType,
        available_at: editAvailableAt || undefined,
        due_by: editDueBy || undefined,
        points_multiplier: parseFloat(editPointsMultiplier) || 1,
        expiry_penalty: editDueBy ? editExpiryPenalty : 'block',
        expiry_penalty_value: editDueBy && editExpiryPenalty === 'penalty' ? parseInt(editExpiryPenaltyValue) || 0 : 0,
      };

      if (editType === 'recurring') {
        const current = schedules.filter(s => g.scheduleIds.includes(s.id));
        const byDay = new Map(current.map(s => [s.day_of_week, s]));
        const keptIds = new Set<number>();
        // Create added days first, so a failed request never removes an old day.
        for (const day of editDays) {
          const existing = byDay.get(day);
          if (existing) {
            await api.chores.updateSchedule(choreId, existing.id, { ...common, day_of_week: day });
            keptIds.add(existing.id);
          } else {
            const reusable = current.find(s => !keptIds.has(s.id));
            if (reusable) {
              await api.chores.updateSchedule(choreId, reusable.id, { ...common, day_of_week: day });
              keptIds.add(reusable.id);
            } else {
              await api.chores.createSchedule(choreId, { ...common, day_of_week: day });
            }
          }
        }
        for (const s of current) {
          if (!keptIds.has(s.id)) {
            await api.chores.deleteSchedule(choreId, s.id);
          }
        }
      } else if (editType === 'interval') {
        await api.chores.updateSchedule(choreId, g.scheduleIds[0], {
          ...common,
          recurrence_interval: parseInt(editInterval),
          recurrence_start: editIntervalStart,
        });
        for (const id of g.scheduleIds.slice(1)) await api.chores.deleteSchedule(choreId, id);
      } else {
        await api.chores.updateSchedule(choreId, g.scheduleIds[0], { ...common, specific_date: editDate });
        for (const id of g.scheduleIds.slice(1)) await api.chores.deleteSchedule(choreId, id);
      }
      setEditingKey(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t('admin.scheduleManager.saveFailed'));
    } finally {
      setSaving(false);
    }
  };

  const formatDays = (days: number[]) => {
    const sorted = [...days].sort((a, b) => a - b);
    if (sorted.length === 7) return t('admin.scheduleManager.presetEveryDay');
    if (sorted.length === 5 && WEEKDAYS.every(d => sorted.includes(d))) return t('admin.scheduleManager.presetWeekdays');
    if (sorted.length === 2 && sorted[0] === 0 && sorted[1] === 6) return t('admin.scheduleManager.presetWeekends');
    return sorted.map(d => DAY_NAMES[d]).join(' ');
  };

  return (
    <section className={styles.manager}>
      <div className={ui.sectionHead}>
        <h3 className={ui.sectionTitle}><Icon name="cal" /> {t('admin.scheduleManager.title')}</h3>
        <button
          type="button"
          className={ui.btnGhost}
          onClick={() => { setAdding(!adding); setEditingKey(null); }}
          aria-expanded={adding}
        >
          {adding ? <X aria-hidden /> : <Icon name="plus" />}
          {adding ? t('admin.scheduleManager.btnCancel') : t('admin.scheduleManager.btnAdd')}
        </button>
      </div>

      {error && <p className={ui.msgError} role="alert">{error}</p>}

      {adding && (
        <div className={ui.inset}>
          <div className={ui.field}>
            <span className={ui.label}>{t('admin.scheduleManager.labelAssignTo')}</span>
            <div className={ui.chips}>
              {users.map(u => (
                <PersonToggle key={u.id} user={u} pressed={selectedUsers.includes(u.id)} onClick={() => toggleUser(u.id)} />
              ))}
              {users.length > 1 && (
                <button
                  type="button"
                  className={ui.chip}
                  aria-pressed={selectedUsers.length === users.length}
                  onClick={() => setSelectedUsers(selectedUsers.length === users.length ? [] : users.map(u => u.id))}
                >
                  {t('admin.scheduleManager.btnAll')}
                </button>
              )}
            </div>
          </div>

          <label className={ui.field}>
            <span className={ui.label} title={t('admin.scheduleManager.scheduleTypeTitle')}>{t('admin.scheduleManager.labelScheduleType')}</span>
            <select className={ui.input} value={schedType} onChange={e => setSchedType(e.target.value as 'recurring' | 'oneoff' | 'interval')}>
              <option value="recurring">{t('admin.scheduleManager.optionRecurring')}</option>
              <option value="interval">{t('admin.scheduleManager.optionInterval')}</option>
              <option value="oneoff">{t('admin.scheduleManager.optionOneoff')}</option>
            </select>
          </label>

          {schedType === 'recurring' ? (
            <div className={ui.field}>
              <div className={styles.days}>
                {DAY_NAMES.map((name, i) => (
                  <button
                    key={i}
                    type="button"
                    className={styles.day}
                    aria-pressed={selectedDays.includes(i)}
                    onClick={() => toggleDay(i)}
                  >
                    {name}
                  </button>
                ))}
              </div>
              <div className={ui.chips}>
                <button type="button" className={ui.chip} aria-pressed={selectedDays.length === 7} onClick={() => setDayPreset(ALL_DAYS)}>{t('admin.scheduleManager.presetEveryDay')}</button>
                <button type="button" className={ui.chip} aria-pressed={selectedDays.length === 5 && WEEKDAYS.every(d => selectedDays.includes(d))} onClick={() => setDayPreset(WEEKDAYS)}>{t('admin.scheduleManager.presetWeekdays')}</button>
                <button type="button" className={ui.chip} aria-pressed={selectedDays.length === 2 && WEEKENDS.every(d => selectedDays.includes(d))} onClick={() => setDayPreset(WEEKENDS)}>{t('admin.scheduleManager.presetWeekends')}</button>
              </div>
            </div>
          ) : schedType === 'interval' ? (
            <div className={ui.formRow}>
              <label className={ui.field}>
                <span className={ui.label}>{t('admin.scheduleManager.labelEvery')}</span>
                <span className={styles.suffixed}>
                  <input className={ui.input} type="number" min="1" max="365" value={intervalDays} onChange={e => setIntervalDays(e.target.value)} />
                  <span>{t('admin.scheduleManager.suffixDays')}</span>
                </span>
              </label>
              <label className={ui.field}>
                <span className={ui.label}>{t('admin.scheduleManager.labelStartingFrom')}</span>
                <input className={ui.input} type="date" value={intervalStart} onChange={e => setIntervalStart(e.target.value)} />
              </label>
            </div>
          ) : (
            <label className={ui.field}>
              <span className={ui.label}>{t('admin.scheduleManager.labelDate')}</span>
              <input className={ui.input} type="date" value={specificDate} onChange={e => setSpecificDate(e.target.value)} />
            </label>
          )}

          <div className={ui.formRow}>
            <label className={ui.field}>
              <span className={ui.label} title={t('admin.scheduleManager.availableAtTitle')}>{t('admin.scheduleManager.labelAvailableAt')}</span>
              <input className={ui.input} type="time" value={availableAt} onChange={e => setAvailableAt(e.target.value)} />
              <span className={ui.help}>{t('admin.scheduleManager.helpAvailableAt')}</span>
            </label>
            <label className={ui.field}>
              <span className={ui.label} title={t('admin.scheduleManager.dueByTitle')}>{t('admin.scheduleManager.labelDueBy')}</span>
              <input className={ui.input} type="time" value={dueBy} onChange={e => setDueBy(e.target.value)} />
              <span className={ui.help}>{t('admin.scheduleManager.helpDueBy')}</span>
            </label>
          </div>

          <label className={ui.field}>
            <span className={ui.label} title={t('admin.scheduleManager.multiplierTitle')}>{t('admin.scheduleManager.labelMultiplier')}</span>
            <input className={ui.input} type="number" min="0.01" step="0.01" value={pointsMultiplier} onChange={e => setPointsMultiplier(e.target.value)} />
            <span className={ui.help}>{t('admin.scheduleManager.helpMultiplier')}</span>
          </label>

          {dueBy && (
            <div className={ui.formRow}>
              <label className={ui.field}>
                <span className={ui.label} title={t('admin.scheduleManager.ifLateTitle')}>{t('admin.scheduleManager.labelIfLate')}</span>
                <select className={ui.input} value={expiryPenalty} onChange={e => setExpiryPenalty(e.target.value as 'block' | 'no_points' | 'penalty')}>
                  <option value="block">{t('admin.scheduleManager.optionBlock')}</option>
                  <option value="no_points">{t('admin.scheduleManager.optionNoPoints')}</option>
                  <option value="penalty">{t('admin.scheduleManager.optionPenalty')}</option>
                </select>
              </label>
              {expiryPenalty === 'penalty' && (
                <label className={ui.field}>
                  <span className={ui.label}>{t('admin.scheduleManager.placeholderPointsToDeduct')}</span>
                  <input className={ui.input} type="number" min="1" value={expiryPenaltyValue} onChange={e => setExpiryPenaltyValue(e.target.value)} />
                </label>
              )}
            </div>
          )}

          <div className={ui.actionsEnd}>
            <button type="button" className={ui.btnPrimary} onClick={handleAdd} disabled={!canAdd || saving}>
              <Icon name="check" /> {selectedUsers.length > 1
                ? t('admin.scheduleManager.btnAssignMultiple', { count: selectedUsers.length })
                : t('admin.scheduleManager.btnAssign')}
            </button>
          </div>
        </div>
      )}

      {schedules.length === 0 ? (
        <p className={ui.emptyInline}>{t('admin.scheduleManager.emptySchedules')}</p>
      ) : (
        <div className={styles.list}>
          {groups.map(g => (
            <React.Fragment key={g.key}>
              <div className={styles.item}>
                <PersonName user={users.find(u => u.id === g.userId)} name={t('admin.scheduleManager.userFallback', { id: g.userId })} />
                <span className={styles.when}>
                  {g.type === 'recurring' ? formatDays(g.days)
                    : g.type === 'interval' ? t('admin.scheduleManager.intervalDisplay', { interval: g.interval, start: g.start })
                    : g.date}
                </span>
                {g.availableAt && <span className={styles.meta}>{t('admin.scheduleManager.displayFrom', { time: g.availableAt })}</span>}
                {g.dueBy && <span className={clsx(styles.meta, styles.metaDue)}>{t('admin.scheduleManager.displayDue', { time: g.dueBy })}</span>}
                <span className={styles.meta}>{t('admin.scheduleManager.displayMultiplier', { multiplier: g.pointsMultiplier })}</span>
                {g.dueBy && g.expiryPenalty && g.expiryPenalty !== 'block' && (
                  <span className={styles.meta}>
                    {g.expiryPenalty === 'no_points'
                      ? t('admin.scheduleManager.displayNoPointsIfLate')
                      : t('admin.scheduleManager.displayDeductIfLate', { points: g.expiryPenaltyValue })}
                  </span>
                )}
                <div className={clsx(styles.itemActions, styles.itemEnd)}>
                  <button type="button" className={ui.iconBtn} aria-label={t('admin.scheduleManager.ariaEditSchedule')} title={t('admin.scheduleManager.ariaEditSchedule')} onClick={() => editingKey === g.key ? setEditingKey(null) : startEdit(g)}>
                    {editingKey === g.key ? <X aria-hidden /> : <Pencil aria-hidden />}
                  </button>
                  <button type="button" className={clsx(ui.iconBtn, ui.iconBtnDanger)} aria-label={t('admin.scheduleManager.ariaDeleteSchedule')} title={t('admin.scheduleManager.ariaDeleteSchedule')} onClick={() => handleDeleteGroup(g.scheduleIds)}>
                    <Trash2 aria-hidden />
                  </button>
                </div>
              </div>

              {editingKey === g.key && (
                <div className={clsx(ui.inset, styles.editPanel)}>
                  <label className={ui.field}>
                    <span className={ui.label}>{t('admin.scheduleManager.labelAssignTo')}</span>
                    <select className={ui.input} value={editUserId} onChange={e => setEditUserId(Number(e.target.value))}>
                      {users.map(u => <option key={u.id} value={u.id}>{u.name}</option>)}
                    </select>
                  </label>

                  {g.type === 'recurring' ? (
                    <div className={ui.field}>
                      <div className={styles.days}>
                        {DAY_NAMES.map((name, i) => (
                          <button key={i} type="button" className={styles.day} aria-pressed={editDays.includes(i)} onClick={() => setEditDays(prev => toggleInArray(prev, i))}>{name}</button>
                        ))}
                      </div>
                    </div>
                  ) : g.type === 'interval' ? (
                    <div className={ui.formRow}>
                      <label className={ui.field}>
                        <span className={ui.label}>{t('admin.scheduleManager.labelEvery')}</span>
                        <span className={styles.suffixed}>
                          <input className={ui.input} type="number" min="1" max="365" value={editInterval} onChange={e => setEditInterval(e.target.value)} />
                          <span>{t('admin.scheduleManager.suffixDays')}</span>
                        </span>
                      </label>
                      <label className={ui.field}>
                        <span className={ui.label}>{t('admin.scheduleManager.labelStartingFrom')}</span>
                        <input className={ui.input} type="date" value={editIntervalStart} onChange={e => setEditIntervalStart(e.target.value)} />
                      </label>
                    </div>
                  ) : (
                    <label className={ui.field}>
                      <span className={ui.label}>{t('admin.scheduleManager.labelDate')}</span>
                      <input className={ui.input} type="date" value={editDate} onChange={e => setEditDate(e.target.value)} />
                    </label>
                  )}

                  <div className={ui.formRow}>
                    <label className={ui.field}>
                      <span className={ui.label}>{t('admin.scheduleManager.labelAvailableAt')}</span>
                      <input className={ui.input} type="time" value={editAvailableAt} onChange={e => setEditAvailableAt(e.target.value)} />
                    </label>
                    <label className={ui.field}>
                      <span className={ui.label}>{t('admin.scheduleManager.labelDueBy')}</span>
                      <input className={ui.input} type="time" value={editDueBy} onChange={e => setEditDueBy(e.target.value)} />
                    </label>
                  </div>

                  <div className={ui.formRow}>
                    <label className={ui.field}>
                      <span className={ui.label}>{t('admin.scheduleManager.labelMultiplier')}</span>
                      <input className={ui.input} type="number" min="0.01" step="0.01" value={editPointsMultiplier} onChange={e => setEditPointsMultiplier(e.target.value)} />
                    </label>
                    {editDueBy && (
                      <label className={ui.field}>
                        <span className={ui.label}>{t('admin.scheduleManager.labelIfLate')}</span>
                        <select className={ui.input} value={editExpiryPenalty} onChange={e => setEditExpiryPenalty(e.target.value as 'block' | 'no_points' | 'penalty')}>
                          <option value="block">{t('admin.scheduleManager.optionBlock')}</option>
                          <option value="no_points">{t('admin.scheduleManager.optionNoPoints')}</option>
                          <option value="penalty">{t('admin.scheduleManager.optionPenalty')}</option>
                        </select>
                      </label>
                    )}
                    {editDueBy && editExpiryPenalty === 'penalty' && (
                      <label className={ui.field}>
                        <span className={ui.label}>{t('admin.scheduleManager.placeholderPointsToDeduct')}</span>
                        <input className={ui.input} type="number" min="1" value={editExpiryPenaltyValue} onChange={e => setEditExpiryPenaltyValue(e.target.value)} />
                      </label>
                    )}
                  </div>

                  <div className={ui.actionsEnd}>
                    <button type="button" className={ui.btnGhost} onClick={() => setEditingKey(null)}>{t('admin.scheduleManager.btnCancel')}</button>
                    <button type="button" className={ui.btnPrimary} onClick={() => handleSaveGroup(g)} disabled={saving || !editUserId || parseFloat(editPointsMultiplier) <= 0 || (g.type === 'recurring' ? editDays.length === 0 : g.type === 'interval' ? !(parseInt(editInterval) >= 1 && editIntervalStart) : !editDate)}>
                      <Icon name="check" /> {t('admin.scheduleManager.btnSave')}
                    </button>
                  </div>
                </div>
              )}
            </React.Fragment>
          ))}
        </div>
      )}
    </section>
  );
};
