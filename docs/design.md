# EditorX 设计与发布说明

EditorX 是一个 Windows 多格式文本编辑器。Go 和 Viva 负责窗口、编辑区和应用状态，WebView2 在同一窗口内承载 HTML、Office、PDF 和终端界面。用户操作见 [使用指南](usage.md)，格式支持见 [功能清单](features.md)。

## 模块划分

程序从 `cmd/editorx/main.go` 启动。下表中的 Go 文件位于 `internal/editor`，测试放在同一包中。内嵌资源位于 `internal/editor/assets`，测试样例位于 `internal/editor/testdata`，构建与打包脚本位于 `tools`。

| 区域 | 主要入口 | 职责 |
| --- | --- | --- |
| 窗口和设置 | `editor.go`、`view.go`、`theme.go`、`settings_view.go`、`application_settings.go`、`sidebar_preferences.go` | 窗口、布局、主题、设置入口与持久化 |
| 编辑与文件 | `pane.go`、`document.go`、`text_encoding.go`、`format.go`、`text_actions.go`、`text_bookmarks.go` | 文本缓冲、输入法、编码、语法识别、文本操作、书签和保存 |
| 大文件 | `large_text.go`、`large_text_view.go`、`large_edit_buffer.go`、`large_edit_view.go`、`large_edit_workflow.go`、`large_edit_draft.go` | 按页浏览、全文编辑、全局位置、流式保存和草稿 |
| 标签和项目 | `tab_strip.go`、`tab_groups.go`、`workspace.go`、`project_rename_windows.go`、`project_delete_windows.go` | 标签分组、项目树、多目录工作区、新建、重命名和删除 |
| 查找与替换 | `search.go`、`search_dialog.go`、`search_replace.go`、`search_large_documents.go`、`large_edit_query.go`、`search_results.go`、`search_results_view.go` | 查找浮窗、全文扫描、跨文档替换、结果分组和定位 |
| JSON | `json_tools.go`、`json_diff.go` | 格式化、转换和对比 |
| 模板与翻译 | `templates.go`、`templates_view.go`、`templates_sidebar.go`、`translation_library.go`、`translation_dictionary.go`、`translation_view.go`、`translation_ai.go`、`document_translation.go` | 模板保存与复用、个人／项目译文匹配、选区和整篇流式翻译 |
| 离线整句 | `offline_translation_packages.go`、`offline_translation_install.go`、`offline_translation_windows.go`、`offline_translation_view.go`、`offline_translation_worker.py` | 固定组件清单、按需安装、Argos 中英 CPU 推理进程、资源释放与实测 |
| 预览与 Office | `html_preview.go`、`markdown.go`、`document_preview.go`、`pdf_preview.go`、`office.go`、`office_workbook.go`、`document_server.go`、`office-ui/src` | 文档分流、嵌入页面、渲染、编辑和原格式保存 |
| 图片识别 | `image_ocr_view.go`、`image_ocr_packages.go`、`image_ocr_windows.go`、`image_ai_view.go`、`image_ai.go` | 中英离线 OCR、模型调用、任务取消及结果复制 |
| 密码本 | `password_vault.go`、`password_vault_state.go`、`password_vault_import.go`、`password_vault_archive.go` | 本机加密存储、分组、多账号条目、文本导入和归档恢复 |
| 链接、会话和备份 | `linked_file.go`、`session.go`、`session_windows.go`、`backup_settings.go` | 链接读取、草稿恢复、数据目录锁和 ZIP 备份 |
| 托盘和更新 | `desktop.go`、`activation_windows.go`、`about_settings.go`、`update.go`、`update_state.go` | 托盘、单实例唤起、共享更新检查与下载、缓存及 EXE 替换 |
| AI、Git 与终端 | `ai.go`、`ai_provider.go`、`ai_session.go`、`project_tools.go`、`project_git_windows.go`、`shell_view.go`、`terminal_windows.go` | 模型和对话、本机 Git 进程、ConPTY 终端及项目联动 |

## 打开、编辑与保存

- 文件选择、命令行和拖放按类型进入对应的标签。文本按文件名或后缀识别语法，无后缀文件和未命名标签可手动选语法。用户在标签内设定的折行与 JSON 缩进优先于全局默认值。
- 普通文本打开时检测编码，也可按指定编码打开或从状态栏重新读取。支持 UTF-8、UTF-16 LE／BE、GBK、GB18030、Big5、Shift-JIS、Windows-1252 和 ISO-8859-1。保存沿用打开时的编码和 BOM。纯 CRLF 文本在普通编辑缓冲中使用 LF，保存时转回 CRLF，混合行尾保留原有样式。覆盖原路径前核对磁盘基线，再用同目录临时文件替换目标。若文件已被外部修改，编辑器保留缓冲，可另存一份。中文输入以输入法提交后的文本参与会话快照。
- 普通文本与大文件完整编辑共用查找浮窗。Ctrl+F 打开查找，Ctrl+R 打开替换。右下角手动指定格式后，当前标签按所选格式决定编辑布局与高亮。选择「自动识别」可恢复检测。
- JSON 左侧保留原始内容，不对打开、输入或粘贴自动格式化。右侧在格式化、压缩和键排序模式下跟随左侧生成结果；只有主动编辑右侧或应用结果才写回左侧，预览刷新不修改原文和撤销历史。右侧与对比区的粘贴仍按当前缩进格式化。「转 XML」和「更多」在右栏预览转换结果。再次选择当前的「更多」项会恢复格式化模式。用户可复制、另存或显式应用结果。对比模式维护独立的两份输入，折叠只影响显示。
- Excel 的编辑页按需加载工作簿。保存先获取最新编辑快照，再将改动应用到原工作簿。本地 Word 编辑通过组件导出 DOCX。公开版从 DOCX 提取正文和标题用于只读预览。PDF 从原始字节读取并渲染，另存原件保留原始字节。

## 大文件与全文查找

### 打开、编辑和保存

- 磁盘上的普通文件达到 64 MiB，且文件类型为纯文本或日志时，默认进入只读的按页浏览。每页从原文件读取约 128 KiB，页边界按所选编码对齐。后台行索引提供全局行号，按页浏览的查找区分大小写，可扫描全文并定位命中所在页。
- 点击「打开完整编辑」后，后台任务分块解码原文，在系统临时文件中保存 UTF-8 字节和追加内容，同时建立行索引。每个修订版用片段偏移引用同一暂存文件。撤销、重做与搜索快照可复用这些片段。
- Viva 只接收约 64 KiB 的可见编辑缓冲，跨视口选区、光标、全局行号和书签在整份文档上定位。视口将 CRLF 显示成 LF，交互位置回映射到原始字节范围。保存以修订版为源流式编码到同目录临时文件，并在覆盖原路径前校验原文件哈希。
- 会话保存全局选区、滚动位置和以已保存版本为基线的改动。小改动内联到 `session.json`，估算超过 512 KiB 时写入数据目录的 `large-drafts` 压缩补丁。ZIP 备份可携带补丁。恢复时校验原文件哈希，若不一致则提示，并可从浏览界面导出未保存改动。
- 关闭标签时清理暂存文件和该标签的搜索结果引用。原生窗口会修剪大文本的排版缓存，并合并调度 Go 内存回收。实测口径和关闭后的数值见 [大文件实测](large-files.md)。

### 查找、替换和搜索结果

- 同一浮窗提供普通文本、转义字符、Go 正则、区分大小写、全词匹配和循环查找。完整编辑的查找内容上限为 1,024 个 UTF-8 字节。全文扫描、计数及替换从当前修订版读取，包含未保存改动。
- 普通字面量按有重叠的字节块扫描，ASCII 忽略大小写走折叠后的字节查找，遇到会改变字节宽度的 Unicode 大小写等价字符时按 Unicode 匹配。正则按 UTF-8 字符流扫描，可跨块匹配。纯 CRLF 文件的正则以 LF 识别行边界，同时保留原文范围用于定位和替换。
- 「查找全部已打开文档」和跨文档替换作用于当前窗口。涉及完整编辑时，后台扫描捕获的文本或修订版快照，应用前检查各标签来源是否仍然有效。替换写入各文档缓冲，一份文档的批量变更可一步撤销，Ctrl+S 后写入磁盘。
- 结果按搜索词、文件和命中行分组。同一行的多次命中汇总展示，保留每次命中的位置。点击结果可跳到正文，F3 可继续在各次命中间定位。面板最多保留 10,000 处匹配，完整编辑计数和批量替换扫描全文。长行的有界预览按可见区域绘制，在高 DPI 下进一步分段。

## 预览、链接与项目工具

- Markdown 在 Viva 中渲染，可在源码和预览之间同步滚动。HTML 源码可先编辑，右侧 WebView2 使用当前缓冲渲染并执行页面脚本，本地 HTML 的路径用于解析相对资源。
- Office 和 PDF 使用独立回环地址和文档会话。构建时逐文件压缩并嵌入前端资源，打开预览时按需传给 WebView。关闭标签时释放 WebView 和文档服务。PDF 按范围读取，既可从本地文件读取，也可从临时链接的内存字节读取。
- 「打开链接文件」在后台读取 HTTP(S) 响应，单文件上限 128 MiB。默认临时打开，按类型处理。选择「保存并打开…」及目标路径后才将原始字节写入该路径。临时文本改动可用正常保存流程落盘。
- Git 面板调用本机 Git。切换分支或拉取后，只刷新磁盘基线仍一致且没有本地改动的文本标签，分支中消失的已打开文件可保留为草稿。终端前端使用嵌入资源，Windows PowerShell 由 ConPTY 提供。
- AI 可使用配置的 API 或本机 CLI。发送时带上当前选择的上下文，建议经审阅后应用。模型密钥和自定义请求头通过 Windows DPAPI 保存，对话与待审阅修改纳入会话。
- 图片工具栏的「识别文字」使用按需安装的中英 OCR 组件。安装校验后在本机子进程运行，识别结束即退出。结果可选择文字或一键复制全文。「AI 识图」复用已配置且支持图像输入的 API 模型，用户选择提取文字或解读内容。开始识图时才发送当前图片，两条路径各自管理组件或网络任务。

## 项目文件和密码本

- 项目树会合并只有一个子目录且没有文件的连续层级。右键菜单可以对末级目录或选定的上级层级进行操作。目录内新建文件可选常见格式、模板或自定义名称和后缀，也可直接新建子目录。
- 文件、目录和项目根可右键重命名。Windows 移动成功后再更新已打开标签、项目树、历史和大文件草稿的路径。已有同名条目时提示换名。删除由确认弹窗触发，成功后刷新项目树。删除的对象进入 Windows 回收站。
- 密码本默认新建一个分组，支持嵌套和同站点多账号。粘贴文本或 Markdown 表格后，先解析成可编辑的候选条目，确认后保存。分组右键支持重命名、新建子分组、新建条目和删除确认。
- `passwords.edxvault` 使用 AES-GCM 保存条目。启用主密码时最少 8 个字符，从主密码派生密钥。关闭主密码时，由当前 Windows 用户的 DPAPI 保护随机密钥。默认闲置 5 分钟锁定、复制密码 20 秒后清理剪贴板，两项均可调整或关闭。
- 忘记主密码时可归档当前库并创建空库。之后用旧密码解锁归档，可选择合并当前库再恢复。恢复时也会归档被替换的当前库。跨设备迁移可使用独立密码保护的加密备份。

## 设置、会话与备份

- Windows 窗口在显示前按实际 DPI 和客户区尺寸完成首次布局与绘制；启动清屏色使用当前主题的窗口背景，避免冷启动暴露尚未绘制的空白客户区。对应 Viva 改动保存在 `patches/viva-startup-frame.patch`。
- 工具侧栏可在左右两侧显示，打开的 AI、翻译和模板面板跟随移动。在「设置 → 侧栏」打开开关后，按钮在空白页和设置页也立即显示，无文档时将依赖文档的按钮置灰。设置齿轮默认显示，可独立关闭。当前无按钮时工具侧栏不占宽度，更新提示出现时按侧栏位置显示。侧栏开关使用等宽双列。设置页中的开启和关闭类选项使用同一开关，备份选文件等多选操作使用复选框。模板库使用非模态侧栏，保存模板和指定目录新建文件继续使用弹窗。侧栏偏好按字段合并保存并同步到其他窗口；AI 分隔比例以编辑区占比保存，恢复时根据位置转换。
- 左侧主菜单和项目栏在新窗口默认展开，会话保存用户选择的展开状态。翻译工具图标可在侧栏设置中启用，也可从选区右键进入。
- 翻译面板共用一组原文、语言方向和译文控件。本地翻译按项目、个人和下载词库查找，未匹配译文时可调用已安装的中英离线引擎。AI 请求包含当前待译文本、方向及匹配的项目和个人术语，项目术语优先。下载词库记录压缩传输字节，校验长度和 SHA-256 后发布 CSV 分片。
- 整篇翻译在 Markdown 和纯文本标签中保存临时状态。用户启动后捕获当前原文，优先按段落切分请求，边界空白和 Markdown 代码块原样保留。沿用 AI 流式响应，停译保留已收到的内容，Markdown 翻译完成后渲染排版预览。
- 离线引擎按需安装嵌入式 Python、CTranslate2、SentencePiece 和 Argos 模型。固定归档校验字节数和 SHA-256 后解压到临时目录。推理使用隐藏的 CPU 子进程，线程数为二，关闭 MKL，每次等待进程退出后返回译文。通过词库管理下载或移除组件，返回翻译面板时可继续下载。 [离线整句翻译](offline-translation.md)

- 默认数据目录是 EXE 同级的 `data`。更换位置由 EXE 同级的 `editorx-data-location.json` 记录。旧进程保存会话并退出后，新进程持有源和目标目录的锁，复制完成才启用新目录。原数据保留为备份，迁移失败时继续使用原目录。`VIVA_EDITOR_DATA_DIR` 优先指定独立实例的数据目录。
- 私密 WebView2 页面共用每次运行的系统临时缓存。关闭窗口和控制器后清理本次缓存，下次启动重试清理残留。识别旧数据目录中的 WebView2 浏览器缓存后清理。
- `application-settings.json` 保存主题、托盘、关闭行为、编辑器默认值和快捷键等偏好。首次运行默认跟随系统主题，主界面按钮在浅色和深色之间切换。
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

## 本地历史与存储感知

- 普通文本、大文件完整编辑和可保存的 Office 文档共用本地历史保存流程。成功覆盖时，只有旧文件大小在 32 MiB 以内才记录快照，按压缩前的字节数判断。首次新建文件、未落盘的草稿和外部编辑交由现有的保存与会话流程处理。相同字节的重复保存不增加新版本。
- 历史按规范化绝对路径分组，保存在当前数据目录的 `local-history` 下。每版一个 gzip 文件，恢复时校验 SHA-256。文件以流的方式压缩和导出，临时快照随保存失败一起清理。迁移数据目录时会复制历史。
- 默认保留近 30 天内每个文件最新的 20 个版本。天数和数量各自可以设为不限。保存后会清理该文件的过期版本，启动后及运行期间会定期清理全部文件的过期版本。应用保留规则也会立即执行清理。
- 存储概览后台扫描当前数据目录，按文件长度统计应用数据和历史版本。顶层 `local-history` 的文件计入历史版本，两项相加得到总占用。应用数据中的可清理字节来自 `preview` 中残留的生成文件和 `cli` 中闲置的临时工作文件。预览和 CLI 工作在创建时登记正在使用的路径。清理按相同规则扫描，只删除数据目录内识别出的闲置缓存文件。
- 历史比对使用独立的系统窗口，有原生标题栏和可缩放边框。当前版本与所属标签共用编辑缓冲区，未打开的文件在首次编辑或合并前复核磁盘内容，再打开标签。右侧编辑、撤销和保存复用同一份文本模型。合并和恢复会缩小实际替换的范围，改动在单次撤销容量内才应用。并排视图复用 Viva 的行对齐、字内高亮和同步滚动，在中央显示原始行号及各块的合并箭头。区别导航首尾循环。「只看区别」保留每块前后四行的可用上下文，重叠范围只显示一次，省略处标出行数。普通文本的历史和当前版本各支持最多 32 MiB、105 万行，另存副本导出原始字节。

## 发布范围

EditorX 的自有源码采用 [Apache-2.0](../LICENSE)。GitHub 仓库提供 Go 和前端源码、依赖锁、vendored Go 源码、补丁、文档和示例。公开发行文件位于 [GitHub Releases](https://github.com/Yuands9/editorx/releases/latest)，提供 Windows x64 免安装 EXE。构建输出、生成的 Web 资源、`node_modules`、工具缓存及会话数据由本地流程管理。

公开版使用 `-PublicRelease` 构建，使用独立的 Office 前端资源和 `editorx_public` 标签。发布时将版本标签指向对应源码，只上传 `EditorX.exe`。GitHub 为该标签自动生成 ZIP 和 TAR.GZ 源码包。EXE 内嵌汇总的第三方许可文本 `THIRD-PARTY-NOTICES.txt`，构建目录也提供明文副本。发布前执行公开版测试和 Excel、Word 预览、PDF 的原生检查。

客户端读取固定 GitHub 最新稳定版 API。复用现有 5 秒维护节拍，每次启动约 10 秒后重新检查，运行期间间隔 6 小时。`update-state.json` 保存最近检查时间、ETag、发布说明与跳过的版本。启动读取缓存并校验版本、下载路径和摘要，条件请求收到 304 时复用说明。应用偏好 `disableAutoUpdate` 缺省为 false，关闭后停止自动检查及进行中的自动请求，手动检查仍可用。自动失败静默，手动失败显示原因。

检查和下载由 `editorDesktop` 共享，关闭单个窗口不取消任务。仅新版且未跳过时显示侧栏绿色图标，无文档时也可进入；弹窗使用现有 Markdown 渲染器和滚动区域。选择「稍后」或关闭弹窗保留提示，跳过只针对一个版本；后台下载仍可编辑。更新器遵循代理环境变量和 Windows 系统代理，按 `EditorX.exe` 文件名定位附件，校验长度、SHA-256 和 Windows x64 PE 后才允许「安装并重启」。先保存全部会话及草稿，失败则中止安装；替换 EXE 时保留 `.editorx-previous` 作为回退副本，新版成功启动后清理。取消下载及退出时清理暂存文件。含 Word 编辑的本地构建仅提供下载公开版入口。

| 依赖或资源 | 许可位置 |
| --- | --- |
| Viva | `vendor/github.com/vivaui/viva` 中的 `LICENSE`、`NOTICE` 和 `THIRD_PARTY_NOTICES.md` |
| 其他 Go 依赖 | `vendor` 各模块的许可证和版权声明 |
| Univer、PDF.js 及相关资源 | 构建生成的 `THIRD-PARTY-NOTICES.txt` 及 npm 包内声明 |
| 本地 Word 构建中的 SuperDoc 与 DOCX Engine | npm 包内声明及各自的许可文档 |
| 终端和助手服务商标识 | `internal/editor/assets/terminal` 与 `internal/editor/assets/ai-providers` 的许可文件 |

Word 编辑使用 AGPL-3.0 的 SuperDoc 和独立授权的 DOCX Engine。合规的本地构建可使用 `./dev.ps1 build` 加载此组件。分发包含 Word 编辑的 EXE 时，需要同时满足两项许可。公开版用自有的 DOCX 只读预览渲染正文、表格、嵌入图片和标题。 [SuperDoc 许可](https://docs.superdoc.dev/resources/license/) · [DOCX Engine 许可](https://docs.superdoc.dev/resources/docx-engine-license/)
