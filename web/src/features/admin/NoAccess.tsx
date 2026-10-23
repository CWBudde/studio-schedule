import { ShieldX } from 'lucide-react';
import { useTranslation } from 'react-i18next';

/**
 * Says that a page is not for the signed-in person (or the visitor). As a
 * `page`, its title is the page's heading.
 */
export function NoAccess({ message, page = false }: { message: string; page?: boolean }) {
  const { t } = useTranslation('admin');
  const Title = page ? 'h1' : 'p';
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-4 py-10 text-center">
      <ShieldX className="size-8 text-muted-foreground" aria-hidden="true" />
      <Title className="font-heading text-lg font-semibold">{t('noAccess.title')}</Title>
      <p className="max-w-prose text-muted-foreground">{message}</p>
    </div>
  );
}
