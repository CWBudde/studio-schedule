import { Loader2, RotateCcw } from 'lucide-react';
import { useState, useSyncExternalStore, type ReactElement } from 'react';
import { useTranslation } from 'react-i18next';
import { demoControls, type DemoTab } from '@/api/transport';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';

/**
 * Header button that resets the web demo: it deletes the data the demo keeps
 * between visits (and with it the session) and reloads the page, which starts
 * again from the sample data. Present only while this tab holds the demo's
 * lock: a reset from any other tab would race with the active tab's saves.
 */
export function ResetDemo() {
  const { t } = useTranslation();
  const tab = useSyncExternalStore(subscribeTab, getTab, getTab);

  if (!demoControls || tab !== 'active') return null;

  return (
    <ResetDemoDialog>
      <Button variant="ghost" size="sm" title={t('demo.reset.open')}>
        <RotateCcw aria-hidden="true" />
        <span className="sr-only">{t('demo.reset.open')}</span>
      </Button>
    </ResetDemoDialog>
  );
}

const noTab = (): DemoTab => 'pending';
const getTab = demoControls ? demoControls.getTab : noTab;
const subscribeTab = demoControls ? demoControls.subscribeTab : () => () => undefined;

/** The confirmation for a reset, opened by its child (a button). */
export function ResetDemoDialog({ children }: { children: ReactElement }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);

  if (!demoControls) return null;
  const controls = demoControls;

  const confirm = () => {
    setPending(true);
    // Reloads the page; a failed delete is reported after the reload.
    void controls.reset();
  };

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (pending) return;
        setOpen(next);
      }}
    >
      <AlertDialogTrigger asChild>{children}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('demo.reset.title')}</AlertDialogTitle>
          <AlertDialogDescription>{t('demo.reset.description')}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{t('demo.reset.cancel')}</AlertDialogCancel>
          {/* No AlertDialogAction: it would close the dialog before the reset. */}
          <Button variant="destructive" disabled={pending} onClick={confirm}>
            {pending ? (
              <Loader2 className="animate-spin" aria-hidden="true" />
            ) : (
              <RotateCcw aria-hidden="true" />
            )}
            {pending ? t('demo.reset.resetting') : t('demo.reset.confirm')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
