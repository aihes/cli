# docs +create（创建飞书云文档）

从 XML（默认）或 Markdown 内容创建一个新的飞书云文档。

## 命令

```bash
# 发布已验收的 XML 草稿（默认格式）
lark-cli docs +create --content @lark-doc-draft.xml

# 用户明确要求 Markdown / 导入 Markdown，或提供 .md 文件时，发布已验收的 Markdown 草稿
lark-cli docs +create --doc-format markdown --title "项目计划" --content @lark-doc-draft.md
```

`@file` 只接受当前工作目录下的相对路径，不要传 `@/tmp/xxx.xml` 这类绝对路径。

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
| `--doc-format`      | 否  | 内容格式：`xml`（默认，始终优先使用）\| `markdown`（用户明确要求 Markdown / 导入 Markdown，或提供 `.md` 文件时） |
| `--parent-token`    | 否  | 父文件夹或知识库节点 token（与 `--parent-position` 互斥）  |
| `--parent-position` | 否  | 父节点位置，如 `my_library`（与 `--parent-token` 互斥） |

## 参考

- [`lark-doc-fetch.md`](lark-doc-fetch.md) — 获取文档
- [`lark-doc-update.md`](lark-doc-update.md) — 更新文档
- [`lark-doc-media-insert.md`](lark-doc-media-insert.md) — 插入图片/文件到文档
