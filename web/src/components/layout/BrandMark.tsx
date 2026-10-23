import { useId } from 'react';
import { cn } from '@/lib/utils';

/** The app's mark: a foot en pointe with crossed ribbons, in a round seal. Follows the theme. */
export function BrandMark({ className }: { className?: string }) {
  const clip = useId();
  return (
    <svg viewBox="0 0 32 32" aria-hidden="true" className={cn('shrink-0', className)}>
      <clipPath id={clip}>
        <circle cx="16" cy="16" r="16" />
      </clipPath>
      <g clipPath={`url(#${clip})`}>
        <rect width="32" height="32" className="fill-primary" />
        <path
          d="M13.3 0h5.2c-.2 4-.4 7.6.4 11.2h-6.2c.6-3.6.8-7.2.6-11.2z"
          className="fill-primary-foreground/45"
        />
        <path
          d="M12.4 10.6c-.6 6 .2 12.4 2 17.6h3c.6-3.6 3.6-8.6 3-13.8-.3-2-1.4-3.1-2.2-3.8-1.6 1.4-4.2 1.4-5.8 0z"
          className="fill-primary-foreground"
        />
        <path
          d="M12.7 3.4l6 4.6M18.7 3.4l-6 4.6"
          strokeWidth="1.15"
          strokeLinecap="round"
          className="stroke-primary-foreground"
        />
      </g>
    </svg>
  );
}
