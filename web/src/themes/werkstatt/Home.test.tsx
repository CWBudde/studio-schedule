import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import type { ScheduleLesson } from '@/api/client';
import { lessonBallet, lessonWorkshop } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import type { HomeMyLessons, HomeProps, HomeStatus } from '../slots';
import { Home } from './Home';

const okStatus: HomeStatus = {
  health: 'ok',
  timezone: 'Europe/Berlin',
  settingsPending: false,
  failed: false,
  onRetry: vi.fn(),
};

/** A stand-in for a connected lesson card. */
const renderLesson = (lesson: ScheduleLesson) => (
  <p data-testid="card">{lesson.courseType?.name}</p>
);

const twoDays: HomeMyLessons = {
  state: 'ready',
  renderLesson,
  days: [
    { date: '2026-10-26', tone: 'tomorrow', headingId: 'd-26', lessons: [lessonBallet] },
    { date: '2026-11-07', tone: 'later', headingId: 'd-07', lessons: [lessonWorkshop] },
  ],
};

function renderHome(over: Partial<HomeProps> = {}) {
  const renderNextLessons = vi.fn((className?: string) => (
    <section aria-label="Next lessons" className={className} />
  ));
  renderInRouter(
    <Home
      schoolName="Tanzwerk Nord"
      announcements={<p>An announcement</p>}
      renderNextLessons={renderNextLessons}
      status={okStatus}
      myLessons={twoDays}
      {...over}
    />,
  );
  return { renderNextLessons };
}

describe('Werkstatt Home', () => {
  it('shows the school, its infos and the member’s lessons by day', async () => {
    const { renderNextLessons } = renderHome();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Tanzwerk Nord' }),
    ).toBeInTheDocument();
    const info = within(screen.getByRole('region', { name: 'Infos' }));
    expect(info.getByText(/Plan classes, publish the timetable/)).toBeInTheDocument();
    expect(info.getByText('An announcement')).toBeInTheDocument();
    const status = within(info.getByRole('region', { name: 'System status' }));
    expect(status.getByText('OK')).toBeInTheDocument();
    expect(status.getByText('Europe/Berlin')).toBeInTheDocument();

    const mine = within(screen.getByRole('region', { name: 'My schedule' }));
    // Each day is a region under its heading (the DayHeading slot), with that day's cards.
    const monday = within(mine.getByRole('region', { name: /Monday, October 26/ }));
    expect(monday.getByTestId('card')).toHaveTextContent('Ballet');
    const saturday = within(mine.getByRole('region', { name: /Saturday, November 7/ }));
    expect(saturday.getByTestId('card')).toHaveTextContent('Jazz');
    expect(mine.getByRole('link', { name: /Full schedule/ })).toHaveAttribute('href', '/schedule');
    // Signed in, the teaser gives way to the member's own lessons.
    expect(renderNextLessons).not.toHaveBeenCalled();
  });

  it('shows the next lessons instead when signed out', async () => {
    const { renderNextLessons } = renderHome({ myLessons: { state: 'signedOut' } });

    expect(await screen.findByRole('region', { name: 'Next lessons' })).toBeInTheDocument();
    expect(renderNextLessons).toHaveBeenCalledWith('min-w-0');
    expect(screen.queryByRole('heading', { name: 'My schedule' })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 2, name: 'Infos' })).toBeInTheDocument();
  });

  it('shows neither while the account is not known', async () => {
    const { renderNextLessons } = renderHome({ myLessons: { state: 'unknown' } });

    await screen.findByRole('heading', { level: 2, name: 'Infos' });
    expect(renderNextLessons).not.toHaveBeenCalled();
    expect(screen.queryByRole('heading', { name: 'My schedule' })).not.toBeInTheDocument();
  });

  it('says so while the member’s lessons load', async () => {
    renderHome({ myLessons: { state: 'loading' } });
    expect(await screen.findByRole('status')).toHaveTextContent('Loading your lessons…');
  });

  it('says so when nothing is booked', async () => {
    renderHome({ myLessons: { state: 'ready', days: [], renderLesson } });
    expect(
      await screen.findByText('You are not booked for any lesson in the next 7 days.'),
    ).toBeInTheDocument();
    expect(screen.queryByTestId('card')).not.toBeInTheDocument();
  });

  it('retries the member’s lessons after an error', async () => {
    const onRetry = vi.fn();
    renderHome({ myLessons: { state: 'error', onRetry } });

    const mine = within(await screen.findByRole('region', { name: 'My schedule' }));
    expect(mine.getByText('Could not load your lessons.')).toBeInTheDocument();
    await userEvent.click(mine.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('reports a failed backend with a retry, and the version where the API names it', async () => {
    const onRetry = vi.fn();
    renderHome({
      schoolName: undefined,
      status: { ...okStatus, health: 'error', failed: true, version: '0.1.0-test', onRetry },
    });

    expect(await screen.findByRole('heading', { level: 1, name: 'Welcome' })).toBeInTheDocument();
    const status = within(screen.getByRole('region', { name: 'System status' }));
    expect(status.getByText('Error')).toBeInTheDocument();
    expect(status.getByText('0.1.0-test')).toBeInTheDocument();
    await userEvent.click(status.getByRole('button', { name: 'Retry' }));
    expect(onRetry).toHaveBeenCalledOnce();
  });
});
