import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  family,
  meMember,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  problem,
  publicSettings,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { AccountPage } from './AccountPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const lina = family.dependents[0]!;
const max = family.dependents[1]!;

const signedInMember = {
  '/api/v1/demo/mailbox': { status: 404, body: undefined },
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/auth/me': { body: meMember },
  '/api/v1/family': { body: family },
};

function renderAccount() {
  render(<Toaster />);
  return renderInRouter(<AccountPage />, { at: '/account' });
}

const dependentsCard = async () =>
  within(await screen.findByRole('region', { name: 'Your children' }));

/** The list entry of the child named `name`. */
async function child(name: string) {
  const card = await dependentsCard();
  const items = await card.findAllByRole('listitem');
  const item = items.find((li) => within(li).queryByText(name));
  if (!item) throw new Error(`no list entry for ${name}`);
  return within(item);
}

describe('DependentsCard', () => {
  it('lists the children with their birth date and other guardians', async () => {
    mockApiGet(signedInMember);

    renderAccount();
    const card = await dependentsCard();

    expect(await card.findAllByRole('listitem')).toHaveLength(2);
    const first = await child('Lina Example');
    expect(first.getByText('Born March 14, 2017')).toBeInTheDocument();
    expect(first.getByText('Also in the family of Paul Example')).toBeInTheDocument();
    const second = await child('Max Example');
    expect(second.queryByText(/^Born/)).not.toBeInTheDocument();
    expect(second.queryByText(/^Also in the family of/)).not.toBeInTheDocument();
  });

  it('formats the birth date in the UI language', async () => {
    await i18n.changeLanguage('de');
    mockApiGet(signedInMember);

    renderAccount();

    const first = within(
      (await screen.findAllByRole('listitem')).find((li) =>
        within(li).queryByText('Lina Example'),
      )!,
    );
    expect(first.getByText('Geboren am 14. März 2017')).toBeInTheDocument();
    expect(first.getByText('Auch in der Familie von Paul Example')).toBeInTheDocument();
  });

  it('says when there are no children yet', async () => {
    mockApiGet({ ...signedInMember, '/api/v1/family': { body: { ...family, dependents: [] } } });

    renderAccount();
    const card = await dependentsCard();

    expect(await card.findByText('You have not added any children yet.')).toBeInTheDocument();
    expect(card.queryByRole('listitem')).not.toBeInTheDocument();
  });

  it('adds a child in a sheet', async () => {
    mockApiGet(signedInMember);
    const post = mockApiPost({
      '/api/v1/family/dependents': {
        status: 201,
        body: { id: lina.id, givenName: 'Ida', familyName: 'Example', birthDate: '2021-01-05' },
      },
    });
    const user = userEvent.setup();

    renderAccount();
    const card = await dependentsCard();
    await user.click(await card.findByRole('button', { name: 'Add child' }));

    const sheet = within(await screen.findByRole('dialog', { name: 'Add a child' }));
    // The family name is the guardian's, as a start.
    expect(sheet.getByLabelText('Last name')).toHaveValue('Example');
    await user.type(sheet.getByLabelText('First name'), 'Ida');
    await user.type(sheet.getByLabelText('Birth date (optional)'), '2021-01-05');
    await user.click(sheet.getByRole('button', { name: 'Add child' }));

    expect(await screen.findByText('Ida Example has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/family/dependents', {
      body: { givenName: 'Ida', familyName: 'Example', birthDate: '2021-01-05' },
    });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('checks the fields of a new child before sending', async () => {
    mockApiGet(signedInMember);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderAccount();
    await user.click(await (await dependentsCard()).findByRole('button', { name: 'Add child' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a child' }));
    await user.type(sheet.getByLabelText('Birth date (optional)'), '2999-01-01');
    await user.click(sheet.getByRole('button', { name: 'Add child' }));

    expect(await sheet.findByText('Please fill in this field.')).toBeInTheDocument();
    expect(sheet.getByText('The birth date cannot be in the future.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('edits a child in a sheet', async () => {
    mockApiGet(signedInMember);
    const put = mockApiPut({
      '/api/v1/family/dependents/{personId}': { body: { ...lina, givenName: 'Linnea' } },
    });
    const user = userEvent.setup();

    renderAccount();
    await user.click(
      (await child('Lina Example')).getByRole('button', { name: 'Edit Lina Example' }),
    );

    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Lina Example' }));
    expect(sheet.getByLabelText('Birth date (optional)')).toHaveValue('2017-03-14');
    await user.clear(sheet.getByLabelText('First name'));
    await user.type(sheet.getByLabelText('First name'), 'Linnea');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('Linnea Example has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/family/dependents/{personId}', {
      params: { path: { personId: lina.id } },
      body: { givenName: 'Linnea', familyName: 'Example', birthDate: '2017-03-14' },
    });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('says when the child is no longer in the family', async () => {
    mockApiGet(signedInMember);
    mockApiPut({ '/api/v1/family/dependents/{personId}': problem(404, 'resource not found') });
    const user = userEvent.setup();

    renderAccount();
    await user.click(
      (await child('Max Example')).getByRole('button', { name: 'Edit Max Example' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Max Example' }));
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByText('This child is no longer in your family.')).toBeInTheDocument();
  });

  describe('remove', () => {
    it('only unlinks a child that has another guardian, after asking', async () => {
      mockApiGet(signedInMember);
      const del = mockApiDelete({ '/api/v1/family/dependents/{personId}': { status: 204 } });
      const user = userEvent.setup();

      renderAccount();
      const entry = await child('Lina Example');
      await user.click(entry.getByRole('button', { name: 'Remove Lina Example' }));

      const confirm = within(
        entry.getByRole('group', { name: 'Remove Lina Example from your family?' }),
      );
      expect(
        confirm.getByText(
          'Lina stays registered with Paul Example and is only removed from your family.',
        ),
      ).toBeInTheDocument();
      expect(del).not.toHaveBeenCalled();
      await user.click(confirm.getByRole('button', { name: 'Yes, remove' }));

      expect(
        await screen.findByText('Lina Example has been removed from your family.'),
      ).toBeInTheDocument();
      expect(del).toHaveBeenCalledWith('/api/v1/family/dependents/{personId}', {
        params: { path: { personId: lina.id } },
      });
    });

    it('says that a child without another guardian is deleted', async () => {
      mockApiGet(signedInMember);
      mockApiDelete({});
      const user = userEvent.setup();

      renderAccount();
      const entry = await child('Max Example');
      await user.click(entry.getByRole('button', { name: 'Remove Max Example' }));

      expect(
        entry.getByText('Max has no other guardian, so the details of Max are deleted.'),
      ).toBeInTheDocument();
    });

    it('explains a child that has bookings or class memberships', async () => {
      mockApiGet(signedInMember);
      mockApiDelete({
        '/api/v1/family/dependents/{personId}': problem(
          409,
          'the dependent has bookings or class memberships',
        ),
      });
      const user = userEvent.setup();

      renderAccount();
      const entry = await child('Max Example');
      await user.click(entry.getByRole('button', { name: 'Remove Max Example' }));
      await user.click(entry.getByRole('button', { name: 'Yes, remove' }));

      expect(
        await entry.findByText(
          'Max Example has bookings or class memberships and cannot be removed. Please contact the office.',
        ),
      ).toBeInTheDocument();
      expect(entry.getByText('Max Example')).toBeInTheDocument();
    });

    it('keeps the child and returns to its remove button when canceled', async () => {
      mockApiGet(signedInMember);
      const del = mockApiDelete({});
      const user = userEvent.setup();

      renderAccount();
      const entry = await child('Lina Example');
      await user.click(entry.getByRole('button', { name: 'Remove Lina Example' }));
      expect(entry.getByRole('button', { name: 'Keep' })).toHaveFocus();
      await user.click(entry.getByRole('button', { name: 'Keep' }));

      expect(entry.queryByRole('group')).not.toBeInTheDocument();
      expect(entry.getByRole('button', { name: 'Remove Lina Example' })).toHaveFocus();
      expect(del).not.toHaveBeenCalled();
    });
  });
});
