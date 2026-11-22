import { useDroppable } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { useTranslation } from "react-i18next";
import type { KanbanColumnType, Message } from "@/lib/api";
import KanbanCardComponent from "./KanbanCard";

interface Props {
  id: KanbanColumnType;
  title: string;
  cardIds: string[];
  contextNotes: Record<string, string>;
  messages: Map<string, Message>;
  onRemove: (id: string) => void;
  onOpen: (messageId: string) => void;
}

export default function KanbanColumn({ id, title, cardIds, contextNotes, messages, onRemove, onOpen }: Props) {
  const { t } = useTranslation();
  const { setNodeRef, isOver } = useDroppable({ id });

  return (
    <div
      style={{
        flex: 1,
        minWidth: "260px",
        maxWidth: "380px",
        display: "flex",
        flexDirection: "column",
        height: "100%",
        backgroundColor: "var(--color-sidebar-bg)",
        border: "1px solid var(--color-border)",
        borderRadius: "var(--ui-radius-lg)",
        overflow: "hidden",
      }}
    >
      <div
        style={{
          padding: "12px 14px",
          fontWeight: 650,
          fontSize: "13.5px",
          color: "var(--color-text-primary)",
          borderBottom: "1px solid var(--color-border)",
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <span>{title}</span>
        <span className="secondary-mail-count">
          {cardIds.length}
        </span>
      </div>
      <div
        ref={setNodeRef}
        className="scroll-region kanban-column-scroll"
        style={{
          flex: 1,
          padding: "10px",
          overflowY: "auto",
          backgroundColor: isOver ? "var(--color-accent-soft)" : "transparent",
          transition: "background-color 0.15s ease",
          minHeight: "100px",
        }}
      >
        <SortableContext items={cardIds} strategy={verticalListSortingStrategy}>
          {cardIds.map((cardId) => (
            <KanbanCardComponent
              key={cardId}
              id={cardId}
              note={contextNotes[cardId]}
              message={messages.get(cardId) || null}
              onRemove={onRemove}
              onOpen={onOpen}
            />
          ))}
        </SortableContext>
        {cardIds.length === 0 && (
          <div
            style={{
              textAlign: "center",
              color: "var(--color-text-secondary)",
              fontSize: "13px",
              padding: "24px 0",
            }}
          >
            {t("kanban.dropHere")}
          </div>
        )}
      </div>
    </div>
  );
}
