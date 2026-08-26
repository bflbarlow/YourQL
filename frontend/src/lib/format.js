// YourQL — shared formatting & metadata helpers (single source; used by
// ConversationView, MinimalistView, and any future chat surface).

export function parsePayload(metadata) {
  if (!metadata) return null;
  try {
    return typeof metadata === "string" ? JSON.parse(metadata) : metadata;
  } catch {
    return null;
  }
}

export function getChartConfig(messageOrMeta) {
  const meta =
    typeof messageOrMeta === "string"
      ? parsePayload(messageOrMeta)
      : messageOrMeta?.metadata !== undefined
        ? parsePayload(messageOrMeta.metadata)
        : messageOrMeta;
  try {
    return meta?.chart_config || null;
  } catch {
    return null;
  }
}

export function getSqlFromMessage(msg) {
  if (!msg || !msg.metadata) return null;
  try {
    const meta = parsePayload(msg.metadata);
    return meta?.sql_query || meta?.sql?.query || null;
  } catch {
    return null;
  }
}

export function fmtTokens(n) {
  if (n >= 1000) return (n / 1000).toFixed(1) + "K";
  return String(n);
}

export function formatTime(dateStr) {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now - date;
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);

  const timeStr = date.toLocaleTimeString([], {
    hour: "numeric",
    minute: "2-digit",
  });

  if (diffMins < 1) return "Just now";
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return timeStr;

  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) {
    return `Yesterday ${timeStr}`;
  }
  return (
    date.toLocaleDateString([], { month: "short", day: "numeric" }) +
    " " +
    timeStr
  );
}

export function escapeCsvCell(s) {
  return '"' + String(s).replace(/"/g, '""') + '"';
}
