// YourQL — single bridge between backend-rendered assistant HTML and the app.
// Backend HTML emits semantic elements (details.sql-block, th.sort-header,
// buttons with data-action attributes). This module wires them up via
// delegated document-level listeners — no window.* globals, no
// MutationObserver rescanning.

let initialized = false;

export function initHtmlBridge() {
  if (initialized || typeof document === "undefined") return;
  initialized = true;

  // ── Legacy inline-handler bridge ─────────────────────────────
  // Backend-generated assistant HTML (pkg/engine/rendering.go) contains
  // onclick="copySQL(...)" / "toggleSQLSection(...)" / "exportCSV(...)"
  // attributes. Until result rendering moves into Svelte components fed by
  // message metadata, these single implementations service them.
  // (Previously defined twice — main.js AND ConversationView.svelte.)
  window.copySQL = function (codeId) {
    const code = document.getElementById(codeId);
    if (!code) return;
    navigator.clipboard
      .writeText(code.textContent)
      .then(() => {
        const btn =
          code.parentElement?.parentElement?.querySelector(".copy-sql-btn");
        if (btn) {
          const orig = btn.textContent;
          btn.textContent = "Copied!";
          setTimeout(() => {
            btn.textContent = orig;
          }, 1500);
        }
      })
      .catch((err) => console.error("Failed to copy SQL:", err));
  };

  window.toggleSQLSection = function (btn, sectionId) {
    const section = document.getElementById(sectionId);
    if (!section) return;
    const isVisible = section.style.display === "block";
    section.style.display = isVisible ? "none" : "block";
  };

  window.exportCSV = function (btn) {
    const table = btn.closest(".results-card")?.querySelector(".result-table");
    if (!table) return;
    const headers = [...table.querySelectorAll("thead th")].map((th) =>
      th.textContent.trim(),
    );
    const rows = [...table.querySelectorAll("tbody tr")].map((tr) =>
      [...tr.querySelectorAll("td")].map(
        (td) => '"' + td.textContent.replace(/"/g, '""') + '"',
      ),
    );
    const csv = [headers.join(","), ...rows.map((r) => r.join(","))].join("\n");
    const blob = new Blob([csv], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "query-results.csv";
    a.click();
    URL.revokeObjectURL(url);
  };

  // ── Sortable table headers: <th class="sort-header" data-col="N"> ──
  document.addEventListener("click", (e) => {
    const th = e.target.closest("th.sort-header");
    if (!th) return;
    const table = th.closest("table.result-table");
    if (!table) return;
    const colIndex = parseInt(th.getAttribute("data-col"));
    if (isNaN(colIndex)) return;

    let state = sortState.get(table);
    if (!state) {
      state = { col: -1, dir: "asc" };
      sortState.set(table, state);
    }
    if (state.col === colIndex) {
      state.dir = state.dir === "asc" ? "desc" : "asc";
    } else {
      state.col = colIndex;
      state.dir = "asc";
    }
    sortTable(table, colIndex, state.dir);
  });

  // ── Show/hide copy button when a SQL <details> block toggles.
  // Capture-phase 'toggle' events do not bubble, so use capture:true — this
  // replaces the old body-wide MutationObserver rescan.
  document.addEventListener(
    "toggle",
    (e) => {
      const block = e.target;
      if (
        !(block instanceof HTMLDetailsElement) ||
        !block.classList.contains("sql-block")
      )
        return;
      const copyBtn = block.querySelector(".copy-sql-btn");
      if (copyBtn) copyBtn.style.display = block.open ? "block" : "none";
    },
    true,
  );
}

// ==================== Table sorting ====================
const sortState = new Map();

function decodeAttr(attr) {
  // Legacy encoding used encodeURIComponent(escape(...)); prefer atob-style
  // fallback chain. Current backend sends plain encodeURIComponent JSON.
  try {
    return decodeURIComponent(attr);
  } catch {
    return attr;
  }
}

function sortTable(tableEl, colIndex, direction) {
  const dataAttr = tableEl.getAttribute("data-sort-rows");
  if (!dataAttr) return;

  try {
    const data = JSON.parse(decodeAttr(dataAttr));
    const rows = data.rows;
    const sorted = [...rows].sort((a, b) => {
      let valA = a[colIndex];
      let valB = b[colIndex];
      if (valA == null) return 1;
      if (valB == null) return -1;
      let comparison = 0;
      const numA = Number(valA);
      const numB = Number(valB);
      if (!isNaN(numA) && !isNaN(numB) && isFinite(numA) && isFinite(numB)) {
        comparison = numA - numB;
      } else {
        comparison = String(valA).localeCompare(String(valB));
      }
      return direction === "asc" ? comparison : -comparison;
    });

    const tbody = tableEl.querySelector("tbody");
    if (!tbody) return;

    const container = tableEl.closest(".results-card");
    const expandBtn = container
      ? container.querySelector(".table-expand-btn")
      : null;
    const isExpanded =
      expandBtn && expandBtn.textContent.includes("Show first 10");
    let collapsedClass = null;
    if (expandBtn) {
      for (const tr of tableEl.querySelectorAll("tr")) {
        for (const cls of tr.classList) {
          if (cls.startsWith("collapsed-row-")) {
            collapsedClass = cls;
            break;
          }
        }
        if (collapsedClass) break;
      }
    }

    tbody.innerHTML = "";

    for (let ri = 0; ri < sorted.length; ri++) {
      const row = sorted[ri];
      const tr = document.createElement("tr");
      let rowClass = "result-row";
      if (collapsedClass && ri >= 10 && !isExpanded) {
        rowClass += " " + collapsedClass;
        tr.style.display = "none";
      }
      tr.className = rowClass;
      for (let i = 0; i < row.length; i++) {
        const td = document.createElement("td");
        const cell = String(row[i]);
        let cellClass = "";
        if (/^-?\d+(\.\d+)?$/.test(cell)) {
          cellClass = "num-cell";
        } else if (/\d{4}-\d{2}-\d{2}/.test(cell)) {
          cellClass = "date-cell";
        }
        td.className = cellClass;
        td.style.border = "1px solid var(--border-primary)";
        td.style.padding = "8px 12px";
        td.style.maxWidth = "400px";
        td.style.overflow = "hidden";
        td.style.textOverflow = "ellipsis";
        td.style.whiteSpace = "nowrap";
        td.title = cell;
        td.textContent = cell;
        tr.appendChild(td);
      }
      tbody.appendChild(tr);
    }

    const headers = tableEl.querySelectorAll("th.sort-header");
    headers.forEach((th, idx) => {
      const indicator = th.querySelector(".sort-indicator");
      if (idx === colIndex) {
        indicator.textContent = direction === "asc" ? " ↑" : " ↓";
        indicator.style.color = "var(--color-accent)";
      } else {
        indicator.textContent = " ↕";
        indicator.style.color = "var(--text-tertiary)";
      }
    });
  } catch (e) {
    console.error("Failed to sort table:", e);
  }
}
