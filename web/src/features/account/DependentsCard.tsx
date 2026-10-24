import { useQuery } from '@tanstack/react-query';
import { CircleAlert, Pencil, Trash2, UserRoundCheck, UserRoundPlus } from 'lucide-react';
import { useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { familyQuery, type FamilyDependent } from '@/api/queries';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { formatDate } from '@/lib/schoolTime';
import { DependentSheet, type DependentSheetTarget } from './DependentSheet';
import { fullName } from './personForm';
import { RemoveDependent } from './RemoveDependent';

/**
 * The signed-in person's dependents (children, usually without their own
 * login): listed with their birth date and other guardians, added and edited
 * in a sheet, removed after an in-page confirmation. A managed dependent (one
 * with an account, a staff link or dependents of its own) is read-only here.
 */
export function DependentsCard() {
  const { t } = useTranslation('account');
  const titleId = useId();
  const family = useQuery(familyQuery);
  const [target, setTarget] = useState<DependentSheetTarget | null>(null);

  return (
    <Card role="region" aria-labelledby={titleId}>
      <CardHeader>
        <CardTitle>
          <h2 id={titleId} className="font-heading text-lg font-semibold">
            {t('dependents.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('dependents.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {family.isPending ? (
          <div role="status">
            <span className="sr-only">{t('dependents.loading')}</span>
            <Skeleton className="h-20 w-full" aria-hidden="true" />
          </div>
        ) : family.isError ? (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{t('dependents.loadFailed')}</AlertTitle>
          </Alert>
        ) : (
          <>
            {family.data.dependents.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t('dependents.empty')}</p>
            ) : (
              <ul className="space-y-3">
                {family.data.dependents.map((dependent) => (
                  <DependentItem
                    key={dependent.id}
                    dependent={dependent}
                    onEdit={() => setTarget({ mode: 'edit', dependent })}
                  />
                ))}
              </ul>
            )}
            <Button
              variant="outline"
              onClick={() => setTarget({ mode: 'add', familyName: family.data.me.familyName })}
            >
              <UserRoundPlus aria-hidden="true" />
              {t('dependents.add')}
            </Button>
          </>
        )}
      </CardContent>
      <DependentSheet target={target} onClose={() => setTarget(null)} />
    </Card>
  );
}

function DependentItem({ dependent, onEdit }: { dependent: FamilyDependent; onEdit: () => void }) {
  const { t, i18n } = useTranslation('account');
  const lang = i18n.resolvedLanguage ?? 'en';
  const name = fullName(dependent);
  const [confirming, setConfirming] = useState(false);
  const removeButton = useRef<HTMLButtonElement>(null);
  const confirmationId = useId();
  const guardians = new Intl.ListFormat(lang, { type: 'conjunction' }).format(
    dependent.guardians.map(fullName),
  );

  return (
    <li className="rounded-lg border p-3">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="min-w-0 space-y-0.5">
          <p className="font-medium break-words">{name}</p>
          {dependent.birthDate && (
            <p className="text-sm text-muted-foreground">
              {t('dependents.born', {
                date: formatDate(dependent.birthDate, lang, {
                  day: 'numeric',
                  month: 'long',
                  year: 'numeric',
                }),
              })}
            </p>
          )}
          {dependent.guardians.length > 0 && (
            <p className="text-sm text-muted-foreground">
              {t('dependents.otherGuardians', { names: guardians })}
            </p>
          )}
          {dependent.managed && (
            <p className="flex items-start gap-1.5 text-sm text-muted-foreground">
              <UserRoundCheck className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
              {t('dependents.managed', { givenName: dependent.givenName })}
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          {/* The owner or the office maintains a managed dependent's data, not its guardians. */}
          {!dependent.managed && (
            <Button
              variant="outline"
              size="sm"
              aria-label={t('dependents.editNamed', { name })}
              onClick={onEdit}
            >
              <Pencil aria-hidden="true" />
              {t('dependents.edit')}
            </Button>
          )}
          <Button
            ref={removeButton}
            variant="outline"
            size="sm"
            aria-label={t('dependents.removeNamed', { name })}
            aria-expanded={confirming}
            aria-controls={confirming ? confirmationId : undefined}
            onClick={() => setConfirming((c) => !c)}
          >
            <Trash2 aria-hidden="true" />
            {t('dependents.remove')}
          </Button>
        </div>
      </div>
      {confirming && (
        <RemoveDependent
          id={confirmationId}
          dependent={dependent}
          guardians={guardians}
          onCancel={() => {
            setConfirming(false);
            removeButton.current?.focus();
          }}
        />
      )}
    </li>
  );
}
