# EditorX 设计与发布说明

EditorX 是一个 Windows 多格式文本编辑器。Go 和 Viva 负责窗口、编辑区和应用状态，WebView2 在同一窗口内承载 HTML、Office、PDF 和终端界面。用户操作见 [使用指南](usage.md)，格式支持见 [功能清单](features.md)。

## 模块划分

程序从 `cmd/editorx/main.go` 启动。下表中的 Go 文件位于 `internal/editor`，测试放在同一包中。内嵌资源位于 `internal/editor/assets`，测试样例位于 `internal/editor/testdata`，构建与打包脚本位于 `tools`。

| 区域 | 主要入口 | 职责 |
| --- | --- | --- |
| 窗口和设置 | `editor.go`、`view.go`、`theme.go`、`settings_view.go`、`application_settings.go`、`sidebar_preferences.go` | 窗口、布局、主题、设置入口与持久化 |
| 编辑与文件 | `pane.go`、`document.go`、`format.go`、`syntax_languages.go`、`editor_preferences.go` | 文本缓冲、输入法、编码、语法识别和保存 |
| 标签和项目 | `tab_strip.go`、`tab_actions.go`、`tab_groups.go`、`workspace.go` | 标签排序、分组、固定、项目树和多目录工作区 |
| 搜索与 JSON | `search.go`、`search_replace.go`、`search_results.go`、`json_tools.go`、`json_diff.go` | 查找、替换、结果定位、JSON 处理与对比 |
| 模板与翻译 | `templates.go`、`templates_view.go`、`templates_sidebar.go`、`translation_library.go`、`translation_dictionary.go`、`translation_view.go`、`translation_ai.go` | 模板保存与复用、按需下载英汉词库、个人／项目译文匹配、选区翻译与占位符校验 |
| 离线整句 | `offline_translation_packages.go`、`offline_translation_install.go`、`offline_translation_windows.go`、`offline_translation_view.go`、`offline_translation_worker.py` | 固定组件清单、按需安装、Argos 中英 CPU 推理进程、资源释放与实测 |
| 预览与 Office | `html_preview.go`、`markdown.go`、`document_preview.go`、`pdf_preview.go`、`office.go`、`office_workbook.go`、`document_server.go`、`office-ui/src` | 文档分流、嵌入页面、渲染、编辑和原格式保存 |
| 链接、会话和备份 | `linked_file.go`、`session.go`、`session_windows.go`、`backup_settings.go` | 链接读取、草稿恢复、数据目录锁和 ZIP 备份 |
| 托盘和更新 | `desktop.go`、`activation_windows.go`、`about_settings.go`、`update.go`、`update_state.go` | 托盘、单实例唤起、共享更新检查与下载、缓存及 EXE 替换 |
| AI、Git 与终端 | `ai.go`、`ai_provider.go`、`ai_session.go`、`project_tools.go`、`project_git_windows.go`、`shell_view.go`、`terminal_windows.go` | 模型和对话、本机 Git 进程、ConPTY 终端及项目联动 |

## 打开、编辑与保存

- 文件选择、命令行和拖放按类型进入对应的标签。文本按文件名或后缀识别语法，无后缀文件和未命名标签可手动选语法。用户在标签内设定的折行与 JSON 缩进优先于全局默认值。
- 纯 CRLF 文本在编辑缓冲中使用 LF，保存时转回 CRLF。混合行尾保留原有换行，UTF-8 BOM 按原文件设置写回。覆盖原路径前核对磁盘基线，再用同目录临时文件替换目标。若文件已被外部修改，编辑器保留缓冲，可另存一份。中文输入以输入法提交后的文本参与会话快照。
- Ctrl+F 和 Ctrl+R 共享查找浮窗。全局查找和替换的范围是当前窗口的已打开可编辑文本。替换写入各文件的缓冲和撤销记录，保存时再写入磁盘。
- JSON 左侧保留原始内容，不对打开、输入或粘贴自动格式化。右侧在格式化、压缩和键排序模式下跟随左侧生成结果；只有主动编辑右侧或应用结果才写回左侧，预览刷新不修改原文和撤销历史。右侧与对比区的粘贴仍按当前缩进格式化。「转 XML」和「更多」在右栏预览转换结果。再次选择当前的「更多」项会恢复格式化模式。用户可复制、另存或显式应用结果。对比模式维护独立的两份输入，折叠只影响显示。
- Excel 的编辑页按需加载工作簿。保存先获取最新编辑快照，再将改动应用到原工作簿。本地 Word 编辑通过组件导出 DOCX。公开版从 DOCX 提取正文和标题用于只读预览。PDF 从原始字节读取并渲染，另存原件保留原始字节。

## 预览、链接与项目工具

- Markdown 在 Viva 中渲染，可在源码和预览之间同步滚动。HTML 源码可先编辑，右侧 WebView2 使用当前缓冲渲染并执行页面脚本，本地 HTML 的路径用于解析相对资源。
- Office 和 PDF 使用独立回环地址和文档会话。构建时嵌入前端资源，关闭标签时释放 WebView 和文档服务。PDF 按范围读取，既可从本地文件读取，也可从临时链接的内存字节读取。
- 「打开链接文件」在后台读取 HTTP(S) 响应，单文件上限 128 MiB。默认临时打开，按类型处理。选择「保存并打开…」及目标路径后才将原始字节写入该路径。临时文本改动可用正常保存流程落盘。
- Git 面板调用本机 Git。切换分支或拉取后，只刷新磁盘基线仍一致且没有本地改动的文本标签，分支中消失的已打开文件可保留为草稿。终端前端使用嵌入资源，Windows PowerShell 由 ConPTY 提供。
- AI 可使用配置的 API 或本机 CLI。发送时带上当前选择的上下文，建议经审阅后应用。模型密钥和自定义请求头通过 Windows DPAPI 保存，对话与待审阅修改纳入会话。

## 设置、会话与备份

- Windows 窗口在显示前按实际 DPI 和客户区尺寸完成首次布局与绘制；启动清屏色使用当前主题的窗口背景，避免冷启动暴露尚未绘制的空白客户区。对应 Viva 改动保存在 `patches/viva-startup-frame.patch`。
- 工具侧栏可在左右两侧显示，打开的 AI、翻译和模板面板跟随移动。在「设置 → 侧栏」打开开关后，按钮在空白页和设置页也立即显示，无文档时将依赖文档的按钮置灰。侧栏开关使用等宽双列。设置页中的开启和关闭类选项使用同一开关，备份选文件等多选操作使用复选框。模板库使用非模态侧栏，保存模板和指定目录新建文件继续使用弹窗。侧栏偏好按字段合并保存并同步到其他窗口；AI 分隔比例以编辑区占比保存，恢复时根据位置转换。
- 翻译默认关闭。「本地词库」和「离线整句」共用原文、语言方向、选区与译文。两页在原文下方提供 AI 操作，已有多个服务时从按钮选择。AI 请求只发送当前原文和语言方向。英汉词库保留常用词、词形及多义释义，个人和项目库保存自定义术语及固定译法。下载词库处理 gzip、记录实际接收字节，校验固定版本的长度和 SHA-256 后发布 CSV 分片，查询时按需读取相关分片。
- 离线整句按需安装嵌入式 Python、CTranslate2 和 SentencePiece，以及 Argos 当前方向的模型。EXE 仅嵌入工作脚本。固定归档校验字节数和 SHA-256 后解压到临时目录，成功才发布组件。推理使用隐藏的 CPU 子进程，线程数为二，关闭 MKL，每次等待进程退出后返回译文。词库和模型可从翻译页下载，下载和移除集中在词库管理。打开管理或切换翻译页时下载继续，取消和关闭面板会中止任务。译文区保留编辑及应用操作，资源用量和性能记录留在实测文档。 [离线整句翻译](offline-translation.md)

- 默认数据目录是 EXE 同级的 `data`。可用 `VIVA_EDITOR_DATA_DIR` 指向独立数据目录。`application-settings.json` 保存主题、托盘、关闭行为、编辑器默认值和快捷键等偏好。首次运行默认跟随系统主题，主界面按钮在浅色和深色之间切换。
- 每个数据目录由一个实例写入。以同一目录重复启动会唤起已有实例并转交命令行路径。主动新建窗口时可在同一实例内保留多个工作窗口。关闭最后一个窗口按设置隐藏或退出。
- `session.json` 定期保存标签、项目和已提交的草稿等状态。读取时检查会话版本，主记录损坏时尝试上一份 `session.json.bak`。仅查看的临时链接会从快照中剔除，已编辑的临时文本可随会话恢复。旧版残留的多窗口记录在加载时合并，显式创建并保存的多窗口可单独恢复。
- ZIP 导出压缩用户所选标签及工作区信息。快照保留文本缓冲，图片和 PDF 等本地预览在恢复时从原路径加载。导入在后台读取和准备，显示进度并支持取消，完成后合并到当前窗口。合并保留现有标签，遇到不同内容的同路径草稿时分别保留。

## 构建和开发

在项目根目录用 PowerShell 7 执行。Go 版本以 `go.mod` 为准。脚本优先使用 `.dev/go` 下的便携 Go，其次使用 PATH 中的 Go。Node.js 用于按锁定的 npm 依赖生成内嵌前端资源。

```powershell
.\dev.ps1 -Action build
.\dev.ps1 -Action build -PublicRelease -RebuildAssets
.\dev.ps1 -Action test
.\dev.ps1 -Action test -PublicRelease
.\dev.ps1 -Action preview
```

构建默认输出到 `dist`，可用 `-OutputDirectory` 指定位置。缓存和截图放在 `.dev` 下。Go 使用 `-mod=vendor`，Viva 的本地修改见 `patches`，升级 Viva 时一并更新补丁并执行原生用例。回归步骤和大文件复测口径见 [测试用例](test-cases.md) 与 [大文件首次打开实测](large-files.md)。

## 发布范围

EditorX 的自有源码采用 [Apache-2.0](../LICENSE)。GitHub 仓库提供 Go 和前端源码、依赖锁、vendored Go 源码、补丁、文档和示例。公开发行文件位于 [GitHub Releases](https://github.com/Yuands9/editorx/releases/latest)，提供 Windows x64 免安装 EXE。构建输出、生成的 Web 资源、`node_modules`、工具缓存及会话数据由本地流程管理。

公开版使用 `-PublicRelease` 构建，使用独立的 Office 前端资源和 `editorx_public` 标签。发布时将版本标签指向对应源码，只上传 `EditorX.exe`。GitHub 为该标签自动生成 ZIP 和 TAR.GZ 源码包。EXE 内嵌汇总的第三方许可文本 `THIRD-PARTY-NOTICES.txt`，构建目录也提供明文副本。发布前执行公开版测试和 Excel、Word 预览、PDF 的原生检查。

客户端读取固定 GitHub 最新稳定版 API，界面没有更新源选择。复用现有 5 秒维护节拍，启动延后 10 秒，后续间隔 6 小时；`update-state.json` 保存最近检查时间、ETag、发布说明与跳过的版本。启动读取缓存并校验版本、下载路径和摘要，条件请求收到 304 时复用说明。应用偏好 `disableAutoUpdate` 缺省为 false，关闭后停止自动检查及进行中的自动请求，手动检查仍可用。自动失败静默，手动失败显示原因。

检查和下载由 `editorDesktop` 共享，关闭单个窗口不取消任务。仅新版且未跳过时显示侧栏绿色图标，无文档时也可进入；弹窗使用现有 Markdown 渲染器和滚动区域。选择「稍后」或关闭弹窗保留提示，跳过只针对一个版本；后台下载仍可编辑。更新器遵循代理环境变量和 Windows 系统代理，按 `EditorX.exe` 文件名定位附件，校验长度、SHA-256 和 Windows x64 PE 后才允许「安装并重启」。先保存全部会话及草稿，失败则中止安装；替换 EXE 时保留 `.editorx-previous` 作为回退副本，新版成功启动后清理。取消下载及退出时清理暂存文件。含 Word 编辑的本地构建仅提供下载公开版入口。

| 依赖或资源 | 许可位置 |
| --- | --- |
| Viva | `vendor/github.com/vivaui/viva` 中的 `LICENSE`、`NOTICE` 和 `THIRD_PARTY_NOTICES.md` |
| 其他 Go 依赖 | `vendor` 各模块的许可证和版权声明 |
| Univer、PDF.js 及相关资源 | 构建生成的 `THIRD-PARTY-NOTICES.txt` 及 npm 包内声明 |
| 本地 Word 构建中的 SuperDoc 与 DOCX Engine | npm 包内声明及各自的许可文档 |
| 终端和助手服务商标识 | `internal/editor/assets/terminal` 与 `internal/editor/assets/ai-providers` 的许可文件 |

Word 编辑使用 AGPL-3.0 的 SuperDoc 和独立授权的 DOCX Engine。合规的本地构建可使用 `./dev.ps1 build` 加载此组件。分发包含 Word 编辑的 EXE 时，需要同时满足两项许可。公开版用自有的 DOCX 只读预览渲染正文、表格、嵌入图片和标题。 [SuperDoc 许可](https://docs.superdoc.dev/resources/license/) · [DOCX Engine 许可](https://docs.superdoc.dev/resources/docx-engine-license/)
