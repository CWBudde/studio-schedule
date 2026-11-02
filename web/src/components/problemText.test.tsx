import { render, renderHook, screen } from '@testing-library/react';
import { Megaphone } from 'lucide-react';
import { describe, expect, it } from 'vitest';
import { ApiError } from '@/api/queries';
import { PendingIcon } from './PendingIcon';
import { ProblemAlert } from './ProblemAlert';
import { useProblemText } from './problemText';

const conflict = (reason: string) =>
  new ApiError(409, { status: 409, title: 'Conflict', detail: 'conflict', reason });

describe('useProblemText', () => {
  const texts = {
    conflicts: { 'mail-limit': 'Three emailed announcements a day.' },
    messages: { 404: 'It is gone.', 409: 'Someone else changed it.' },
  };

  it('says what ProblemAlert says, as text for a toast', () => {
    const { result } = renderHook(() => useProblemText());
    const text = result.current;

    // The sentence of the 409's reason before the status's.
    expect(text(conflict('mail-limit'), texts)).toBe('Three emailed announcements a day.');
    // A reason without a sentence falls back to the status's message.
    expect(text(conflict('expired'), texts)).toBe('Someone else changed it.');
    expect(text(new ApiError(404, {}), texts)).toBe('It is gone.');
    // A status without a message, a 429 and an error that is no API answer.
    expect(text(new ApiError(500, {}), texts)).toBe('Something went wrong. Please try again.');
    expect(text(new ApiError(429, {}))).toBe('Too many attempts');
    expect(text(new Error('offline'))).toBe('Something went wrong. Please try again.');
  });

  it('matches the title of ProblemAlert', () => {
    render(<ProblemAlert error={conflict('mail-limit')} {...texts} />);
    expect(screen.getByRole('alert')).toHaveTextContent('Three emailed announcements a day.');
  });
});

describe('PendingIcon', () => {
  it('shows a spinner while pending and the icon otherwise', () => {
    const { container, rerender } = render(
      <PendingIcon pending={false} icon={<Megaphone data-testid="icon" />} />,
    );
    expect(screen.getByTestId('icon')).toBeInTheDocument();
    expect(container.querySelector('[data-slot="spinner"]')).toBeNull();

    rerender(<PendingIcon pending icon={<Megaphone data-testid="icon" />} />);
    expect(screen.queryByTestId('icon')).not.toBeInTheDocument();
    const spinner = container.querySelector('[data-slot="spinner"]');
    expect(spinner).toHaveAttribute('aria-hidden', 'true');
  });
});
