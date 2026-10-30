import { useQuery } from '@tanstack/react-query';
import type { Me } from '@/api/client';
import { meQuery } from '@/api/queries';
import { CalendarFeedsCard } from '@/features/calendar-feeds/CalendarFeedsCard';
import { PushCard } from '@/features/push/PushCard';
import { useSlot } from '@/themes/useSlot';
import type { ProfileProps } from '@/themes/slots';
import { ChangeEmailCard } from './ChangeEmailCard';
import { ChangePasswordCard } from './ChangePasswordCard';
import { DeleteAccountCard } from './DeleteAccountCard';
import { DependentsCard } from './DependentsCard';
import { MyDataCard } from './MyDataCard';
import { ProfileCard } from './ProfileCard';

/**
 * The signed-in person's account settings (/account): their person data and
 * mail language, their children, their calendar feeds, notifications on
 * this device, the download of
 * their data, the password, the email address, and deleting the account.
 * Signed out, the page asks to sign in. While an admin is signed in as
 * someone else, the person data and children stay editable (for support),
 * but the API refuses credential changes, which the page explains instead.
 */
export function AccountPage() {
  const Profile = useSlot('Profile');
  const me = useQuery(meQuery);

  const props: ProfileProps = me.isPending
    ? { state: 'loading' }
    : me.isError
      ? { state: 'error', error: me.error }
      : !me.data
        ? { state: 'signedOut' }
        : ready(me.data);
  // The slot changes only with the theme, and then remounting what it renders is intended.
  // eslint-disable-next-line react-hooks/static-components
  return <Profile {...props} />;
}

/** The sections of a signed-in account. */
function ready(me: Me): ProfileProps {
  // An admin signed in as the account would get its notifications, and the
  // API refuses them its credentials.
  const impersonating = Boolean(me.impersonator);
  return {
    state: 'ready',
    me,
    sections: {
      profile: <ProfileCard me={me} />,
      dependents: <DependentsCard />,
      calendarFeeds: <CalendarFeedsCard />,
      push: impersonating ? undefined : <PushCard accountId={me.accountId} />,
      myData: <MyDataCard />,
      password: impersonating ? undefined : <ChangePasswordCard />,
      email: impersonating ? undefined : <ChangeEmailCard me={me} />,
      deleteAccount: impersonating ? undefined : <DeleteAccountCard />,
    },
    impersonating: impersonating ? { name: `${me.givenName} ${me.familyName}` } : undefined,
  };
}
