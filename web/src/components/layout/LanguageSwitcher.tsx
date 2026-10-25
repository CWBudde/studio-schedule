import { Languages } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  chooseLanguage,
  supportedLanguage,
  supportedLanguages,
  type SupportedLanguage,
} from '@/i18n';

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  const current: SupportedLanguage = i18n.resolvedLanguage === 'de' ? 'de' : 'en';

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" aria-label={t('language.change')} className="gap-1.5">
          <Languages aria-hidden="true" />
          <span className="font-medium uppercase">{current}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-40">
        <DropdownMenuLabel>{t('language.label')}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup
          value={current}
          onValueChange={(value) => {
            const lng = supportedLanguage(value);
            if (lng) void chooseLanguage(lng);
          }}
        >
          {supportedLanguages.map((lng) => (
            <DropdownMenuRadioItem key={lng} value={lng} lang={lng}>
              {t(`language.${lng}`)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
