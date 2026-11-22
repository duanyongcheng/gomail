import type { CSSProperties } from "react";

export const inputStyle: CSSProperties = {
  width: "100%",
  padding: "8px 11px",
  borderRadius: "var(--ui-radius-md, 8px)",
  border: "1px solid var(--color-border)",
  backgroundColor: "var(--color-bg)",
  color: "var(--color-text-primary)",
  fontSize: "13px",
  lineHeight: 1.45,
  boxSizing: "border-box",
};

export const labelStyle: CSSProperties = {
  display: "block",
  fontSize: "12.5px",
  fontWeight: 500,
  color: "var(--color-text-secondary)",
  marginBottom: "5px",
};

export const fieldGroupStyle: CSSProperties = {
  marginBottom: "16px",
};
