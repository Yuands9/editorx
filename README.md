# Viva Editor

Viva Editor 是一款基于 Viva 的 Windows 文本编辑器，适合编辑代码、配置和本地文档。一个窗口可以管理多个项目和标签，提供 JSON 工具、Markdown 和 HTML 预览、Office 编辑、PDF 阅读，以及 Git 和 PowerShell。

## 开始使用

本机已有便携版时，运行 `VivaEditor.exe`，按 Ctrl+O 打开文件，按 Ctrl+Shift+O 添加项目文件夹。从源码构建使用 Windows、Go 1.26、Node.js 24 或更新版本和 PowerShell 7。内嵌文档与终端使用 Microsoft Edge WebView2 Runtime。

```powershell
.\dev.ps1 build
.\dist\VivaEditor.exe
```

首次构建按锁定版本准备文档编辑和预览资源，随后生成带图标的 EXE。Go 便携工具链也可放在项目下的 `.dev/go`。开发缓存和验收文件位于 `.dev`。

## 文档

- [使用指南](docs/usage.md)
- [功能说明](docs/features.md)
- [设计与发布说明](docs/design.md)
- [大文件首次打开实测](docs/large-files.md)

## 验证

```powershell
.\dev.ps1 test
.\tools\native-smoke.ps1 -TestName TestNativeOfficeEditing -TimeoutSeconds 100
.\tools\large-open.ps1 -Runs 3
```

最后两条命令在独立 Windows 桌面运行。大文件测试从点击项目树中的文件开始计时，到一次编辑生效或 PDF 首屏渲染结束。

## 分发

主项目的发布许可证由项目所有者确定。Word 功能使用 SuperDoc 和单独授权的 DOCX Engine，包含该组件的 EXE 对外分发前需核对授权。依赖来源与构建边界见[设计与发布说明](docs/design.md#准备发布)。
