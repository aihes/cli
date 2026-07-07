# docs +create（创建飞书云文档）

从 XML（默认）或 Markdown 内容创建一个新的飞书云文档。

> **⚠️ 格式选择规则：** 创建 / 导入场景下 XML 和 Markdown 都可以——用户提供 `.md` 本地文件、或明确说"导入 Markdown"时，直接用 Markdown；没有明确指示时默认 XML（表达能力更强，支持 callout、grid、checkbox 等富 block 类型）。不要在用户没要求的情况下主动从 XML 切到 Markdown，也不要在用户已给出 Markdown 时强行改成 XML。

## Document Authoring Loop

从零创作文档时，以本地草稿文件作为唯一正文源（默认 XML；用户明确要求 Markdown 或提供 `.md` 文件时使用 Markdown），主 Agent 串行完成写作、审阅和修订，草稿达标后再调用 `docs +create`。不要先创建线上骨架再让多个 Agent 并行拆章节写入；这容易造成风格漂移、重复铺陈、结构断流和事实口径不一致。

1. **Plan（规划）**：明确受众、目的、范围、体裁、结构和验收标准；事实来源或产品口径不确定时先询问用户，能安全假设时说明假设并继续。
2. **Draft（起草）**：在当前工作目录创建相对路径草稿文件，主 Agent 按文档顺序串行写作；
3. **Observe（观察）**：读取当前草稿，检查结构、重复、断流、事实、语气和格式；
4. **Revise（修订）**：局部修订本地文件，优先调整顺序、补桥接句、删重复、统一术语和修正 XML；不追求第一稿一次完美。
5. **Enrich（增强）**：决定一篇文档是否真正好读、可信、可传播。必要时加入表格、列表、callout、grid、画板、引用或附件；增强必须降低理解成本，不为了“丰富”而装饰。
   - 新增画板按复杂度处理：简单 Mermaid 或 SVG 图可由主 Agent 直接写入本地 XML 草稿；复杂 SVG 可启动 SubAgent 只产出完整 `<whiteboard type="svg">...</whiteboard>` 片段；特别复杂或已有画板更新，再创建空白画板并让 SubAgent 读取 `lark-whiteboard` 完成写入。
   - 创建后才能插入的图片、附件或资源，先在草稿中标记位置和素材路径，Deliver 后用 `docs +media-insert` 等命令补齐并 fetch 验证。
6. **Validate（验证）**：检查用户要求、字数、格式、事实、可读性和风格；任何一项不满足，就回到 Observe / Revise 继续 loop。
7. **Deliver（交付）**：本地草稿通过验证后，用 `docs +create --content @lark-doc-draft.xml` 创建 XML 文档；Markdown 草稿用 `docs +create --doc-format markdown --content @lark-doc-draft.md`。如需创建后插入图片、附件或资源，完成后再次 fetch 确认。

`@file` 只接受当前工作目录下的相对路径，不要传 `@/tmp/xxx.xml` 这类绝对路径。SubAgent 只用于边界清晰的视觉或资源片段（如完整 SVG whiteboard XML），主 Agent 必须审核并合并结果；不要让 SubAgent 并行撰写正文。

## 命令

```bash
# 创建 XML 文档（默认格式，推荐）
lark-cli docs +create --content '<title>项目计划</title><h1>目标</h1><p>记录本周重点。</p>'

# 仅当用户明确要求导入 Markdown 时才使用；文档标题用 --title，正文标题按内容自然组织
lark-cli docs +create --doc-format markdown --title "项目计划" --content $'## 目标\n\n- 明确重点\n- 记录待办'
```

## 返回值

```json
{
  "ok": true,
  "identity": "user",
  "data": {
    "document": {
      "document_id": "docx_token",
      "revision_id": 1,
      "url": "https://xxx.feishu.cn/docx/docx_token",
      "new_blocks": [
        { "block_id": "blkcnXXXX", "block_type": "whiteboard", "block_token": "boardXXXX" }
      ]
    }
  }
}
```

- **`document.new_blocks`**：本次操作新增的 block 列表（如画板）。`block_id` 可用于 `docs +update` 的 `--block-id` 做精确编辑；`block_token` 是资源块（如画板）的 token，可交给 `lark-whiteboard` 等 skill 继续操作

> \[!IMPORTANT]
> 如果文档是**以应用身份（bot）创建**的，如 `lark-cli docs +create --as bot` 在文档创建成功后，CLI 会**尝试为当前 CLI 用户自动授予该文档的 `full_access`（可管理权限）**。
>
> 以应用身份创建时，结果里会额外返回 `permission_grant` 字段，明确说明授权结果：
> - `status = granted`：当前 CLI 用户已获得该文档的可管理权限
> - `status = skipped`：本地没有可用的当前用户 `open_id`，因此不会自动授权；可提示用户先完成 `lark-cli auth login`，再让 AI / agent 继续使用应用身份（bot）授予当前用户权限
> - `status = failed`：文档已创建成功，但自动授权用户失败；会带上失败原因，并提示稍后重试或继续使用 bot 身份处理该文档
>
> `permission_grant.perm = full_access` 表示该资源已授予”可管理权限”。
>
> **不要擅自执行 owner 转移。** 如果用户需要把 owner 转给自己，必须单独确认。

## 参数

| 参数                  | 必填 | 说明                                          |
| ------------------- | -- |---------------------------------------------|
| `--title`           | 否  | 文档标题，Markdown 导入时使用；XML 创建推荐在 `--content` 开头写 `<title>...</title>`；多个标题仅保留第一个并在 `warnings` / `degrade_details` 提示 |
| `--content`         | 视情况 | 文档内容（XML 或 Markdown 格式）；不传 `--content` 时必须传 `--title` |
| `--reference-map` | 否 | 结构化 `reference_map` JSON object；必须与 `--content` 一起使用。普通写入优先把结构写在正文里；该参数主要用于保留或回放已有 `document.reference_map`。支持直接 JSON、`@reference-map.json`（相对路径）或 `-` 从 stdin 读取。 |
| `--doc-format`      | 否  | 内容格式：`xml`（默认，始终优先使用）\| `markdown`（仅用户明确要求时） |
| `--parent-token`    | 否  | 父文件夹或知识库节点 token（与 `--parent-position` 互斥）  |
| `--parent-position` | 否  | 父节点位置，如 `my_library`（与 `--parent-token` 互斥） |

## 参考

- [`lark-doc-design-philosophy.md`](lark-doc-design-philosophy.md) — 文档设计判断与组件取舍
- [`lark-doc-xml.md`](lark-doc-xml.md) — XML 语法规范
- [`lark-doc-fetch.md`](lark-doc-fetch.md) — 获取文档
- [`lark-doc-update.md`](lark-doc-update.md) — 更新文档
- [`lark-doc-media-insert.md`](lark-doc-media-insert.md) — 插入图片/文件到文档
