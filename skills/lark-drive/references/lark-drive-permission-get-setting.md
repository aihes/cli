# drive +permission-get-setting（查询权限设置）

本 skill 对应 shortcut：`lark-cli drive +permission-get-setting`。它读取文件、文件夹或云文档自身的公开访问、分享、协作者管理、安全与评论权限设置。

## 适用场景

- 用户明确要查看“权限设置”“分享设置”“公开访问 / 协作者管理 / 安全 / 评论权限”。
- 输入是 Lark / Drive URL，或已经拿到裸 token 并能明确资源类型。
- 只需要读取当前目标自身设置，不需要递归扫描子文件、子文件夹或文档权限。

如果用户要做文件夹下所有文档的权限风险报告、批量整改、owner 转移或密级标签治理，进入 [`lark-drive-workflow-permission-governance.md`](lark-drive-workflow-permission-governance.md)。

## 命令

```bash
# 通过 URL 查询，type 会从 URL 自动推断
lark-cli drive +permission-get-setting \
  --token "https://example.feishu.cn/drive/folder/fldcnxxxxxxxxx" \
  --as user --format json

# 通过裸 folder token 查询
lark-cli drive +permission-get-setting \
  --token "fldcnxxxxxxxxx" --type folder \
  --as bot --format json

# 通过 docx URL 查询
lark-cli drive +permission-get-setting \
  --token "https://example.feishu.cn/docx/doxcnxxxxxxxxx" \
  --as user --format json
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--token` | 是 | 目标 URL 或裸 token。URL 会自动推断 token 和 type；裸 token 必须同时传 `--type`。 |
| `--type` | 裸 token 必填 | 目标类型。可选值：`doc`、`sheet`、`file`、`wiki`、`bitable`、`docx`、`mindnote`、`minutes`、`slides`、`folder`。 |

URL 与显式 `--type` 不一致时会被拒绝。身份与输出格式沿用全局参数约定：按需使用 `--as user|bot`；自动化解析时使用 `--format json`。权限取决于当前身份是否能访问目标，以及应用 / 用户授权是否满足 API 要求。

## 输出

成功时 `data` 只返回 `permission_public`，并完整透传服务端当前返回的权限设置：

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

`permission_public` 是服务端当前返回的完整权限设置对象。

## 边界

- 只读操作，不修改权限，不需要 `--yes`。
- 只查询目标自身设置；对文件夹不会递归读取子文件夹或子文档权限。

## 常见错误

| 症状 | 原因 | 处理 |
|------|------|------|
| `--token is required` | 没传目标 | 传目标 URL 或裸 token。 |
| `--type is required when --token is a bare token` | 裸 token 无法自动推断类型 | 补充 `--type docx|folder|file|...`。 |
| `unsupported --token URL` | URL 不是当前 parser 支持的文档、文件、wiki 或 folder 路径 | 确认 URL 类型；裸 token 场景直接传 `--token` 和 `--type`。 |
| `--type ... conflicts with URL path type ...` | URL 已能推断类型，但显式 `--type` 不一致 | 删除 `--type`，或改成与 URL 匹配的类型。 |
| Permission denied / missing scope | 当前身份无目标访问权或缺 `docs:permission.setting:read` 授权 | 按 [`lark-shared`](../../lark-shared/SKILL.md) 处理。bot 不能访问用户私有目标时，改用 `--as user` 或先授权 bot。 |
