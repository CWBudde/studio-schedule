import { useQuery } from '@tanstack/react-query';
import { Mail } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { demoMailboxQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { setDemoMailboxOpen } from './mailboxOpen';

/** Where to find a mail in the demo, with a button that opens the mailbox. Absent elsewhere. */
export function DemoMailboxHint() {
  const { t } = useTranslation('auth');
  const mailbox = useQuery(demoMailboxQuery);
  if (!mailbox.data) return null;

  return (
    <div className="mt-2 space-y-2">
      <p>{t('demoMailbox.hint')}</p>
      <Button variant="outline" size="sm" onClick={() => setDemoMailboxOpen(true)}>
        <Mail aria-hidden="true" />
        {t('demoMailbox.open')}
      </Button>
    </div>
  );
}
