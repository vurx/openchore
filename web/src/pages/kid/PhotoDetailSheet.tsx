import React, { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../api';
import type { ScheduledChore } from '../../types';
import { Icon } from '../../design';
import { Sheet } from './Sheet';
import s from './kid.module.css';

interface PhotoDetailSheetProps {
  chore: ScheduledChore;
  onClose: () => void;
  onUpdated?: () => void;
}

export function PhotoDetailSheet({ chore, onClose, onUpdated }: PhotoDetailSheetProps) {
  const { t } = useTranslation();
  const [photoUrl, setPhotoUrl] = useState<string>(chore.photo_url || '');
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const isPending = chore.completion_status === 'pending';
  const isApproved = chore.completion_status === 'approved' || (chore.completed && chore.completion_status !== 'pending' && chore.completion_status !== 'rejected');
  const isRejected = chore.completion_status === 'rejected';

  const handleChangePhoto = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true);
    setError(null);
    try {
      const { url } = await api.chores.upload(file);
      if (chore.completion_id) {
        await api.chores.attachPhoto(chore.completion_id, url);
      }
      setPhotoUrl(url);
      onUpdated?.();
    } catch (err) {
      setError((err as Error)?.message || t('kid.photo.uploadFailed', { defaultValue: '照片上传失败，请重试' }));
    } finally {
      setUploading(false);
    }
  };

  return (
    <Sheet title={chore.title} onClose={onClose}>
      <div className={s.photoDetailWrap}>
        {photoUrl ? (
          <div className={s.photoPreviewContainer}>
            <img src={photoUrl} alt={chore.title} className={s.photoPreviewImg} />
          </div>
        ) : (
          <div className={s.noPhotoPlaceholder}>
            <Icon name="camera" size={32} />
            <p>{t('kid.photo.noPhoto', { defaultValue: '暂无照片' })}</p>
          </div>
        )}

        <div className={s.photoStatusRow}>
          {isPending && (
            <span className={s.statusBadgeWaiting}>
              <Icon name="clock" /> {t('kid.chore.waitingApproval', { defaultValue: '待家长审核' })}
            </span>
          )}
          {isApproved && (
            <span className={s.statusBadgeDone}>
              <Icon name="check" /> {t('kid.chore.approved', { defaultValue: '已审核完成' })}
            </span>
          )}
          {isRejected && (
            <span className={s.statusBadgeRejected}>
              {t('kid.chore.rejected', { defaultValue: '已驳回' })}
            </span>
          )}
          {chore.date && <span className={s.photoDateLabel}>{chore.date}</span>}
        </div>

        {chore.ai_feedback && (
          <div className={s.aiFeedbackBox}>
            <span className={s.aiFeedbackTitle}>🤖 {t('kid.photo.aiNote', { defaultValue: 'AI 评语' })}:</span>
            <p className={s.aiFeedbackText}>{chore.ai_feedback}</p>
          </div>
        )}

        {isPending && (
          <div className={s.photoEditSection}>
            <p className={s.photoEditHelp}>
              {t('kid.photo.canChangeNotice', { defaultValue: '家长审核通过前，你可以重新上传或修改打卡照片：' })}
            </p>
            <input
              ref={fileRef}
              type="file"
              accept="image/*"
              capture="environment"
              onChange={handleChangePhoto}
              hidden
            />
            <button
              type="button"
              className={s.primaryAction}
              onClick={() => fileRef.current?.click()}
              disabled={uploading}
            >
              <Icon name="camera" />
              {uploading
                ? t('kid.photo.uploading', { defaultValue: '正在上传...' })
                : t('kid.photo.changePhoto', { defaultValue: '重新拍照 / 更换照片' })}
            </button>
            {error && <p className={s.errorLine} role="alert">{error}</p>}
          </div>
        )}

        {isApproved && (
          <p className={s.photoReadOnlyNotice}>
            {t('kid.photo.approvedNotice', { defaultValue: '此任务已完成并审核通过，照片不可修改。' })}
          </p>
        )}
      </div>
    </Sheet>
  );
}
