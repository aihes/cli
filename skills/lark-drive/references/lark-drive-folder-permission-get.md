# drive +folder-permission-get（查询文件夹权限设置）

> **前置条件：** 先阅读 [`../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、身份选择、全局参数和权限错误处理。

本 skill 对应 shortcut：`lark-cli drive +folder-permission-get`。它直接读取 Drive 文件夹自身的公开访问和协作权限设置。
## 适用场景

- 用户明确要查看“文件夹权限设置”“文件夹分享设置”“文件夹公开访问 / 协作者管理 / 安全 / 评论权限”。
- 输入是 `/drive/folder/<folder_token>` URL，或已经拿到裸 `folder_token`。
- 只需要读取当前文件夹自身设置，不需要递归扫描子文件、子文件夹或文档权限。

如果用户要做文件夹下所有文档的权限风险报告、批量整改、owner 转移或密级标签治理，进入 [`lark-drive-workflow-permission-governance.md`](lark-drive-workflow-permission-governance.md)。

## 命令

```bash
# 通过文件夹 URL 查询
lark-cli drive +folder-permission-get \
  --url "https://example.feishu.cn/drive/folder/fldcnxxxxxxxxx" \
  --as user --format json

# 通过裸 folder token 查询
lark-cli drive +folder-permission-get \
  --folder-token "fldcnxxxxxxxxx" \
  --as bot --format json
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--url` | 二选一 | Drive 文件夹 URL，必须是 `/drive/folder/<folder_token>` 路径。非 folder URL 会被拒绝。 |
| `--folder-token` | 二选一 | 裸 folder token。适合已经从 `drive +inspect`、`drive files list` 或其他流程中拿到 token 的场景。 |

`--url` 与 `--folder-token` 必须且只能传一个。不要把文档、Wiki、Sheet、Base 或普通文件 URL 传给本 shortcut。

身份与输出格式沿用全局参数约定：按需使用 `--as user|bot`；自动化解析时使用 `--format json`。权限取决于当前身份是否能访问该文件夹，以及应用 / 用户授权是否满足 API 要求。

## 输出

成功时 `data` 只返回 `permission_public`，并完整透传服务端当前返回的公共访问和协作权限设置：

```json
{
  "ok": true,
  "identity": "user",
  "data": {
    "permission_public": {
      "comment_entity": "anyone_can_edit",
      "external_access_entity": "open",
      "link_share_entity": "anyone_readable",
      "lock_switch": false,
      "manage_collaborator_entity": "collaborator_can_edit",
      "security_entity": "only_full_access",
      "share_entity": "same_tenant"
    }
  }
}
```

`permission_public` 是服务端当前返回的完整权限设置对象。字段可能随 OpenAPI 演进增加或缺失；只根据实际返回字段做判断，不要臆造未返回的权限状态。JSON `data` 不包含 `type`、`folder_token` 或 `url`；如需定位目标，复用调用命令中的 `--url` / `--folder-token` 输入。

`--dry-run` 输出只展示待请求的 API、method 和 params，不额外输出顶层 `folder_token`。

## 边界

- 只读操作，不修改权限，不需要 `--yes`。
- 只查询文件夹自身设置，不递归读取子文件夹或子文档权限。
- 不返回协作者列表、继承链、历史权限变更、访问记录、DLP 或 AI 索引状态。
- 本 shortcut 是 folder-only；其他文档类型继续使用 `drive permission.public get` 或权限治理 workflow。
- 当前 raw command schema 未把 `folder` 纳入 `drive permission.public get --type`，不要用 raw command 猜 `type=folder`；文件夹读取走本 shortcut 的 v2 endpoint。

## 常见错误

| 症状 | 原因 | 处理 |
|------|------|------|
| `--url and --folder-token are mutually exclusive` | 同时传了两种输入 | 只保留一个输入。 |
| `--url or --folder-token is required` | 没传目标文件夹 | 传 `/drive/folder/<token>` URL 或裸 `folder_token`。 |
| `--url must be a Drive folder URL` | URL 不是 `/drive/folder/<token>` | 先确认资源类型；文档 / Wiki / Sheet 不走本 shortcut。 |
| Permission denied / missing scope | 当前身份无文件夹访问权或缺授权 | 按 [`lark-shared`](../../lark-shared/SKILL.md) 处理。bot 不能访问用户私有文件夹时，改用 `--as user` 或先授权 bot。 |
