import { mutationOptions } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Toaster } from '@/components/ui/sonner';
import { renderWithQuery } from '@/test/utils';
import { DeleteDialog, type DeleteMutation } from './DeleteDialog';

type Studio = { id: string; name: string };
const attic = { id: 's1', name: 'Attic' };
const cellar = { id: 's2', name: 'Cellar' };

/** Two studios with a delete button each; `remove` deletes by id. */
function Harness({ remove }: { remove: (id: string) => Promise<void> }) {
  const [deleting, setDeleting] = useState<Studio | null>(null);
  const mutation: DeleteMutation = () => mutationOptions({ mutationFn: remove });
  return (
    <>
      <Button onClick={() => setDeleting(attic)}>Delete Attic</Button>
      <Button onClick={() => setDeleting(cellar)}>Delete Cellar</Button>
      <DeleteDialog
        target={deleting}
        id={(s) => s.id}
        mutation={mutation}
        texts={({ name }) => ({
          title: `Delete ${name}?`,
          description: 'It is deleted for good.',
          inUse: `${name} is in use.`,
          notFound: `${name} is gone.`,
          deleted: `${name} has been deleted.`,
        })}
        onClose={() => setDeleting(null)}
      />
    </>
  );
}

describe('DeleteDialog', () => {
  it('deletes the record by its id and says so', async () => {
    const remove = vi.fn(() => Promise.resolve());
    const user = userEvent.setup();
    render(<Toaster />);
    renderWithQuery(<Harness remove={remove} />);

    await user.click(screen.getByRole('button', { name: 'Delete Cellar' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Cellar?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await screen.findByText('Cellar has been deleted.')).toBeInTheDocument();
    expect(remove).toHaveBeenCalledWith('s2', expect.anything());
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('starts without the error of the record before', async () => {
    const remove = vi.fn((id: string) =>
      id === 's1'
        ? Promise.reject(new ApiError(409, { status: 409, reason: 'in-use' }))
        : Promise.resolve(),
    );
    const user = userEvent.setup();
    renderWithQuery(<Harness remove={remove} />);

    await user.click(screen.getByRole('button', { name: 'Delete Attic' }));
    const first = within(await screen.findByRole('alertdialog', { name: 'Delete Attic?' }));
    await user.click(first.getByRole('button', { name: 'Delete' }));
    expect(await first.findByRole('alert')).toHaveTextContent('Attic is in use.');
    await user.click(first.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: 'Delete Cellar' }));
    const second = within(await screen.findByRole('alertdialog', { name: 'Delete Cellar?' }));
    expect(second.queryByRole('alert')).not.toBeInTheDocument();
  });
});
