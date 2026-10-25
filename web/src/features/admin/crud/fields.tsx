import type { ComponentProps } from 'react';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { useDescription } from './useDescription';

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

/** A labelled on/off switch with an optional hint and error text. */
export function SwitchField({
  label,
  checked,
  disabled,
  onChange,
  onBlur,
  error,
  hint,
}: {
  label: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
  onBlur?: () => void;
  error?: string;
  hint?: string;
}) {
  const { id, describedBy, lines } = useDescription(hint, error);
  return (
    <div className="grid gap-1.5">
      <div className="flex items-center gap-3">
        <Switch
          id={id}
          checked={checked}
          disabled={disabled}
          onCheckedChange={onChange}
          onBlur={onBlur}
          aria-invalid={error ? true : undefined}
          aria-describedby={describedBy}
        />
        <Label htmlFor={id}>{label}</Label>
      </div>
      {lines}
    </div>
  );
}
