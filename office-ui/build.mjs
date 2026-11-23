import { build } from "esbuild";
import { mkdir, copyFile, readFile, writeFile, cp, readdir } from "node:fs/promises";
import { basename, join, resolve } from "node:path";
const output = resolve(process.argv[2] ?? "../internal/editor/assets/office");
const publicRelease = process.argv.includes("--public-release");
const officeKinds = publicRelease ? ["xlsx"] : ["xlsx", "docx"];
const entryPoints = ["src/main.js", ...officeKinds.map((kind) => "src/" + kind + ".js"), "src/pdf.js"];
await mkdir(output, { recursive: true });
const { metafile } = await build({ metafile: true, entryPoints, outdir: output, bundle: true, format: "esm", splitting: true, minify: true, target: "chrome120", loader: { ".woff2": "file", ".woff": "file", ".ttf": "file", ".png": "file", ".gif": "file", ".svg": "file" }, assetNames: "assets/[name]-[hash]", legalComments: "external", logLevel: "info" });
if (publicRelease) {
  const wordInput = Object.keys(metafile.inputs).find((input) => /(?:^|\/)(?:@superdoc\/|superdoc\/|src\/docx\.js$)/.test(input));
  if (wordInput) {
    throw new Error("Public release includes a Word editor input: " + wordInput);
  }
} else {
  await cp("node_modules/@superdoc/docx-engine/dist/assets", join(output, "assets"), { recursive: true });
}
function preloadViewerChunk(entry, chunk) {
  const output = Object.values(metafile.outputs).find((file) => file.entryPoint === "src/" + entry + ".js");
  const module = output?.imports.find((item) => item.kind === "dynamic-import" && basename(item.path).startsWith(chunk));
  return module ? '<link rel="modulepreload" href="' + basename(module.path) + '">' : "";
}
const template = await readFile("src/index.html", "utf8");
for (const kind of officeKinds) {
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
const officeLicenses = [["Univer", "@univerjs/core/LICENSE"], ["PDF.js", "pdfjs-dist/LICENSE"]];
if (!publicRelease) {
  officeLicenses.push(["SuperDoc", "superdoc/LICENSE"], ["DOCX Engine", "@superdoc/docx-engine/DOCX-ENGINE-LICENSE.md"]);
}
for (const [name, file] of officeLicenses) {
  notices.push(`
## ${name}
` + await readFile(`node_modules/${file}`, "utf8"));
}
if (publicRelease) {
  notices.unshift("EditorX uses a modified copy of Viva. Source and patches: https://github.com/coderyds/editorx\n");
  const files = new Set(["../LICENSE"]);
  const licenseName = /^(?:licen[cs]e|copying|notice|third[-_ ]party[-_ ](?:notices|licenses))(?:[._-].*)?$/i;
  const directories = new Set([pdfPackage, join(pdfPackage, "cmaps"), join(pdfPackage, "iccs"),
    join(pdfPackage, "standard_fonts"), join(pdfPackage, "wasm")]);
  for (const input of Object.keys(metafile.inputs)) {
    const packagePath = input.match(/^.*node_modules\/(?:@[^/]+\/)?[^/]+(?=\/)/)?.[0];
    if (packagePath) {
      directories.add(packagePath);
    }
  }
  for (const directory of directories) {
    for (const item of await readdir(directory, { withFileTypes: true })) {
      if (item.isFile() && licenseName.test(item.name)) {
        files.add(join(directory, item.name));
      }
    }
  }
  for (const directory of ["../vendor", "../internal/editor/assets/terminal", "../internal/editor/assets/ai-providers", output]) {
    for (const item of await readdir(directory, { withFileTypes: true, recursive: true })) {
      if (item.isFile() && (licenseName.test(item.name) || /(?:[-.]LICENSE|\.LEGAL)\.txt$/i.test(item.name))) {
        files.add(join(item.parentPath, item.name));
      }
    }
  }
  for (const file of Array.from(files).sort()) {
    const noticePath = file.startsWith(output) ? basename(file) : file;
    notices.push("\n## " + noticePath.replaceAll("\\", "/") + "\n" + await readFile(file, "utf8"));
  }
}
await writeFile(join(output, "THIRD-PARTY-NOTICES.txt"), notices.join("\n"));
