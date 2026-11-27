import "./style.css";
import { bindDocumentOutline, groupHeadings } from "./document-outline.js";
import { setText, text } from "./i18n.js";
let editor, workbook, api, univer, ready = false, revision = 0, exportedRevision = 0, composing = false, disposed = false, snapshotQueued = false, timer, exporting = Promise.resolve();
let refreshWordOutline;
const requests = new AbortController();
const state = document.querySelector("#state"), notice = document.querySelector("#notice");
const page = document.documentElement;
setText(document.querySelector("#name"), "正在打开…", "Opening…");
function setTheme(light) {
  page.classList.toggle("light", light);
  if (api && api.isDarkMode() === light) {
    api.toggleDarkMode(!light);
  }
}
const send = (message) => window.viva?.postMessage(JSON.stringify(message));
function failure(error, request = 0) {
  const message = error?.message || String(error);
  notice.textContent = message;
  notice.hidden = false;
  send({ kind: "error", message, request });
}
function changed() {
  if (!ready || disposed) {
    return;
  }
  revision++;
  refreshWordOutline?.();
  setText(state, "未保存", "Unsaved");
  send({ kind: "dirty", revision });
  clearTimeout(timer);
  timer = setTimeout(() => snapshot(0), 900);
}
async function snapshot(request) {
  if (disposed || (!request && snapshotQueued)) {
    return exporting;
  }
  if (composing) {
    if (request) {
      failure(new Error(text("请先完成中文选词，再保存或关闭文档", "Finish composing text before saving or closing the document")), request);
    }
    return;
  }
  if (request) {
    clearTimeout(timer);
  } else {
    snapshotQueued = true;
  }
  const execute = async () => {
    if (!request) {
      snapshotQueued = false;
    }
    if (disposed) {
      return;
    }
    try {
      if (composing) {
        throw new Error(text("请先完成中文选词，再保存或关闭文档", "Finish composing text before saving or closing the document"));
      }
      if (request) {
        document.querySelector("#busy").hidden = false;
        if (workbook) {
          await workbook.endEditingAsync(true);
        } else {
          document.activeElement?.blur();
        }
      }
      if (disposed) {
        return;
      }
      const capturedRevision = revision;
      if (capturedRevision === exportedRevision) {
        if (request) {
          send({ kind: "flushed", revision: capturedRevision, request });
        }
        return;
      }
      const body = workbook ? JSON.stringify(workbook.save()) : await editor.export({ triggerDownload: false });
      const result = await fetch(`snapshot?revision=${capturedRevision}`, { method: "POST", body, signal: requests.signal });
      if (!result.ok) {
        throw new Error(await result.text());
      }
      exportedRevision = capturedRevision;
      send({ kind: "snapshot", revision: capturedRevision, request });
    } catch (error) {
      if (!disposed) {
        failure(error, request);
      }
    }
  };
  exporting = exporting.then(execute, execute);
  return exporting;
}
window.office = { setTheme, flush: snapshot, release() {
  document.querySelector("#busy").hidden = true;
}, ack(savedRevision) {
  if (revision === savedRevision) {
    setText(state, "已保存", "Saved");
  }
  notice.hidden = true;
}, get ready() {
  return ready;
}, get workbook() {
  return workbook;
}, get editor() {
  return editor;
} };
window.addEventListener("pagehide", () => {
  disposed = true;
  ready = false;
  clearTimeout(timer);
  requests.abort();
  editor?.destroy();
  univer?.dispose();
  editor = workbook = api = univer = undefined;
}, { once: true });
document.addEventListener("compositionstart", () => {
  composing = true;
});
document.addEventListener("compositionend", () => {
  composing = false;
});
document.addEventListener("keydown", (event) => {
  if (!document.querySelector("#busy").hidden) {
    event.preventDefault();
    event.stopImmediatePropagation();
    return;
  }
  if (!(event.ctrlKey || event.metaKey)) {
    return;
  }
  const key = event.key.toLowerCase();
  const kind = { s: event.shiftKey ? "save-as" : "save", o: "open", n: event.shiftKey ? "new-window" : "new", w: "close", tab: event.shiftKey ? "previous-tab" : "next-tab" }[key];
  if (kind) {
    event.preventDefault();
    event.stopImmediatePropagation();
    send({ kind });
  }
}, true);
document.querySelector("#save").onclick = () => send({ kind: "save" });
document.querySelector("#save-as").onclick = () => send({ kind: "save-as" });
async function documentResponse(path) {
  const response = await fetch(path, { signal: requests.signal });
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return response;
}

try {
  const kind = page.dataset.kind;
  // Load the editor engine and read the local document while the metadata is in flight.
  const [info, engine, documentData] = await Promise.all([
    documentResponse("info").then((response) => response.json()),
    import(new URL(kind + ".js", document.baseURI).href),
    documentResponse("document").then((response) => kind === "xlsx" ? response.json() : response.blob()),
  ]);
  revision = exportedRevision = info.revision || 0;
  const name = document.querySelector("#name");
  name.title = document.title = info.name;
  setText(name, info.name);
  document.querySelector("#editor").dataset.kind = info.kind;
  if (info.kind === "xlsx") {
    const { createUniver, LocaleType, CommandType, ICommandService, sheets, locale, englishLocale } = engine;
    const currentLocale = () => page.lang === "en" ? LocaleType.EN_US : LocaleType.ZH_CN;
    const { UniverSheetsCorePreset } = sheets;
    const unsupported = /^sheet\.command\.(insert-(row|col|multi|range|sheet|defined)|remove-(row|col|sheet|defined)|delete-(row|col|sheet|range)|move-|copy-sheet|duplicate-sheet|append-row|set-(worksheet-name|sheet-name|worksheet-order|worksheet-hide|worksheet-show|defined-name|protection|frozen|tab-color)|add-.*protection|delete-.*protection|cancel-frozen)/;
    const menu = {};
    for (const command of Object.values(sheets)) {
      if (command?.id && unsupported.test(command.id)) {
        menu[command.id] = { hidden: true, disabled: true };
      }
    }
    const { univer: instance, univerAPI } = createUniver({
      darkMode: !page.classList.contains("light"),
      locale: currentLocale(),
      locales: { [LocaleType.ZH_CN]: locale, [LocaleType.EN_US]: englishLocale },
      presets: [UniverSheetsCorePreset({ container: "editor", menu })],
    });
    univer = instance;
    api = univerAPI;
    workbook = api.createWorkbook(documentData);
    const injector = univer.__getInjector();
    const commands = injector.get(ICommandService);
    document.addEventListener("editorx-language", () => api.setLocale(currentLocale()));
    commands.beforeCommandExecuted((command) => {
      if (unsupported.test(command.id)) {
        failure(new Error(text("当前版本暂不支持此工作表结构操作", "This workbook structure operation is unavailable")));
        throw new Error(text("当前版本支持单元格、公式和格式编辑；暂不支持增删或移动工作表、行列。", "You can edit cells, formulas, and formatting. Sheet, row, and column structure changes are unavailable."));
      }
    });
    commands.onCommandExecuted((command, options) => {
      if (command.type === CommandType.MUTATION && command.id.startsWith("sheet.mutation.") && !options?.onlyLocal && !options?.applyFormulaCalculationResult) {
        changed();
      }
    });
    ready = true;
  } else {
    const { SuperDoc, createTheme } = engine;
    const zoom = document.querySelector("#word-zoom");
    const smaller = document.querySelector("#word-smaller");
    const fit = document.querySelector("#word-fit");
    const larger = document.querySelector("#word-larger");
    const minimumZoom = 25, maximumZoom = 400, zoomStep = 10;
    function renderZoom(value) {
      fit.textContent = Math.round(value) + "%";
      smaller.disabled = value <= minimumZoom;
      larger.disabled = value >= maximumZoom;
    }
    smaller.onclick = () => editor.setZoom(Math.max(minimumZoom, Math.round(editor.getZoom()) - zoomStep));
    fit.onclick = () => editor.setZoomMode("fit-width");
    larger.onclick = () => editor.setZoom(Math.min(maximumZoom, Math.round(editor.getZoom()) + zoomStep));
    const officeTheme = createTheme({
      name: "viva",
      radius: "10px",
      colors: {
        action: "var(--viva-action)",
        actionHover: "var(--viva-action-hover)",
        actionText: "var(--viva-action-text)",
        bg: "var(--viva-surface)",
        hoverBg: "var(--viva-hover)",
        activeBg: "var(--viva-selection)",
        disabledBg: "var(--viva-card)",
        text: "var(--viva-text)",
        textMuted: "var(--viva-muted)",
        textDisabled: "var(--viva-disabled)",
        border: "var(--viva-border)",
      },
      vars: {
        "--sd-ui-toolbar-bg": "var(--viva-card)",
        "--sd-layout-page-bg": "#fff",
      },
    });
    page.classList.add(officeTheme);
    const toolbarLabels = {
      bold: ["粗体", "Bold"], "font-family": ["字体", "Font"], "font-size": ["字号", "Font Size"],
      italic: ["斜体", "Italic"], underline: ["下划线", "Underline"], strikethrough: ["删除线", "Strikethrough"],
      "text-color": ["文字颜色", "Text Color"], "highlight-color": ["高亮", "Highlight Color"],
      search: ["查找", "Find"], link: ["链接", "Link"], image: ["插入图片", "Insert Image"],
      table: ["插入表格", "Insert Table"], "table-actions": ["表格操作", "Table Actions"],
      "insert-row-before": ["上方插入行", "Insert Row Above"], "insert-row-after": ["下方插入行", "Insert Row Below"],
      "insert-column-before": ["左侧插入列", "Insert Column Left"], "insert-column-after": ["右侧插入列", "Insert Column Right"],
      "delete-row": ["删除行", "Delete Row"], "delete-column": ["删除列", "Delete Column"], "delete-table": ["删除表格", "Delete Table"],
      "merge-cells": ["合并单元格", "Merge Cells"], "split-cell": ["拆分单元格", "Split Cell"], "text-align": ["对齐", "Align"],
      "bullet-list": ["项目符号", "Bulleted List"], "numbered-list": ["编号", "Numbered List"],
      "indent-decrease": ["减少缩进", "Decrease Indent"], "indent-increase": ["增加缩进", "Increase Indent"],
      zoom: ["缩放", "Zoom"], undo: ["撤销", "Undo"], redo: ["重做", "Redo"],
      "clear-formatting": ["清除格式", "Clear Formatting"], "copy-format": ["格式刷", "Format Painter"], "line-height": ["行距", "Line Spacing"],
      "linked-style-label": ["段落格式", "Paragraph Format"], "linked-style": ["段落样式", "Paragraph Style"],
      ruler: ["标尺", "Ruler"], "measurement-unit": ["度量单位", "Measurement Unit"],
      "document-mode-editing": ["编辑", "Editing"], "document-mode-viewing": ["查看", "Viewing"], "document-mode-suggesting": ["修订", "Suggesting"],
    };
    const strings = Object.fromEntries(Object.entries(toolbarLabels).map(([key, label]) => [key, text(...label)]));
    document.addEventListener("editorx-language", () => {
      for (const [key, labels] of Object.entries(toolbarLabels)) {
        const translated = text(...labels);
        strings[key] = translated;
        const item = editor?.toolbar?.getToolbarItemByName?.(key);
        for (const ref of [item?.defaultLabel, item?.label, item?.tooltip]) {
          if (labels.includes(ref?.value)) ref.value = translated;
        }
      }
    });
    editor = new SuperDoc({ selector: "#editor", document: new File([documentData], info.name, { type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document" }), contained: true, telemetry: { enabled: false }, uiDisplayFallbackFont: "Segoe UI, Microsoft YaHei, sans-serif", ui: { toolbar: { container: "#toolbar", responsiveTo: "container", strings }, comments: false }, onZoomChange: ({ zoom }) => renderZoom(zoom), onReady() {
      ready = true;
      renderZoom(editor?.getZoom() ?? 100);
      zoom.hidden = false;
      refreshWordOutline ??= bindDocumentOutline(async () => {
        const { outline } = await editor.activeEditor.doc.info({});
        return groupHeadings(outline.map(({ nodeId, level, text }) => ({
          key: nodeId,
          level,
          title: text,
          target: nodeId,
        })));
      }, async (nodeId) => {
        const result = await editor.ui.viewport.scrollIntoView({
          target: { kind: "text", blockId: nodeId, range: { start: 0, end: 0 } },
          block: "start",
          behavior: "instant",
        });
        return result.success;
      }, () => text("暂无标题。为段落应用标题样式后，标题会显示在这里。", "Apply heading styles to show paragraphs in the outline."));
      if (info.dirty) setText(state, "未保存", "Unsaved");
      else setText(state, "就绪", "Ready");
      send({ kind: "ready" });
    }, onEditorUpdate: changed, onContentError: ({ error }) => failure(error), onException: (event) => {
      if (event.error) {
        failure(event.error);
      }
    } });
  }
  if (ready) {
    if (info.dirty) setText(state, "未保存", "Unsaved");
    else setText(state, "就绪", "Ready");
    send({ kind: "ready" });
  }
} catch (error) {
  if (!disposed) {
    failure(error);
  }
}
