import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { TextField } from '@/features/auth/TextField';
import { PasswordInput } from './PasswordInput';
import { PasswordStrengthMeter } from './PasswordStrengthMeter';

describe('PasswordInput', () => {
  it('shows and hides the password with a toggle button', async () => {
    const user = userEvent.setup();
    render(<PasswordInput aria-label="Password" autoComplete="current-password" />);

    const input = screen.getByLabelText('Password');
    expect(input).toHaveAttribute('type', 'password');
    expect(input).toHaveAttribute('autocomplete', 'current-password');
    await user.type(input, 'secret-words');

    const show = screen.getByRole('button', { name: 'Show password' });
    expect(show).toHaveAttribute('aria-pressed', 'false');
    expect(show).toHaveAttribute('type', 'button');
    await user.click(show);

    expect(input).toHaveAttribute('type', 'text');
    expect(input).toHaveValue('secret-words');
    const hide = screen.getByRole('button', { name: 'Hide password' });
    expect(hide).toHaveAttribute('aria-pressed', 'true');

    await user.click(hide);
    expect(input).toHaveAttribute('type', 'password');
    expect(screen.getByRole('button', { name: 'Show password' })).toBeInTheDocument();
  });

  it('is what TextField renders for a password, and only its label names the input', () => {
    render(<TextField label="Password" type="password" hint="At least 8 characters." />);

    // The toggle's name is no label, so the label query stays unambiguous.
    const input = screen.getByLabelText('Password');
    expect(input).toHaveAttribute('type', 'password');
    expect(input).toHaveAccessibleDescription('At least 8 characters.');
    expect(screen.getByRole('button', { name: 'Show password' })).toBeInTheDocument();
  });
});

describe('PasswordStrengthMeter', () => {
  it('names the strength in text and shows nothing while empty', () => {
    const { rerender } = render(<PasswordStrengthMeter password="" />);
    expect(screen.queryByText(/Password strength/)).not.toBeInTheDocument();

    rerender(<PasswordStrengthMeter password="abcdefgh" />);
    expect(screen.getByText('Password strength: weak')).toBeInTheDocument();

    rerender(<PasswordStrengthMeter password="correct horse battery" />);
    expect(screen.getByText('Password strength: strong')).toBeInTheDocument();
  });
});
