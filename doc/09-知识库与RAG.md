# 09 知识库与 RAG

## 定位

`internal/rag/` 实现本地知识库：文档导入（file/url/text）→ 解析 → 分块 → FTS5 索引 → BM25 检索。供 `knowledge_search` 工具与 knowledge 能力自动召回使用；service 层（`KnowledgeService`）负责受管导入与状态机。

## 设计要点

- **loader**（Text / PDF / Docx / HTML 四实现，按 MIME 选择）：单文件上限 60MB（与导入上限对齐，防 OOM 一次性读入）；URL 抓取 5MB / 30s
- **chunker**（默认 500 rune / 50 重叠）：按空行切段落聚合出块；超长单段按句切；Markdown `#` 标题记入块 meta.title
- **受管导入**：file 型一律复制到 `{home}/knowledge/{docID}.{ext}`（DB 不裸引用用户路径，删除级联清理）；扩展名白名单含 txt/md/pdf/json/yaml/xml/csv/html/docx
- **FTS5 虚拟表 `knowledge_chunks_fts`** + trigram 分词器（中文友好）；写入与实体表同事务
- **检索兜底**：trigram 最小窗口 3 字符——2 字中文高频查询（「部署」「报错」）FTS MATCH 必然零命中 → searchLike LIKE 兜底（整串 OR 任一 token，切词共用 pkg.SplitTokens，保留 2 字短词）
- **状态机**：pending → parsing → indexed | failed；索引失败状态落在文档上，可 Reindex 重试

## 核心契约

### 数据库表

| 表 | 关键列 | 说明 |
|---|---|---|
| `knowledge_docs` | `folder_id, name, source_type, source, mime, size_bytes, chunk_count, status, error_msg` | 文档元数据 |
| `knowledge_chunks` | `doc_id, sequence, content, meta_json` | 分块内容 |
| `knowledge_chunks_fts` | FTS5（title, content） | trigram 分词 |

### 接口

```go
type Retriever interface {
    Search(ctx, query string, topK int) ([]Hit, error) // BM25 + 短词兜底
}
// Indexer.Index：加载 → 切分 → 事务内删旧（DB + FTS5）+ 写新 → 状态置 indexed
// 删除：KnowledgeDocRepo.Delete 同事务清 FTS5 行
```

### `knowledge_search` 工具

| 字段 | 类型 | 说明 |
|---|---|---|
| `query` | string | 检索字符串 |
| `top_k` | int | 返回条数（默认 5，上限 50） |

返回带出处的正文块（doc 名 + 标题 + 内容 + bm25 分数）。

## 关键流程

### 入库（Indexer.Index，幂等替换）

```
1. 按 source_type 取内容（file → loader 解析；url → 抓取；text → 原文）
2. PickLoader(MIME) 解析为纯文本
3. DefaultChunker 切块
4. 事务内：删旧 chunks（DB + FTS5）→ 批量写新 → 状态 indexed（chunk_count / size 回填）
```

### 检索（FTS5Retriever.Search）

```
query → pkg.BuildMatchQuery（双引号包裹 token，OR 连接；知识库与记忆共用）
  非空 → FTS5 MATCH + bm25（score 取负翻转为越大越相关）→ join docs 过滤软删
  为空（token 全部 <3 字）→ searchLike：整串 OR 任一 token 的 LIKE 子串扫描
```

### Agent 注入

```
capability/knowledge.Preload（order=40）→ Search(userInput, topK=3)
  → ContextPiece{Key:"knowledge"}（3000 rune 上限）
  → 注入 system（memory 之后、Skill 之前）
  → 另暴露 knowledge_search 工具供模型主动深挖
```

## 约束

- 同一 doc 重复索引走幂等替换（按 doc_id 删旧写新）；不依赖文件名
- FTS5 重建 / 迁移必须用 `trigram` 分词器（中文必须）
- 单文件 ≤ 60MB 一次性读入（防 OOM 上限）；切分后批量入库（100/批）
- 文件型文档的受管副本是索引的唯一数据源；源文件变动不自动同步（需 Reindex）
