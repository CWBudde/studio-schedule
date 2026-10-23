import { cn } from '@/lib/utils';

/** A programme's divider: two hairlines around a small diamond. Decorative only. */
export function Ornament({ className }: { className?: string }) {
  return (
    <div aria-hidden="true" className={cn('flex items-center gap-3 text-primary', className)}>
      <span className="h-px flex-1 bg-gradient-to-r from-transparent to-current opacity-40" />
      <span className="size-1.5 rotate-45 bg-current" />
      <span className="h-px flex-1 bg-gradient-to-l from-transparent to-current opacity-40" />
    </div>
  );
}
