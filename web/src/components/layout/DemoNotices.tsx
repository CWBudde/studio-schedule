import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { demoControls, type DemoNotice } from '@/api/transport';

const messages = {
  'save-failed': 'demo.notices.saveFailed',
  'reset-failed': 'demo.notices.resetFailed',
} as const satisfies Record<DemoNotice, string>;

/**
 * Tells the visitor of the web demo, once, when its data could not be saved
 * (e.g. a private window without storage) or a reset did not delete it.
 * Renders nothing; mount it after the Toaster.
 */
export function DemoNotices() {
  const { t } = useTranslation();

  useEffect(
    () =>
      demoControls?.subscribeNotices((notice) =>
        toast.warning(t(messages[notice]), { duration: Infinity, closeButton: true }),
      ),
    [t],
  );

  return null;
}
