import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { conflictReason } from '@/api/conflict';
import {
  familyQuery,
  meQuery,
  myEventsQuery,
  registerForEventMutation,
  type PublicEvent,
} from '@/api/queries';
import { PendingIcon } from '@/components/PendingIcon';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { LoadError } from '@/features/admin/crud/LoadError';
import { bookablePersons } from '@/features/schedule/booking/bookingRules';
import { shownName } from '@/lib/personName';
import { formatDateTime } from '@/lib/schoolTime';
import { RegistrationControls } from './RegistrationControls';
import { RegistrationProblem } from './RegistrationProblem';

export function RegisterSection({ event, timeZone }: { event: PublicEvent; timeZone: string }) {
  const { t, i18n } = useTranslation('events');
  const me = useQuery(meQuery);
  const family = useQuery({ ...familyQuery, enabled: !!me.data && event.signupOpen });
  const registrations = useQuery({ ...myEventsQuery, enabled: !!me.data });
  const write = useMutation(registerForEventMutation(useQueryClient()));
  const [selected, setSelected] = useState('');
  const fieldId = useId();
  const lang = i18n.resolvedLanguage ?? 'en';
  const at = (instant: string) => formatDateTime(instant, timeZone, lang);

  const active = (registrations.data ?? []).filter(
    (entry) => entry.event.slug === event.slug && entry.registration.status !== 'canceled',
  );
  const people =
    me.data && family.data
      ? bookablePersons(me.data, family.data.dependents).filter(
          (person) => !active.some((entry) => entry.registration.person.id === person.id),
        )
      : [];
  const personId = people.find((person) => person.id === selected)?.id ?? people[0]?.id;
  const refused = conflictReason(write.error);
  const full = refused === 'full' || (refused !== 'not-full' && event.freePlaces === 0);
  const failure =
    me.isError || (me.data && (registrations.isError || (event.signupOpen && family.isError)));

  return (
    <section className="space-y-4 rounded-xl border bg-card p-5 text-card-foreground">
      <h2 className="font-heading text-2xl font-medium">{t('registration')}</h2>
      {active.length > 0 && (
        <ul className="divide-y">
          {active.map((item) => (
            <li key={item.registration.id} className="space-y-2 py-2">
              <p className="flex flex-wrap items-center gap-2">
                {shownName(item.registration.person)}
                <Badge variant="secondary">{t(`states.${item.registration.status}`)}</Badge>
              </p>
              <RegistrationControls item={{ ...item, event }} timeZone={timeZone} />
            </li>
          ))}
        </ul>
      )}
      {event.status === 'published' && (
        <div className="space-y-1 text-sm text-muted-foreground">
          {event.signupOpensAt && <p>{t('opensAt', { at: at(event.signupOpensAt) })}</p>}
          {event.signupClosesAt && <p>{t('closesAt', { at: at(event.signupClosesAt) })}</p>}
          {event.cancelUntil && <p>{t('cancelUntil', { at: at(event.cancelUntil) })}</p>}
        </div>
      )}
      {failure && (
        <LoadError
          message={t('loadRegistrationError')}
          onRetry={() => {
            if (me.isError) void me.refetch();
            if (family.isError) void family.refetch();
            if (registrations.isError) void registrations.refetch();
          }}
        />
      )}
      {!event.signupOpen ? (
        <p>{t(event.status === 'canceled' ? 'canceled' : 'signupClosed')}</p>
      ) : me.data === null ? (
        <Button asChild>
          <Link to="/login">{t('signIn')}</Link>
        </Button>
      ) : registrations.data && personId ? (
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (write.isPending) return;
            write.mutate({ slug: event.slug, personId, waiting: full });
          }}
        >
          <div className="flex min-w-36 flex-col gap-1.5">
            <label htmlFor={fieldId} className="text-sm font-medium">
              {t('participant')}
            </label>
            <select
              id={fieldId}
              value={personId}
              disabled={write.isPending}
              onChange={(e) => {
                setSelected(e.target.value);
                write.reset();
              }}
              className="h-8 rounded-lg border bg-background px-2 text-sm"
            >
              {people.map((person) => (
                <option key={person.id} value={person.id}>
                  {person.name}
                </option>
              ))}
            </select>
          </div>
          <Button requiresOnline type="submit" disabled={write.isPending}>
            <PendingIcon pending={write.isPending} />
            {t(full ? 'join' : 'register')}
          </Button>
        </form>
      ) : null}
      {write.error && <RegistrationProblem error={write.error} />}
    </section>
  );
}
