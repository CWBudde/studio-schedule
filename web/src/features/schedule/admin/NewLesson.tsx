import { CalendarPlus } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { useScheduleSearch } from '../useScheduleSearch';
import { LessonFormSheet } from './LessonFormSheet';

/**
 * The office's button for a new one-off lesson, with its form; the new
 * lesson opens in the lesson sheet. Shown by OfficeActions.
 */
export function NewLesson() {
  const { t } = useTranslation('admin');
  const { update } = useScheduleSearch();
  const [adding, setAdding] = useState(false);
  return (
    <>
      <Button variant="outline" onClick={() => setAdding(true)}>
        <CalendarPlus aria-hidden="true" />
        {t('lessons.new')}
      </Button>
      <LessonFormSheet
        target={adding ? { mode: 'add' } : null}
        onClose={() => setAdding(false)}
        onCreated={(lesson) => void update({ lesson })}
      />
    </>
  );
}
