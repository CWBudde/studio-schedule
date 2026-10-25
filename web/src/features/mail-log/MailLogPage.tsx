import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CircleAlert, FlaskConical, RotateCcw } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type MailLogFilter,
  type MailLogItem,
  mailLogQuery,
  mailQuery,
  meQuery,
  publicSettingsQuery,
  requeueMailMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { SelectField } from '@/features/admin/crud/fields';
import { useDebouncedValue } from '@/features/admin/crud/useDebouncedValue';
import { NoAccess } from '@/features/admin/NoAccess';
import { isDemo } from '@/lib/env';
import { formatTimestamp } from '@/lib/schoolTime';
import { isTemplate, mailTemplates } from './mailTemplates';

const statuses = ['pending', 'sent', 'failed'] as const;
/** "Any" in the selects, which have no empty value. */
const any = 'any';

const statusVariant = { pending: 'outline', sent: 'secondary', failed: 'destructive' } as const;

/**
 * The mail log (/admin/mails), for admins only (ADR 0014): every mail of
 * the outbox, newest first, filtered by status, template and recipient,
 * with each mail's details and, for a failed one, a way to queue it again.
 * The bodies of mails with a secret link never show.
 */
export function MailLogPage() {
  const { t } = useTranslation('mailLog');
  const me = useQuery(meQuery);
  const isAdmin = me.data?.role === 'admin';

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('title')}</h1>
        {isAdmin && <p className="max-w-prose text-muted-foreground">{t('description')}</p>}
      </div>
      {me.isPending ? (
        <Skeleton className="h-40 w-full" />
      ) : !isAdmin ? (
        <NoAccess message={t('noAccess')} />
      ) : (
        <MailLog />
      )}
    </div>
  );
}

function MailLog() {
  const { t, i18n } = useTranslation('mailLog');
  const lang = i18n.resolvedLanguage ?? 'en';
  const settings = useQuery(publicSettingsQuery);
  const timeZone = settings.data?.timezone;
  const [status, setStatus] = useState<string>(any);
  const [template, setTemplate] = useState<string>(any);
  const [recipientInput, setRecipientInput] = useState('');
  const recipient = useDebouncedValue(recipientInput.trim(), 300);
  const [shown, setShown] = useState<MailLogItem | null>(null);

  const filter: MailLogFilter = {
    ...(status !== any ? { status: status as (typeof statuses)[number] } : {}),
    ...(isTemplate(template) ? { template } : {}),
    ...(recipient ? { recipient } : {}),
  };
  const mails = useInfiniteQuery(mailLogQuery(filter));
  const items = mails.data?.pages.flatMap((p) => p.items) ?? [];
  const templateName = (name: string | undefined) =>
    isTemplate(name) ? t(`templates.${name}`) : (name ?? '');

  return (
    <div className="space-y-4">
      {isDemo && (
        <Alert>
          <FlaskConical aria-hidden="true" />
          <AlertDescription>{t('demoHint')}</AlertDescription>
        </Alert>
      )}
      <fieldset className="grid gap-3 sm:grid-cols-3">
        <legend className="sr-only">{t('filters')}</legend>
        <SelectField
          label={t('status')}
          placeholder={t('anyStatus')}
          options={[
            { value: any, label: t('anyStatus') },
            ...statuses.map((s) => ({ value: s, label: t(`statuses.${s}`) })),
          ]}
          value={status}
          onChange={setStatus}
        />
        <SelectField
          label={t('template')}
          placeholder={t('anyTemplate')}
          options={[
            { value: any, label: t('anyTemplate') },
            ...mailTemplates.map((name) => ({ value: name, label: t(`templates.${name}`) })),
          ]}
          value={template}
          onChange={setTemplate}
        />
        <div className="grid gap-1.5">
          <Label htmlFor="mail-log-recipient">{t('recipient')}</Label>
          <Input
            id="mail-log-recipient"
            type="search"
            placeholder={t('recipientPlaceholder')}
            value={recipientInput}
            onChange={(e) => setRecipientInput(e.target.value)}
          />
        </div>
      </fieldset>

      {mails.isError || settings.isError ? (
        <Alert variant="destructive">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{t('loadError')}</AlertTitle>
          <AlertDescription>
            <Button
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={() => {
                if (mails.isError) void mails.refetch();
                if (settings.isError) void settings.refetch();
              }}
            >
              <RotateCcw aria-hidden="true" />
              {t('retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : mails.isPending || timeZone === undefined ? (
        <Skeleton className="h-40 w-full" />
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed px-4 py-10 text-center text-muted-foreground">
          {t('empty')}
        </p>
      ) : (
        <>
          <Table aria-label={t('table')}>
            <TableHeader>
              <TableRow>
                <TableHead>{t('at')}</TableHead>
                <TableHead>{t('to')}</TableHead>
                <TableHead>{t('subject')}</TableHead>
                <TableHead>{t('statusColumn')}</TableHead>
                <TableHead>
                  <span className="sr-only">{t('details')}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((m) => (
                <TableRow key={m.id}>
                  <TableCell className="whitespace-nowrap">
                    <time dateTime={m.createdAt}>
                      {formatTimestamp(m.createdAt, timeZone, lang)}
                    </time>
                  </TableCell>
                  <TableCell className="break-all">{m.to}</TableCell>
                  <TableCell>
                    <span className="block break-words">{m.subject}</span>
                    <span className="block text-xs text-muted-foreground">
                      {templateName(m.template)}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={statusVariant[m.status]}>{t(`statuses.${m.status}`)}</Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={t('show', { to: m.to })}
                      onClick={() => setShown(m)}
                    >
                      {t('details')}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {mails.hasNextPage && (
            <Button
              variant="outline"
              disabled={mails.isFetchingNextPage}
              onClick={() => void mails.fetchNextPage()}
            >
              {mails.isFetchingNextPage ? t('loadingMore') : t('more')}
            </Button>
          )}
        </>
      )}

      {timeZone && (
        <MailSheet
          mail={shown}
          timeZone={timeZone}
          templateName={templateName}
          onClose={() => setShown(null)}
        />
      )}
    </div>
  );
}

/** The details of a mail of the log; closed while `mail` is null. */
function MailSheet({
  mail,
  timeZone,
  templateName,
  onClose,
}: {
  mail: MailLogItem | null;
  timeZone: string;
  templateName: (name: string | undefined) => string;
  onClose: () => void;
}) {
  const { t } = useTranslation('mailLog');
  // Keep showing the last mail while the sheet animates out.
  const [last, setLast] = useState(mail);
  if (mail !== null && mail !== last) setLast(mail);

  return (
    <Sheet
      open={mail !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        side="right"
        className="w-full overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-lg"
      >
        {last && (
          <>
            <SheetHeader className="pr-12">
              <SheetTitle className="text-lg break-all">
                {t('sheet.title', { to: last.to })}
              </SheetTitle>
              <SheetDescription>{last.subject}</SheetDescription>
            </SheetHeader>
            <MailDetails
              key={last.id}
              mail={last}
              timeZone={timeZone}
              templateName={templateName}
            />
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

function MailDetails({
  mail,
  timeZone,
  templateName,
}: {
  mail: MailLogItem;
  timeZone: string;
  templateName: (name: string | undefined) => string;
}) {
  const { t, i18n } = useTranslation('mailLog');
  const lang = i18n.resolvedLanguage ?? 'en';
  const queryClient = useQueryClient();
  const detail = useQuery(mailQuery(mail.id));
  const requeue = useMutation(requeueMailMutation(queryClient));
  const m = detail.data ?? mail;
  const at = (instant: string | undefined) =>
    instant ? formatTimestamp(instant, timeZone, lang) : undefined;

  const rows: [string, string | undefined][] = [
    [t('sheet.template'), templateName(m.template)],
    [t('sheet.createdAt'), at(m.createdAt)],
    [t('sheet.status'), t(`statuses.${m.status}`)],
    [t('sheet.attempts'), String(m.attempts)],
    [t('sheet.nextAttempt'), at(m.nextAttemptAt)],
    [t('sheet.sentAt'), at(m.sentAt)],
    [t('sheet.lastError'), m.lastError],
    [t('sheet.attachment'), m.attachment],
    [t('sheet.replyTo'), m.replyTo],
  ];

  return (
    <div className="space-y-4 px-4 pb-4 text-sm">
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
        {rows
          .filter((row): row is [string, string] => Boolean(row[1]))
          .map(([label, value]) => (
            <div key={label} className="contents">
              <dt className="text-muted-foreground">{label}</dt>
              <dd className="min-w-0 break-words">{value}</dd>
            </div>
          ))}
      </dl>

      {m.status === 'failed' && (
        <div className="space-y-2">
          <ProblemAlert
            error={requeue.error}
            conflicts={{ 'secret-link': t('sheet.secretLink'), 'not-failed': t('sheet.notFailed') }}
          />
          {m.body === 'withheld' ? (
            <p className="text-muted-foreground">{t('sheet.secretLink')}</p>
          ) : (
            <>
              <Button
                size="sm"
                disabled={requeue.isPending}
                onClick={() =>
                  requeue.mutate(m.id, {
                    onSuccess: () => toast.success(t('sheet.requeued', { to: m.to })),
                  })
                }
              >
                <RotateCcw aria-hidden="true" />
                {requeue.isPending ? t('sheet.requeuing') : t('sheet.requeue')}
              </Button>
              <p className="text-xs text-muted-foreground">{t('sheet.requeueHint')}</p>
            </>
          )}
        </div>
      )}

      <section aria-label={t('sheet.text')} className="space-y-1.5">
        <h3 className="font-medium">{t('sheet.text')}</h3>
        {m.body === 'withheld' ? (
          <p className="text-muted-foreground">{t('sheet.withheld')}</p>
        ) : m.body === 'purged' ? (
          <p className="text-muted-foreground">{t('sheet.purged')}</p>
        ) : detail.isPending ? (
          <Skeleton className="h-24 w-full" />
        ) : detail.isError ? (
          <p role="alert" className="text-destructive">
            {t('sheet.loadError')}
          </p>
        ) : (
          <pre className="max-h-96 overflow-auto rounded-md bg-muted p-3 font-sans text-sm whitespace-pre-wrap">
            {detail.data.text}
          </pre>
        )}
      </section>
    </div>
  );
}
