import { useQuery } from "@tanstack/react-query";
import { getMessage } from "@/lib/api";

export const messageQueryKey = (messageId: string) =>
  ["message", messageId] as const;

export function useMessageQuery(messageId: string | null) {
  return useQuery({
    queryKey: messageQueryKey(messageId ?? ""),
    queryFn: () => getMessage(messageId!),
    enabled: !!messageId,
    networkMode: "always", // Local SQLite metadata, including when offline.
    // A newly opened detail must observe local changes made from other views.
    // This is a local DB read, not a Gmail network refresh.
    staleTime: 0,
  });
}
