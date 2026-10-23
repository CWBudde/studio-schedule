import { Pencil, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';

/** The edit and delete buttons of a table row; the labels name the record. */
export function RowActions({
  editLabel,
  deleteLabel,
  onEdit,
  onDelete,
}: {
  editLabel: string;
  deleteLabel: string;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation('admin');
  return (
    <>
      <Button variant="outline" size="sm" aria-label={editLabel} onClick={onEdit}>
        <Pencil aria-hidden="true" />
        <span className="hidden md:inline">{t('form.edit')}</span>
      </Button>
      <Button variant="outline" size="sm" aria-label={deleteLabel} onClick={onDelete}>
        <Trash2 aria-hidden="true" />
        <span className="hidden md:inline">{t('form.delete')}</span>
      </Button>
    </>
  );
}
