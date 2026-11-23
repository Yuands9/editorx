import "./document-outline.css";

export function groupHeadings(headings) {
  const roots = [], parents = [];
  for (const heading of headings) {
    const title = heading.title?.trim();
    if (!title) continue;
    while (parents.length && parents.at(-1).level >= heading.level) {
      parents.pop();
    }
    const entry = { ...heading, title, children: [] };
    (parents.at(-1)?.children ?? roots).push(entry);
    parents.push(entry);
  }
  return roots;
}

export function bindDocumentOutline(readEntries, navigate, emptyMessage) {
  const toggle = document.getElementById("outline-toggle");
  const panel = document.getElementById("document-outline");
  const status = document.getElementById("outline-status");
  const list = document.getElementById("outline-entries");
  const collapsed = new Set();
  const buttons = new Map();
  let selected, navigation = 0, dirty = true, reading = false, timer;

  function showStatus(text) {
    status.textContent = text;
    status.hidden = !text;
  }

  function drawEntries(entries, depth = 0) {
    const group = document.createElement("ul");
    for (const entry of entries) {
      const item = document.createElement("li");
      const row = document.createElement("div");
      row.className = "outline-row";
      row.style.paddingInlineStart = Math.min(depth, 8) * 12 + "px";
      const heading = document.createElement("button");
      heading.className = "outline-entry";
      heading.textContent = heading.title = entry.title;
      heading.disabled = entry.target == null;
      buttons.set(entry.key, heading);
      if (entry.key === selected) heading.setAttribute("aria-current", "location");
      heading.onclick = async () => {
        const request = ++navigation;
        try {
          const found = await navigate(entry.target);
          if (request !== navigation) return;
          if (found === false) {
            showStatus("无法跳转到此标题。");
            return;
          }
          showStatus("");
          buttons.get(selected)?.removeAttribute("aria-current");
          selected = entry.key;
          buttons.get(selected)?.setAttribute("aria-current", "location");
        } catch {
          if (request === navigation) showStatus("无法跳转到此标题。");
        }
      };

      if (entry.children.length) {
        const children = drawEntries(entry.children, depth + 1);
        const disclosure = document.createElement("button");
        disclosure.className = "outline-disclosure";
        disclosure.textContent = "›";
        function updateDisclosure() {
          children.hidden = collapsed.has(entry.key);
          disclosure.setAttribute("aria-expanded", String(!children.hidden));
          disclosure.setAttribute("aria-label", (children.hidden ? "展开 " : "收起 ") + entry.title);
        }
        disclosure.onclick = () => {
          if (children.hidden) collapsed.delete(entry.key);
          else collapsed.add(entry.key);
          updateDisclosure();
        };
        updateDisclosure();
        row.append(disclosure, heading);
        item.append(row, children);
      } else {
        row.append(heading);
        item.append(row);
      }
      group.append(item);
    }
    return group;
  }

  async function refresh() {
    if (panel.hidden || reading || !dirty) return;
    dirty = false;
    reading = true;
    if (!list.firstChild) showStatus("正在读取标题…");
    try {
      const entries = await readEntries();
      const scroll = panel.scrollTop;
      buttons.clear();
      list.replaceChildren();
      if (entries.length) list.append(drawEntries(entries));
      panel.scrollTop = scroll;
      showStatus(entries.length ? "" : emptyMessage);
    } catch {
      dirty = true;
      showStatus("无法读取标题。关闭标题列表后可重新打开。");
      return;
    } finally {
      reading = false;
    }
    if (dirty) scheduleRefresh();
  }

  function scheduleRefresh() {
    clearTimeout(timer);
    if (!panel.hidden) timer = setTimeout(refresh, 160);
  }

  toggle.onclick = () => {
    panel.hidden = !panel.hidden;
    toggle.setAttribute("aria-expanded", String(!panel.hidden));
    toggle.title = panel.hidden ? "显示标题列表" : "收起标题列表";
    if (!panel.hidden) void refresh();
  };
  toggle.disabled = false;
  toggle.hidden = false;
  window.addEventListener("pagehide", () => clearTimeout(timer), { once: true });
  return () => {
    dirty = true;
    scheduleRefresh();
  };
}
