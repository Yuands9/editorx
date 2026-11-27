# EditorX 测试用例

自动化测试使用生成的样例和本地服务。手工验证时使用独立的 EXE 和 `VIVA_EDITOR_DATA_DIR`，样例、日志及 WebView2 用户数据放在项目的 `.dev` 下。

## 效率入口验收

新增回归：`TestLanguageCommentShortcut`、`TestActiveProjectContext`、`TestJSONPreviewEditPreservesSourceLayout`、`TestLargeJSONRightEditSaveUndoAndSession`、`TestProjectPreviewPreservesEditsAndReveal`、`TestProjectWatcherRefreshAndRelease`、`TestDirectoryReplacementPreservesDraftEncodingAndConflict`、`TestDirectorySearchUsesOpenBuffersAndRegexPreview`。

- 大 JSON：右侧修改后立即保存、未完成语法、撤销重做及会话快照；中文、字符串空格和转义符映射；旧后台结果不覆盖新输入。
- 项目：多根目录和嵌套根上下文、后台加载、展开恢复、定位文件、外部新增和修改刷新、稳定行 ID、移除后释放监听；预览切换保留草稿。
- 目录搜索与替换：非模态结果面板、跳转后保留结果、未保存缓冲命中、正则分组预览、逐文件勾选、编辑区替换一步撤销、UTF-16/BOM/CRLF 保留、外部修改冲突跳过。

2026-11-27 同机基准（Core Ultra 9 285H，3 次）：JSON 右侧单字符修改同步，3.8 MiB 从约 162 ms / 364 MB 分配降到约 27 ms / 13 MB；15.1 MiB 从约 640 ms / 1.46 GB 降到约 107 ms / 53 MB。这是同步函数耗时，不包含后续后台格式化和原生绘制，也不代表全部编辑位置。大文件首次同步仍有缓冲重建成本。

| 场景 | 预期 |
| --- | --- |
| 设置中切换五种预设，再逐项改绑、恢复默认、重启并切换另一窗口 | 绑定持久化并同步，无重复触发。原有自定义键优先于新默认键。Sublime Ctrl+D 选择匹配。IDEA Ctrl+S 全部保存 |
| Ctrl+P 输入中文、文件名缩写、路径片段；打开同名不同路径文件 | 标题和路径能区分；已打开标签优先且不重复；↑↓ 循环、Enter 打开、Esc 返回后可以继续输入正文 |
| 项目扫描中关闭面板、移除项目、重开面板；项目含被排除目录或不可读目录 | 工作可取消，重新打开重新扫描；不会创建索引；不完整时显示提示，文件选择仍可用 |
| Ctrl+Shift+P 搜索中文命令或英文 ID；在零标签状态执行命令 | 搜索、当前快捷键、不可用状态一致；新建等全局命令正常，依赖文件的命令置灰 |
| Ctrl+Shift+T 恢复普通草稿、选区及磁盘文件；重复打开相同路径；连续关闭超过 20 个 | 草稿文本恢复，未修改文件读取磁盘最新内容，无重复标签；数量和 16 MiB 文本预算生效 |
| 全部保存混合普通文件、未命名文件、Office 与未激活大文件草稿 | 依次保存；取消另存为或制造磁盘冲突后停止，剩余修改保留；不会在保存中退出 |
| 深／浅主题、900×560 小窗口下主菜单、子菜单和 JSON 工具栏 | 高频操作可见，图标列对齐，二级入口可点；同步滚动仍有效；JSON 不再有单独空白工具行 |

专项与筛选性能复测：

```powershell
. .\tools\dev-env.ps1
$editorxGo = Initialize-VivaDevEnvironment $PWD
& $editorxGo test -mod=vendor -tags editorx_public -count=1 -run '^Test(Efficiency|EditorShortcut|DesktopExit)' -bench '^BenchmarkEfficiencyPalette20K$' ./internal/editor
```

2026-11-27 本机 Core Ultra 9 285H 的 20,000 候选文件名筛选基准约 0.74 ms／次、160 B／次（稀疏缩写匹配）。此数据只代表内存筛选，不包含磁盘遍历、打开文件和原生绘制。

本批已通过公开版和本地版全量测试，以及实际 EXE 的隔离桌面启动、关闭确认、会话恢复和重复唤起检查。日志与深浅色截图位于 `.dev/efficiency-qa`；此目录中的 EXE 为开发试用包。

## 第二批核心编辑验收

| 场景 | 预期 |
| --- | --- |
| Ctrl+Shift+F／项目与子目录右键，过滤 `*.txt;*.md`，大小写、整词和正则，非法正则 | 范围和条件生效，异常可读；结果打开对应文件并定位，不覆盖已有草稿 |
| 中文路径、二进制、不可读文件、超过 64 MiB 文件、取消、大量匹配 | 跳过有统计；结果限 5,000 处、目录项限 100,000；取消或截断显示不完整，不创建索引文件 |
| Alt 单击／拖选、Tab 和中文列宽、短行、Ctrl+Alt+↑↓、相同内容多选、拆行 | 主／次选区均绘制，短行不填充；多处输入／删除／复制／撤销重做正确，Esc 清除次光标 |
| Sublime Ctrl+D／Ctrl+Shift+L，自定义原有 Ctrl+Alt+L | 不执行旧破坏性动作，不覆盖用户自定义快捷键 |
| 编码转换、UTF-8 BOM、UTF-16、LF／CRLF／CR、混合换行、无文本修改的格式草稿 | 保存字节准确，格式变化参与未保存提示，重启和恢复关闭标签保留；不支持的字符保存失败且原文件不变 |
| 1 MiB 以上普通文件打开、保存中继续输入、全部保存、外部同时修改 | 读写走后台，按保存时快照写盘，随后输入仍为未保存；批量保存等完成，磁盘冲突不覆盖 |
| 大 JSON 连续输入、改变模式／缩进、切换标签、关闭窗口 | 新内容不被旧预览覆盖，工作可结束，预览更新期间不能导出／应用旧结果；折叠摘要与原行为相同 |
| 深浅主题、900×560 搜索弹框、编码二级菜单 | 包含／排除字段明确、无裁切，原有菜单及键盘操作通过 |

`TestCore*` 覆盖上述新增主路径，`TestJSONFold*`、`TestTextEncodings*`、`TestReopenTextEncoding*` 和现有全文回归覆盖兼容行为。

`BenchmarkCoreJSONFolds4MiB` 使用约 4 MB、12 万个单行对象的 JSON。本机 Core Ultra 9 285H 实测约 13.6 ms／次、339 B／次、7 次分配；只衡量折叠扫描，不含文件读取、缓冲区构建和原生绘制，也不代表所有嵌套形态。

```powershell
. .\tools\dev-env.ps1
$editorxGo = Initialize-VivaDevEnvironment $PWD
& $editorxGo test -mod=vendor -tags editorx_public -count=1 -run '^TestCore|^TestJSONFold|^TestTextEncoding' -bench '^BenchmarkCoreJSONFolds4MiB$' ./internal/editor
```

2026-11-27 已通过公开版、本地版全量测试，新增 `TestCore*` 与真实 EXE 的 `TestNativeEditorBackground` 原生检查。目录搜索和列选区截图覆盖深浅主题及 900×560 小窗口；日志与试用 EXE 位于 `.dev/core-qa`。试用构建使用独立输出目录，不替换安装目录或用户数据。

## 自动化回归

在项目根目录执行。两套测试分别覆盖公开版的 DOCX 只读预览和本地构建的 Word 编辑路径。

```powershell
.\dev.ps1 -Action test -PublicRelease
.\dev.ps1 -Action test
```

| 改动范围 | 对应自动化入口 |
| --- | --- |
| 效率入口与预设 | `TestEfficiency*`、`TestEditorShortcutRebinding`，以及主菜单、模板、密码本、最近文件和指定编码 UI 回归 |
| 打开、保存和会话恢复 | `TestFileDropOpensDocuments`、`TestOpenTextFormatPrompt`、`TestSuggestOpenedFormat`、`TestSessionExternalChangesAndRecovery`、`TestSessionIMEAndExplicitClose` |
| 查找、替换和 JSON | `TestSearchReplaceCurrentFileWithMultilineInput`、`TestSearchReplaceAllOpenWithEscapesAndMixedEndings`、`TestJSONAutoWrapAndReversibleConversions`、`TestJSONFoldSummaryRenderingAndPreservation`、`TestJSONSourceKeepsFreeInput` |
| 多编码及大文件 | `TestTextEncodingsPreserveOriginalBytes`、`TestLargeTextEncodedBoundariesAndSearch`、`TestLargeEditPreservesEncodingAndMixedLineEndings`、`TestLargeEditViewportKeyboardIMEAndDraft`、`TestLargeEditStreamsDraftAndBackup`、`TestLargeEditReturnsToBrowsing` |
| 大文件查找替换和结果 | `TestLargeEditFloatingFindAndReplace`、`TestLargeEditSearchMatchesNormalizedText`、`TestLargeEditSearchAcrossPiecesAndDenseReplace`、`TestLargeEditGroupedSearchResultsReveal`、`TestSearchResultsRenderWideLineAtHighDPI`、`TestClosedSearchResultsReleaseAndRefill` |
| 链接打开和落盘 | `TestLinkedFileWithHTTPSRedirectAndDifferentFormats`、`TestLinkedFileSaveChoiceKeepsOriginalBytes`、`TestLinkedFileDraftsAndAttachmentsAfterRestart` |
| 标签、项目和 ZIP | `TestPinnedGroupRestoresPinsAndAllowsExplicitClose`、`TestMultipleProjectsThroughUIAndSession`、`TestMergeBackupKeepsSeparateDraftsAndGroupIDs`、`TestLegacySessionRestoresImportedWindowsIntoTheMainWindow` |
| 项目文件操作 | `TestProjectCreateOnCompactFolder`、`TestProjectRenameThroughContextMenu`、`TestProjectDeleteConfirmation` |
| 密码本 | `TestPasswordVaultAuthenticatedStorage`、`TestPasswordVaultImportFormats`、`TestPasswordVaultOptionalMasterPassword`、`TestPasswordVaultTimingOptions`、`TestPasswordVaultDeleteFromContextMenu`、`TestPasswordVaultSidebarNavigation`、`TestPasswordVaultArchiveRestore` |
| 图片 OCR 和 AI 识图 | `TestImageOCRIsOptIn`、`TestImageOCRPixels`、`TestImageAIOnlySendsCurrentImageOnStart`、`TestImageAICancelsAnInFlightRequest` |
| 设置和 AI | `TestEditorSettingsApplyAcrossWindowsAndRestore`、`TestEditorShortcutRebinding`、`TestAIHistoryRestoresReviewAgainstOriginalDocument` |
| 退出与保存确认 | `TestDesktopExitSaveConfirmation`、`TestDesktopExitSaveConfirmationSetting`、`TestSaveConfirmationCanBeDisabledAndRestored` |
| 侧栏设置 | `TestSidebarPreferencesAndTemplatePanel`、`TestSidebarAcrossWindowsAndSession`、`TestSidebarButtonsWithoutDocumentAndAlignment`、`TestSidebarCollapsesWithoutVisibleButtons` |
| 模板与翻译 | `TestDocumentTemplatesThroughUI`、`TestLocalTranslationThroughUI`、`TestSelectionTranslationAIThroughUI`、`TestDownloadedDictionaryThroughUI`、`TestDictionaryDownloadIntegrityAndCleanup`、`TestAITranslationWithoutDictionary`、`TestDocumentTranslationStreamsInSplit`、`TestDocumentTranslationChunksPreserveBoundaries` |
| 离线组件与压缩下载 | `TestOfflineComponentIntegrityAndCleanup`、`TestOfflineTranslationOptIn`、`TestOfflineDownloadStopsWithPanel`、`TestDictionaryCompressedTransfer`、`TestDictionaryDownloadStopsWithPanel` |
| Git | `TestProjectGitStageCommitPushFetchAndPull` |
| 应用内更新 | `TestUpdateConditionalRequestAndPersistentCache`、`TestUpdateAutomaticChecksShareWorkAndRespectOptOut`、`TestUpdateHomeIndicatorSkipAndManualCheck`、`TestUpdateDownloadContinuesAcrossWindowsAndCleansCancellation`、`TestUpdateDownloadsVerifiedExecutableAndPreservesPrevious` |
| 本地历史和存储管理 | `TestLocalHistoryCapturesOverwrittenBytes`、`TestLocalHistorySnapshotSizeLimit`、`TestLocalHistoryRetentionAndClear`、`TestLocalHistoryThroughStorageSettings`、`TestEditorStorageCountsAndClearsCache`、`TestHistoryCompareFromEditorAndProject`、`TestHistoryComparisonEditsCurrentBuffer`、`TestHistoryComparisonLargerText`、`TestHistoryComparisonBoundsAndUndo`、`TestHistoryDiffRetainsFourContextLines`、`TestHistoryDiffPreservesLineBoundaries` |

## Windows 原生检查

按改动范围选择命令，分别执行。`native-smoke.ps1` 在隔离桌面运行，日志默认写入 `.dev/native-smoke.log`。

```powershell
.\tools\native-smoke.ps1 -PublicRelease -TestName TestNativeOpenTiming
.\tools\native-smoke.ps1 -PublicRelease -TestName TestNativeStartupFrame
.\tools\native-smoke.ps1 -PublicRelease -TestName TestNativeOfficeEditing
.\tools\native-smoke.ps1 -PublicRelease -TestName TestNativeDocumentOutline
.\tools\native-smoke.ps1 -TestName TestNativeSearchDrag -Scale 1
.\tools\native-smoke.ps1 -TestName TestNativeSearchDrag -Scale 2
```

`TestNativeOpenTiming` 检查 Excel、Word、PDF、PowerShell 和 Git 是否进入可用状态，也检查临时链接 PDF 的渲染及字节范围请求。验证本地 Word 编辑时，执行 Office 和大纲检查的本地构建版本。涉及托盘或重复启动时，将 `VIVA_EDITOR_TEST_EXE` 指向打包后的 `EditorX.exe`，再运行 `TestNativeEditorBackground`。

## 手工验收

模板与翻译的界面截图可设置 `VIVA_PREVIEW_DIR` 后运行 `TestTextAssetsPreviews`，覆盖深浅色、900×560 小窗口、保存模板、译文预览、词库下载卡片、离线组件下载、无文档侧栏和设置双列对齐。真实词库验证将 `EDITORX_DICTIONARY_SOURCE` 指向已下载的固定版本 `ecdict.csv`，或将 `EDITORX_DICTIONARY_GZIP` 指向原始 gzip 响应体，运行 `TestInstalledDictionaryFromRealSource`，检查实际传输字节、下载校验、安装大小和常用单词／词形命中；该用例不会自动访问外网。

首次运行用独立数据目录，确认左侧主菜单和项目栏展开，模板库和翻译图标默认隐藏。用 Ctrl+B 收起后重启，确认恢复栏位状态。在没有打开文件时打开侧栏开关，确认设置页和空白页立即显示相应按钮，保存／查找／折行置灰，AI／Git／终端和模板入口可用。核对两列开关对齐和深浅主题的开关状态。切换左右位置，确认面板随之移动，重启验证。模板侧栏中选择、填写、预览并新建或插入，确认编辑区可继续操作。

翻译面板直接显示「AI 翻译」和「本地翻译」。无任何下载时用已配置的服务翻译，核对请求中的原文、方向和匹配术语。单服务直接开始，多服务先选择。通过「词库管理」验证下载进度、取消、失败重试和移除。用 `hello`、`files` 和一段英文复测词义参考和整句译文，保存的项目译文优先命中。

通过词库管理主动安装组件后，用「本地翻译」验证中英两个方向的短句、段落和带占位符的多行原文。检查替换、撤销及占位符验证。返回翻译面板时下载继续，取消或关闭面板后清理临时目录。完成或取消后检查离线子进程退出，并从词库管理移除已装组件。

分别在 Markdown 和纯文本正文右键点「整篇翻译」，检查有无选区都能打开分屏，顶部工具栏没有此入口。手动开始后用测试服务分多次推送译文，检查首段在请求完成前显示、长文分段、代码块保留、停止后保留已输出内容，以及切换标签后译文留在原标签。用译文栏的关闭按钮退出分屏。

在正文右键或按 Shift+F10，检查选区和历史记录对应的菜单项是否可用。复测剪切后撤销、重新粘贴、查找、替换及转到行。对照按页浏览和完整编辑中的全选范围。

### 项目文件、密码本和图片

| 检查项 | 操作与预期 |
| --- | --- |
| 项目内新建 | 在合并目录末级右键新建文件，分别选自定义、常见格式和模板。自定义名称可带任意合法后缀，也可省略后缀。检查实际落盘目录，再新建子目录。 |
| 项目重命名与删除 | 用临时项目验证同名冲突及取消删除。重命名含有已打开文件的目录后，从原标签保存到新路径并检查未保存内容。删除确认后到 Windows 回收站核对目标。 |
| 密码本导入和分组 | 用虚构网站、账号和密码验证文本及 Markdown 表格识别。检查一个站点的多个账号都保留、候选可编辑、右键可创建子分组、重命名和确认删除。 |
| 主密码与恢复 | 在独立数据目录验证不设主密码、开启和关闭主密码、8 字符边界及眼睛显隐。用测试密码验证忘记密码后归档和新建，再分别验证恢复旧库及合并当前库。 |
| 密码本锁定与剪贴板 | 初次创建检查 5 分钟闲置锁定、20 秒剪贴板清理。验证自定义和关闭开关，剪贴板写入新内容后保留该内容。 |
| 中英 OCR | 新数据目录打开中英文夹杂的图片，经「识别文字」主动下载组件。安装后识别，验证选区复制、复制全文，以及任务结束后识别子进程退出。 |
| AI 识图 | 使用本地测试服务记录请求。打开图片和弹窗时无图片请求，点击开始后接收当前图片。分别检查提取文字、解读内容、取消，并在未安装 OCR 时验证。 |

## 实际模型与网络验证

以下为显式启用的较大验证，默认测试不下载任何组件。缓存归档需采用 `offline_translation_packages.go` 中的官方文件名，全部位于项目 `.dev` 下。

```powershell
. .\tools\dev-env.ps1
$go = Initialize-VivaDevEnvironment $PWD.Path
$env:EDITORX_OFFLINE_DOWNLOADS = Join-Path $PWD.Path '.dev\offline-translation\downloads'
$env:EDITORX_OFFLINE_DATA = Join-Path $PWD.Path '.dev\offline-translation\data'
& $go test -mod=vendor -tags editorx_public -run '^TestOfflineTranslationRealModels$' -count=1 -v ./internal/editor
```

该用例通过真实安装器验证缓存包，测中英双向各短句与段落、占位符／换行、超时取消，并执行真实模型的界面翻译、替换和撤销。记录写入指定数据目录的 `offline-measurements.json`，不是模拟译文。

运行 `TestOfflineTranslationNetworkInstall` 可从词库管理下载、翻译并移除英译中组件，临时数据在测试后清理。设置 `EDITORX_OFFLINE_DOWNLOADS` 可回放上述官方归档，或设 `EDITORX_OFFLINE_NETWORK=1` 通过生产客户端真实下载约 102 MiB。传入 `-timeout=150s`。普通全量回归可不设置这些变量。

可在新 PowerShell 中执行以下命令创建用于测试的公开版及其数据目录。测试旧版升级时，将旧版 EXE 放到另一个独立目录，并为它指定新的测试数据目录。

```powershell
.\dev.ps1 -Action build -PublicRelease -OutputDirectory .dev\qa\public
$env:VIVA_EDITOR_DATA_DIR = Join-Path $PWD '.dev\qa\public\data'
& .\.dev\qa\public\EditorX.exe
```

| 用例 | 操作 | 预期 |
| --- | --- | --- |
| F-01 打开、输入法与保存 | 将 UTF-8 BOM + CRLF 文本、`.xml`、`.md` 和无后缀文件拖入窗口。用拼音输入法提交「中文编辑」，再退出一次尚未提交的组词。保存并重新打开。用另一程序修改原文件后再次尝试保存。 | 标签类型正确，可手动指定语法。已选汉字只出现一次，取消组词后没有残留，光标可见。原有 BOM 与 CRLF 保存后仍在。外部修改触发冲突，保留磁盘内容和当前缓冲。 |
| F-02 多行查找和替换 | 在两个临时文件中分别写入相邻两行 `alpha` 和 `beta`，再放入一个不匹配的标签。按 Ctrl+F 粘贴两行原文，拖动浮窗标题栏，执行「查找全部已打开文档」后点击结果定位。按 Ctrl+R，用两行 `甲` 和 `乙` 进行当前文件替换，再执行「替换全部已打开文档」。分别撤销。 | 查找结果按文件分组，点击可定位到匹配，浮窗拖动跟随鼠标。替换按所选范围生效，第三个标签不受影响，撤销能逐文件恢复。 |
| J-01 JSON 双栏和转换 | 新建 JSON，分别从左右两栏粘贴下方样例。编辑嵌套字段并临时删掉一个结构字符，再补回。切换 2 和 4 空格，折叠对象和数组，切换压缩，点两次「更多 → 去转义」。 | 两侧可复制和编辑。左侧原样保留粘贴和输入，在大括号／数组中连续回车、添加空行和 Tab 后不能被自动清除。格式化显示在右侧，主动编辑右侧才同步回原文。保存、撤销重做和重启均保留左侧内容。遗漏结构字符时保留输入。折叠显示 `Object{...}` 和 `Array[2]`，右侧压缩结果只有一行，长内容可自动折行显示。去转义显示嵌套数据，再点一次退出预览，源文语义保持一致。 |
| T-01 标签、分组和项目 | 拖入两个不同目录和三份文件。双击标签和组名重命名并回车，分别拖动标签与整组，固定其中一组。执行批量关闭，然后显式关闭该组，再移除一个项目。 | 拖动与展开、收起有效。批量关闭跳过固定项，显式关闭可移除固定组。移除项目不删除磁盘文件。关完所有标签后主界面仍可打开文件。 |
| S-01 ZIP 备份与会话 | 打开文件和有内容的未命名草稿，导出选中的文件与草稿。改动当前草稿后导入 ZIP，观察进度，另做一轮在任务完成前取消。正常退出并重新打开。 | 完成后备份内容加入当前窗口，当前草稿仍可找回。取消时未合并内容。重启后会话仍在原窗口。旧多窗口备份分别覆盖在 `TestLegacySessionRestoresImportedWindowsIntoTheMainWindow`。 |
| S-02 托盘和重复启动 | 使用一个测试数据目录，设置关闭时隐藏，再关闭最后一个窗口。再次运行同一个 EXE，并从命令行传入一个文件。切换到关闭即退出，再关窗。 | 隐藏时重复启动只唤起已有窗口，命令行文件在已有实例打开。关闭即退出后进程结束，下次启动能恢复会话。 |
| S-03 数据目录迁移 | 将试用 EXE 放在独立测试目录，使用默认 `data`，创建未保存草稿并改一项设置。在「存储管理 → 应用数据」点「更换位置」并选择空文件夹，先取消一次，再重新选择并迁移。另试非空目标。 | 重启后草稿和设置从新目录恢复，原数据可作为备份。非空目标保持原有文件。打开 HTML 预览后，WebView2 缓存位于系统临时目录，关闭后清理。 |
| L-01 临时打开和另存 | 用测试 HTTP 或 HTTPS 地址依次打开小型 JSON 和 PDF。重新打开链接，选择「保存并打开…」并取消选址，再另存到指定路径。另对一个临时文本做修改并重启。 | 仅查看时只有临时标签，无自动下载副本，重启时不恢复。取消不会生成文件，指定路径保存时原始字节一致。编辑过的文本能作为草稿恢复。PDF 首屏渲染，可另存原件。 |
| D-01 文档和预览 | 打开带两级标题的 Markdown、HTML（带本地图片）、图片、带公式的 XLSX、带书签的 PDF、带两级标题的 DOCX。点击大纲、切换深浅主题、缩放，在 Excel 修改单元格并保存重开。 | Markdown 左侧大纲可全部展开或收起，分屏可同步滚动。HTML 相对资源正常加载，源码可编辑。图片可缩放。PDF 和 DOCX 显示文件名，标题可跳转。两种主题下阅读正常，Excel 改动保存。公开版 DOCX 只读，本地版可在测试副本上修改并保存。 |
| P-01 设置和 AI | 新数据目录中验证跟随系统主题、右下角深浅切换。修改字体、折行类型、JSON 缩进和快捷键后重启。打开 AI 模型选择，点击搜索与配置输入框。使用测试服务或已配置的本机 CLI 发送普通问题，拖选文字复制，重启后查看对话。 | 设置自动保存，重启生效。输入框聚焦时无白色外圈，模型菜单有选中反馈。问答文字可选取与复制，对话仍可接续。 |
| G-01 Git 分支和文件联动 | 准备两个含不同文本内容的本地分支，其中一支独有一份文件。打开共用文件和独有文件，从面板切换分支和新建分支。再编辑一个已打开文件，执行不会覆盖磁盘修改的切换。使用本地 bare 仓库验证提交、推送、获取和拉取。 | 分支名、项目树和干净缓冲更新。消失的文件保留为草稿，未提交的编辑缓冲不丢。如果 Git 拒绝切换，显示原因并保留当前状态。 |
| U-01 从旧公开版升级 | 在独立目录运行 `v0.32.10`，创建一个草稿。在「设置 → 关于」依次选择「检查更新」「下载更新」「更新并重启」。 | 重启后「关于」显示目标版本，草稿保留，成功启动后旧 EXE 回退副本会清理。校验失败、替换失败和回退由自动化验证。 |

JSON 样例使用合成数据，可直接粘贴到任一栏。

```json
{"msg":"交易成功","payload":"{\"body\":\"{\\\"enabled\\\":true}\"}","items":[{"id":1},{"id":2}]}
```

## 大文件复测

### 大文件和全文查找回归

在项目根目录执行以下命令。首次复测时先生成公开版内嵌资源，默认夹具由测试生成。

```powershell
. .\tools\dev-env.ps1
$editorxGo = Initialize-VivaDevEnvironment $PWD
.\tools\web-assets.ps1 -PublicRelease
& $editorxGo test -mod=vendor -tags editorx_public -count=1 -run '^Test(LargeEdit|LargeText|LargeFindAll|TextEncoding|Search|ClosedSearchResults)' ./internal/editor
```

| 检查项 | 操作 | 预期 |
| --- | --- | --- |
| 打开与跨视口编辑 | 打开 64 MiB 以上的本地 .out，先翻页再进入完整编辑。跳到末尾，用中文输入法输入，选择跨视口复制，撤销和重做。 | 浏览页与完整编辑位置连贯，光标、行号和选区按全文计算，组词取消或撤销后正文恢复。 |
| 编码与行尾 | 分别用 UTF-8 BOM、UTF-16、GBK／GB18030 和混合行尾样例打开、查找和保存副本。手动改编码重读一次。 | 边界处字符和换行完整，保存回原编码、BOM 和行尾。 |
| 共享浮窗 | 从工具栏及 Ctrl+F、Ctrl+R 进入查找和替换。测试 Ctrl+Enter 输入多行、Enter／Shift+Enter 导航、F3 跳转及 Esc 返回。 | 完整编辑和普通文档共用浮窗。跨视口命中可定位，Esc 后可继续输入正文。 |
| Unicode 和正则 | 测普通字面量的全词和大小写，放置跨读取边界的中文及 `ſDK`。在 CRLF 样例测试行首、行尾、EOF 空匹配和捕获组替换。 | 匹配范围、实际顺序和计数与同样内容的普通文档一致，字符边界完整。 |
| 批量替换 | 同时打开普通文本和完整编辑日志，查找并替换全部打开文档。使用密集命中夹具测超过结果显示上限的替换。 | 两类标签的内容和计数正确，每个标签可单步撤销批量替换。发生关闭或版本变化时提示重新操作。 |
| 分组与高 DPI | 同一行放两处命中，做一个带 CJK、制表符和 emoji 的长行，在 Windows 200% 缩放下横向滚动到末尾。 | 结果按文件与行分组，匹配次数保留两次。末尾文字和高亮可见，点击结果和 F3 均跳到准确位置，无渲染错误。 |
| 草稿与磁盘冲突 | 修改两处，退出并恢复。用大补丁导出 ZIP，再用独立数据目录导入。另用副本在编辑期间从外部改动原文件。 | 草稿和位置恢复。原文件变动时覆盖保存受阻，可另存或导出草稿，原内容可核对。 |
| 关闭后的引用释放 | 搜索后保持结果面板开着，关闭大文件并再开另一份文件。 | 已关文档的结果与暂存清理，剩余文件的结果继续可用。 |

桌面 `catalina.out` 的固定样例为 178,691,302 字节。可以选跑真实文件的结果测试。该用例只读样例，查找 `fee_type` 预期为 9,410 处命中及 4,705 行结果。

```powershell
$env:VIVA_SEARCH_RESULTS_LOG = Join-Path ([Environment]::GetFolderPath('Desktop')) 'catalina.out'
& $editorxGo test -mod=vendor -tags editorx_public -count=1 -v -run '^TestLargeEditGroupedSearchResultsReveal$' ./internal/editor
Remove-Item Env:VIVA_SEARCH_RESULTS_LOG
```

### Windows 原生响应和关页内存

使用独立 PowerShell 7 窗口在项目根目录运行。首次运行前先用 `.\tools\web-assets.ps1 -PublicRelease` 生成公开版内嵌资源。样例需含 `Cache-Control: no-cache`。测试先复制文件，每轮在隔离桌面启动独立进程，在副本中验证跳转、输入、撤销重做、查找及流式保存。

```powershell
. .\tools\dev-env.ps1
$editorxGo = Initialize-VivaDevEnvironment $PWD
$env:VIVA_LARGE_EDIT_FIXTURE = Join-Path ([Environment]::GetFolderPath('Desktop')) 'catalina.out'
$runDirectory = Join-Path $PWD '.dev\large-edit-check'
New-Item -ItemType Directory -Path $runDirectory -Force | Out-Null
$testExe = Join-Path $runDirectory 'editorx-large-edit.test.exe'
& $editorxGo test -mod=vendor -tags editorx_public -c -o $testExe ./internal/editor
if ($LASTEXITCODE -ne 0) { throw '完整编辑原生测试编译失败' }
if (!('VivaIsolatedDesktop' -as [type])) {
    Add-Type -Path (Join-Path $PWD 'tools\IsolatedDesktop.cs')
}
for ($round = 1; $round -le 3; $round++) {
    $log = Join-Path $runDirectory "round-$round.log"
    $exit = [VivaIsolatedDesktop]::Run($testExe, $PWD.Path, $log, 48, 'TestNativeLargeEdit')
    Get-Content -LiteralPath $log -Tail 8
    if ($exit -ne 0) { throw "完整编辑原生测试失败（第 $round 轮）" }
}
```

记录 `LARGE_EDIT` 行中的首屏、输入、查找和保存用时，比较进入完整编辑前、编辑期间峰值与关闭标签 3 秒后的进程私有内存。v0.36.0 在同一份日志上的范围为首屏 164–166 ms、全文查找 283–299 ms、峰值约 212 MiB、关闭后 166–172 MiB。跨机器比较时注明源码提交或 EXE 版本，以及 Windows、样例大小和缩放。 [实测口径及基线](large-files.md)

项目树打开 Office、PDF 等大文件时，使用 `.\tools\large-open.ps1 -Runs 3`，比较 `.dev/large-open-*/results.csv` 中同一份样例的记录。计时从项目树点击开始，文本和 Office 到首次修改生效，PDF 到首屏渲染完成。

## 自动更新检查与首页提示

使用新数据目录确认自动检查默认开启、启动约 10 秒后执行，没有自动弹窗。即使近期已有检查记录，每次重启仍重新检查；持续运行期间间隔 6 小时。断网或 HTTP 错误时自动检查保持静默，手动检查应显示失败原因。关闭「关于 → 自动检查更新」开关后停止自动请求，手动检查仍可用。

本地测试服务提供更高版本时，确认无文件的首页也显示绿色图标，左右侧栏均对齐。打开弹窗，滚动查看完整 Markdown 更新说明；「稍后」保留图标，「跳过此版本」重启后仍生效，下一个版本重新提示，手动检查可查看已跳过版本。界面不提供更新源选择。

多个窗口共用请求，下载时可后台继续编辑，关闭发起下载的窗口后其他窗口仍可查看进度。取消下载和退出应用均清理暂存 EXE。下载长度、SHA-256 或 PE 不符时不能安装；正常完成才显示「安装并重启」。用独立 EXE 和数据目录验收升级前保存所有草稿，保存失败时不退出不替换。设置 `VIVA_PREVIEW_DIR` 后运行 `TestUpdate` 可生成首页、更新说明、滚动到底与下载完成的实际 UI 截图，示例版本为测试夹具，并非已发布版本。

## 发布验证

1. 运行公开版和本地版全量测试，按改动范围运行 Windows 原生用例。定位大文件回归时保留同机、同样例的三轮日志。
2. 用 `.\dev.ps1 -Action build -PublicRelease -OutputDirectory '.dev\release-preview\public'` 构建公开版。确认资源中的 ProductVersion 和 SHA-256，在隔离桌面用 `tools/memory-smoke.ps1` 的 `-ExecutablePath` 分支检查该 EXE 启动和退出。
3. 发布时核对 `main`、版本标签、发布说明中的 SHA-256 和 GitHub 附件摘要。发布页包含 `EditorX.exe` 及 GitHub 生成的源码 ZIP 和 tar.gz。
4. 如需验收自动安装，用独立 EXE 副本与数据目录从上一公开版升级，验证会话保留、替换和回退副本清理。

## 第三批工作流验收

| 场景 | 预期 |
| --- | --- |
| 菜单／命令面板／正文右键打开通用对比；两份磁盘文本、两个标签、未保存草稿 | 选择明确，左侧只读快照、右侧共享当前缓冲；独立窗口可移动缩放；关闭标签释放对比引用 |
| 逐块合并、整份采用、Ctrl+Z、右侧中文输入、Ctrl+S | 一次合并可撤销，不绕过保存冲突；大改动超过撤销预算时不执行 |
| 外部覆盖、原子替换、删除／重建、程序自己的保存 | 外部变化提示，草稿不被覆盖；重读脏文本二次确认；自己的相同文件不误报 |
| 重读进行期间继续编辑，保留当前内容后保存 | 新修改保留；保留操作不绕过原文件哈希检查 |
| 日志追加、截断、删除／重建、暂停／翻页／查找、切换完整编辑 | 只保留末页且字符完整，暂停后不再覆盖视图；不生成日志副本；关闭标签关闭监听 |
| 宏录制输入、粘贴、删除、缩进、移动、中文输入法；命名保存后重开管理 | 步骤可重放，组合候选不进入宏；背景计算后一次应用，Ctrl+Z 撤销整次操作 |
| 宏重复、相对位置越界、输出／步数超限、取消、源文本变化 | 失败不部分修改原文；不记录跨文件／多光标／外部命令 |
| 自定义工具参数含中文／空格、选区标准输入、非零退出、取消、输出超过上限 | 参数边界和输入保留；错误与输出可读；取消结束进程；结果限 1 MiB，预览限 16 KiB |
| UTF-8 → GB18030／UTF-16 LE／UTF-16 BE → UTF-8，中文和 😀、CRLF | 固定目标字节与 BOM 准确，补充平面字符和换行不丢失；无法表示字符时原文件不变 |
| 深浅主题、900×560 小窗口，有工具输出时继续操作 | 字段含义明确，弹框内部滚动，底部保存按钮始终可用 |

专项命令（在仓库目录）：

```powershell
. .\tools\dev-env.ps1
$editorxGo = Initialize-VivaDevEnvironment $PWD
$env:VIVA_PREVIEW_DIR = Join-Path $PWD '.dev/workflow-qa'
& $editorxGo test -mod=vendor -tags editorx_public -count=1 -run '^TestWorkflow' ./internal/editor
.\dev.ps1 -Action test -PublicRelease
.\dev.ps1 -Action test
.\tools\native-smoke.ps1 -PublicRelease -TestName TestNativeHistoryComparisonWindow
```

截图与日志输出到 .dev/workflow-qa，开发试用包使用该目录下 public/EditorX.exe，不替换安装目录或日常 data。普通全量测试不会运行需隔离桌面的原生测试，使用上面的 native-smoke 命令单独复测。

2026-11-27 本批已通过公开版／本地版全量测试、最终工作流／历史比对／大文件专项、原生 TestNativeHistoryComparisonWindow 和实际试用 EXE 的 TestNativeEditorBackground。日志轮转共享读取与原路径重建连续复测 5 次通过，跟随状态确认不启动全文行号索引。GB18030／UTF-16 LE／BE 用固定字节、中文、😀、BOM 和 CRLF 验证往返。验证清单与 SHA-256 见 .dev/workflow-qa/verified.json；这些是有界回归验证，不代表无限时长的内存稳定性证明。
## v0.39.0 发布检查

2026-11-27 公开版与本地版全量测试通过；公开版 Windows 原生检查覆盖 Excel 编辑保存、DOCX 预览、PDF 大纲、文件拖放和独立历史对比窗口移动缩放。资源循环测试已适配后台打开：通过完成回调记录实际标签，所有文件打开后才进入关闭阶段，避免把原活动标签当作尚未完成的新文件。

七种混合文件连续打开关闭 6 轮，每轮关闭后标签数为 0、goroutine 回到 3；第 3～6 轮 GC 后 Go 堆约 13.9 MiB，窗口关闭后约 4.1 MiB、goroutine 为 2。恢复关闭标签的有限内存保留属于预期行为。这是有界资源回归结果，不替代长时间运行观测。发布日志和公开 EXE 保存在 `.dev/release-0.39.0`，不进入源码发布。

## Git 审阅与编辑深度回归

- `TestGitReviewPartialStageHistory`：两个远隔改动、逐块暂存与取消、拒绝旧补丁、工作区字节不变、根提交及普通提交历史、中文空格路径和重命名。
- `TestGitReviewUIAndConflictUndo`：差异窗口按钮、历史按需读取、diff3 标记、保留双方与单次撤销。
- `TestGitWatchAndConflictResolution`：真实临时仓库制造冲突、保存并暂存解决、外部提交与文件变化自动刷新、关闭面板释放监听。
- `TestEditingDepthFoldsAndCompletion`、`TestEditingDepthSplitAndNavigation`、`TestEditingDepthKeyboardColumn` 覆盖通用折叠、Alt+/ 手动补全与撤销、左右编辑焦点、同文件共享撤销、位置前后导航、Alt+Shift 列选择与一次撤销。
- `TestEditingDepthReleasesClosedBuffers`：关闭分屏／冲突编辑文件后回收编辑器对象；恢复关闭标签只保留原有有界文本快照。
- 原生用例：`./tools/native-smoke.ps1 -PublicRelease -TestName TestNativeEditingDepth`，隔离桌面直接发送 Windows Alt+Shift 方向键、文字和 Ctrl+Z，核对列选择及撤销。

专项命令：初始化 `tools/dev-env.ps1` 后执行 `go test -mod=vendor -tags editorx_public -run 'TestGitReview|TestGitWatchAndConflict|TestEditingDepth|TestSessionRestart' ./internal/editor`。截图可用 `VIVA_PREVIEW_DIR` 指定目录。整体测试、原生历史窗口和内存复测仍执行本文原有命令。

`TestDirectoryReplacementKeepsUndoBudget` 和 `TestGitConflictKeepsUndoBudget` 验证超出撤销容量时原文不变且显示错误。目录替换正常路径、未保存草稿、UTF-16/BOM/换行保留和磁盘变更拒绝由 `TestDirectoryReplacementPreservesDraftEncodingAndConflict` 覆盖。

2026-11-27 四批优化已通过公开版、本地版全量测试及最终 Git／目录替换／编辑深度专项。Windows 原生 `TestNativeEditingDepth` 验证 Alt+Shift 方向键、输入和 Ctrl+Z；`TestNativeHistoryComparisonWindow` 验证独立窗口编辑及移动缩放；最终 EXE 通过 `TestNativeEditorBackground` 的启动、恢复、退出保存确认和会话保存。六轮混合文件开关后标签均为 0，goroutine 回到 3；第 3～6 轮 GC 后 Go 堆为 14.02～14.04 MiB，关闭窗口后为 4.09 MiB、goroutine 为 2，进程退出后无残留子进程。日志位于 `.dev/memory-four-batches-20261127-102042`；试用包和截图位于 `.dev/batch-four-qa`。以上为有界回归结果，不代表无限时长的内存稳定性证明。

## IDEA 预设与输入时补全

- `TestIdeaShortcutPreset` 验证通过界面切换并保存预设。实际组合键打开文件、命令和目录替换，切换标签及导航历史，验证补全与正文和搜索框的不同 Ctrl+Y 行为。
- `TestAutomaticCompletionSettingsAndTyping` 验证开关默认关闭、修改后持久化、最后一键后延迟、Enter 换行、Tab 补全词中后缀、单次撤销及中文组词结束后点选候选。
- `./tools/native-smoke.ps1 -PublicRelease -TestName TestNativeEditingDepth` 验证 Windows 的 Alt+/、Ctrl+Alt+Space、Ctrl+Y、无额外输入时自动弹出及 Tab 接受。
- 在仓库目录初始化 `tools/dev-env.ps1` 后，运行 Go 测试的 `-run '^$' -bench '^BenchmarkAutomaticCompletionCandidates$' -benchmem ./internal/editor` 测量一次候选扫描。本机约 15.5 MiB 样本的单次扫描约 0.05 ms、分配 56 字节。此数值不含界面渲染和 150 ms 的输入停顿。
