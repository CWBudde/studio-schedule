import type { ReactNode } from 'react';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { cn } from '@/lib/utils';

/**
 * The card of a page that a mailed link opens (confirming an address,
 * resetting a password, a waiting-list invite): an optional round icon, the
 * page's h1 and a description, then `children` (CardContent, CardFooter).
 * `tone` colours the icon: `destructive` for a link that does not work.
 * `status` announces the description, e.g. after a change took effect.
 */
export function LinkPageCard({
  icon,
  tone = 'primary',
  title,
  description,
  status = false,
  children,
}: {
  icon?: ReactNode;
  tone?: 'primary' | 'destructive';
  title: string;
  description?: ReactNode;
  status?: boolean;
  children?: ReactNode;
}) {
  return (
    <Card className="mx-auto w-full max-w-md">
      <CardHeader>
        {icon && (
          <div
            aria-hidden="true"
            className={cn(
              'mb-2 flex size-10 items-center justify-center rounded-full [&_svg]:size-5',
              tone === 'destructive'
                ? 'bg-destructive/10 text-destructive'
                : 'bg-primary/10 text-primary',
            )}
          >
            {icon}
          </div>
        )}
        <CardTitle>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">{title}</h1>
        </CardTitle>
        {description !== undefined && (
          <CardDescription role={status ? 'status' : undefined}>{description}</CardDescription>
        )}
      </CardHeader>
      {children}
    </Card>
  );
}
