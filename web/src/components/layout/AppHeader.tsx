import { BookmarkCheck, CalendarDays, Presentation, Shapes } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useMe } from '@/api/useMe';
import { DemoMailbox } from '@/features/demo/DemoMailbox';
import { canTeach } from '@/features/teach/access';
import type { NavItem, ShellProps, ShellSession } from '@/themes/slots';
import { AccountMenu } from './AccountMenu';
import {
  useApplyBranding,
  usePublicSettings,
  useSchoolBackground,
  useSchoolLogoUrl,
} from './branding';
import { LanguageSwitcher } from './LanguageSwitcher';
import { ResetDemo } from './ResetDemo';
import { useSignOut } from './useSignOut';

/**
 * The header of the Shell slot: the school's name and logo, the main
 * navigation for the caller's role, and the tools. It also applies the
 * school's branding (title, accent, language, background) to the page.
 */
export function useShellHeader(): ShellProps['header'] {
  const { t } = useTranslation();
  const me = useMe();
  const settings = usePublicSettings().data;
  useApplyBranding(settings);
  useSchoolBackground(settings?.hasBackground);
  const logoUrl = useSchoolLogoUrl(settings?.hasLogo);
  const logout = useSignOut();

  const nav: NavItem[] = [
    { to: '/schedule', label: t('nav.schedule'), icon: CalendarDays },
    { to: '/classes', label: t('nav.classes'), icon: Shapes },
  ];
  if (me.data) nav.push({ to: '/my-lessons', label: t('nav.myLessons'), icon: BookmarkCheck });
  if (canTeach(me.data?.role))
    nav.push({ to: '/teach', label: t('nav.teach'), icon: Presentation });

  return {
    schoolName: settings?.schoolName,
    logoUrl,
    nav,
    tools: (
      <>
        <DemoMailbox />
        <ResetDemo />
      </>
    ),
    account: <AccountMenu />,
    language: <LanguageSwitcher />,
    session: sessionOf(me, logout),
  };
}

function sessionOf(
  me: ReturnType<typeof useMe>,
  logout: ReturnType<typeof useSignOut>,
): ShellSession {
  // As the account menu: neither while unknown.
  if (me.isPending) return { state: 'unknown' };
  if (!me.data) return { state: 'signedOut' };
  return { state: 'signedIn', signOut: logout.signOut, signingOut: logout.pending };
}
