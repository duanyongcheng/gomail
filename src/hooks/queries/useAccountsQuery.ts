import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { listen } from "@tauri-apps/api/event";
import { listAccounts } from "@/lib/api";

export const accountsQueryKey = ["accounts"] as const;

export function useAccountsQuery() {
  const client = useQueryClient();
  useEffect(() => {
    const subscription = listen("mail:account-changed", () => {
      void client.invalidateQueries({ queryKey: accountsQueryKey });
      void client.invalidateQueries({ queryKey: ["account-connections"] });
    }).catch(() => () => {});
    // The browser-only preview has no desktop event transport. Local reads and
    // explicit action refreshes remain usable there.
    return () => { void subscription.then((off) => off()); };
  }, [client]);
  return useQuery({
    queryKey: accountsQueryKey,
    queryFn: listAccounts,
    networkMode: "always", // Local account metadata, not provider validation.
  });
}
