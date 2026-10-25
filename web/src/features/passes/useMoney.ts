import { useQuery } from '@tanstack/react-query';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { publicSettingsQuery } from '@/api/queries';
import { formatMoney } from '@/lib/money';

/**
 * The school's currency (undefined until the public settings are there), a
 * formatter of amounts in its minor units in the UI language (stable while
 * both stay the same), and whether asking for the settings failed, with a
 * retry.
 */
export function useMoney() {
  const { i18n } = useTranslation();
  const settings = useQuery(publicSettingsQuery);
  const currency = settings.data?.currency;
  const lang = i18n.resolvedLanguage ?? 'en';
  const format = useCallback(
    (minor: number) => (currency ? formatMoney(minor, currency, lang) : ''),
    [currency, lang],
  );
  return {
    currency,
    format,
    isError: settings.isError && !currency,
    retry: () => void settings.refetch(),
  };
}
