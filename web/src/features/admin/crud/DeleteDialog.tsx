import {
  type QueryClient,
  type UseMutationOptions,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ConfirmDelete } from './ConfirmDelete';

/** A delete mutation of the catalogue, such as deleteRoomMutation: its variable is the id. */
export type DeleteMutation = (queryClient: QueryClient) => UseMutationOptions<void, Error, string>;

/** The words of the delete dialog of one record. */
export type DeleteTexts = {
  title: string;
  description: string;
  /** Why the API refuses (409 `in-use`). */
  inUse: string;
  /** The record is gone already (404). */
  notFound: string;
  /** The toast after the delete. */
  deleted: string;
};

/**
 * Asks before deleting `target` (closed while it is null) with `mutation`,
 * and says why when the API refuses. The texts stay while the dialog
 * animates out, and every record gets a fresh mutation without the error
 * of the last one.
 */
export function DeleteDialog<T>({
  target,
  id,
  mutation,
  texts,
  onClose,
}: {
  target: T | null;
  /** The id of a record, the mutation's variable. */
  id: (record: T) => string;
  mutation: DeleteMutation;
  texts: (record: T) => DeleteTexts;
  onClose: () => void;
}) {
  // Keep the texts while the dialog animates out.
  const [shown, setShown] = useState(target);
  if (target !== null && target !== shown) setShown(target);
  if (shown === null) return null;
  const recordId = id(shown);
  return (
    <DeleteConfirm
      key={recordId}
      recordId={recordId}
      mutation={mutation}
      texts={texts(shown)}
      open={target !== null}
      onClose={onClose}
    />
  );
}

function DeleteConfirm({
  recordId,
  mutation,
  texts,
  open,
  onClose,
}: {
  recordId: string;
  mutation: DeleteMutation;
  texts: DeleteTexts;
  open: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useMutation(mutation(queryClient));
  return (
    <ConfirmDelete
      open={open}
      pending={remove.isPending}
      error={remove.error}
      title={texts.title}
      description={texts.description}
      conflicts={{ 'in-use': texts.inUse }}
      messages={{ 404: texts.notFound }}
      onCancel={onClose}
      onConfirm={() =>
        remove.mutate(recordId, {
          onSuccess: () => {
            toast.success(texts.deleted);
            onClose();
          },
        })
      }
    />
  );
}
