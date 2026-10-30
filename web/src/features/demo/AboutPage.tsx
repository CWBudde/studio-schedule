import { Link } from '@tanstack/react-router';
import type { MouseEvent } from 'react';
import {
  ArrowRight,
  BookOpen,
  CalendarDays,
  Cpu,
  Database,
  ExternalLink,
  FileText,
  Inbox,
  LayoutGrid,
  ListChecks,
  Monitor,
  Palette,
  RotateCcw,
  Server,
  Smartphone,
  Ticket,
  Users,
  type LucideIcon,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { BrandMark } from '@/components/layout/BrandMark';
import { LanguageSwitcher } from '@/components/layout/LanguageSwitcher';
import { Ornament } from '@/components/layout/Ornament';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { switchSchool } from '@/api/transport';
import { repositoryUrl } from '@/lib/env';
import { markDemoEntered } from './entered';
import { type DemoSchool, demoSchool } from './school';

const features = [
  { key: 'schedule', icon: CalendarDays },
  { key: 'booking', icon: ListChecks },
  { key: 'family', icon: Users },
  { key: 'passes', icon: Ticket },
  { key: 'teaching', icon: BookOpen },
  { key: 'office', icon: LayoutGrid },
  { key: 'devices', icon: Smartphone },
] as const satisfies readonly { key: string; icon: LucideIcon }[];

const howItWorks = [
  { key: 'wasm', icon: Cpu },
  { key: 'data', icon: Database },
  { key: 'saved', icon: RotateCcw },
  { key: 'schools', icon: Palette },
  { key: 'mail', icon: Inbox },
  { key: 'tab', icon: Monitor },
  { key: 'receipts', icon: FileText },
] as const satisfies readonly { key: string; icon: LucideIcon }[];

const roles = ['visitor', 'member', 'instructor', 'office', 'admin'] as const;

const docsUrl = (file: string) => `${repositoryUrl}/blob/main/docs/${file}`;

const sectionTitle = 'font-heading text-2xl font-medium sm:text-3xl';

/**
 * The web demo's entrance (route `/about`, demo build only): what the app is,
 * how the demo works and what to try. It is rendered without the app around
 * it and calls no API, so the backend starts only once the visitor enters.
 */
export function AboutPage() {
  const { t } = useTranslation('about');
  const { t: tCommon } = useTranslation();

  const links = [
    { label: t('more.demoDocs'), href: docsUrl('demo-mode.md'), icon: Cpu },
    { label: t('more.operations'), href: docsUrl('operations.md'), icon: Server },
    { label: t('more.source'), href: repositoryUrl, icon: BookOpen },
  ];

  return (
    <div className="flex min-h-svh flex-col">
      <header className="border-b">
        <div className="mx-auto flex h-16 max-w-4xl items-center gap-1 px-4">
          <span className="mr-auto flex items-center gap-2.5 font-heading text-xl font-semibold tracking-tight">
            <BrandMark className="size-9" />
            <span className="hidden sm:inline">{tCommon('app.name')}</span>
          </span>
          <LanguageSwitcher />
          <Button asChild variant="ghost" size="sm">
            <a href={repositoryUrl} target="_blank" rel="noopener noreferrer">
              <ExternalLink aria-hidden="true" />
              {t('source')}
            </a>
          </Button>
        </div>
      </header>

      <main id="main" className="mx-auto w-full max-w-4xl flex-1 space-y-16 px-4 py-10 sm:py-14">
        <section aria-labelledby="about-title" className="mx-auto max-w-2xl text-center">
          <p className="eyebrow text-primary">{t('hero.eyebrow')}</p>
          <h1
            id="about-title"
            className="mt-4 font-heading text-4xl leading-[1.05] font-medium tracking-tight text-balance sm:text-6xl"
          >
            {tCommon('app.name')}
          </h1>
          <Ornament className="mx-auto mt-7 max-w-56" />
          <p className="mx-auto mt-6 max-w-prose text-pretty sm:text-lg">{t('hero.intro')}</p>
          <p className="mx-auto mt-3 max-w-prose text-pretty text-muted-foreground">
            {t('hero.demo')}
          </p>
          <div className="mt-8 flex flex-col items-center justify-center gap-3 sm:flex-row sm:flex-wrap">
            <OpenDemo />
            <OpenDemo school="werkstatt" />
            <Button asChild size="lg" variant="outline">
              <Link to="/login">{t('hero.signIn')}</Link>
            </Button>
          </div>
        </section>

        <section aria-labelledby="about-features" className="space-y-6">
          <h2 id="about-features" className={sectionTitle}>
            {t('features.title')}
          </h2>
          <ul className="grid gap-x-8 gap-y-6 sm:grid-cols-2">
            {features.map(({ key, icon: FeatureIcon }) => (
              <li key={key} className="flex gap-3">
                <FeatureIcon className="mt-1 size-5 shrink-0 text-primary" aria-hidden="true" />
                <div>
                  <h3 className="font-medium">{t(`features.${key}.title`)}</h3>
                  <p className="text-sm text-pretty text-muted-foreground">
                    {t(`features.${key}.text`)}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        </section>

        <section aria-labelledby="about-how" className="space-y-6">
          <div className="space-y-2">
            <h2 id="about-how" className={sectionTitle}>
              {t('how.title')}
            </h2>
            <p className="text-muted-foreground">{t('how.intro')}</p>
          </div>
          <Card>
            <CardContent>
              <ul className="space-y-4">
                {howItWorks.map(({ key, icon: StepIcon }) => (
                  <li key={key} className="flex gap-3">
                    <StepIcon className="mt-0.5 size-5 shrink-0 text-primary" aria-hidden="true" />
                    <p className="text-pretty">{t(`how.${key}`)}</p>
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>
          <p className="text-sm text-pretty text-muted-foreground">{t('how.same')}</p>
        </section>

        <section aria-labelledby="about-try" className="space-y-6">
          <div className="space-y-2">
            <h2 id="about-try" className={sectionTitle}>
              {t('try.title')}
            </h2>
            <p className="text-muted-foreground">
              {t('try.intro')}{' '}
              <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-sm whitespace-nowrap text-foreground">
                demo-password
              </code>
              .
            </p>
          </div>
          <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {roles.map((role) => (
              <li key={role}>
                <Card className="h-full gap-2">
                  <CardHeader>
                    <CardTitle>
                      <h3>{t(`try.${role}.title`)}</h3>
                    </CardTitle>
                  </CardHeader>
                  <CardContent>
                    <p className="text-sm text-pretty text-muted-foreground">
                      {t(`try.${role}.text`)}
                    </p>
                  </CardContent>
                </Card>
              </li>
            ))}
          </ul>
          <div className="flex justify-center">
            <OpenDemo />
          </div>
        </section>

        <section aria-labelledby="about-more" className="space-y-4">
          <h2 id="about-more" className={sectionTitle}>
            {t('more.title')}
          </h2>
          <ul className="space-y-2">
            {links.map(({ label, href, icon: LinkIcon }) => (
              <li key={href}>
                <a
                  href={href}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1.5 rounded-sm font-medium underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  <LinkIcon className="size-4 text-primary" aria-hidden="true" />
                  {label}
                  <ExternalLink className="size-3.5 text-muted-foreground" aria-hidden="true" />
                </a>
              </li>
            ))}
          </ul>
        </section>
      </main>
    </div>
  );
}

/**
 * Enters the demo with a school (docs/demo-mode.md, "Two demo schools"): the
 * chosen one in place, another one by reloading the page, so that a backend
 * already running with the chosen school's data does not keep serving.
 */
function OpenDemo({ school = 'programme' }: { school?: DemoSchool }) {
  const { t } = useTranslation('about');
  const werkstatt = school === 'werkstatt';
  const open = (event: MouseEvent) => {
    if (school === demoSchool()) {
      markDemoEntered();
    } else {
      event.preventDefault();
      void switchSchool(school);
    }
  };
  return (
    <Button asChild size="lg" variant={werkstatt ? 'outline' : 'default'}>
      {/* Marked before the home page's redirect back to here is checked; a
          preload on hover would meet that redirect before the click. */}
      <Link to="/" preload={false} onClick={open}>
        {werkstatt && <WerkstattPreview />}
        {t(werkstatt ? 'hero.openWerkstatt' : 'hero.open')}
        <ArrowRight aria-hidden="true" />
      </Link>
    </Button>
  );
}

/** The Werkstatt look's colours, plum and mint: decoration without text. */
function WerkstattPreview() {
  return (
    <span
      data-slot="theme-preview"
      aria-hidden="true"
      className="flex h-4 w-6 shrink-0 overflow-hidden rounded-[3px] ring-1 ring-black/10"
    >
      <span className="flex-1 bg-[#9c215b]" />
      <span className="w-2 bg-[#74b391]" />
    </span>
  );
}
