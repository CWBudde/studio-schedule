import { useQuery } from '@tanstack/react-query';
import { meQuery } from '@/api/queries';
import { canAdminister } from '@/features/admin/access';
import { type CloneContext, CloneLessons } from './CloneLessons';
import { NewLesson } from './NewLesson';

/**
 * The office's actions in the schedule's toolbar: a new one-off lesson and
 * cloning the lessons of a day or a week. Nothing for anyone else.
 */
export function OfficeActions(context: CloneContext) {
  const me = useQuery(meQuery);
  if (!canAdminister(me.data?.role)) return null;
  return (
    <div className="flex flex-wrap gap-2">
      <NewLesson />
      <CloneLessons {...context} />
    </div>
  );
}
