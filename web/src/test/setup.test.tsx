import { render, screen } from '@testing-library/react';
import { afterAll, expect, it } from 'vitest';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';

// Radix restores the focus in a timer after a dialog unmounts. The shared
// afterEach must let that timer run before the environment can be torn down.
let restoredFocus = false;

it('ends with a dialog open', () => {
  render(
    <Dialog open>
      <DialogContent onCloseAutoFocus={() => (restoredFocus = true)}>
        <DialogTitle>Title</DialogTitle>
        <DialogDescription>Description</DialogDescription>
      </DialogContent>
    </Dialog>,
  );
  expect(screen.getByRole('dialog', { name: 'Title' })).toBeInTheDocument();
});

afterAll(() => {
  expect(restoredFocus).toBe(true);
});
