import "./style.css";
let editor, workbook, api, univer, ready = false, revision = 0, exportedRevision = 0, composing = false, disposed = false, snapshotQueued = false, timer, exporting = Promise.resolve();
const requests = new AbortController();
const state = document.querySelector("#state"), notice = document.querySelector("#notice");
const page = document.documentElement;
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
  state.textContent = "未保存";
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
      failure(new Error("请先完成中文选词，再保存或关闭文档"), request);
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
        throw new Error("请先完成中文选词，再保存或关闭文档");
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
    state.textContent = "已保存";
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
  document.querySelector("#name").textContent = info.name;
  document.querySelector("#editor").dataset.kind = info.kind;
  if (info.kind === "xlsx") {
    const { createUniver, LocaleType, CommandType, ICommandService, sheets, locale } = engine;
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
      locale: LocaleType.ZH_CN,
      locales: { [LocaleType.ZH_CN]: locale },
      presets: [UniverSheetsCorePreset({ container: "editor", menu })],
    });
    univer = instance;
    api = univerAPI;
    workbook = api.createWorkbook(documentData);
    const commands = univer.__getInjector().get(ICommandService);
    commands.beforeCommandExecuted((command) => {
      if (unsupported.test(command.id)) {
        failure(new Error("当前版本暂不支持此工作表结构操作"));
        throw new Error("当前版本支持单元格、公式和格式编辑；暂不支持增删或移动工作表、行列。");
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
    editor = new SuperDoc({ selector: "#editor", document: new File([documentData], info.name, { type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document" }), contained: true, telemetry: { enabled: false }, uiDisplayFallbackFont: "Segoe UI, Microsoft YaHei, sans-serif", ui: { toolbar: { container: "#toolbar", responsiveTo: "container", strings: { bold: "粗体", "font-family": "字体", "font-size": "字号", italic: "斜体", underline: "下划线", strikethrough: "删除线", "text-color": "文字颜色", "highlight-color": "高亮", search: "查找", link: "链接", image: "插入图片", table: "插入表格", "table-actions": "表格操作", "insert-row-before": "上方插入行", "insert-row-after": "下方插入行", "insert-column-before": "左侧插入列", "insert-column-after": "右侧插入列", "delete-row": "删除行", "delete-column": "删除列", "delete-table": "删除表格", "merge-cells": "合并单元格", "split-cell": "拆分单元格", "text-align": "对齐", "bullet-list": "项目符号", "numbered-list": "编号", "indent-decrease": "减少缩进", "indent-increase": "增加缩进", zoom: "缩放", undo: "撤销", redo: "重做", "clear-formatting": "清除格式", "copy-format": "格式刷", "line-height": "行距", "linked-style-label": "段落格式", "linked-style": "段落样式", ruler: "标尺", "measurement-unit": "度量单位", "document-mode-editing": "编辑", "document-mode-viewing": "查看", "document-mode-suggesting": "修订" } }, comments: false }, onReady() {
      ready = true;
      state.textContent = info.dirty ? "未保存" : "就绪";
      send({ kind: "ready" });
    }, onEditorUpdate: changed, onContentError: ({ error }) => failure(error), onException: (event) => {
      if (event.error) {
        failure(event.error);
      }
    } });
  }
  if (ready) {
    state.textContent = info.dirty ? "未保存" : "就绪";
    send({ kind: "ready" });
  }
} catch (error) {
  if (!disposed) {
    failure(error);
  }
}
