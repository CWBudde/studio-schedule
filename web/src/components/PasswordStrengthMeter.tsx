import { useTranslation } from 'react-i18next';
import { cn } from 'cn';
import { type PasswordStrength, passwordStrength } from '@/lib/passwordStrength';

const levels: PasswordStrength[] = ['weak', 'fair', 'good', 'strong'];

const barColor: Record<PasswordStrength, string> = {
  weak: 'bg-destructive',
  fair: 'bg-amber-500',
  good: 'bg-emerald-500',
  strong: 'bg-emerald-600',
};

/**
 * A strength meter for a new password: four bars and the level as text, so
 * it does not rely on color. Nothing shows while the field is empty.
 */
export function PasswordStrengthMeter({ password }: { password: string }) {
  const { t } = useTranslation();
  const strength = password ? passwordStrength(password) : undefined;
  const filled = strength ? levels.indexOf(strength) + 1 : 0;

  return (
    <div aria-live="polite" className="grid gap-1">
      {strength && (
        <>
          <div aria-hidden="true" className="grid grid-cols-4 gap-1">
            {levels.map((level, i) => (
              <span
                key={level}
                className={cn('h-1 rounded-full', i < filled ? barColor[strength] : 'bg-muted')}
              />
            ))}
          </div>
          <p className="text-xs text-muted-foreground">
            {t('password.strength.label', { level: t(`password.strength.${strength}`) })}
          </p>
        </>
      )}
    </div>
  );
}
