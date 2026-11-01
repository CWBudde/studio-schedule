import { ArrowDown, ArrowUp } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';

/**
 * "Move <name> up/down" icon buttons; the first item has no up, the last no
 * down (an empty space keeps the other in place).
 */
export function MoveButtons({
  name,
  index,
  count,
  disabled = false,
  onMove,
}: {
  name: string;
  index: number;
  count: number;
  disabled?: boolean;
  onMove: (delta: -1 | 1) => void;
}) {
  const { t } = useTranslation('admin');
  return (
    <div className="flex gap-1">
      {index > 0 ? (
        <Button
          variant="outline"
          size="icon"
          requiresOnline
          aria-label={t('reorder.up', { name })}
          disabled={disabled}
          onClick={() => onMove(-1)}
        >
          <ArrowUp aria-hidden="true" />
        </Button>
      ) : (
        <span className="size-8" aria-hidden="true" />
      )}
      {index < count - 1 ? (
        <Button
          variant="outline"
          size="icon"
          requiresOnline
          aria-label={t('reorder.down', { name })}
          disabled={disabled}
          onClick={() => onMove(1)}
        >
          <ArrowDown aria-hidden="true" />
        </Button>
      ) : (
        <span className="size-8" aria-hidden="true" />
      )}
    </div>
  );
}
