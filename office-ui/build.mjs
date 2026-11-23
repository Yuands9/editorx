import { build } from "esbuild";
import { mkdir, copyFile, readFile, writeFile, cp } from "node:fs/promises";
import { basename, join, resolve } from "node:path";
const output = resolve(process.argv[2] ?? "../internal/editor/assets/office");
await mkdir(output, { recursive: true });
const { metafile } = await build({ metafile: true, entryPoints: ["src/main.js", "src/xlsx.js", "src/docx.js", "src/pdf.js"], outdir: output, bundle: true, format: "esm", splitting: true, minify: true, target: "chrome120", loader: { ".woff2": "file", ".woff": "file", ".ttf": "file", ".png": "file", ".gif": "file", ".svg": "file" }, assetNames: "assets/[name]-[hash]", legalComments: "external", logLevel: "info" });
await cp("node_modules/@superdoc/docx-engine/dist/assets", join(output, "assets"), { recursive: true });
function preloadViewerChunk(entry, chunk) {
  const output = Object.values(metafile.outputs).find((file) => file.entryPoint === "src/" + entry + ".js");
  const module = output?.imports.find((item) => item.kind === "dynamic-import" && basename(item.path).startsWith(chunk));
  return module ? '<link rel="modulepreload" href="' + basename(module.path) + '">' : "";
}
const template = await readFile("src/index.html", "utf8");
for (const kind of ["xlsx", "docx"]) {
  const page = template
    .replace('<html lang="zh-CN">', '<html lang="zh-CN" data-kind="' + kind + '">')
    .replace("</head>", '<link rel="modulepreload" href="' + kind + '.js"><link rel="stylesheet" href="' + kind + '.css">' +
      (kind === "docx" ? preloadViewerChunk(kind, "docx-engine.") : "") + "</head>");
  await writeFile(join(output, kind + ".html"), page);
}
const pdfPage = await readFile("src/pdf.html", "utf8");
await writeFile(join(output, "pdf.html"), pdfPage.replace("</head>", preloadViewerChunk("pdf", "pdf_viewer-") + "</head>"));
const pdfPackage = "node_modules/pdfjs-dist";
const pdfResources = join(output, "pdf-resources");
await mkdir(pdfResources, { recursive: true });
await copyFile(join(pdfPackage, "build/pdf.worker.min.mjs"), join(pdfResources, "pdf.worker.min.mjs"));
for (const directory of ["cmaps", "iccs", "standard_fonts", "wasm"]) {
  await cp(join(pdfPackage, directory), join(pdfResources, directory), { recursive: true });
}
await cp(join(pdfPackage, "web/images"), join(pdfResources, "images"), { recursive: true });
const notices = ["Bundled document viewing and editing components. Full component notices are retained below.\n"];
for (const [name, file] of [["SuperDoc", "superdoc/LICENSE"], ["DOCX Engine", "@superdoc/docx-engine/DOCX-ENGINE-LICENSE.md"], ["Univer", "@univerjs/core/LICENSE"], ["PDF.js", "pdfjs-dist/LICENSE"]]) {
  notices.push(`
## ${name}
` + await readFile(`node_modules/${file}`, "utf8"));
}
await writeFile(join(output, "THIRD-PARTY-NOTICES.txt"), notices.join("\n"));
