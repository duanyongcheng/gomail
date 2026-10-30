import { useQuery } from "@tanstack/react-query";
import { listThreadMessages } from "@/lib/api";

export const threadMessagesQueryKey = (threadId: string) =>
  ["threadMessages", threadId] as const;

export function useThreadMessagesQuery(threadId: string | null) {
  return useQuery({
    queryKey: threadMessagesQueryKey(threadId ?? ""),
    queryFn: () => listThreadMessages(threadId!),
    enabled: !!threadId,
    networkMode: "always", // This reads the local conversation, not Gmail.
    // Every conversation opening reads the current local DB metadata, even
    // when reopening within the application's default 30-second stale time.
    staleTime: 0,
  });
}
