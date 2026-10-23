import { EyeOff, UsersRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { LessonVisibility } from '@/api/client';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

/**
 * Marks a lesson that not everyone sees: one for members, or a hidden one
 * that only staff see. Public lessons get no badge.
 */
export function VisibilityBadge({
  visibility,
  className,
}: {
  visibility: LessonVisibility;
  className?: string;
}) {
  const { t } = useTranslation('schedule');
  if (visibility === 'public') return null;

  const Icon = visibility === 'members' ? UsersRound : EyeOff;
  return (
    <Badge
      variant="outline"
      data-visibility={visibility}
      className={cn(
        'max-w-full',
        visibility === 'members'
          ? 'border-primary/30 bg-primary/10 text-primary'
          : 'border-dashed border-foreground/40 bg-background text-foreground',
        className,
      )}
    >
      <Icon aria-hidden="true" />
      <span className="truncate">{t(`visibility.${visibility}`)}</span>
    </Badge>
  );
}
