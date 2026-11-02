import { Loader2 } from 'lucide-react';
import type { ReactNode } from 'react';

/**
 * The icon of a button whose request runs: a spinner while `pending`, else
 * `icon`. The spinner turns only without a reduced-motion preference; the
 * button's label says that it runs either way. Tests find the spinner by
 * `data-slot="spinner"`.
 */
export function PendingIcon({ pending, icon }: { pending: boolean; icon?: ReactNode }) {
  if (!pending) return icon;
  return <Loader2 aria-hidden="true" data-slot="spinner" className="motion-safe:animate-spin" />;
}
