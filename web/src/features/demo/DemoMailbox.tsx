import { useQuery } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { CircleAlert, Mail, RotateCcw, XIcon } from 'lucide-react';
import { useEffect, useId } from 'react';
import { useTranslation } from 'react-i18next';
import { useBackendStatus } from '@/api/backendStatus';
import type { MailboxMail, MailStatus } from '@/api/client';
import { demoMailboxQuery, publicSettingsQuery } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import { appPathOf, splitLinks } from '@/lib/appLinks';
import { routerHistory } from '@/lib/env';
import { formatDateTime } from '@/lib/schoolTime';
import { setDemoMailboxOpen, useDemoMailboxOpen } from './mailboxOpen';

/**
 * Header button and sheet of the demo mailbox: the mails the app would have
 * sent. Present only where the API has a mailbox (a demo installation).
 */
export function DemoMailbox() {
  const { t } = useTranslation();
  const backend = useBackendStatus();
  const open = useDemoMailboxOpen();
  const mailbox = useQuery({ ...demoMailboxQuery, enabled: backend.state === 'ready' });

  // A closed mailbox is the default for the next page that shows one.
  useEffect(() => () => setDemoMailboxOpen(false), []);

  if (!mailbox.data) return null;
  const count = mailbox.data.length;

  return (
    <Sheet open={open} onOpenChange={setDemoMailboxOpen}>
      <SheetTrigger asChild>
        <Button variant="ghost" size="sm" className="relative gap-1">
          <Mail aria-hidden="true" />
          <span className="sr-only">
            {t('demo.mailbox.open')} ({t('demo.mailbox.count', { count })})
          </span>
          {count > 0 && (
            <span
              aria-hidden="true"
              className="min-w-4 rounded-full bg-primary px-1 text-[0.625rem] leading-4 font-semibold text-primary-foreground"
            >
              {count}
            </span>
          )}
        </Button>
      </SheetTrigger>
      <SheetContent
        side="right"
        showCloseButton={false}
        className="w-full overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-lg"
      >
        <SheetHeader className="pr-12">
          <SheetTitle className="text-lg">{t('demo.mailbox.title')}</SheetTitle>
          <SheetDescription>{t('demo.mailbox.description')}</SheetDescription>
        </SheetHeader>
        {open && <MailList />}
        <SheetClose asChild>
          <Button variant="ghost" size="icon-sm" className="absolute top-3 right-3">
            <XIcon aria-hidden="true" />
            <span className="sr-only">{t('demo.mailbox.close')}</span>
          </Button>
        </SheetClose>
      </SheetContent>
    </Sheet>
  );
}

/** The mails, refetched whenever the sheet opens. */
function MailList() {
  const { t } = useTranslation();
  const mailbox = useQuery(demoMailboxQuery);
  const settings = useQuery(publicSettingsQuery);

  if (mailbox.isError || settings.isError) {
    return (
      <div className="px-4">
        <Alert variant="destructive">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{t('demo.mailbox.loadError')}</AlertTitle>
          <AlertDescription>
            <Button
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={() => {
                if (mailbox.isError) void mailbox.refetch();
                if (settings.isError) void settings.refetch();
              }}
            >
              <RotateCcw aria-hidden="true" />
              {t('demo.mailbox.retry')}
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  if (!mailbox.data || !settings.data) {
    return (
      <div className="space-y-3 px-4" role="status" aria-label={t('demo.mailbox.loading')}>
        <Skeleton className="h-5 w-2/3" />
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }

  if (mailbox.data.length === 0) {
    return <p className="px-4 text-sm text-muted-foreground">{t('demo.mailbox.empty')}</p>;
  }

  return (
    <ol className="space-y-4 px-4 pb-6">
      {mailbox.data.map((mail) => (
        <li key={mail.id}>
          <MailItem mail={mail} timeZone={settings.data.timezone} />
        </li>
      ))}
    </ol>
  );
}

const statusVariant: Record<MailStatus, 'secondary' | 'outline' | 'destructive'> = {
  pending: 'outline',
  sent: 'secondary',
  failed: 'destructive',
};

function MailItem({ mail, timeZone }: { mail: MailboxMail; timeZone: string }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.resolvedLanguage ?? 'en';
  const titleId = useId();

  return (
    <article aria-labelledby={titleId} className="space-y-2 rounded-lg border p-3">
      <div className="flex items-start justify-between gap-2">
        <h3 id={titleId} className="font-heading text-sm font-semibold">
          {mail.subject}
        </h3>
        <Badge variant={statusVariant[mail.status]}>
          {t(`demo.mailbox.status.${mail.status}`)}
        </Badge>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
        <dt>{t('demo.mailbox.to')}</dt>
        <dd className="min-w-0 truncate">{mail.to}</dd>
        {mail.replyTo && (
          <>
            <dt>{t('demo.mailbox.replyTo')}</dt>
            <dd className="min-w-0 truncate">{mail.replyTo}</dd>
          </>
        )}
        <dt>{t('demo.mailbox.sentAt')}</dt>
        <dd>
          <time dateTime={mail.createdAt}>{formatDateTime(mail.createdAt, timeZone, lang)}</time>
        </dd>
      </dl>
      <MailText text={mail.text} />
    </article>
  );
}

/** The plain-text body with its links clickable; links to the app open within it. */
function MailText({ text }: { text: string }) {
  const router = useRouter();

  return (
    <p className="text-sm break-words whitespace-pre-wrap">
      {splitLinks(text).map((part, i) => {
        if ('text' in part) return part.text;
        const path = appPathOf(part.url, window.location, routerHistory);
        if (path === null) {
          return (
            <a
              key={i}
              href={part.url}
              target="_blank"
              rel="noopener noreferrer"
              className="font-medium break-all text-primary underline underline-offset-4"
            >
              {part.url}
            </a>
          );
        }
        return (
          <a
            key={i}
            href={part.url}
            className="font-medium break-all text-primary underline underline-offset-4"
            onClick={(event) => {
              if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey) return;
              event.preventDefault();
              setDemoMailboxOpen(false);
              router.history.push(path);
            }}
          >
            {part.url}
          </a>
        );
      })}
    </p>
  );
}
