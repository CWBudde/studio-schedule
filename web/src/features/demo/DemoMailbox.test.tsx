import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { confirmationMail, mockApiGet, publicSettings } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { DemoMailbox } from './DemoMailbox';

afterEach(() => {
  vi.restoreAllMocks();
});

const appUrl = `${window.location.origin}/`;

function mockMailbox(mails: unknown) {
  return mockApiGet({
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/demo/mailbox': mails === null ? { status: 404, body: undefined } : { body: { mails } },
  });
}

describe('DemoMailbox', () => {
  it('is absent when the installation is not a demo', async () => {
    const get = mockMailbox(null);

    renderInRouter(
      <>
        <h1>Page</h1>
        <DemoMailbox />
      </>,
    );

    expect(await screen.findByRole('heading', { name: 'Page' })).toBeInTheDocument();
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith('/api/v1/demo/mailbox', expect.anything()),
    );
    expect(screen.queryByRole('button', { name: /Demo mailbox/ })).not.toBeInTheDocument();
  });

  it('lists the mails with recipient, school time, status and plain-text body', async () => {
    mockMailbox([confirmationMail(appUrl)]);
    const user = userEvent.setup();

    renderInRouter(<DemoMailbox />);

    await user.click(await screen.findByRole('button', { name: 'Demo mailbox (1 mail)' }));

    const sheet = within(await screen.findByRole('dialog', { name: 'Demo mailbox' }));
    const mail = within(sheet.getByRole('article', { name: 'Confirm your email address' }));
    expect(mail.getByText('new.dancer@example.org')).toBeInTheDocument();
    // 10:00 UTC in the school's time zone (Berlin), not the browser's.
    expect(mail.getByText('Fri, Oct 23, 12:00')).toBeInTheDocument();
    expect(mail.getByText('Pending')).toBeInTheDocument();
    expect(mail.getByText(/please confirm your email address:/)).toBeInTheDocument();
    // The HTML body is not rendered.
    expect(mail.queryByText('Hello Nina')).not.toBeInTheDocument();

    expect(mail.getByRole('link', { name: `${appUrl}confirm?token=tok-123` })).toHaveAttribute(
      'href',
      `${appUrl}confirm?token=tok-123`,
    );
    const external = mail.getByRole('link', { name: 'https://example.org/help' });
    expect(external).toHaveAttribute('target', '_blank');
    expect(external).toHaveAttribute('rel', 'noopener noreferrer');
    // No address to reply to but the sender's.
    expect(mail.queryByText('Reply to')).not.toBeInTheDocument();
  });

  it('shows the address replies go to, if the mail has one', async () => {
    mockMailbox([
      {
        ...confirmationMail(appUrl),
        subject: 'Message about Ballet',
        template: 'lesson_notice',
        replyTo: 'alex.example@example.com',
      },
    ]);
    const user = userEvent.setup();

    renderInRouter(<DemoMailbox />);
    await user.click(await screen.findByRole('button', { name: 'Demo mailbox (1 mail)' }));

    const sheet = within(await screen.findByRole('dialog', { name: 'Demo mailbox' }));
    const mail = within(sheet.getByRole('article', { name: 'Message about Ballet' }));
    expect(mail.getByText('Reply to')).toBeInTheDocument();
    expect(mail.getByText('alex.example@example.com')).toBeInTheDocument();
  });

  it('follows a link to the app within the app and closes', async () => {
    mockMailbox([confirmationMail(appUrl)]);
    const user = userEvent.setup();

    const { router } = renderInRouter(<DemoMailbox />);

    await user.click(await screen.findByRole('button', { name: /Demo mailbox/ }));
    const sheet = within(await screen.findByRole('dialog'));
    await user.click(sheet.getByRole('link', { name: /confirm\?token=tok-123/ }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/confirm'));
    expect(router.state.location.search).toEqual({ token: 'tok-123' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('says when there are no mails', async () => {
    mockMailbox([]);
    const user = userEvent.setup();

    renderInRouter(<DemoMailbox />);

    await user.click(await screen.findByRole('button', { name: 'Demo mailbox (0 mails)' }));

    expect(
      await within(await screen.findByRole('dialog')).findByText('No mails yet.'),
    ).toBeInTheDocument();
  });
});
