import { XIcon } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';

/**
 * A side sheet with the form of `target` (e.g. a new record or the one to
 * edit); closed while `target` is null. `children` renders the form, keyed
 * by the caller so a new target gets fresh form state.
 */
export function FormSheet<T>({
  target,
  onClose,
  children,
}: {
  target: T | null;
  onClose: () => void;
  children: (target: T) => ReactNode;
}) {
  const { t } = useTranslation('admin');

  // Keep showing the last form while the sheet animates out.
  const [shown, setShown] = useState(target);
  if (target !== null && target !== shown) setShown(target);

  return (
    <Sheet
      open={target !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        side="right"
        showCloseButton={false}
        className="w-full overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-md"
      >
        {shown !== null && children(shown)}
        <SheetClose asChild>
          <Button variant="ghost" size="icon-sm" className="absolute top-3 right-3">
            <XIcon aria-hidden="true" />
            <span className="sr-only">{t('form.close')}</span>
          </Button>
        </SheetClose>
      </SheetContent>
    </Sheet>
  );
}

/** The title and description of a form sheet. */
export function FormSheetHeader({ title, description }: { title: string; description?: string }) {
  return (
    <SheetHeader className="pr-12">
      <SheetTitle className="text-lg">{title}</SheetTitle>
      {description && <SheetDescription>{description}</SheetDescription>}
    </SheetHeader>
  );
}

/** The submit and cancel buttons of a form sheet. */
export function FormSheetActions({
  pending,
  submitLabel,
  icon,
}: {
  pending: boolean;
  submitLabel: string;
  icon?: ReactNode;
}) {
  const { t } = useTranslation('admin');
  return (
    <div className="flex flex-wrap gap-2 pt-2">
      <Button type="submit" disabled={pending}>
        {icon}
        {pending ? t('form.saving') : submitLabel}
      </Button>
      <SheetClose asChild>
        <Button type="button" variant="outline">
          {t('form.cancel')}
        </Button>
      </SheetClose>
    </div>
  );
}
