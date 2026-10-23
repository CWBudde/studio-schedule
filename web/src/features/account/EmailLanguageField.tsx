import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { Me } from '@/api/client';
import { updateLocaleMutation } from '@/api/queries';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { FormError } from '@/features/auth/FormError';
import { supportedLanguages } from '@/i18n';

/** Value of the "school default" option; Radix Select does not allow an empty value. */
const SCHOOL_DEFAULT = 'default';

const isLanguage = (value: string): value is (typeof supportedLanguages)[number] =>
  (supportedLanguages as readonly string[]).includes(value);

/**
 * The language of the mails the account receives (e.g. a cancellation),
 * saved as soon as it is chosen. The UI language is the header's switcher's
 * business and stays as it is.
 */
export function EmailLanguageField({ me }: { me: Me }) {
  const { t } = useTranslation('account');
  const { t: tCommon } = useTranslation('common');
  const id = useId();
  const hintId = `${id}-hint`;
  const queryClient = useQueryClient();
  const update = useMutation(updateLocaleMutation(queryClient));

  const choose = (value: string) =>
    update.mutate(isLanguage(value) ? { locale: value } : {}, {
      onSuccess: () => toast.success(t('profile.emailLanguageSaved')),
    });

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{t('profile.emailLanguage')}</Label>
      <Select
        value={me.locale ?? SCHOOL_DEFAULT}
        onValueChange={choose}
        disabled={update.isPending}
      >
        <SelectTrigger id={id} aria-describedby={hintId} className="w-full sm:w-64">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={SCHOOL_DEFAULT}>{t('profile.schoolDefault')}</SelectItem>
          {supportedLanguages.map((lng) => (
            <SelectItem key={lng} value={lng} lang={lng}>
              {tCommon(`language.${lng}`)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p id={hintId} className="text-xs text-muted-foreground">
        {t('profile.emailLanguageHint')}
      </p>
      <FormError error={update.error} />
    </div>
  );
}
