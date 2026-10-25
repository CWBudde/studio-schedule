import { type ComponentProps, type ReactNode, useId } from 'react';
import { PasswordInput } from '@/components/PasswordInput';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

type TextFieldProps = ComponentProps<'input'> & {
  label: string;
  error?: string;
  hint?: string;
  /** Shown below the input, e.g. a PasswordStrengthMeter. */
  meter?: ReactNode;
};

/**
 * A labelled input with an optional hint and error text. A password input
 * gets a button that shows what was typed (PasswordInput).
 */
export function TextField({
  label,
  error,
  hint,
  meter,
  id: idProp,
  type,
  ...input
}: TextFieldProps) {
  const autoId = useId();
  const id = idProp ?? autoId;
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const describedBy = [hint ? hintId : '', error ? errorId : ''].filter(Boolean).join(' ');

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      {type === 'password' ? (
        <PasswordInput
          id={id}
          aria-invalid={error ? true : undefined}
          aria-describedby={describedBy || undefined}
          {...input}
        />
      ) : (
        <Input
          id={id}
          aria-invalid={error ? true : undefined}
          aria-describedby={describedBy || undefined}
          type={type}
          {...input}
        />
      )}
      {meter}
      {hint && (
        <p id={hintId} className="text-xs text-muted-foreground">
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} className="text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
