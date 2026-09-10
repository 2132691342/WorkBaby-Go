# 知识库与 RAG

本地文档检索增强：导入 → 分块 → 索引 → 检索 → 注入上下文 / 工具调用。

## 数据与落点

| 表 | 说明 |
|---|---|
| knowledge_docs | 文档元信息：名称、来源、来源类型（file / url / text）、mime、大小、chunk_count、状态（pending / parsing / indexed / failed）、错误信息 |
| knowledge_chunks | 分块：doc_id、sequence、content、tokens、meta |

- 导入的本地文件复制到受管目录 `{home}/knowledge`，与用户原文件解耦。
- 索引状态有明确状态机：导入 → parsing → indexed（失败落 failed + 原因，可重试）。

## 索引

- 分块：按配置的分块器切分文本，chunk 顺序与元信息落 `knowledge_chunks`，同时写入 FTS5 虚拟表（`tokenize='trigram'`，中文可用）。
- 重建语义是**替换而非追加**：reindex 时先删该文档的旧 chunks 与 FTS 行再写新块，避免重复命中与陈旧内容。
- 索引在后台进行，不阻塞导入请求返回。

## 检索

- 主路径：FTS5 `MATCH` + BM25 排序，多词按词 OR 组合。
- 兜底：查询不足 3 字符（trigram 最小窗口）时退化子串扫描并按相关度重排。
- 结果带出处（文档名与片段），供模型引用与前端展示。

## 与 Agent 的集成

- 上下文注入：用户输入达到最小长度（2 rune）时自动召回 topK=3（上限 3000 字符）作为知识库段注入 system。
- 工具：`knowledge_search(query, topK)` 供模型主动检索（只读工具，免审批）。

## 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /kdocs | 文档列表 |
| POST | /kdocs | 新增（文本 / URL） |
| POST | /kdocs/import-file | 导入本地文件到受管目录并后台索引 |
| GET | /kdocs/search | 检索（q + limit） |
| GET | /kdocs/groups | 分组列表 |
| GET | /kdocs/group/:group | 按分组列文档 |
| GET | /kdocs/:id | 文档详情 |
| POST | /kdocs/:id/update | 更新元信息 |
| POST | /kdocs/:id/delete | 删除（含 chunks 与 FTS 行） |
| POST | /kdocs/:id/reindex | 重建索引（替换语义） |

## 不变量

- 导入文件只读受管副本，不改动用户原文件。
- reindex 为替换语义，不产生重复 chunk 或陈旧 FTS 行。
- 检索结果必有出处；短查询有兜底路径，不静默返回空。
- 召回有长度与条数上限，不挤占上下文预算。
