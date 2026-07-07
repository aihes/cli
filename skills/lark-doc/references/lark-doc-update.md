
# docs +update（更新飞书云文档）

通过八种指令精确更新飞书云文档。支持字符串级别和 block 级别的操作。

> **⚠️ 格式选择规则：**
> - **局部精修**（`str_replace` / `block_insert_after` / `block_replace` / `block_delete` / `block_move_after`）：优先使用 XML（默认）。XML 能稳定表达 block 结构和样式，精准编辑更可控；不要因为 Markdown 写起来更简单就自行切换。
> - **整段写入**（`append` / `overwrite`）：XML 和 Markdown 都可以。用户提供 `.md` 本地文件或明确要求 Markdown 时直接用 Markdown；否则默认 XML。
>
> **Markdown 局限 & block ID 前提：** Markdown 不携带 block ID，也无样式（颜色、对齐、callout 等）。需要按 block ID 定位（`block_*` 指令的 `--block-id`）时，先 `docs +fetch --detail with-ids` **配合 `--scope`（`outline` / `range` / `keyword` / `section`）局部获取**目标段落，不要全量 fetch。拿到 block ID 后 `--content` 仍可用 Markdown，只是写入内容不带样式。

## Observe-Diagnose-Patch Loop

适用于修改已有飞书文档：调整语气、精简冗余、增补章节、修复结构混乱、按领导意见修改，或在已有图片、引用、表格、评论、资源块的文档上做保真改写。

> [!IMPORTANT]
> 核心原则：先观察，再诊断，再局部 patch，最后 fetch 验证。这个流程比全文重写安全；除非用户明确要求完全重建，或文档确实已无保留价值，不要轻易使用 `overwrite`，否则会丢失评论和未支持的资源。
> 每次 `docs +update` 后，都按 block ID 已发生变更处理。如果需要继续修改、重复修改同一处内容，或引用刚插入 / 替换后的内容，必须先重新 `docs +fetch --detail with-ids` 拉取最新内容和 block ID，再执行下一轮 patch。

1. **Observe（读取现状）**：先 `docs +fetch` 读取当前文档状态，并按意图选择最小范围。
   - 改某一节或大文档：先 `--scope outline --max-depth 2` 找章节，再 `--scope section --start-block-id <标题id> --detail with-ids`
   - 精确跨节区间：用 `--scope range --start-block-id xxx --end-block-id yyy`
   - 只有模糊关键词：用 `--scope keyword --keyword xxx --context-before 1 --context-after 1 --detail with-ids`
   - 明确整篇重构才读 `--detail with-ids` 全文；只读摘要或确认事实时用更轻的 fetch
2. **Diagnose（诊断问题）**：判断用户目标、当前结构、语气、重复、断流、事实口径和需要保留的资源；识别哪些 block 必须原样保留。
3. **Patch Plan（制定局部计划）**：把修改拆成最小安全操作：简单行内替换用 `str_replace`；整段/整块重写用 `block_replace`；增补章节用 `block_insert_after`；删冗余用 `block_delete`；调整顺序用 `block_move_after`。
4. **Patch（精确修改）**：按 block / section 执行局部命令。保护 `<cite>`、`<img>`、`<source>`、`<whiteboard>`、`<sheet>`、`<bitable>`、`<synced_reference>` 等 token 化内容，不要改成纯文本或占位符。同一 block 的多处修改合并成一次 `block_replace`。
5. **Verify（fetch 验证）**：每轮写操作后按影响范围重新 fetch，检查用户要求、结构、语气、事实、资源块和 block ID 是否符合预期；不满足就基于最新 fetch 结果继续 Diagnose / Patch，不要沿用上一轮 block ID。

复杂结构重组时，优先“先插入新结构，再删除旧 block”：用 `block_insert_after` 插入 grid / table / callout / 新章节，再用 `block_delete` 删除旧段落。这样比 `overwrite` 更能保住图片、评论、引用、资源块和不相关内容。`str_replace` 的匹配范围取决于格式：XML 模式只适合行内匹配；Markdown 模式可跨行和使用 `前缀...后缀`，但跨 block 或容器级重写仍优先用 block 指令。

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--doc` | 是 | 文档 URL 或 token |
| `--command` | 是 | 操作指令（见下方指令速查表） |
| `--doc-format` | 否 | 内容格式：`xml`（默认，始终优先使用）\| `markdown`（仅用户明确要求时） |
| `--content` | 视指令 | 写入内容（`str_replace` 传空字符串可实现删除） |
| `--reference-map` | 否 | 结构化 `reference_map` JSON object；必须与 `--content` 一起使用。普通写入优先把结构写在正文里；该参数主要用于保留或回放已有 `document.reference_map`。支持直接 JSON、`@reference-map.json`（相对路径）或 `-` 从 stdin 读取。 |
| `--pattern` | 视指令 | 匹配文本（str_replace） |
| `--block-id` | 视指令 | 目标 block ID（block_* 操作），逗号分隔可批量删除，-1 表示末尾 |
| `--src-block-ids` | 视指令 | 源 block ID（逗号分隔），用于 block_copy_insert_after / block_move_after |
| `--revision-id` | 否 | 基准版本号，-1 = 最新（默认 `-1`） |

## 指令速查表

| 指令 | 说明 | 必需参数 |
|------|------|----------|
| `str_replace` | 全文文本查找替换（replacement 支持富文本标签；`--content` 传空字符串即为删除） | `--pattern` `--content` |
| `block_insert_after` | 在指定 block 之后插入新内容 | `--block-id` `--content` |
| `block_copy_insert_after` | 复制源 block 并插入到锚点之后（源块不变） | `--block-id` `--src-block-ids` |
| `block_replace` | 替换指定 block（同一 block 仅限一次） | `--block-id` `--content` |
| `block_delete` | 删除指定 block（逗号分隔可批量） | `--block-id` |
| `overwrite` | ⚠️ 清空文档后全文重写（可能丢失图片、评论） | `--content` |
| `append` | ⚠️ 在文档**末尾**追加内容（等价于 `block_insert_after --block-id -1`）。**不适用于逐章填充**——逐章写入请用 `block_insert_after` 并指定对应标题的 `--block-id` | `--content` |
| `block_move_after` | 移动已有 block 到指定位置 | `--block-id` `--src-block-ids` |

## Block ID 生命周期

安全规则：每次写操作后都按 block ID 已变更处理。需要连续修改、重复修改或操作新插入内容时，必须重新 fetch 最新内容和 block ID；不要默认复用之前 fetch 到的 block ID。

- `overwrite` / `block_replace` / `block_delete`：受影响旧 ID 失效，继续 block 级操作前必须重新 fetch
- `block_insert_after` / `append` / `block_copy_insert_after`：新内容一定是新 ID；要操作新内容或继续编辑插入点附近内容，先重新 fetch
- `block_move_after`：位置、章节、range 语义已变化；后续依赖位置或章节边界时重新 fetch
- `str_replace`：即使是简单行内替换，也不要在后续 block 级操作中假设旧 ID 仍正确；跨行 / 大段替换后必须重新 fetch

## 指令示例

### str_replace — 全文文本替换

> **匹配范围：**
> - **XML 模式（默认）**：`--pattern` 只支持**行内匹配**，不能跨 block / 跨段落匹配。涉及整段或多 block 的改动，请改用 `block_replace`。
> - **Markdown 模式**（`--doc-format markdown`）：`--pattern` 同时支持**行内和跨行匹配**，可以用多行字符串匹配并替换一整段内容。
>   - 还支持**`前缀...后缀` 省略号语法**：用 `...`（三个英文句点）串联起始与结束片段，匹配从前缀到后缀之间的全部内容（含中间被省略部分）。适合一段很长、但首尾特征明显的文本，避免把整段都塞进 `--pattern`。
>   - 前缀、后缀本身仍遵循 Markdown 转义规则；省略号中间的内容**会被替换**为 `--content` 的完整文本，不会被保留。

```bash
# 简单文本替换
lark-cli docs +update --doc "<doc_id>" --command str_replace \
  --pattern "张三" --content "李四"

# 替换为富文本（加粗 + 链接）
lark-cli docs +update --doc "<doc_id>" --command str_replace \
  --pattern "旧链接" --content '<b>新链接</b> <a href="https://example.com">点击查看</a>'

# 仅当用户明确要求时才使用 Markdown
lark-cli docs +update --doc "<doc_id>" --command str_replace \
  --doc-format markdown --pattern "旧内容" --content "新内容"

# Markdown 模式下支持跨行匹配（--pattern 与 --content 都需要真实换行；"..."/'...' 里的 \n 是字面量）
# 多行内容推荐 heredoc 或 --content @file.md，避免 shell 转义踩坑
lark-cli docs +update --doc "<doc_id>" --command str_replace \
  --doc-format markdown \
  --pattern "$(printf '## 旧标题\n\n第一段原文\n\n第二段原文')" \
  --content - <<'EOF'
## 新标题

改写后的第一段

改写后的第二段
EOF

# Markdown 模式下使用 `前缀...后缀` 省略号匹配首尾特征明显的大段内容
# 下例会把「## 旧标题」到「结束语。」之间的所有内容整体替换
lark-cli docs +update --doc "<doc_id>" --command str_replace \
  --doc-format markdown \
  --pattern "## 旧标题...结束语。" \
  --content - <<'EOF'
## 新标题

重写后的正文...

新的结束语。
EOF

# 删除文本：--content 传空字符串即可
lark-cli docs +update --doc "<doc_id>" --command str_replace \
  --pattern "废弃的内容" --content ""
```

### block_insert_after — 在指定 block 之后插入

```bash
lark-cli docs +update --doc "<doc_id>" --command block_insert_after \
  --block-id "目标 block_id" \
  --content '<h2>新章节</h2><ul><li>要点 1</li><li>要点 2</li></ul>'
```

### block_replace — 替换指定 block

```bash
lark-cli docs +update --doc "<doc_id>" --command block_replace \
  --block-id "目标 block_id" \
  --content '<p>替换后的段落内容</p>'
```

### block_delete — 删除指定 block

```bash
# 删除多个块时用逗号 "," 分隔
lark-cli docs +update --doc "<doc_id>" --command block_delete \
  --block-id "block_id_1,block_id_2,block_id_3"
```

### overwrite — 全文覆盖

```bash
lark-cli docs +update --doc "<doc_id>" --command overwrite \
  --content '<title>全新文档</title><h1>概述</h1><p>新的内容</p>'
```

> ⚠️ 会清空文档后重写，可能丢失图片、评论等。仅在需要完全重建文档时使用。

### append — 在文档末尾追加

```bash
lark-cli docs +update --doc "<doc_id>" --command append \
  --content '<h2>新增章节</h2><p>追加的内容</p>'
```

> 等价于 `block_insert_after --block-id -1`，无需先获取 block ID。

### block_copy_insert_after — 复制块并插入

将一个或多个源块复制到锚点块之后，源块保持不变。`--src-block-ids` 为逗号分隔的源块 ID，按顺序依次插入到锚点之后。

```bash
# 复制多个块（按顺序插入：anchor → a → b → c）
lark-cli docs +update --doc "<doc_id>" --command block_copy_insert_after \
  --block-id "锚点 block_id" \
  --src-block-ids "block_a,block_b,block_c"
```

### block_move_after — 移动已有 block

将文档中已有的 block 移动到指定锚点之后。使用 `--src-block-ids` 指定要移动的块 ID，无需 `--content`。

```bash
# 移动到页面末尾
lark-cli docs +update --doc "<doc_id>" --command block_move_after \
  --block-id "-1表示末尾，page_id表示开头，blk" \
  --src-block-ids "block_a,block_b"
```

## 返回值

```json
{
  "ok": true,
  "identity": "user",
  "data": {
    "document": {
      "revision_id": 13,
      "new_blocks": [
        { "block_id": "blkcnXXXX", "block_type": "whiteboard", "block_token": "boardXXXX" }
      ]
    },
    "result": "success",
    "updated_blocks_count": 3,
    "warnings": []
  }
}
```

| 字段 | 说明 |
|------|------|
| `result` | `success` \| `partial_success` \| `failed` |
| `updated_blocks_count` | 实际更新的 block 数量 |
| `warnings` | 警告信息列表 |
| `document.new_blocks` | 本次操作新增的 block 列表（如画板）。`block_id` 可用于后续精确编辑；`block_token` 是资源块 token（如画板）可交给 `lark-whiteboard` 等 skill 继续操作 |

## 画板处理

> **`docs +update` 不能直接编辑已有画板的内容。** 本命令只能**新增**画板块；要修改已有画板，先用 `docs +fetch` 取到 `<whiteboard token="...">`，再按 [`lark-doc-whiteboard.md`](lark-doc-whiteboard.md) 启动 SubAgent 读取 [`lark-whiteboard`](../../lark-whiteboard/SKILL.md) 并写入。

新增画板的语法选型见 [`lark-doc-xml.md`](lark-doc-xml.md) 的资源块说明；插入和复杂画板处理见 [`lark-doc-whiteboard.md`](lark-doc-whiteboard.md)。

## 参考

- [`lark-doc-design-philosophy.md`](lark-doc-design-philosophy.md) — 文档设计判断与组件取舍
- [`lark-doc-xml.md`](lark-doc-xml.md) — XML 语法规范
- [`lark-doc-fetch.md`](lark-doc-fetch.md) — 获取文档
- [`lark-doc-create.md`](lark-doc-create.md) — 创建文档
- [`lark-doc-media-insert.md`](lark-doc-media-insert.md) — 插入图片/文件到文档
