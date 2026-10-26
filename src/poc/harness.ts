import { Backend, GmailService } from "./mygo";
import { queryClient } from "../lib/query-client";
import { useMailStore } from "../stores/mail.store";
import { useUIStore } from "../stores/ui.store";

export async function activateGmailInbox() {
  useMailStore.getState().setActiveAccountId("poc-account");
  useMailStore.getState().setActiveFolderId("gmail-label:INBOX");
  useUIStore.getState().setActiveView("inbox");
  await queryClient.invalidateQueries({ queryKey: ["accounts"] });
  await queryClient.invalidateQueries({ queryKey: ["folders"] });
  await queryClient.invalidateQueries({ queryKey: ["messages"] });
  await queryClient.invalidateQueries({ queryKey: ["threads"] });
}
export function setGmailCapabilities(modify: boolean) {
  Object.assign(window, { __gmailCapabilities: { skipAutomaticRead: !modify } });
}

const errors: string[] = [];
addEventListener("error", (e) => errors.push(e.message));
addEventListener("unhandledrejection", (e) => errors.push(String(e.reason)));
Object.assign(window, {
  __gmailTest: {
    api: {
      ...GmailService,
      sync: async () => {
        const result = await GmailService.sync();
        await activateGmailInbox();
        return result;
      },
    },
    activateInbox: activateGmailInbox,
    setCapabilities: setGmailCapabilities,
    backend: Backend,
    errors,
    select: (id: string) => useMailStore.getState().setSelectedMessage(id),
    search: (q: string) => {
      useUIStore.getState().setSearchQuery(q);
      useUIStore.getState().setActiveView("search");
    },
  },
});
