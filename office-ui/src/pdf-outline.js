import { groupHeadings } from "./document-outline.js";

function bookmarkEntries(bookmarks, parent = "") {
  return bookmarks.map((bookmark, index) => {
    const key = parent + "/" + index;
    return {
      key,
      title: bookmark.title.trim() || "未命名标题",
      target: bookmark.dest,
      children: bookmarkEntries(bookmark.items, key),
    };
  });
}

function markedContentIDs(node) {
  if (node.type === "content") return [node.id];
  return (node.children ?? []).flatMap(markedContentIDs);
}

function taggedHeadings(node, headings, depth = 0) {
  const heading = /^H(\d+)?$/.exec(node.role);
  if (heading) {
    const ids = markedContentIDs(node);
    if (ids.length) headings.push({ level: Number(heading[1]) || Math.max(1, depth), ids });
    return;
  }
  const childDepth = /^(Part|Art|Sect)$/.test(node.role) ? depth + 1 : depth;
  for (const child of node.children ?? []) {
    if (child.role) taggedHeadings(child, headings, childDepth);
  }
}

function readMarkedText(items, wantedIDs) {
  const stack = [], spans = new Map();
  for (const item of items) {
    if (item.type === "endMarkedContent") {
      stack.pop();
    } else if (item.type === "beginMarkedContent" || item.type === "beginMarkedContentProps") {
      stack.push(item.id);
    } else if (item.str) {
      for (const id of stack) {
        if (!wantedIDs.has(id)) continue;
        let span = spans.get(id);
        if (!span) {
          const [, , , , x, y] = item.transform;
          span = { text: "", x, y: y + item.height };
          spans.set(id, span);
        }
        span.text += item.str + (item.hasEOL ? " " : "");
      }
    }
  }
  return spans;
}

export async function readPDFOutline(documentPDF) {
  const bookmarks = await documentPDF.getOutline();
  if (bookmarks?.length) return bookmarkEntries(bookmarks);
  if (!(await documentPDF.getMarkInfo())?.Marked) return [];

  const headings = [];
  for (let pageNumber = 1; pageNumber <= documentPDF.numPages; pageNumber++) {
    const page = await documentPDF.getPage(pageNumber);
    const structure = await page.getStructTree();
    if (!structure) continue;
    const tags = [];
    taggedHeadings(structure, tags);
    if (!tags.length) continue;

    const ids = new Set(tags.flatMap(({ ids }) => ids));
    const { items } = await page.getTextContent({ includeMarkedContent: true });
    const spans = readMarkedText(items, ids);
    for (const { level, ids } of tags) {
      const parts = [...new Set(ids)].map((id) => spans.get(id)).filter(Boolean);
      if (!parts.length) continue;
      const { x, y } = parts[0];
      headings.push({
        key: headings.length,
        level,
        title: parts.map(({ text }) => text).join(" ").replace(/\s+/g, " ").trim(),
        target: [pageNumber - 1, { name: "XYZ" }, x, y, null],
      });
    }
  }
  return groupHeadings(headings);
}
