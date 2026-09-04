# 09 · 知识库与 RAG

定义 `internal/rag/`：文档加载 → 切分 → FTS5 索引 → 统一检索接口。
检索主力是 SQLite FTS5（trigram + BM25）：零外部依赖、中文友好、离线可用。

---

## 1. 体系

```
上传（file / url / text）
  ↓
Loader（按 MIME 分发：pdf / docx / md / txt / html）
  ↓
Chunker（按段落聚合，~500 字符/块，50 字符 overlap）
  ↓
Indexer（事务内：写 knowledge_chunks + 同步 knowledge_chunks_fts）

检索：
Query → Retriever.Search → Hits[{docName, content, score, meta}]
```

**导入来源**：
- 手动录入：`url` / `text` / `file`（本地路径），经 `POST /api/v1/kdocs`；
- 受管文件导入：`POST /api/v1/kdocs/import-file`（原生对话框选路径）。

**file 型统一受管**：无论从哪个入口新增或换源，`source_type=file` 的文件一律复制进 `{home}/knowledge/{docid}.{ext}`（扩展名白名单 + ≤60MB 校验），DB 只存受管路径，删除文档时级联清理副本——源文件删除不影响文档与索引。

## 2. 文件结构

```
internal/rag/
├── loader.go    # 多格式文本抽取（txt/md/pdf/docx/html/url）
├── chunker.go   # 文本切分（段落优先 + 句子边界硬切 + overlap）
├── indexer.go   # 文档 → 分块 → FTS5 落库索引
└── retriever.go # 统一检索接口 + FTS5 实现
```

## 3. Loader

```go
type Loader interface {
    Load(ctx context.Context, source string) (string, error)  // 返回纯文本
}
func PickLoader(mime string) Loader
```

| Loader | 说明 |
|---|---|
| TextLoader | .txt / .md |
| PDFLoader | ledongthuc/pdf（纯 Go 轻量）；扫描版无文本层报错 |
| DocxLoader | archive/zip 读 word/document.xml |
| HTMLLoader | 剥离 script/style/标签（自研 StripHTML） |
| URLLoader | 30s 超时、5MB 上限、仅 http/https |

## 4. Chunker

```go
type Chunker struct {
    ChunkSize        int  // 默认 500
    Overlap          int  // 默认 50
    RespectParagraph bool // 段落优先
}
```

算法：按空行切段落 → 逐段累积超过 ChunkSize 出块（保留尾部 Overlap 进入下一块）→ 单段超长按句子边界硬切 → 记录来源标题进 Meta。

## 5. Indexer

```go
func (ix *Indexer) Index(ctx, docID string) error {
    // status → parsing
    // Load 文本 → Chunk → 事务内：删旧 chunks → 批量写 chunks → 同步 FTS5 → status indexed
    // 失败 → status failed（可 Reindex）
}
```

状态机：`pending → parsing → indexed | failed`。索引在后台 goroutine 执行，上传接口立即返回。

## 6. Retriever

```go
type Retriever interface {
    Search(ctx context.Context, query string, topK int) ([]Hit, error)
}

type Hit struct {
    DocID   string
    DocName string
    ChunkID string
    Content string
    Score   float64   // bm25 负翻转，越大越相关
    Source  string
    Meta    map[string]string
}
```

- FTS5 trigram（3 字滑窗），中文无需分词器；
- 查询串先转义清洗（防 MATCH 语法注入）；
- < 3 字符查询返回空结果不报错。

## 7. 文档生命周期

```
上传 → pending → parsing → indexed
                      └──► failed（ReindexDoc 可重试）
删除：软删 knowledge_docs + 物理清 FTS5 行
```

## 8. 与工具集成

`knowledge_search` 工具持有 `rag.Retriever` 接口，是 Agent 检索知识库的唯一入口；结果格式化为 `[i] 文档名 · 章节\n内容`。

## 9. 错误码

| Code | 含义 |
|---|---|
| 7000 | Knowledge 通用错误 |
| 7001 | 文档加载失败 |
| 7002 | 切分失败 |
| 7003 | 索引失败 |
| 7004 | 检索失败 |
| 7005 | 文档不存在 |
| 7006 | 文件格式不支持 |
