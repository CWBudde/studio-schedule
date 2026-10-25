import { Eye, EyeOff } from 'lucide-react';
import { type ComponentProps, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from '@/components/ui/input-group';

type PasswordInputProps = Omit<ComponentProps<'input'>, 'type'>;

/**
 * A password input with a button that shows or hides what was typed
 * (ASVS 2.1.12). The other props, `ref` included, go to the input, so it
 * works with react-hook-form's `register`. The button's name is visually
 * hidden text rather than an aria-label, so it does not match the input's
 * label in label queries.
 */
export function PasswordInput({ className, disabled, ...input }: PasswordInputProps) {
  const { t } = useTranslation();
  const [visible, setVisible] = useState(false);

  return (
    <InputGroup className={className} data-disabled={disabled ? true : undefined}>
      <InputGroupInput type={visible ? 'text' : 'password'} disabled={disabled} {...input} />
      <InputGroupAddon align="inline-end">
        <InputGroupButton
          size="icon-xs"
          aria-pressed={visible}
          disabled={disabled}
          onClick={() => setVisible((v) => !v)}
        >
          {visible ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
          <span className="sr-only">{visible ? t('password.hide') : t('password.show')}</span>
        </InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  );
}
