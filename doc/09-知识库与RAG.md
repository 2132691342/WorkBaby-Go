# 09 知识库与 RAG

## 定位

`internal/rag/` 实现本地知识库：文档入库（PDF / Word / Excel / 文本）→ 分块 → FTS5 索引 → 检索召回。供 `knowledge_search` 工具使用。

## 设计要点

- **chunker** 按段落 + 长度阈值切（默认 800 字符），保留 `chunk_index` 与 `source_doc_id` 关联
- **FTS5 虚拟表 `knowledge_chunk_fts`** + trigram 分词器（与 memory 共用分词策略，中文友好）
- **检索** FTS5 match + BM25 score → top-k chunks → 拼装为 ContextPiece
- **重建**：删除该 doc 旧 chunks + FTS5 行 → 重新入库（幂等）
- **快照路径**：`{dataHome}/knowledge/{doc_id}.txt` 持久化提取出的纯文本（不重复解析原文件）

## 核心契约

### 数据库表

| 表 | 列 | 说明 |
|---|---|---|
| `knowledge_docs` | `id, name, mime, size, source_path, snapshot_path, status, ts` | 文档元数据 |
| `knowledge_chunks` | `id, doc_id, idx, content, token_count, ts` | 分块内容 |
| `knowledge_chunk_fts` | FTS5 虚拟表（content 全文检索） | trigram 分词 |

### Retriever 接口

```go
type Retriever interface {
    Index(ctx, doc Doc, chunks []Chunk) error         // 原子替换（删旧+写新+FTS5 同步）
    Search(ctx, query string, topK int) ([]Hit, error) // BM25 + 内容
    Delete(ctx, docID string) error
}
```

### `knowledge_search` 工具入参 / 返回

| 字段 | 类型 | 说明 |
|---|---|---|
| `query` | string | 检索字符串 |
| `top_k` | int | 返回条数（默认 5） |
| `doc_ids` | []string | 限定文档（可选） |

返回 `{hits: [{doc_id, chunk_id, idx, content, score}]}`。

## 关键流程

### 入库（`Index`）

```
1. 解析文件 → 纯文本（PDF/Word/Excel 各 loader）
2. 落盘 snapshot_path（便于重建与导出）
3. chunker.Split(text) → []Chunk
4. tx := db.Begin
   delete old chunks for doc_id (DB + FTS5)
   insert new chunks (DB + FTS5 sync)
   tx.Commit
5. update docs.status = "indexed"
```

### 检索（`Search`）

```
query → FTS5 match('knowledge_chunk_fts', query)
       + BM25 score
       + filter by doc_ids（若指定）
       → topK
       → join knowledge_chunks 取内容
       → return []Hit
```

### Agent 注入

```
Preload → knowledge.Search(query=user_input, topK=3)
  → 转为 ContextPiece{Key:"knowledge", Title:"相关资料", Body: hits}
  → 注入 system（排在 memory 后、Skill 前）
```

## 约束

- 同一 doc 重复入库走幂等替换（按 `doc_id` 删旧写新）；不依赖文件名
- FTS5 重建 / 迁移必须用 `trigram` 分词器（中文必须）
- 大文件按 chunker 流式切，不一次性读全文
- 快照路径**只存纯文本**，不再含原文件二进制
