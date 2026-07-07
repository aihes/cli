# Lark Doc Genre Routers

本目录按“一级 router 大类模板 →（仅 report/workplace 可选）二类体裁文件”组织文档体裁指南。不要一次读取整个目录。

使用流程：

1. 先读 `../lark-doc-design-philosophy.md` 的 `Route by genre`。
2. 选择一个最匹配的 `_router-*.md`。
3. 多数 router 本身就是一级大类模板，提供写作思路、风格、结构、元素、反模板和 final check；读完即停。
4. 只有 `_router-report.md` 和 `_router-workplace.md` 保留二类 routing。它们明确命中具体体裁时，只读取一个具体体裁文件。
5. 体裁文件提供结构偏好，不是固定模板。用户明确要求、已有文档风格和事实材料优先于体裁默认结构。

## Routers

- `_router-creative.md` — 文学故事大类模板
- `_router-media.md` — 资讯媒体大类模板
- `_router-opinion.md` — 观点评论大类模板
- `_router-knowledge.md` — 知识教程大类模板
- `_router-report.md` — 报告研究大类模板 + 二类 routing
- `_router-consumer.md` — 种草消费大类模板
- `_router-marketing.md` — 营销转化大类模板
- `_router-workplace.md` — 职场协作大类模板 + 二类 routing
- `_router-personal-brand.md` — 个人品牌大类模板
- `_router-platform.md` — 平台原生大类模板

## Retained second-level genre files

`_router-report.md` may route to:

- `research-report.md`
- `data-report.md`
- `white-paper.md`
- `business-analysis.md`

`_router-workplace.md` may route to:

- `memo-brief.md`
- `weekly-report.md`
- `proposal.md`
- `formal-doc.md`
- `official-redhead.md`
- `meeting-minutes.md`
- `retrospective.md`
- `prd.md`
- `technical-doc.md`
- `sop-tutorial.md`

## Expansion rule

新增二类体裁前，先确认一级大类模板无法满足当前任务；新增后必须同步更新对应 router 和 `../lark-doc-design-philosophy.md` 的路由说明，避免 agent 读取多个无关文件。
