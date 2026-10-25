import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { PersonMatch } from '@/api/queries';
import { ids, mockApiPost, persons, problem, searchQueryOf } from '@/test/mockApi';
import type { MockRequest } from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { PersonField } from './PersonField';

afterEach(() => {
  vi.restoreAllMocks();
});

/** The persons whose name or email contains every word of the request's `query`. */
function search(init: MockRequest) {
  const words = searchQueryOf(init).toLowerCase().split(/\s+/).filter(Boolean);
  const items = persons.filter((p) =>
    words.every((w) => `${p.givenName} ${p.familyName} ${p.email ?? ''}`.toLowerCase().includes(w)),
  );
  return { body: { items } };
}

function Harness({
  initial = '',
  person,
  error,
  onChange,
}: {
  initial?: string;
  person?: PersonMatch;
  error?: string;
  onChange?: (id: string) => void;
}) {
  const [value, setValue] = useState(initial);
  return (
    <PersonField
      label="Person (optional)"
      value={value}
      person={person}
      error={error}
      onChange={(id) => {
        setValue(id);
        onChange?.(id);
      }}
    />
  );
}

const trigger = () => screen.getByRole('combobox', { name: 'Person (optional)' });

describe('PersonField', () => {
  it('searches the persons as one types and picks one', async () => {
    const post = mockApiPost({ '/api/v1/admin/persons/search': search });
    const onChange = vi.fn();
    const user = userEvent.setup();

    renderWithQuery(<Harness onChange={onChange} />);
    expect(trigger()).toHaveTextContent('No person');
    await user.click(trigger());

    const list = within(await screen.findByRole('listbox', { name: 'Persons' }));
    expect(await list.findByRole('option', { name: /Alex Example/ })).toBeInTheDocument();
    expect(list.getAllByRole('option')).toHaveLength(3);
    expect(post).toHaveBeenCalledWith('/api/v1/admin/persons/search', {
      body: {},
      signal: expect.anything(),
    });

    await user.type(screen.getByRole('combobox', { name: 'Search by name or email' }), 'sam');
    await waitFor(() => expect(list.getAllByRole('option')).toHaveLength(2));
    expect(post).toHaveBeenCalledWith('/api/v1/admin/persons/search', {
      body: { query: 'sam' },
      signal: expect.anything(),
    });
    // Debounced: not every keystroke is a request.
    expect(post).not.toHaveBeenCalledWith('/api/v1/admin/persons/search', {
      body: { query: 's' },
      signal: expect.anything(),
    });

    await user.click(list.getByRole('option', { name: /Mia Sample/ }));

    expect(onChange).toHaveBeenCalledWith(ids.personMia);
    await waitFor(() => expect(screen.queryByRole('listbox')).not.toBeInTheDocument());
    expect(trigger()).toHaveTextContent('Mia Sample');
  });

  it('shows the linked person and removes the link', async () => {
    mockApiPost({ '/api/v1/admin/persons/search': search });
    const onChange = vi.fn();
    const user = userEvent.setup();

    renderWithQuery(<Harness initial={ids.personAlex} person={persons[0]} onChange={onChange} />);
    expect(trigger()).toHaveTextContent('Alex Example');
    await user.click(screen.getByRole('button', { name: 'Remove the person' }));

    expect(onChange).toHaveBeenCalledWith('');
    expect(trigger()).toHaveTextContent('No person');
    expect(screen.queryByRole('button', { name: 'Remove the person' })).not.toBeInTheDocument();
  });

  it('says that only the first matches are shown when the search hits its limit', async () => {
    const many = Array.from({ length: 50 }, (_, i) => ({
      id: `person-${i}`,
      givenName: 'Kim',
      familyName: `Sample ${i}`,
    }));
    let items = many;
    mockApiPost({ '/api/v1/admin/persons/search': () => ({ body: { items } }) });
    const user = userEvent.setup();

    renderWithQuery(<Harness />);
    await user.click(trigger());

    expect(
      await screen.findByText('Showing the first 50 matches. Type more to narrow the list.'),
    ).toBeInTheDocument();

    items = many.slice(0, 49);
    await user.type(screen.getByRole('combobox', { name: 'Search by name or email' }), 'kim');
    await waitFor(() => expect(screen.getAllByRole('option')).toHaveLength(49));
    expect(screen.queryByText(/^Showing the first/)).not.toBeInTheDocument();
  });

  it('says when nobody matches', async () => {
    mockApiPost({ '/api/v1/admin/persons/search': search });
    const user = userEvent.setup();

    renderWithQuery(<Harness />);
    await user.click(trigger());
    await user.type(
      await screen.findByRole('combobox', { name: 'Search by name or email' }),
      'nobody',
    );

    expect(await screen.findByText('Nobody matches “nobody”.')).toBeInTheDocument();
  });

  it('says when the search fails', async () => {
    mockApiPost({ '/api/v1/admin/persons/search': problem(500, 'boom') });
    const user = userEvent.setup();

    renderWithQuery(<Harness />);
    await user.click(trigger());

    expect(await screen.findByText('Could not search the persons.')).toBeInTheDocument();
  });

  it('shows an error', () => {
    mockApiPost({});

    renderWithQuery(<Harness error="Please check this entry." />);

    expect(trigger()).toHaveAttribute('aria-invalid', 'true');
    expect(trigger()).toHaveAccessibleDescription('Please check this entry.');
  });
});
