import { useTranslation } from 'react-i18next';
import type { ImportField } from '@/api/queries';
import { splitField } from './importMapping';

/** The label of a target field: the member's, or a guardian's with its number. */
export function useFieldLabel() {
  const { t } = useTranslation('imports');
  return (field: string) => {
    const { slot, base } = splitField(field as ImportField);
    const name = t(`fields.${base}`);
    return slot === 0 ? name : t('guardianField', { slot, field: name });
  };
}
