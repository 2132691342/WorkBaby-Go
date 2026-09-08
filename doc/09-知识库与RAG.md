# 知识库与 RAG

位置：`internal/rag/`（索引与检索）+ `internal/service/knowledge.go`（编排）。

## 数据流

```
导入（文本 / Markdown / PDF / Office / zip）
  → 解析为纯文本
  → DefaultChunker 分块（按段落与长度，保留标题上下文）
  → 写 knowledge_docs（文档行）+ chunks（FTS5 虚表）
  → 检索 = MATCH 查询 + bm25 排序
```

- 文档原文落 `{home}/knowledge/` 受管目录，DB 存元数据与分块。
- 分组（group）用于组织与按组检索；重新索引（reindex）会替换旧分块并重建 FTS 条目。

## 检索接入

- **上下文注入**：能力注册表在每轮 run 前按用户输入检索 top-K 分块注入 system（与记忆召回同一条通道，互不挤占）。
- **工具检索**：`knowledge_search` 工具让模型在对话中主动查知识库。

## 中文支持

FTS5 默认 tokenizer 对中文不友好，索引入口对文本做预处理（按标点 / 字符切分生成可检索 token），保证中文问答能命中；索引与检索两侧使用同一预处理，口径一致。

## 接口

| 端点 | 用途 |
|---|---|
| `GET /kdocs` / `GET /kdocs/:id` | 列表 / 详情 |
| `POST /kdocs` / `POST /kdocs/import-file` | 新增 / 导入本地文件 |
| `GET /kdocs/search` | 检索 |
| `GET /kdocs/groups` / `GET /kdocs/group/:group` | 分组导航 |
| `POST /kdocs/:id/update` / `delete` / `reindex` | 维护 |
