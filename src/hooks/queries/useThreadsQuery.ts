import { useInfiniteQuery } from "@tanstack/react-query";
import { listThreads } from "@/lib/api";

export const threadsQueryKey = (
  folderId: string, limit: number, offset: number, folderIds?: string[],
) => ["threads", folderId, folderIds, limit, offset] as const;

export function useThreadsQuery(folderId: string | null, limit = 50, offset = 0, folderIds?: string[]) {
  const query = useInfiniteQuery({
    queryKey: threadsQueryKey(folderId ?? "", limit, offset, folderIds),
    queryFn: ({ pageParam }) => listThreads(folderId!, limit, pageParam, folderIds),
    initialPageParam: offset,
    getNextPageParam: (lastPage, allPages) => lastPage.length < limit ? undefined : offset + allPages.length * limit,
    enabled: !!folderId,
    staleTime: 60_000,
  });
  // Offset pagination can overlap after a concurrent arrival; never render the
  // same account-scoped thread twice. Refresh re-fetches all loaded pages.
  const seen = new Set<string>();
  const data = (query.data?.pages.flat() ?? []).filter((thread) => {
    if (seen.has(thread.thread_id)) return false;
    seen.add(thread.thread_id); return true;
  });
  return { ...query, data };
}
