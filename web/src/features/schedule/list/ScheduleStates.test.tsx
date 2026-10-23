import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { ScheduleError, ScheduleLoading } from './ScheduleStates';

describe('ScheduleLoading', () => {
  it('announces loading as a status', () => {
    render(<ScheduleLoading />);

    expect(screen.getByRole('status')).toHaveTextContent('Loading schedule…');
  });
});

describe('ScheduleError', () => {
  it('shows an alert and retries on request', async () => {
    const user = userEvent.setup();
    const onRetry = vi.fn();
    render(<ScheduleError onRetry={onRetry} />);

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load the schedule.');
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    render(<ScheduleError onRetry={() => undefined} />);

    expect(screen.getByRole('button', { name: 'Erneut versuchen' })).toBeInTheDocument();
  });
});
