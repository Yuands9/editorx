# Viva Editor 设计与发布说明

Viva Editor 使用 Go 和 Viva 管理 Windows 窗口、编辑区及应用状态。文本编辑走原生控件，文档预览和富文档编辑在同一窗口内使用 WebView2。

## 模块划分

| 区域 | 主要入口 | 职责 |
| --- | --- | --- |
| 应用与窗口 | `main.go`、`view.go`、`theme.go` | 原生窗口配置、打开路径分派、快捷键、主题和页面布局 |
| 编辑与文件 | `pane.go`、`document.go`、`format.go`、`syntax_languages.go` | 文本模型、输入法、语法检测、编码、保存与磁盘冲突检查 |
| 标签和项目 | `tab_strip.go`、`tab_actions.go`、`tab_groups.go`、`workspace.go` | 标签、拖放、分组、固定保护、项目树与目录操作 |
| 搜索与 JSON | `search.go`、`search_dialog.go`、`search_results.go`、`json_tools.go`、`json_diff.go` | 文本搜索、匹配定位、JSON 转换和结构对比 |
| 预览 | `html_preview.go`、`markdown.go`、`document_preview.go`、`pdf_preview.go`、`image_view.go` | 文本及媒体预览、资源加载、生命周期和主题 |
| Office | `office.go`、`office_workbook.go`、`document_server.go`、`office-ui/src` | 工作簿转换、文档桥接、按需加载、编辑快照与原格式保存 |
| 本地会话 | `session.go`、`session_windows.go`、`history.go` | 窗口与编辑草稿恢复、数据目录写入锁、最近打开记录 |
| AI | `ai.go`、`ai_provider.go`、`ai_model_picker.go`、`ai_session.go` | 服务商、配置、模型、对话状态与审阅应用 |
| 项目工具 | `project_tools.go`、`project_git_windows.go`、`shell_view.go`、`terminal_windows.go` | Git 子进程、本机 ConPTY、内嵌终端与文件树联动 |

## 打开、编辑与保存

选择路径后按格式进入相应标签。纯文本在 Go 端加载为可编辑缓冲。Office 标签启动本地编辑页面，同时读取文档和加载编辑前端。Excel 工作簿在本地请求处理中解析，Office 前端通过消息桥通知就绪和修改。PDF 通过本地服务读取原文件并渲染可见页。HTML 默认使用编辑缓冲生成的预览副本，以原路径为相对资源基准。

保存文本时先核对磁盘基线，再用同目录临时文件替换目标。Office 保存前等待最近一次编辑快照。Excel 保存会基于原工作簿应用改动。Word 由编辑组件导出 DOCX。PDF 原件另存沿用原始字节流。关闭窗口前把已提交编辑写入会话，后台也按间隔自动保存。

## 嵌入页面与本地数据

Office 和 PDF 使用独立的回环地址，并通过文档会话限制请求路径。前端资源在构建时嵌入 EXE。关闭相关标签会终止本地服务并释放 WebView。HTML 预览执行页面脚本，终端前端从嵌入资源加载，PowerShell 由 ConPTY 提供。

会话、AI 配置、WebView 缓存和预览副本属于本地数据。Windows 使用 DPAPI 保护已保存的密钥及自定义请求头。AI 服务使用提示和已选上下文，生成的改动经过应用内审阅。

## 构建和开发

在 Windows 的 PowerShell 7 中执行以下命令。Go 版本以 `go.mod` 为准。Node.js 用于从锁定的 npm 依赖生成嵌入资源。优先使用项目下的便携 Go，找不到时使用 PATH 中的 Go。

```powershell
.\dev.ps1 build
.\dev.ps1 build -RebuildAssets
.\dev.ps1 test
.\dev.ps1 preview
.\tools\large-open.ps1 -Runs 3
```

预览截图写入 `.dev/previews`，EXE 默认写入 `dist`。构建时会带上 Office/PDF、终端及服务商图标的第三方声明。Go 依赖使用 `-mod=vendor`，`vendor` 中保留了项目使用的 Viva 补丁。升级 Viva 时应同时检查 `patches` 并跑原生测试。

## 准备发布

GitHub 源码仓库包括 Go 文件、前端源文件、依赖锁、vendored Go 源码、补丁、文档和示例。构建输出、生成的 Web 资源、`node_modules`、工具缓存及会话数据由本地流程管理。将 `.dev` 或 `dist` 整个目录上传会把旧程序和本机数据一并带入发布内容。

| 依赖或资源 | 许可位置 |
| --- | --- |
| Viva 与 Go 依赖 | `vendor` 各模块的 LICENSE 和 NOTICE |
| Univer、SuperDoc、DOCX Engine 与 PDF.js | 构建生成的 `THIRD-PARTY-NOTICES.txt` 及 npm 包内声明 |
| 终端和助手服务商标识 | `assets/terminal` 与 `assets/ai-providers` 的许可文件 |

项目所有者决定源码仓库的发布许可证。若要分发带 Word 功能的 EXE，应核对 SuperDoc 与其独立 DOCX Engine 的适用许可或书面授权。[DOCX Engine 许可](https://docs.superdoc.dev/resources/docx-engine-license/) · [GitHub 仓库许可说明](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/licensing-a-repository)
