# Lark Sheet History

## 概念回顾

每张飞书电子表格保留一串历史版本（`minor_histories`）。每个版本由 `history_version_id` 标识，并附带创建时间（`create_time`）、动作（`action`）与块修订信息（`all_block_revision`）。历史是**工作簿级**的（针对整张电子表格，不针对单个子表）。

回滚（revert）把电子表格的当前内容覆盖回某个历史版本——这是一个**写入 / 不可逆**操作，且为**异步**：发起后立即返回受理标识，真正的回滚在后台进行，需通过状态查询轮询最终结果（进行中 / 成功 / 失败）。

`+undo` 是另一类撤销：撤销当前 CLI 用户在该电子表格里最近一次或最近多次由 AI 工具产生、且尚未撤销的写入。每个用户有独立 undo 栈；多人编辑同一篇文档时，只撤当前用户自己的 undo 栈，不撤其他用户的操作。它和历史版本回滚不同，不需要选择 `history_version_id`，也不是把整表恢复到某个历史快照。

`+history-list` 读取版本列表以挑选目标；`+history-revert` 发起回滚；`+history-revert-status` 轮询回滚结果；`+undo` 撤销当前用户最近的 AI 工具写入。若只是想拿**当前文档版本号（revision）**当作 recover / undo / `+changeset-get` 的起点锚点，直接用 `+revision-get` 更轻量。

## 使用场景

读取历史版本、发起回滚、查询回滚状态，或撤销当前用户最近一次 AI 工具写入。本 reference 覆盖 4 个 shortcut：

| 操作需求 | 使用工具 | 说明 |
|---------|---------|------|
| 查看历史版本列表 | `+history-list` | 返回 `minor_histories`，每条含 `history_version_id` / `create_time` / `action` / `all_block_revision` 四个字段；支持向前分页（可选 `--end-version`） |
| 回滚到指定历史版本 | `+history-revert` | 传入 `--history-version-id`；异步受理，返回可查询标识 |
| 查询回滚状态 | `+history-revert-status` | 传入 `--transaction-id`（取自 `+history-revert` 的异步受理标识）；轮询某次回滚的进行中 / 成功 / 失败状态 |
| 撤销自己最近的 AI 写入 | `+undo` | 只需 spreadsheet 定位；默认撤当前用户 undo 栈顶；可用 `--count` 连续撤销多步；支持普通单元格/结构写入，以及图表、透视表 update 的反向操作 |

典型工作流：`+history-list` 拿到目标版本的 `history_version_id`（必要时翻页拉取更早历史）→ `+history-revert` 发起回滚并取回 `transaction_id` → `+history-revert-status --transaction-id <transaction_id>` 轮询直到成功或失败。

`+undo` 的典型工作流更短：执行某个 AI 写入工具后，如果用户要求撤销刚才自己的修改，直接运行 `+undo`。返回 `undone=1` 表示已撤销一条当前用户栈顶；传 `--count N` 时会按当前用户栈顶从新到旧连续撤销，返回的 `undone` 是实际撤销数量。返回 `undone=0` 且 `reason=undo_stack_empty` 表示当前用户没有可撤销项。

**注意事项（必须了解）**：
- **回滚是写入 / 不可逆操作**：会用历史版本内容覆盖当前表格，发起前请确认目标 `history_version_id` 正确。
- **回滚是异步的**：`+history-revert` 返回的是 `transaction_id`（受理标识），不代表回滚已完成；必须用 `+history-revert-status --transaction-id <transaction_id>` 确认最终结果。
- **`history_version_id` 与 `transaction_id` 不是同一个**：`history_version_id` 用于 `+history-revert`（取自 `+history-list`）；`transaction_id` 用于 `+history-revert-status`（取自 `+history-revert` 的输出）。
- **历史是工作簿级**：定位只需 `--url` / `--spreadsheet-token`（XOR），不需要子表选择器。
- **`+history-list` 倒序分页**：首次查省略 `--end-version`，返回最新一页；若响应里附带 `next_end_version` 与 `has_more=true`，把 `next_end_version` 作为下一次的 `--end-version` 即可继续向更早翻页；当响应**不包含**这两个字段时表示已到最早一页，不必再翻。
- **`+undo` 是用户维度**：只撤当前登录用户的 undo 栈顶；同一文档里其他用户的写入不会被撤销。
- **`+undo --count` 是顺序多步撤销**：从当前用户最新栈项开始逐条撤销，最多 20 步；如果可撤销项不足或中途遇到不可撤销项，会停止并返回实际 `undone` 数量与 `reason`。
- **`+undo` 不区分 session / agent**：同一用户的不同 CLI 会话或 agent 共用同一个用户 undo 栈。
- **`+undo` 不进入 `+batch-update`**：撤销本身依赖用户栈状态，不能作为批量子操作嵌套执行。

## Shortcuts

| Shortcut | Risk | 分组 |
| --- | --- | --- |
| `+history-list` | read | 历史版本 |
| `+history-revert` | write | 历史版本 |
| `+history-revert-status` | read | 历史版本 |
| `+undo` | write | 历史版本 |

## Flags

### `+history-list`

_公共：URL/token（无 sheet 定位） · 系统：`--dry-run`_

| Flag | Type | 必填 | 说明 |
| --- | --- | --- | --- |
| `--end-version` | int | optional | 分页查询的最大版本（倒序）；首次查询省略，下一页传上一页返回的 next_end_version。 |

### `+history-revert`

_公共：URL/token（无 sheet 定位） · 系统：`--dry-run`_

| Flag | Type | 必填 | 说明 |
| --- | --- | --- | --- |
| `--history-version-id` | string | required | 要回滚到的历史版本（取自 +history-list） |

### `+history-revert-status`

_公共：URL/token（无 sheet 定位） · 系统：`--dry-run`_

| Flag | Type | 必填 | 说明 |
| --- | --- | --- | --- |
| `--transaction-id` | string | required | 异步回滚的受理标识（取自 +history-revert） |

### `+undo`

_公共：URL/token（无 sheet 定位） · 系统：`--dry-run`_

| Flag | Type | 必填 | 说明 |
| --- | --- | --- | --- |
| `--count` | int | optional | 连续撤销的用户 undo 栈项数量，默认 1，最大 20。按当前用户栈顶从新到旧顺序撤销，实际撤销数量以返回的 undone 为准。 |

## Examples

公共定位：所有 shortcut 顶部排列 `--url` / `--spreadsheet-token`（XOR，二选一）。`+history-revert` 用 `--history-version-id`（取自 `+history-list`）；`+history-revert-status` 用 `--transaction-id`（取自 `+history-revert` 的异步受理标识）。`+undo` 不需要 sheet-id，也不需要 history version；连续撤销多步时传 `--count`。

### `+history-list`

```bash
# 列出某张电子表格的最新一页历史版本
lark-cli sheets +history-list --url "https://sample.feishu.cn/sheets/SHTxxxxxx"

# 用原始 spreadsheet token 定位
lark-cli sheets +history-list --spreadsheet-token "SHTxxxxxx"

# 翻到下一页：把上次响应里的 next_end_version 作为 --end-version 传入
lark-cli sheets +history-list --url "https://sample.feishu.cn/sheets/SHTxxxxxx" --end-version 12345
```

### `+history-revert`

```bash
# 回滚到指定历史版本（异步受理）
lark-cli sheets +history-revert --url "https://sample.feishu.cn/sheets/SHTxxxxxx" --history-version-id "<id-from-history-list>"
```

### `+history-revert-status`

```bash
# 查询某次回滚的当前状态（进行中 / 成功 / 失败）
lark-cli sheets +history-revert-status --url "https://sample.feishu.cn/sheets/SHTxxxxxx" --transaction-id "<transaction-id-from-history-revert>"
```

### `+undo`

```bash
# 撤销当前用户最近一次 AI 工具写入
lark-cli sheets +undo --url "https://sample.feishu.cn/sheets/SHTxxxxxx"

# 连续撤销当前用户最近 3 次 AI 工具写入；实际撤销数量以返回的 undone 为准
lark-cli sheets +undo --url "https://sample.feishu.cn/sheets/SHTxxxxxx" --count 3

# 先预览将调用的底层 undo_last 请求
lark-cli sheets +undo --url "https://sample.feishu.cn/sheets/SHTxxxxxx" --count 3 --dry-run
```
