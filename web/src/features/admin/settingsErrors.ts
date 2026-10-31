import type { FieldError } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { useAdminErrorText } from './crud/form';

const settingsErrors = [
  'hexColor',
  'contrast',
  'timezone',
  'currency',
  'weeksAhead',
  'visibleDays',
  'cutoff',
  'inviteExpiry',
  'releasedThroughLatest',
] as const;
type SettingsErrorKey = (typeof settingsErrors)[number];
const isSettingsErrorKey = (key: unknown): key is SettingsErrorKey =>
  settingsErrors.includes(key as SettingsErrorKey);

/** Translates a field error of the settings forms: their own keys, else the admin forms'. */
export function useSettingsErrorText() {
  const { t } = useTranslation('settings');
  const adminText = useAdminErrorText();
  return (error: FieldError | undefined): string | undefined =>
    isSettingsErrorKey(error?.message) ? t(`errors.${error.message}`) : adminText(error);
}
