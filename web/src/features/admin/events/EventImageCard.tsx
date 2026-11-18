import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ImageUp, Trash2 } from 'lucide-react';
import { type ChangeEvent, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type AdminEvent,
  adminEventImageQuery,
  deleteEventImageMutation,
  putEventImageMutation,
} from '@/api/queries';
import { PendingIcon } from '@/components/PendingIcon';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { invalidFields } from '@/features/auth/form';
import { useDataUrl } from '@/lib/useDataUrl';
import { LoadError } from '../crud/LoadError';
import { fileBase64 } from '../receiptSettingsForms';

const imageTypes = ['image/jpeg', 'image/png', 'image/webp'] as const;
const maxImageBytes = 1 << 20;

export function EventImageCard({ event }: { event: AdminEvent }) {
  const { t } = useTranslation('adminEvents');
  const queryClient = useQueryClient();
  const inputId = useId();
  const hintId = useId();
  const errorId = useId();
  const image = useQuery(adminEventImageQuery(event.id, event.image?.digest));
  const upload = useMutation(putEventImageMutation(queryClient, event.id));
  const remove = useMutation(deleteEventImageMutation(queryClient, event.id));
  const [reading, setReading] = useState(false);
  const [refused, setRefused] = useState<'invalid' | 'unreadable'>();
  const url = useDataUrl(image.data);
  const pending = reading || upload.isPending || remove.isPending;
  const error =
    refused ??
    (invalidFields(upload.error, ['contentType', 'data']).length ? 'invalid' : undefined);

  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    upload.reset();
    remove.reset();
    const contentType = imageTypes.find((type) => type === file.type);
    if (!contentType || file.size === 0 || file.size > maxImageBytes) {
      setRefused('invalid');
      return;
    }

    setRefused(undefined);
    setReading(true);
    try {
      const data = await fileBase64(file);
      upload.mutate(
        { contentType, data },
        {
          onSuccess: () => toast.success(t('image.saved')),
        },
      );
    } catch {
      setRefused('unreadable');
    } finally {
      setReading(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>{t('image.title')}</h2>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {url ? (
          <img
            src={url}
            alt={t('image.alt', { title: event.title })}
            className="max-h-56 max-w-full rounded border object-contain"
          />
        ) : image.isError ? (
          <LoadError message={t('image.loadError')} onRetry={() => void image.refetch()} />
        ) : event.image && image.data !== null ? (
          <Skeleton className="h-24 w-40" />
        ) : (
          <p className="text-sm text-muted-foreground">{t('image.none')}</p>
        )}
        {error ? (
          <p id={errorId} role="alert" className="text-sm text-destructive">
            {t(`image.${error}`)}
          </p>
        ) : (
          <ProblemAlert error={upload.error ?? remove.error} messages={{ 404: t('notFound') }} />
        )}
        <div className="flex flex-wrap items-center gap-2">
          <input
            id={inputId}
            type="file"
            accept={imageTypes.join(',')}
            className="peer sr-only"
            disabled={pending}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? `${errorId} ${hintId}` : hintId}
            onChange={(e) => void onFile(e)}
          />
          <Button asChild variant="outline" size="sm" disabled={pending}>
            <label
              htmlFor={inputId}
              className="cursor-pointer peer-focus-visible:ring-[3px] peer-focus-visible:ring-ring/50"
            >
              <PendingIcon
                pending={reading || upload.isPending}
                icon={<ImageUp aria-hidden="true" />}
              />
              {t(
                reading || upload.isPending
                  ? 'image.uploading'
                  : event.image
                    ? 'image.replace'
                    : 'image.upload',
              )}
            </label>
          </Button>
          {event.image && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={pending}
              onClick={() => {
                upload.reset();
                setRefused(undefined);
                remove.mutate(undefined, { onSuccess: () => toast.success(t('image.removed')) });
              }}
            >
              <PendingIcon pending={remove.isPending} icon={<Trash2 aria-hidden="true" />} />
              {t('image.remove')}
            </Button>
          )}
        </div>
        <p id={hintId} className="text-xs text-muted-foreground">
          {t('image.hint')}
        </p>
      </CardContent>
    </Card>
  );
}
