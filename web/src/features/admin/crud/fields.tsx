import { type ComponentProps, type ReactNode, useId } from 'react';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';

/** The hint and error lines under a field, and the ids that describe the field. */
function useDescription(hint: string | undefined, error: string | undefined) {
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const describedBy = [hint ? hintId : '', error ? errorId : ''].filter(Boolean).join(' ');
  const lines: ReactNode = (
    <>
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
    </>
  );
  return { id, describedBy: describedBy || undefined, lines };
}

/** A labelled multi-line text input with an optional hint and error text. */
export function TextareaField({
  label,
  error,
  hint,
  ...textarea
}: ComponentProps<'textarea'> & { label: string; error?: string; hint?: string }) {
  const { id, describedBy, lines } = useDescription(hint, error);
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Textarea
        id={id}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy}
        {...textarea}
      />
      {lines}
    </div>
  );
}

/** A labelled choice of one option with an optional hint and error text. */
export function SelectField({
  label,
  placeholder,
  options,
  value,
  onChange,
  onBlur,
  error,
  hint,
}: {
  label: string;
  placeholder: string;
  options: { value: string; label: string }[];
  value: string;
  onChange: (value: string) => void;
  onBlur?: () => void;
  error?: string;
  hint?: string;
}) {
  const { id, describedBy, lines } = useDescription(hint, error);
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger
          id={id}
          className="w-full"
          aria-invalid={error ? true : undefined}
          aria-describedby={describedBy}
          onBlur={onBlur}
        >
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {lines}
    </div>
  );
}
