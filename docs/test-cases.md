# EditorX 测试用例

自动化测试使用生成的样例和本地服务。手工验证时使用独立的 EXE 和 `VIVA_EDITOR_DATA_DIR`，样例、日志及 WebView2 用户数据放在项目的 `.dev` 下。

## 自动化回归

在项目根目录执行。两套测试分别覆盖公开版的 DOCX 只读预览和本地构建的 Word 编辑路径。

```powershell
.\dev.ps1 -Action test -PublicRelease
.\dev.ps1 -Action test
```

| 改动范围 | 对应自动化入口 |
| --- | --- |
| 打开、保存和会话恢复 | `TestFileDropOpensDocuments`、`TestSessionExternalChangesAndRecovery`、`TestSessionIMEAndExplicitClose` |
| 查找、替换和 JSON | `TestSearchReplaceCurrentFileWithMultilineInput`、`TestSearchReplaceAllOpenWithEscapesAndMixedEndings`、`TestJSONAutoWrapAndReversibleConversions`、`TestJSONFoldSummaryRenderingAndPreservation`、`TestJSONSourceKeepsFreeInput` |
| 链接打开和落盘 | `TestLinkedFileWithHTTPSRedirectAndDifferentFormats`、`TestLinkedFileSaveChoiceKeepsOriginalBytes`、`TestLinkedFileDraftsAndAttachmentsAfterRestart` |
| 标签、项目和 ZIP | `TestPinnedGroupRestoresPinsAndAllowsExplicitClose`、`TestMultipleProjectsThroughUIAndSession`、`TestMergeBackupKeepsSeparateDraftsAndGroupIDs`、`TestLegacySessionRestoresImportedWindowsIntoTheMainWindow` |
| 设置和 AI | `TestEditorSettingsApplyAcrossWindowsAndRestore`、`TestEditorShortcutRebinding`、`TestAIHistoryRestoresReviewAgainstOriginalDocument` |
| 侧栏设置 | `TestSidebarPreferencesAndTemplatePanel`、`TestSidebarAcrossWindowsAndSession`、`TestSidebarButtonsWithoutDocumentAndAlignment` |
| 模板与翻译 | `TestDocumentTemplatesThroughUI`、`TestLocalTranslationThroughUI`、`TestSelectionTranslationAIThroughUI`、`TestDownloadedDictionaryThroughUI`、`TestDictionaryDownloadIntegrityAndCleanup`、`TestAITranslationWithoutDictionary` |
| 离线组件与压缩下载 | `TestOfflineComponentIntegrityAndCleanup`、`TestOfflineTranslationOptIn`、`TestOfflineDownloadStopsWithPanel`、`TestDictionaryCompressedTransfer`、`TestDictionaryDownloadStopsWithPanel` |
| Git | `TestProjectGitStageCommitPushFetchAndPull` |
| 应用内更新 | `TestUpdateConditionalRequestAndPersistentCache`、`TestUpdateAutomaticChecksShareWorkAndRespectOptOut`、`TestUpdateHomeIndicatorSkipAndManualCheck`、`TestUpdateDownloadContinuesAcrossWindowsAndCleansCancellation`、`TestUpdateDownloadsVerifiedExecutableAndPreservesPrevious` |

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

首次运行用独立数据目录，确认模板库和翻译图标默认不显示。在没有打开文件时打开侧栏开关，确认设置页和空白页立即显示相应按钮，保存／查找／折行置灰，AI／Git／终端和模板入口可用。核对两列开关对齐和深浅主题的开关状态。切换左右位置，确认面板随之移动，重启验证。模板侧栏中选择、填写、预览并新建或插入，确认编辑区可继续操作。存为模板时，变量名输入仅在设为变量时出现，并可从「保存到」选择个人库或项目库。

翻译面板顶部选择「本地词库」或「离线整句」。两个页面的原文下方都应显示 AI 操作。无任何下载时用已配置的 AI 服务翻译，验证只发送当前原文及语言方向。单服务直接开始，多服务先选择。分别从翻译页和右上角「词库管理」验证下载入口、下载进度、取消、失败重试和移除。用 `hello`、`files` 和一段英文分别查词与查看词义参考，段落不应被词义拼接替换。保存到个人或项目库后再次查找优先命中自建库，移除下载词库后仍可查找自建词条。

离线整句未安装时，「下载离线组件」与「AI 翻译」并排显示。点击下载才创建组件目录，安装后同一位置可开始离线翻译。打开词库管理或切换页面时下载能继续，取消或关闭面板后清理临时目录。用安装后的两种语言方向翻译短句、段落和带占位符的多行原文。结果区只显示译文和操作，检查替换、撤销及占位符验证。完成或取消后检查没有组件目录下的 `python.exe` 驻留，通过词库管理移除已装组件。

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

```powershell
.\tools\large-open.ps1 -Runs 3
```

记录 EXE 版本、Windows 和硬件、样例大小及每次耗时。以项目树点击为起点，文本和 Office 到首次修改生效为止，PDF 到首屏渲染完成为止。使用 `.dev/large-open-*/results.csv` 比较相同样例的结果，历史基线和生成方式见 [大文件首次打开实测](large-files.md)。

完整编辑复测使用 `TestNativeLargeEdit`。将 `VIVA_LARGE_EDIT_FIXTURE` 指向含 `Cache-Control: no-cache` 的日志，测试自行复制样例，然后用原生窗口验证中文输入、撤销重做、全文查找和流式保存。输出记录进程私有内存、首屏、查找、保存及会话大小。`TestLargeEdit` 前缀测试覆盖跨视口滚动、输入法提交与取消、多种编码、混合换行和草稿恢复。

## 自动更新检查与首页提示

使用新数据目录确认自动检查默认开启、启动约 10 秒后执行，没有自动弹窗。近期已有检查记录时不重复请求，后续间隔 6 小时。断网或 HTTP 错误时自动检查保持静默，手动检查应显示失败原因。关闭「关于 → 自动检查更新」开关后停止自动请求，手动检查仍可用。

本地测试服务提供更高版本时，确认无文件的首页也显示绿色图标，左右侧栏均对齐。打开弹窗，滚动查看完整 Markdown 更新说明；「稍后」保留图标，「跳过此版本」重启后仍生效，下一个版本重新提示，手动检查可查看已跳过版本。界面不提供更新源选择。

多个窗口共用请求，下载时可后台继续编辑，关闭发起下载的窗口后其他窗口仍可查看进度。取消下载和退出应用均清理暂存 EXE。下载长度、SHA-256 或 PE 不符时不能安装；正常完成才显示「安装并重启」。用独立 EXE 和数据目录验收升级前保存所有草稿，保存失败时不退出不替换。设置 `VIVA_PREVIEW_DIR` 后运行 `TestUpdate` 可生成首页、更新说明、滚动到底与下载完成的实际 UI 截图，示例版本为测试夹具，并非已发布版本。
