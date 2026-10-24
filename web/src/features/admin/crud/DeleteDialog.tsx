import {
  type QueryClient,
  type UseMutationOptions,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { ConfirmDelete } from './ConfirmDelete';
import { conflictReason } from '@/api/conflict';

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
 * The way out of a delete the API refuses as in use (409 `in-use`), such as
 * deactivating the record instead; its variable is the record.
 */
export type InUseAction<T> = {
  label: string;
  /** The label while it is under way. */
  pending: string;
  /** The toast after it. */
  done: string;
  mutation: (queryClient: QueryClient) => UseMutationOptions<void, Error, T>;
};

/**
 * Asks before deleting `target` (closed while it is null) with `mutation`,
 * and says why when the API refuses. The texts stay while the dialog
 * animates out, and every record gets a fresh mutation without the error
 * of the last one. Once the API refuses the delete as in use, the dialog
 * offers `inUseAction` of the record, if it has one.
 */
export function DeleteDialog<T>({
  target,
  id,
  mutation,
  texts,
  inUseAction,
  onDeleted,
  onClose,
}: {
  target: T | null;
  /** The id of a record, the mutation's variable. */
  id: (record: T) => string;
  mutation: DeleteMutation;
  texts: (record: T) => DeleteTexts;
  inUseAction?: (record: T) => InUseAction<T> | undefined;
  /** Called after the delete, before the dialog closes (e.g. to leave the record's page). */
  onDeleted?: (record: T) => void;
  onClose: () => void;
}) {
  // Keep the texts while the dialog animates out.
  const [shown, setShown] = useState(target);
  if (target !== null && target !== shown) setShown(target);
  // Every opening starts afresh, also for the same record as the last time.
  const open = target !== null;
  const [openings, setOpenings] = useState({ open, count: 0 });
  if (openings.open !== open) setOpenings({ open, count: openings.count + (open ? 1 : 0) });
  if (shown === null) return null;
  const recordId = id(shown);
  return (
    <DeleteConfirm
      key={`${recordId}:${openings.count}`}
      record={shown}
      recordId={recordId}
      mutation={mutation}
      texts={texts(shown)}
      inUseAction={inUseAction?.(shown)}
      open={open}
      onDeleted={onDeleted}
      onClose={onClose}
    />
  );
}

function DeleteConfirm<T>({
  record,
  recordId,
  mutation,
  texts,
  inUseAction,
  open,
  onDeleted,
  onClose,
}: {
  record: T;
  recordId: string;
  mutation: DeleteMutation;
  texts: DeleteTexts;
  inUseAction: InUseAction<T> | undefined;
  open: boolean;
  onDeleted: ((record: T) => void) | undefined;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useMutation(mutation(queryClient));
  // A hook can't be conditional: without an action, this one is never run.
  const alternative = useMutation(
    inUseAction?.mutation(queryClient) ?? { mutationFn: () => Promise.resolve() },
  );
  const pending = remove.isPending || alternative.isPending;
  const offer = inUseAction && conflictReason(remove.error) === 'in-use';
  return (
    <ConfirmDelete
      open={open}
      pending={pending}
      deleting={remove.isPending}
      error={alternative.error ?? remove.error}
      title={texts.title}
      description={texts.description}
      conflicts={{ 'in-use': texts.inUse }}
      messages={{ 404: texts.notFound }}
      alternative={
        offer && (
          <Button
            variant="outline"
            disabled={pending}
            onClick={() =>
              alternative.mutate(record, {
                onSuccess: () => {
                  toast.success(inUseAction.done);
                  onClose();
                },
              })
            }
          >
            {alternative.isPending ? inUseAction.pending : inUseAction.label}
          </Button>
        )
      }
      onCancel={onClose}
      onConfirm={() => {
        // The answer of this delete is the one to show.
        alternative.reset();
        remove.mutate(recordId, {
          onSuccess: () => {
            toast.success(texts.deleted);
            onDeleted?.(record);
            onClose();
          },
        });
      }}
    />
  );
}
