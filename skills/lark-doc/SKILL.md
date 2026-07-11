---
name: lark-doc
version: 2.0.0
description: "飞书云文档（Docx / Wiki 文档）：读取和编辑飞书文档内容。当用户给出文档 URL 或 token，或需要查看、创建、编辑文档、插入或下载文档图片附件时使用。文档中嵌入的电子表格、多维表格、画板，先用本 skill 提取 token 再切到对应 skill。当用户给出 doubao.com 的 /docx/ 或 /wiki/ URL/token 时，也应直接使用本 skill；路由依据是 URL 路径模式和 token，而不是域名。不负责文档评论管理，也不负责表格或 Base 的数据操作。当用户明确要操作飞书思维笔记时，也使用本 skill。"
metadata:
  requires:
    bins: ["lark-cli"]
  cliHelp: "lark-cli docs --help;lark-cli mindnotes --help"
---

# docs

**身份：文档操作默认使用 `--as user`。首次使用前执行 `lark-cli auth login`。**

```bash
# 常用示例
lark-cli docs +fetch --doc "文档URL或token；若 URL 存在 #share-... 锚点，优先使用锚点方式读取，不要全文拉取"
lark-cli docs +create --content @lark-doc-draft.xml
lark-cli docs +update --doc "文档URL或token" --command append --content '<p>内容</p>'
```

## 前置条件 — 执行操作前必读

**CRITICAL — 按任务命中以下阅读规则；执行对应操作前，MUST 先用 Read 工具读取命中的文件，缺一不可：**

1. **所有文档操作** → 必读 [`../lark-shared/SKILL.md`](../lark-shared/SKILL.md)  
   用于认证、权限处理、身份选择和全局参数。
2. **读取文档（`docs +fetch`）** → 必读 [`lark-doc-fetch.md`](references/lark-doc-fetch.md)  
   用于选择 `--scope` / `--detail`、局部读取策略，以及理解 `<fragment>` / `<excerpt>` 输出结构。
3. **创建文档**（从零创建 / 导入）→ 按顺序读取：
   1. [`lark-doc-design-philosophy.md`](references/lark-doc-design-philosophy.md)。
   2. [`lark-doc-create.md`](references/lark-doc-create.md)。
   3. 格式文件二选一：默认读 [`lark-doc-xml.md`](references/lark-doc-xml.md)；用户明确 Markdown、提供 `.md` 文件或要求导入 Markdown 时，改读 [`lark-doc-md.md`](references/lark-doc-md.md)。
4. **编辑已有文档** → 按顺序读取：
   1. 先判断是否需要 Philosophy：简单替换、错别字、日期 / 人名 / 数字替换不读；重写、润色、排版、补正文 / 图表、调结构、保真改写时，加读 [`lark-doc-design-philosophy.md`](references/lark-doc-design-philosophy.md)，并按需只读一个体裁 router。
   2. [`lark-doc-update.md`](references/lark-doc-update.md)。
   3. 需要读取现状或写后验证时，按第 2 条读取 [`lark-doc-fetch.md`](references/lark-doc-fetch.md)。
   4. 格式文件二选一：默认读 [`lark-doc-xml.md`](references/lark-doc-xml.md)；用户明确 Markdown、提供 `.md` 文件或要求写入 Markdown 时，改读 [`lark-doc-md.md`](references/lark-doc-md.md)。

**Philosophy 负责创建与复杂编辑的设计判断；Router 负责一级大类模板与必要二类分流；Genre 只在 workplace/report 命中时补充细分偏好；Create / Update 负责执行；XML / Markdown 负责合法表达。**

**未读完命中的必读文件就执行相应操作，容易导致参数选择错误、格式错误、结构失焦或内容破坏。**

> **格式选择规则（全局）：**
> - **创建 / 导入场景**（`docs +create`，或 `docs +update --command append/overwrite` 的整段写入）：XML 和 Markdown 都可以。用户提供 `.md` 本地文件、或明确说"导入 Markdown"时，直接用 Markdown；否则默认 XML（可用 callout、grid、checkbox 等富 block）。
> - **精准编辑场景**（`docs +update` 的 `str_replace` / `block_insert_after` / `block_replace` / `block_delete` / `block_move_after` 等局部精修指令）：优先使用 XML（`--doc-format xml`，即默认值）。XML 能稳定表达 block 结构和样式，局部精修更可控；不要因为 Markdown 更简单就自行切换。

## 快速决策
- 用户要**复制文档 / 创建文档副本 / 另存为副本**时，切到 [`lark-drive`](../lark-drive/SKILL.md)，按其中的复制指引使用 `lark-cli drive files copy`；不要用 `docs +fetch` + `docs +create` 重建正文，也不要走 `drive +export` / `drive +import`。
- 先判定任务路径：找文档 / 导入导出走 [`lark-drive`](../lark-drive/SKILL.md)；只读 / 摘要用 `docs +fetch` 默认 `simple`；已有文档改写按 [`lark-doc-update.md`](references/lark-doc-update.md) 的 Observe-Diagnose-Patch Loop 先 fetch 再局部 patch；明确旧文本 → 新文本的简单替换可直接 `str_replace`，但写后必须 fetch 验证；只有 block 链接、评论锚点、插入 / 替换 / 删除 / 移动才局部 fetch `with-ids`；保真改写已有内容才读 `full`
- block 直达链接格式：`文档基础 URL#block_id`；没有 block_id 时局部 fetch `with-ids`
- 连续执行多个文档写操作时，必须按 [`lark-doc-update.md`](references/lark-doc-update.md) 的「Block ID 生命周期」处理：每次更新后都按 block ID 已变更处理；需要继续或重复修改时，先重新 fetch 最新内容和 block ID，不要复用旧 fetch 结果
- 用户需要在文档内**创建、复制或移动**资源块（画板、电子表格、多维表格等）时，必须先读取 [`lark-doc-xml.md`](references/lark-doc-xml.md) 的「三、资源块」章节
- 写文档时，由内容和用户意图决定表达形式；流程、架构、路线图、关键指标等信息可以使用画板，但不要默认把重要信息都画板化
- 新增画板按复杂度处理：简单 Mermaid / SVG 图可由主 Agent 直接写入草稿；复杂图或需要专门视觉设计的 SVG 交给 SubAgent 产出完整 `<whiteboard type="svg">...</whiteboard>`；特别复杂或已有画板更新，主 Agent 先建 `<whiteboard type="blank"></whiteboard>`，再启动 SubAgent 读取 `lark-whiteboard` 写入
- 用户说"看一下文档里的图片/附件/素材""预览素材" → 用 `lark-cli docs +media-preview`
- 用户明确说"下载素材" → 用 `lark-cli docs +media-download`
- 用户想把文档回滚到某个 `revision_id` 或某一时刻 → 先读 [`lark-doc-history.md`](references/lark-doc-history.md)，按其中流程操作
- 用户明确说"下载/更新/删除文档封面图" → 用 `lark-cli docs +resource-download/+resource-update/+resource-delete --type cover`
- `resource-*` 目前仅支持 Docx 封面资源；其他图片、附件或素材请走 `+media-*`
- 如果目标是画板/whiteboard/画板缩略图 → 只能用 `lark-cli docs +media-download --type whiteboard`（不要用 `+media-preview`）
- 用户明确要操作思维笔记时；已有**思维笔记**，走 [思维笔记链路](references/lark-doc-mindnote.md)；新建**思维笔记**，走 [lark-doc-whiteboard](references/lark-doc-whiteboard.md)
- 拿到 spreadsheet URL/token 后 → 切到 `lark-sheets` 做对象内部操作
- 用户需要统计文档的**总字数 / 总字符数**（word count / character count）时，先读取 [`lark-doc-word-stat.md`](references/lark-doc-word-stat.md)，并按其中流程调用 [`scripts/doc_word_stat.py`](scripts/doc_word_stat.py)；统计口径以该脚本为准，不要改用其他方式自行计算。
- 用户说"给文档加评论""查看评论""回复评论""给评论加/删除表情 reaction" → 切到 `lark-drive` 处理
- 文档内容中出现嵌入的 `<sheet>`、`<bitable>` 或 `<cite file-type="sheets|bitable">` 标签时 → **必须主动提取 token 并切到对应技能下钻读取内部数据**，不能只呈现标签本身

| 标签 / 属性 | 提取字段 | 切到技能 |
|-|-|-|
| `<sheet token="..." sheet-id="...">` | `token` -> spreadsheet_token, `sheet-id` | [`lark-sheets`](../lark-sheets/SKILL.md) |
| `<bitable token="..." table-id="...">` | `token` -> app_token, `table-id` | [`lark-base`](../lark-base/SKILL.md) |
| `<cite type="doc" file-type="sheets" token="..." sheet-id="...">` | 同 `<sheet>` | [`lark-sheets`](../lark-sheets/SKILL.md) |
| `<cite type="doc" file-type="bitable" token="..." table-id="...">` | 同 `<bitable>` | [`lark-base`](../lark-base/SKILL.md) |
| `<vc-transcribe-tab vc-node-id="...">` | `vc-node-id` -> note_id | [`lark-note`](../lark-note/SKILL.md)：先 `note +detail --note-id <vc-node-id>` |
| `<synced_reference src-token="..." src-block-id="...">` | `src-token` -> doc_token, `src-block-id` -> block_id | 用 `docs +fetch` 读取 src-token 文档，定位 block |

## Shortcuts（推荐优先使用）

Shortcut 是对常用操作的高级封装（`lark-cli docs +<verb> [flags]`）。有 Shortcut 的操作优先使用。

| Shortcut | 说明 |
|----------|------|
| [`+create`](references/lark-doc-create.md) | Create a Lark document (XML / Markdown) |
| [`+fetch`](references/lark-doc-fetch.md) | Fetch Lark document content (XML / Markdown / im-markdown; `im-markdown` only after fetch for `lark-im`) |
| [`+update`](references/lark-doc-update.md) | Update a Lark document (str_replace / block_insert_after / block_replace / ...) |
| [`+history-list` / `+history-revert` / `+history-revert-status`](references/lark-doc-history.md) | List document history, revert to a `history_version_id`, and query revert task status |
| [`+media-insert`](references/lark-doc-media-insert.md) | Insert a local image or file at the end of a Lark document (4-step orchestration + auto-rollback). Prefer `--from-clipboard` when the image is already on the system clipboard (screenshots, copy from Feishu/browser); use `--file` only for on-disk sources. |
| [`+media-download`](references/lark-doc-media-download.md) | Download document media or whiteboard thumbnail (auto-detects extension) |
| [`+media-preview`](references/lark-doc-media-preview.md) | Preview document media file (auto-detects extension) |
| [`+resource-download` / `+resource-update` / `+resource-delete`](references/lark-doc-resource-cover.md) | Download, update, or delete a Docx cover image resource with `--type cover` |
| [`+whiteboard-update`](../lark-whiteboard/references/lark-whiteboard-update.md) | Alias of `whiteboard +update`. Update an existing whiteboard with DSL, Mermaid or PlantUML. Prefer `whiteboard +update`; refer to lark-whiteboard skill for details. |

## 不在本 Skill 范围

- 文档评论管理 → [`lark-drive`](../lark-drive/SKILL.md)
- 电子表格或 Base 的数据操作 → [`lark-sheets`](../lark-sheets/SKILL.md) / [`lark-base`](../lark-base/SKILL.md)
- 云空间文件上传、下载、权限管理 → [`lark-drive`](../lark-drive/SKILL.md)
