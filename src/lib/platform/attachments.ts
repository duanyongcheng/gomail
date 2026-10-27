import { FileService } from "@/mygo";

// Separate from the upstream invoke surface: null is a normal dialog cancel.
export const saveAttachmentAs = (attachmentId: string): Promise<string | null> =>
  FileService.saveAttachment({ attachmentId });
