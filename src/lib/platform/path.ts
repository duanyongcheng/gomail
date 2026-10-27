import { FileService } from "@/mygo";

export const downloadDir = (): Promise<string> => FileService.downloads({});
