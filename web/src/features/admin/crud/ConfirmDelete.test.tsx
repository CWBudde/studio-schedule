import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/queries';
import { ConfirmDelete } from './ConfirmDelete';

function renderDialog(props: Partial<Parameters<typeof ConfirmDelete>[0]> = {}) {
  const onConfirm = vi.fn();
  const onCancel = vi.fn();
  render(
    <ConfirmDelete
      open
      title="Delete Attic?"
      description="The room is deleted for good."
      pending={false}
      error={null}
      conflicts={{ 'in-use': 'Attic is used by classes, so it cannot be deleted.' }}
      onConfirm={onConfirm}
      onCancel={onCancel}
      {...props}
    />,
  );
  return { onConfirm, onCancel, dialog: within(screen.getByRole('alertdialog')) };
}

describe('ConfirmDelete', () => {
  it('asks before deleting and deletes on confirmation', async () => {
    const user = userEvent.setup();
    const { onConfirm, dialog } = renderDialog();

    expect(screen.getByRole('alertdialog', { name: 'Delete Attic?' })).toHaveAccessibleDescription(
      'The room is deleted for good.',
    );
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it('cancels', async () => {
    const user = userEvent.setup();
    const { onConfirm, onCancel, dialog } = renderDialog();

    await user.click(dialog.getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalledOnce();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('says why a delete was refused and stays usable', () => {
    const { dialog } = renderDialog({
      error: new ApiError(409, { status: 409, reason: 'in-use', detail: 'room in use' }),
    });

    expect(dialog.getByRole('alert')).toHaveTextContent(
      'Attic is used by classes, so it cannot be deleted.',
    );
    expect(dialog.queryByText('room in use')).not.toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Delete' })).toBeEnabled();
    expect(dialog.getByRole('button', { name: 'Cancel' })).toBeEnabled();
  });

  it('shows a general error for other failures', () => {
    const { dialog } = renderDialog({ error: new ApiError(500, { detail: 'boom' }) });

    expect(dialog.getByRole('alert')).toHaveTextContent('Something went wrong. Please try again.');
  });

  it('shows that it is deleting', () => {
    const { dialog } = renderDialog({ pending: true });

    expect(dialog.getByRole('button', { name: 'Deleting…' })).toBeDisabled();
  });
});
