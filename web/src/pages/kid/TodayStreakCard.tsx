import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { Icon } from '../../design';
import type { ScheduledChore, UserStreakData } from '../../types';
import { isDone } from './choreView';
import s from './kid.module.css';

interface TodayStreakCardProps {
  streak: UserStreakData | null;
  chores: ScheduledChore[];
}

export function TodayStreakCard({ streak, chores }: TodayStreakCardProps) {
  const { t } = useTranslation();

  // Streak requires all non-bonus chores (required / essential + core / daily)
  const streakChores = chores.filter(c => c.category !== 'bonus');
  const essentialChores = chores.filter(c => c.category === 'required');
  const dailyChores = chores.filter(c => c.category === 'core');

  const total = streakChores.length;
  const done = streakChores.filter(isDone).length;
  const remaining = total - done;

  const essentialLeft = essentialChores.filter(c => !isDone(c)).length;
  const dailyLeft = dailyChores.filter(c => !isDone(c)).length;

  const isAchieved = total > 0 && remaining === 0;
  const currentStreak = streak?.current_streak ?? 0;
  const longestStreak = streak?.longest_streak ?? 0;
  const nextReward = streak?.next_reward;

  const progressPercent = total > 0 ? Math.round((done / total) * 100) : 100;

  // If there are no chores today, show rest day notice
  if (total === 0) {
    return (
      <div className={clsx(s.streakCard, s.streakCardRest)}>
        <div className={s.streakCardTop}>
          <div className={s.streakCardLeft}>
            <span className={clsx(s.streakCardFlame, s.flameRest)}>
              <Icon name="flame" fill size={22} />
            </span>
            <div className={s.streakCardTitleBox}>
              <div className={s.streakTitleRow}>
                <span className={s.streakCardTitle}>
                  {t('kid.week.streakDays', { count: currentStreak })}
                </span>
                <span className={s.streakBestBadge}>
                  {t('kid.week.best', { count: longestStreak })}
                </span>
              </div>
            </div>
          </div>
          <span className={clsx(s.streakPill, s.streakPillSafe)}>
            {t('kid.today.streakRestDay')}
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className={clsx(s.streakCard, isAchieved && s.streakCardAchieved)}>
      {/* Top row: Flame + Title + Status Pill */}
      <div className={s.streakCardTop}>
        <div className={s.streakCardLeft}>
          <span className={clsx(s.streakCardFlame, isAchieved ? s.flameActive : s.flameInProgress)}>
            <Icon name="flame" fill size={26} />
          </span>
          <div className={s.streakCardTitleBox}>
            <div className={s.streakTitleRow}>
              <span className={s.streakCardTitle}>
                {t('kid.week.streakDays', { count: currentStreak })}
              </span>
              <span className={s.streakBestBadge}>
                {t('kid.week.best', { count: longestStreak })}
              </span>
            </div>
            <span className={s.streakCardSubtitle}>
              {isAchieved
                ? t('kid.today.streakAchievedSub')
                : t('kid.today.streakProgress', { done, total })}
            </span>
          </div>
        </div>

        <span className={clsx(s.streakPill, isAchieved ? s.streakPillDone : s.streakPillPending)}>
          {isAchieved ? (
            <>
              <Icon name="check" size={13} />
              <span>{t('kid.today.streakAchieved')}</span>
            </>
          ) : (
            <>
              <Icon name="flame" size={13} fill />
              <span>{t('kid.today.streakRemaining', { count: remaining })}</span>
            </>
          )}
        </span>
      </div>

      {/* Progress Bar */}
      <div className={s.streakProgressTrack} role="progressbar" aria-valuenow={progressPercent} aria-valuemin={0} aria-valuemax={100}>
        <div
          className={clsx(s.streakProgressBar, isAchieved && s.streakProgressBarDone)}
          style={{ width: `${progressPercent}%` }}
        />
      </div>

      {/* Explanation & Requirement Details */}
      <div className={s.streakRuleRow}>
        <div className={s.streakRuleText}>
          {isAchieved ? (
            <span className={s.streakRuleSuccess}>
              <Icon name="spark" size={14} />
              {t('kid.today.streakAchievedSub')}
            </span>
          ) : (
            <>
              <span className={s.streakRuleTip}>
                <Icon name="sprout" size={14} />
                {t('kid.today.streakRuleTip')}
              </span>
              {(essentialLeft > 0 || dailyLeft > 0) && (
                <span className={s.streakBreakdown}>
                  {t('kid.today.streakBreakdown', { essential: essentialLeft, daily: dailyLeft })}
                </span>
              )}
            </>
          )}
        </div>
      </div>

      {/* Next Milestone Reward */}
      {nextReward && (
        <div className={s.streakMilestoneRow}>
          <span className={s.streakMilestoneIcon}>🎁</span>
          <span className={s.streakMilestoneText}>
            {t('kid.today.streakNextMilestone', {
              count: nextReward.days_remaining,
              label: nextReward.label,
              points: nextReward.bonus_points,
            })}
          </span>
        </div>
      )}
    </div>
  );
}
