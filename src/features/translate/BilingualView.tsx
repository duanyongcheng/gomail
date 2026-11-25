interface Props {
  segments: { source: string; target: string }[];
  displayMode?: "bilingual" | "translated";
}

/**
 * Paragraph-aligned bilingual (or translated-only) reading view for plain-text
 * emails, styled to match GoMail's Clarity reader typography.
 */
export default function BilingualView({ segments, displayMode = "bilingual" }: Props) {
  if (displayMode === "translated") {
    return (
      <div>
        {segments.map((segment, index) => (
          <p
            key={index}
            style={{
              margin: "0 0 12px",
              fontSize: "14px",
              lineHeight: 1.7,
              color: "var(--color-text-primary)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {segment.target || segment.source}
          </p>
        ))}
      </div>
    );
  }

  const pairs = segments.filter(
    (segment) =>
      segment.target.trim() !== "" && segment.source.trim() !== segment.target.trim(),
  );

  if (pairs.length === 0) {
    return (
      <div style={{ fontSize: "13px", color: "var(--color-text-secondary)" }}>
        {segments.length > 0 ? segments.map((segment) => segment.target).join("\n") : null}
      </div>
    );
  }

  return (
    <div>
      {pairs.map((segment, index) => (
        <div key={index} style={{ marginBottom: "14px" }}>
          <div
            style={{
              fontSize: "14px",
              lineHeight: 1.7,
              color: "var(--color-text-primary)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {segment.target}
          </div>
          <div
            style={{
              fontSize: "12px",
              lineHeight: 1.6,
              marginTop: "3px",
              paddingLeft: "8px",
              borderLeft: "2px solid var(--color-border)",
              color: "var(--color-text-secondary)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {segment.source}
          </div>
        </div>
      ))}
    </div>
  );
}
