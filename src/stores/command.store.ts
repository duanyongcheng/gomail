import { create } from "zustand";
import { isActionAvailable, unavailableReason } from "@/lib/availability";
import { useToastStore } from "./toast.store";

export interface Command {
  id: string;
  name: string;
  shortcut?: string;
  category: string;
  execute: () => void | Promise<void>;
}

interface CommandState {
  isOpen: boolean;
  query: string;
  commands: Command[];
  filteredCommands: Command[];
  open: () => void;
  close: () => void;
  setQuery: (q: string) => void;
  execute: (commandId: string) => Promise<void>;
  registerCommands: (cmds: Command[]) => void;
}

export const useCommandStore = create<CommandState>((set, get) => ({
  isOpen: false,
  query: "",
  commands: [],
  filteredCommands: [],

  open: () => set({ isOpen: true, query: "", filteredCommands: get().commands.filter((cmd) => isActionAvailable(cmd.id)) }),

  close: () => set({ isOpen: false, query: "" }),

  setQuery: (q: string) => {
    const lower = q.toLowerCase();
    const available = get().commands.filter((cmd) => isActionAvailable(cmd.id));
    const filtered = lower
      ? available.filter(
          (c) =>
            c.name.toLowerCase().includes(lower) ||
            c.category.toLowerCase().includes(lower),
        )
      : available;
    set({ query: q, filteredCommands: filtered });
  },

  execute: async (commandId: string) => {
    if (!isActionAvailable(commandId)) {
      useToastStore.getState().addToast({ message: unavailableReason(), type: "info" });
      return;
    }
    const cmd = get().commands.find((c) => c.id === commandId);
    if (cmd) {
      get().close();
      await cmd.execute();
    }
  },

  registerCommands: (cmds: Command[]) =>
    set({ commands: cmds.filter((cmd) => isActionAvailable(cmd.id)), filteredCommands: cmds.filter((cmd) => isActionAvailable(cmd.id)) }),
}));
