# EditorX 设计与发布说明

EditorX 使用 Go 和 Viva 管理 Windows 窗口、编辑区及应用状态。文本编辑走原生控件，文档预览和富文档编辑在同一窗口内使用 WebView2。

## 模块划分

程序从 `cmd/editorx/main.go` 启动。下表中的 Go 文件位于 `internal/editor`，测试与对应实现放在同一包中。内嵌资源位于 `internal/editor/assets`，测试素材位于 `internal/editor/testdata`，构建与打包脚本位于 `tools`。

| 区域 | 主要入口 | 职责 |
| --- | --- | --- |
| 应用与窗口 | `editor.go`、`view.go`、`theme.go` | 原生窗口配置、打开路径分派、快捷键、主题和页面布局 |
| 编辑与文件 | `pane.go`、`document.go`、`format.go`、`syntax_languages.go` | 文本模型、输入法、语法检测、编码、保存与磁盘冲突检查 |
| 标签和项目 | `tab_strip.go`、`tab_actions.go`、`tab_groups.go`、`workspace.go` | 标签、拖放、分组、固定与批量关闭、项目树与目录操作 |
| 搜索与 JSON | `search.go`、`search_replace.go`、`search_results.go`、`json_tools.go`、`json_diff.go` | 多行查找、匹配定位、跨文件替换、JSON 转换和结构对比 |
| 预览 | `html_preview.go`、`markdown.go`、`document_preview.go`、`pdf_preview.go`、`image_view.go` | 文本及媒体预览、资源加载、生命周期和主题 |
| Office | `office.go`、`office_workbook.go`、`document_server.go`、`office-ui/src` | 工作簿转换、文档桥接、按需加载、编辑快照与原格式保存 |
| 全局设置 | `settings_view.go`、`application_settings.go`、`editor_preferences.go`、`ai_settings_view.go` | 设置入口、主题、窗口行为、编辑器默认值、AI 模型配置和对话偏好 |
| 后台和启动 | `desktop.go`、`activation_windows.go` | Viva 系统托盘、隐藏与退出、同一数据目录内唤起已有进程 |
| 本地会话 | `session.go`、`session_windows.go`、`history.go` | 窗口与编辑草稿恢复、数据目录写入锁、最近打开记录 |
| AI | `ai.go`、`ai_provider.go`、`ai_model_picker.go`、`ai_session.go` | 服务商、配置、模型、对话状态与审阅应用 |
| 项目工具 | `project_tools.go`、`project_git_windows.go`、`shell_view.go`、`terminal_windows.go` | Git 子进程、本机 ConPTY、内嵌终端与文件树联动 |

## 打开、编辑与保存

选择路径后按格式进入相应标签。纯文本在 Go 端加载为可编辑缓冲。Office 标签启动本地编辑页面，同时读取文档和加载编辑前端。Excel 工作簿在本地请求处理中解析，Office 前端通过消息桥通知就绪和修改。PDF 通过本地服务读取原文件并渲染可见页。HTML 默认使用编辑缓冲生成的预览副本，以原路径为相对资源基准。

保存文本时先核对磁盘基线，再用同目录临时文件替换目标。Office 保存前等待最近一次编辑快照。Excel 保存会基于原工作簿应用改动。Word 由编辑组件导出 DOCX。PDF 原件另存沿用原始字节流。退出程序前把已提交编辑写入会话，后台也按间隔自动保存。主题、关闭行为、托盘开关和编辑器默认值保存在数据目录的 `application-settings.json`。重复启动通过 Windows 消息唤起同一数据目录的 EditorX，命令行中的文件和目录交给已打开的窗口。

## 嵌入页面与本地数据

HTTP 或 HTTPS 链接由后台请求下载到数据目录的 `downloads` 下，再通过相应的文件格式处理器打开。ZIP 会话备份先在后台准备文件和项目，再合并到当前窗口。

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

预览截图写入 `.dev/previews`，EXE 默认写入 `dist`。构建时会带上 Office/PDF、终端及服务商图标的第三方声明。Go 依赖使用 `-mod=vendor`，`vendor` 中保留了项目使用的 Viva 补丁。升级 Viva 时应同时检查 `patches`，保留修改声明，并跑原生测试。

## 发布范围

EditorX 的自有源码采用 [Apache-2.0](../LICENSE)。GitHub 仓库提供 Go 和前端源码、依赖锁、vendored Go 源码、补丁、文档和示例。公开发行文件位于 [GitHub Releases](https://github.com/Yuands9/editorx/releases/latest)，提供 Windows x64 免安装 EXE 和第三方许可声明。构建输出、生成的 Web 资源、`node_modules`、工具缓存及会话数据由本地流程管理。

使用 `./dev.ps1 build -PublicRelease -RebuildAssets` 生成公开版。此模式将 Excel 和 PDF 资源生成到独立目录，通过 `editorx_public` 构建标签只嵌入该目录。发布前可运行 `./dev.ps1 test -PublicRelease` 验证，并用 `./tools/native-smoke.ps1 -PublicRelease -TestName TestNativeOfficeEditing` 和 `-TestName TestNativeDocumentOutline` 检查 Excel 编辑与 PDF 大纲。在 GitHub Releases 中给手动下载的 EXE 文件名加上版本号，让 Windows 从新路径读取图标。同时保留相同内容的 EditorX.exe，供已发布版本的应用内更新器使用。更新器遵循代理环境变量和 Windows 系统代理，读取最新稳定版并在应用更新前校验 EXE 的 SHA-256。

| 依赖或资源 | 许可位置 |
| --- | --- |
| Viva | `vendor/github.com/vivaui/viva` 中的 `LICENSE`、`NOTICE` 和 `THIRD_PARTY_NOTICES.md` |
| 其他 Go 依赖 | `vendor` 各模块的许可证和版权声明 |
| Univer、PDF.js 及相关资源 | 构建生成的 `THIRD-PARTY-NOTICES.txt` 及 npm 包内声明 |
| 本地 Word 构建中的 SuperDoc 与 DOCX Engine | npm 包内声明及各自的许可文档 |
| 终端和助手服务商标识 | `internal/editor/assets/terminal` 与 `internal/editor/assets/ai-providers` 的许可文件 |

Word 编辑使用 AGPL-3.0 的 SuperDoc 和独立授权的 DOCX Engine。合规的本地构建可使用 `./dev.ps1 build` 加载此组件。分发包含 Word 编辑的 EXE 时，需要同时满足两项许可。公开版用自有的 DOCX 只读预览渲染正文、表格、嵌入图片和标题。 [SuperDoc 许可](https://docs.superdoc.dev/resources/license/) · [DOCX Engine 许可](https://docs.superdoc.dev/resources/docx-engine-license/)
