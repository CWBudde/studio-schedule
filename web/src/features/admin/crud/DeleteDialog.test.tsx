import { mutationOptions } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Toaster } from '@/components/ui/sonner';
import { renderWithQuery } from '@/test/utils';
import { DeleteDialog, type DeleteMutation, type InUseAction } from './DeleteDialog';

type Studio = { id: string; name: string };
const attic = { id: 's1', name: 'Attic' };
const cellar = { id: 's2', name: 'Cellar' };

/**
 * Two studios with a delete button each; `remove` deletes by id, and
 * `retire`, if given, is offered for a studio in use.
 */
function Harness({
  remove,
  retire,
}: {
  remove: (id: string) => Promise<void>;
  retire?: (studio: Studio) => Promise<void>;
}) {
  const [deleting, setDeleting] = useState<Studio | null>(null);
  const mutation: DeleteMutation = () => mutationOptions({ mutationFn: remove });
  const inUseAction = (studio: Studio): InUseAction<Studio> | undefined =>
    retire && {
      label: 'Retire instead',
      pending: 'Retiring…',
      done: `${studio.name} has been retired.`,
      mutation: () => mutationOptions({ mutationFn: retire }),
    };
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
          conflicts: { generated: `${name} comes back by itself.` },
          notFound: `${name} is gone.`,
          deleted: `${name} has been deleted.`,
        })}
        inUseAction={inUseAction}
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

  it('says why for the other 409 reasons of the record', async () => {
    const remove = vi.fn(() =>
      Promise.reject(new ApiError(409, { status: 409, reason: 'generated' })),
    );
    const user = userEvent.setup();
    renderWithQuery(<Harness remove={remove} />);

    await user.click(screen.getByRole('button', { name: 'Delete Attic' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Attic?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await dialog.findByRole('alert')).toHaveTextContent('Attic comes back by itself.');
  });

  it('offers the way out of an in-use delete', async () => {
    const remove = vi.fn(() =>
      Promise.reject(new ApiError(409, { status: 409, reason: 'in-use' })),
    );
    const retire = vi.fn(() => Promise.resolve());
    const user = userEvent.setup();
    render(<Toaster />);
    renderWithQuery(<Harness remove={remove} retire={retire} />);

    await user.click(screen.getByRole('button', { name: 'Delete Attic' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Attic?' }));
    expect(dialog.queryByRole('button', { name: 'Retire instead' })).not.toBeInTheDocument();
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    expect(await dialog.findByRole('alert')).toHaveTextContent('Attic is in use.');
    await user.click(dialog.getByRole('button', { name: 'Retire instead' }));

    expect(await screen.findByText('Attic has been retired.')).toBeInTheDocument();
    expect(retire).toHaveBeenCalledWith(attic, expect.anything());
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('keeps the dialog open and says why when the way out fails', async () => {
    const remove = vi.fn(() =>
      Promise.reject(new ApiError(409, { status: 409, reason: 'in-use' })),
    );
    const retire = vi.fn(() => Promise.reject(new ApiError(404, { status: 404 })));
    const user = userEvent.setup();
    renderWithQuery(<Harness remove={remove} retire={retire} />);

    await user.click(screen.getByRole('button', { name: 'Delete Attic' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Attic?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    await user.click(await dialog.findByRole('button', { name: 'Retire instead' }));

    await waitFor(() => expect(dialog.getByRole('alert')).toHaveTextContent('Attic is gone.'));
    expect(screen.getByRole('alertdialog', { name: 'Delete Attic?' })).toBeInTheDocument();
  });

  it('opens the same record again without the answer of the last time', async () => {
    const remove = vi.fn(() =>
      Promise.reject(new ApiError(409, { status: 409, reason: 'in-use' })),
    );
    const user = userEvent.setup();
    renderWithQuery(<Harness remove={remove} retire={() => Promise.resolve()} />);

    await user.click(screen.getByRole('button', { name: 'Delete Attic' }));
    const first = within(await screen.findByRole('alertdialog', { name: 'Delete Attic?' }));
    await user.click(first.getByRole('button', { name: 'Delete' }));
    expect(await first.findByRole('alert')).toHaveTextContent('Attic is in use.');
    await user.click(first.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: 'Delete Attic' }));
    const again = within(await screen.findByRole('alertdialog', { name: 'Delete Attic?' }));
    expect(again.queryByRole('alert')).not.toBeInTheDocument();
    expect(again.queryByRole('button', { name: 'Retire instead' })).not.toBeInTheDocument();
  });

  it('says why a delete tried again after a failed way out is refused', async () => {
    const remove = vi.fn(() =>
      Promise.reject(new ApiError(409, { status: 409, reason: 'in-use' })),
    );
    const retire = vi.fn(() => Promise.reject(new ApiError(404, { status: 404 })));
    const user = userEvent.setup();
    renderWithQuery(<Harness remove={remove} retire={retire} />);

    await user.click(screen.getByRole('button', { name: 'Delete Attic' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Attic?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    await user.click(await dialog.findByRole('button', { name: 'Retire instead' }));
    await waitFor(() => expect(dialog.getByRole('alert')).toHaveTextContent('Attic is gone.'));

    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(dialog.getByRole('alert')).toHaveTextContent('Attic is in use.'));
    expect(remove).toHaveBeenCalledTimes(2);
  });

  it('does not call the way out a delete while it runs', async () => {
    const remove = vi.fn(() =>
      Promise.reject(new ApiError(409, { status: 409, reason: 'in-use' })),
    );
    const retire = vi.fn(() => new Promise<void>(() => {}));
    const user = userEvent.setup();
    renderWithQuery(<Harness remove={remove} retire={retire} />);

    await user.click(screen.getByRole('button', { name: 'Delete Attic' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Attic?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    await user.click(await dialog.findByRole('button', { name: 'Retire instead' }));

    expect(await dialog.findByRole('button', { name: 'Retiring…' })).toBeDisabled();
    expect(dialog.getByRole('button', { name: 'Delete' })).toBeDisabled();
    expect(dialog.queryByRole('button', { name: 'Deleting…' })).not.toBeInTheDocument();
  });
});
