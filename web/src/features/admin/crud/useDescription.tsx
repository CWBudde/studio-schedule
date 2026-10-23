import { type ReactNode, useId } from 'react';

/** The hint and error lines under a field, and the ids that describe the field. */
export function useDescription(hint: string | undefined, error: string | undefined) {
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
