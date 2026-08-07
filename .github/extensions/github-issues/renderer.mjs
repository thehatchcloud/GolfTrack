export function renderHtml() {
    return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GitHub Issues</title>
  <style>
    :root { color-scheme: light dark; }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--background-color-default, #fff);
      color: var(--text-color-default, #1f2328);
      font-family: var(--font-sans, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);
      font-size: var(--text-body-medium, 14px);
      line-height: var(--leading-body-medium, 20px);
    }
    button, input, select { font: inherit; }
    a { color: var(--true-color-blue, #0969da); text-decoration: none; }
    a:hover { text-decoration: underline; }
    .shell { max-width: 1440px; margin: 0 auto; padding: 24px; }
    .header { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
    h1 {
      margin: 0;
      font-family: var(--font-sans-display, var(--font-sans, sans-serif));
      font-size: var(--text-title-large, 26px);
      line-height: var(--leading-title-large, 32px);
    }
    .muted { color: var(--text-color-muted, #656d76); }
    .button {
      border: 1px solid var(--border-color-default, #d0d7de);
      border-radius: 6px;
      background: var(--background-color-default, #fff);
      color: var(--text-color-default, #1f2328);
      cursor: pointer;
      padding: 7px 12px;
      font-weight: var(--font-weight-semibold, 600);
    }
    .button:hover { background: color-mix(in srgb, var(--text-color-muted, #656d76) 9%, transparent); }
    .button:disabled { cursor: wait; opacity: .6; }
    .summary {
      display: grid;
      grid-template-columns: repeat(5, minmax(110px, 1fr));
      gap: 12px;
      margin: 24px 0 16px;
    }
    .card {
      border: 1px solid var(--border-color-default, #d0d7de);
      border-radius: 8px;
      padding: 14px 16px;
    }
    .card strong { display: block; margin-top: 3px; font-size: 22px; line-height: 28px; }
    .filters {
      display: grid;
      grid-template-columns: minmax(220px, 2fr) repeat(3, minmax(140px, 1fr));
      gap: 10px;
      margin-bottom: 12px;
    }
    .control {
      width: 100%;
      border: 1px solid var(--border-color-default, #d0d7de);
      border-radius: 6px;
      background: var(--background-color-default, #fff);
      color: var(--text-color-default, #1f2328);
      padding: 8px 10px;
    }
    .table-wrap {
      overflow: auto;
      border: 1px solid var(--border-color-default, #d0d7de);
      border-radius: 8px;
    }
    table { width: 100%; border-collapse: collapse; }
    th {
      position: sticky;
      top: 0;
      z-index: 1;
      background: var(--background-color-default, #fff);
      color: var(--text-color-muted, #656d76);
      font-size: 12px;
      text-align: left;
      text-transform: uppercase;
      letter-spacing: .04em;
    }
    th, td { padding: 11px 12px; border-bottom: 1px solid var(--border-color-default, #d0d7de); vertical-align: top; }
    tbody tr:last-child td { border-bottom: 0; }
    .issue-title { min-width: 300px; font-weight: var(--font-weight-semibold, 600); }
    .issue-number { color: var(--text-color-muted, #656d76); font-weight: 400; margin-left: 5px; }
    .pill, .label {
      display: inline-flex;
      align-items: center;
      border-radius: 999px;
      white-space: nowrap;
      font-size: 12px;
      font-weight: var(--font-weight-semibold, 600);
      line-height: 20px;
    }
    .pill { padding: 1px 8px; }
    .pill-open { background: color-mix(in srgb, var(--true-color-blue, #0969da) 16%, transparent); color: var(--true-color-blue, #0969da); }
    .pill-progress { background: color-mix(in srgb, #bf8700 18%, transparent); color: #9a6700; }
    .pill-review { background: color-mix(in srgb, #8250df 16%, transparent); color: #8250df; }
    .pill-blocked { background: color-mix(in srgb, var(--true-color-red, #cf222e) 16%, transparent); color: var(--true-color-red, #cf222e); }
    .pill-closed { background: color-mix(in srgb, #8250df 16%, transparent); color: #8250df; }
    .labels { display: flex; flex-wrap: wrap; gap: 4px; min-width: 180px; }
    .label { border: 1px solid currentColor; padding: 0 7px; }
    .empty, .error { padding: 48px 24px; text-align: center; }
    .error { color: var(--true-color-red, #cf222e); }
    .footer { display: flex; justify-content: space-between; gap: 12px; margin-top: 10px; font-size: 12px; }
    @media (max-width: 800px) {
      .shell { padding: 16px; }
      .summary { grid-template-columns: repeat(2, 1fr); }
      .filters { grid-template-columns: 1fr 1fr; }
    }
  </style>
</head>
<body>
  <main class="shell">
    <header class="header">
      <div>
        <h1>GitHub Issues</h1>
        <a id="repository" class="muted" target="_blank" rel="noreferrer"></a>
      </div>
      <button id="refresh" class="button" type="button">Refresh</button>
    </header>

    <section class="summary" aria-label="Issue totals">
      <div class="card"><span class="muted">All issues</span><strong id="total">-</strong></div>
      <div class="card"><span class="muted">Open</span><strong id="open">-</strong></div>
      <div class="card"><span class="muted">In progress</span><strong id="in-progress">-</strong></div>
      <div class="card"><span class="muted">Blocked</span><strong id="blocked">-</strong></div>
      <div class="card"><span class="muted">Closed</span><strong id="closed">-</strong></div>
    </section>

    <section class="filters" aria-label="Issue filters">
      <input id="search" class="control" type="search" placeholder="Search issues">
      <select id="status" class="control"><option value="">All statuses</option></select>
      <select id="assignee" class="control"><option value="">All assignees</option></select>
      <select id="label" class="control"><option value="">All labels</option></select>
    </section>

    <div id="content" class="table-wrap"><div class="empty muted">Loading issues...</div></div>
    <footer class="footer muted">
      <span id="visible"></span>
      <span id="updated"></span>
    </footer>
  </main>
  <script>
    const state = { issues: [], snapshot: null };
    const elements = Object.fromEntries(
      ["repository", "refresh", "total", "open", "in-progress", "blocked", "closed",
       "search", "status", "assignee", "label", "content", "visible", "updated"]
        .map((id) => [id, document.getElementById(id)])
    );

    function addOptions(select, values, emptyLabel) {
      const selected = select.value;
      select.replaceChildren(new Option(emptyLabel, ""));
      for (const value of values) select.add(new Option(value, value));
      if (values.includes(selected)) select.value = selected;
    }

    function statusClass(status) {
      return {
        "Open": "pill-open",
        "In progress": "pill-progress",
        "In review": "pill-review",
        "Blocked": "pill-blocked",
        "Closed": "pill-closed"
      }[status] || "pill-open";
    }

    function labelTextColor(hex) {
      if (!/^[0-9a-f]{6}$/i.test(hex)) return "var(--text-color-default, #1f2328)";
      const [r, g, b] = [0, 2, 4].map((offset) => parseInt(hex.slice(offset, offset + 2), 16));
      return (r * 299 + g * 587 + b * 114) / 1000 > 145 ? "#1f2328" : "#ffffff";
    }

    function makeCell(row, className) {
      const cell = document.createElement("td");
      if (className) cell.className = className;
      row.append(cell);
      return cell;
    }

    function renderRows(issues) {
      if (!issues.length) {
        elements.content.innerHTML = '<div class="empty muted">No issues match these filters.</div>';
        return;
      }

      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Issue</th><th>Status</th><th>Assignees</th><th>Labels</th><th>Updated</th></tr></thead>";
      const body = document.createElement("tbody");

      for (const issue of issues) {
        const row = document.createElement("tr");
        const titleCell = makeCell(row, "issue-title");
        const link = document.createElement("a");
        link.href = issue.url;
        link.target = "_blank";
        link.rel = "noreferrer";
        link.textContent = issue.title;
        const number = document.createElement("span");
        number.className = "issue-number";
        number.textContent = "#" + issue.number;
        titleCell.append(link, number);

        const statusCell = makeCell(row);
        const status = document.createElement("span");
        status.className = "pill " + statusClass(issue.workflowStatus);
        status.textContent = issue.workflowStatus;
        statusCell.append(status);

        const assigneeCell = makeCell(row);
        assigneeCell.textContent = issue.assignees.length
          ? issue.assignees.map((assignee) => "@" + assignee.login).join(", ")
          : "Unassigned";
        if (!issue.assignees.length) assigneeCell.className = "muted";

        const labelCell = makeCell(row, "labels");
        for (const item of issue.labels) {
          const label = document.createElement("span");
          label.className = "label";
          label.textContent = item.name;
          if (/^[0-9a-f]{6}$/i.test(item.color)) {
            label.style.backgroundColor = "#" + item.color;
            label.style.borderColor = "#" + item.color;
            label.style.color = labelTextColor(item.color);
          }
          labelCell.append(label);
        }

        const updatedCell = makeCell(row, "muted");
        updatedCell.textContent = new Intl.DateTimeFormat(undefined, {
          dateStyle: "medium"
        }).format(new Date(issue.updatedAt));
        body.append(row);
      }
      table.append(body);
      elements.content.replaceChildren(table);
    }

    function applyFilters() {
      const query = elements.search.value.trim().toLowerCase();
      const issues = state.issues.filter((issue) => {
        const searchable = [issue.title, String(issue.number), issue.author?.login || "",
          ...issue.labels.map((item) => item.name),
          ...issue.assignees.map((item) => item.login)].join(" ").toLowerCase();
        return (!query || searchable.includes(query))
          && (!elements.status.value || issue.workflowStatus === elements.status.value)
          && (!elements.assignee.value || (elements.assignee.value === "__unassigned__"
            ? issue.assignees.length === 0
            : issue.assignees.some((item) => item.login === elements.assignee.value)))
          && (!elements.label.value || issue.labels.some((item) => item.name === elements.label.value));
      });
      renderRows(issues);
      elements.visible.textContent = "Showing " + issues.length + " of " + state.issues.length + " issues";
    }

    async function load(force = false) {
      elements.refresh.disabled = true;
      elements.refresh.textContent = "Refreshing...";
      try {
        const response = await fetch("/api/issues" + (force ? "?refresh=1" : ""));
        const snapshot = await response.json();
        if (!response.ok) throw new Error(snapshot.error || "Unable to load issues");

        state.snapshot = snapshot;
        state.issues = snapshot.issues;
        elements.repository.textContent = snapshot.repository.nameWithOwner;
        elements.repository.href = snapshot.repository.url + "/issues";
        for (const [id, key] of [["total", "total"], ["open", "open"],
          ["in-progress", "inProgress"], ["blocked", "blocked"], ["closed", "closed"]]) {
          elements[id].textContent = snapshot.summary[key];
        }
        addOptions(elements.status,
          [...new Set(state.issues.map((issue) => issue.workflowStatus))].sort(),
          "All statuses");
        addOptions(elements.assignee,
          ["__unassigned__", ...new Set(state.issues.flatMap((issue) => issue.assignees.map((item) => item.login)))],
          "All assignees");
        elements.assignee.options[1]?.text === "__unassigned__" &&
          (elements.assignee.options[1].text = "Unassigned");
        addOptions(elements.label,
          [...new Set(state.issues.flatMap((issue) => issue.labels.map((item) => item.name)))].sort(),
          "All labels");
        elements.updated.textContent = "Updated " + new Intl.DateTimeFormat(undefined, {
          dateStyle: "medium", timeStyle: "short"
        }).format(new Date(snapshot.fetchedAt));
        applyFilters();
      } catch (error) {
        elements.content.innerHTML = '<div class="error"></div>';
        elements.content.firstElementChild.textContent = error.message;
      } finally {
        elements.refresh.disabled = false;
        elements.refresh.textContent = "Refresh";
      }
    }

    for (const id of ["search", "status", "assignee", "label"]) {
      elements[id].addEventListener(id === "search" ? "input" : "change", applyFilters);
    }
    elements.refresh.addEventListener("click", () => load(true));
    new EventSource("/events").addEventListener("refresh", () => load());
    load();
  </script>
</body>
</html>`;
}
