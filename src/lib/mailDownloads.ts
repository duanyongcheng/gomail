import { SyncService } from "@/mygo";

export function listDownloadFailures(accountId: string, limit: number, offset: number) {
  return SyncService.downloadFailures({ accountId, limit, offset });
}

export async function retryDownloads(accountId: string) {
  await SyncService.retryDownloads({ accountId });
}
