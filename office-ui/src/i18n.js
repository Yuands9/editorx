const attributeNames = {
  text: "textContent",
  title: "title",
  label: "aria-label",
  placeholder: "placeholder",
};
const controls = [];
for (const [tag, property] of Object.entries(attributeNames)) {
  const marker = `data-en-${tag}`;
  for (const node of document.querySelectorAll(`[${marker}]`)) {
    const chinese = property === "textContent" ? node.textContent : node.getAttribute(property);
    controls.push([node, property, chinese, node.getAttribute(marker)]);
  }
}
const dynamic = new Map();
let english = false;

export function text(chinese, translated) {
  return english ? translated : chinese;
}

export function setText(node, chinese, translated = chinese) {
  dynamic.set(node, [chinese, translated]);
  node.textContent = text(chinese, translated);
}

export function setLanguage(language) {
  english = language === "en";
  document.documentElement.lang = english ? "en" : "zh-CN";
  for (const [node, property, chinese, translated] of controls) {
    if (property === "textContent") {
      if (!dynamic.has(node)) node.textContent = text(chinese, translated);
    } else {
      node.setAttribute(property, text(chinese, translated));
    }
  }
  for (const [node, [chinese, translated]] of dynamic) {
    node.textContent = text(chinese, translated);
  }
  document.dispatchEvent(new Event("editorx-language"));
}

window.editorxSetLanguage = setLanguage;
setLanguage(new URLSearchParams(location.search).get("lang"));
