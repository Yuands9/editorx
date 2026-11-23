import * as pdfjs from "pdfjs-dist";
import "./pdf-view.css";
import { bindDocumentOutline } from "./document-outline.js";
import { readPDFOutline } from "./pdf-outline.js";

const element = (name) => document.getElementById(name);
const send = (kind, text = "") => window.viva?.postMessage(JSON.stringify({kind, text}));
const name = element("name");
const options = new URLSearchParams(location.search);
const title = options.get("title") || name.textContent;
name.textContent = name.title = document.title = title;
const message = element("message");
const page = element("page");
const container = element("viewer-container");
const find = element("find");
const query = element("find-query");
const count = element("find-count");
const passwordForm = element("password-form");
const password = element("password");
const assets = new URL("pdf-resources/", document.baseURI);
const localAsset = (path) => new URL(path, assets).href;

globalThis.pdfjsLib = pdfjs;
pdfjs.GlobalWorkerOptions.workerSrc = localAsset("pdf.worker.min.mjs");
const task = pdfjs.getDocument({
  url: new URL("document.pdf", document.baseURI).href,
  cMapUrl: localAsset("cmaps/"),
  cMapPacked: true,
  standardFontDataUrl: localAsset("standard_fonts/"),
  iccUrl: localAsset("iccs/"),
  wasmUrl: localAsset("wasm/"),
  isEvalSupported: false,
});
let unlock, viewer, disposed = false;
task.onPassword = (update, reason) => {
  unlock = update;
  message.textContent = reason === pdfjs.PasswordResponses.INCORRECT_PASSWORD ? "密码有误，请重试" : "此 PDF 需要打开密码";
  passwordForm.hidden = false;
  password.focus();
};
passwordForm.onsubmit = (event) => {
  event.preventDefault();
  unlock?.(password.value);
  password.value = "";
  passwordForm.hidden = true;
};
window.addEventListener("pagehide", () => {
  disposed = true;
  viewer?.setDocument(null);
  void task.destroy();
}, {once: true});
const external = element("external");
external.hidden = options.has("temporary");
external.onclick = () => send("external");
element("save-copy").onclick = () => send("save-copy");

try {
  const [documentPDF, components] = await Promise.all([task.promise, import("pdfjs-dist/web/pdf_viewer.mjs")]);
  if (!disposed) {
    const { EventBus, FindState, LinkTarget, PDFLinkService, PDFFindController, PDFViewer } = components;
    const events = new EventBus();
    const links = new PDFLinkService({eventBus: events, externalLinkTarget: LinkTarget.BLANK});
    const search = new PDFFindController({eventBus: events, linkService: links});
    viewer = new PDFViewer({
      container, eventBus: events, linkService: links, findController: search,
      removePageBorders: true, imageResourcesPath: localAsset("images/"),
      annotationMode: pdfjs.AnnotationMode.ENABLE, annotationEditorMode: pdfjs.AnnotationEditorType.DISABLE,
      enableAutoLinking: false,
    });
    links.setViewer(viewer);
    links.setDocument(documentPDF);
    const go = (delta) => {
      viewer.currentPageNumber = Math.min(documentPDF.numPages, Math.max(1, viewer.currentPageNumber + delta));
    };
    element("back").onclick = () => go(-1);
    element("forward").onclick = () => go(1);
    page.onchange = () => {
      const value = Math.round(Number(page.value)) || viewer.currentPageNumber;
      viewer.currentPageNumber = Math.max(1, Math.min(documentPDF.numPages, value));
      page.value = viewer.currentPageNumber;
    };
    element("smaller").onclick = () => viewer.decreaseScale();
    element("larger").onclick = () => viewer.increaseScale();
    element("fit").onclick = () => { viewer.currentScaleValue = "page-width"; };
    events.on("pagesinit", () => {
      viewer.currentScaleValue = "auto";
      page.max = documentPDF.numPages;
      page.disabled = false;
      element("total").textContent = "/ " + documentPDF.numPages;
      element("forward").disabled = documentPDF.numPages === 1;
      for (const control of ["smaller", "fit", "larger"]) {
        element(control).disabled = false;
      }
      bindDocumentOutline(() => readPDFOutline(documentPDF),
        (destination) => links.goToDestination(destination),
        "这份 PDF 没有书签或已标记的标题。");
    });
    events.on("pagechanging", ({pageNumber}) => {
      page.value = pageNumber;
      element("back").disabled = pageNumber <= 1;
      element("forward").disabled = pageNumber >= documentPDF.numPages;
    });
    events.on("scalechanging", ({scale}) => {
      element("fit").textContent = Math.round(scale * 100) + "%";
    });
    events.on("pagerendered", ({error, cssTransform}) => {
      if (error) {
        message.hidden = false;
        message.textContent = error.message;
        send("error", error.message);
      } else if (!cssTransform && !message.hidden) {
        message.hidden = true;
        send("ready");
      }
    });
    const openFind = (open) => {
      find.hidden = !open;
      element("find-toggle").setAttribute("aria-expanded", open);
      if (open) {
        query.focus();
        query.select();
      } else {
        events.dispatch("findbarclose", {source: find});
        container.focus();
      }
    };
    const findText = (type = "", previous = false) => events.dispatch("find", {
      source: find, type, query: query.value, caseSensitive: false, entireWord: false,
      highlightAll: true, findPrevious: previous, matchDiacritics: false,
    });
    element("find-toggle").onclick = () => openFind(find.hidden);
    element("find-close").onclick = () => openFind(false);
    element("find-previous").onclick = () => findText("again", true);
    element("find-next").onclick = () => findText("again");
    query.oninput = () => findText();
    query.onkeydown = (event) => {
      if (event.key === "Enter" && !event.isComposing) {
        event.preventDefault();
        findText("again", event.shiftKey);
      }
    };
    const showCount = ({current, total}) => {
      count.textContent = total ? current + " / " + total : "";
    };
    events.on("updatefindmatchescount", ({matchesCount}) => showCount(matchesCount));
    events.on("updatefindcontrolstate", ({state, matchesCount}) => {
      if (state === FindState.NOT_FOUND) {
        count.textContent = "无匹配";
      } else {
        showCount(matchesCount);
      }
    });
    document.addEventListener("keydown", (event) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
        event.preventDefault();
        openFind(true);
      } else if (event.key === "Escape" && !find.hidden) {
        openFind(false);
      }
    });
    viewer.setDocument(documentPDF);
  }
} catch (error) {
  if (!disposed) {
    message.textContent = error?.message || String(error);
    send("error", message.textContent);
  }
}
