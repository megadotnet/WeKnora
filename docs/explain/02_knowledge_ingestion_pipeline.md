# 知识入库流程深度解析 (Knowledge Ingestion Pipeline)

## 1. 业务目标

知识入库是 RAG 系统的“粮仓”。本流程负责将非结构化的原始文件（PDF, Word, Excel, Images等）转化为计算机可理解、可检索的结构化数据。只有高质量的入库，才有高质量的回答。

**核心痛点：**
- **格式繁杂**：不同文件格式解析难度大（如扫描件PDF）。
- **语义丢失**：简单的按字数切分可能把一句完整的话切断。
- **数据更新**：文档修改后，如何快速同步更新索引？

---

## 2. 文档处理全流程 (End-to-End Flow)

从用户点击“上传”到文档变为“可搜索状态”，经历以下几个关键步骤：

```mermaid
graph TD
    subgraph 用户操作 [User Action]
        Upload[上传文件 Upload]
        Config[配置参数 Config]
    end

    subgraph 解析层 [Parsing Layer]
        FormatCheck[格式校验 Format Check]
        OCR[OCR识别 OCR]
        TextExtract[文本提取 Text Extraction]
    end

    subgraph 切分层 [Chunking Layer]
        Clean[数据清洗 Cleaning]
        Split[语义切分 Semantic Splitting]
        MetaTag[元数据标注 Metadata Tagging]
    end

    subgraph 索引层 [Indexing Layer]
        Embedding[向量化 Embedding]
        IndexBuild[构建索引 Index Building]
        Storage[(存入数据库 Storage)]
    end

    Upload --> FormatCheck
    FormatCheck -->|格式支持| TextExtract
    FormatCheck -->|不支持| Error[报错 Error]

    TextExtract -->|是图片/扫描件| OCR
    TextExtract -->|纯文本| Clean
    OCR --> Clean

    Clean --> Split
    Split --> MetaTag
    MetaTag --> Embedding
    Embedding --> IndexBuild
    IndexBuild --> Storage

    style Upload fill:#f9f,stroke:#333,stroke-width:2px
    style Storage fill:#bbf,stroke:#333,stroke-width:2px
    style Error fill:#f99,stroke:#333,stroke-width:2px
```

---

## 3. 文档状态流转 (Document State Machine)

在系统后台，每个文档都有明确的生命周期状态。这对于监控任务进度和排查错误至关重要。

```mermaid
stateDiagram-v2
    [*] --> Uploading : 开始上传
    Uploading --> Pending : 上传完成，等待处理
    Pending --> Parsing : 进入解析队列

    Parsing --> Chunking : 解析成功
    Parsing --> Failed : 解析失败 [格式损坏/加密]

    Chunking --> Embedding : 切分完成
    Chunking --> Failed : 切分失败

    Embedding --> Indexed : 向量化完成，已入库
    Embedding --> Failed : 模型调用失败

    Indexed --> [*] : 可被检索
    Failed --> [*] : 任务终止，需人工介入

    state Failed {
        ErrorLog : 记录错误日志
        Retry : 支持手动重试
    }
```

---

## 4. 异常处理机制 (Exception Handling)

| 异常场景 | 系统行为 | 业务建议 |
| :--- | :--- | :--- |
| **文件过大 (>50MB)** | 拒绝上传，提示限制。 | 建议拆分文件或联系管理员扩容。 |
| **格式不支持 (如 .exe)** | 校验拦截。 | 仅允许上传 PDF/Word/TXT/MD 等文本类文件。 |
| **解析超时 (OCR慢)** | 异步处理，状态长时间为 "Processing"。 | 增加超时时间设置，或优化 OCR 服务性能。 |
| **向量化失败 (API额度)** | 任务标记为 Failed，保留原始文件。 | 检查 API Key 余额，支持一键重试。 |

---

## 5. 关键技术细节 (Technical Deep Dive for PM)

虽然我们不需了解代码，但需知晓影响效果的几个关键参数：

1.  **切片大小 (Chunk Size)**：
    *   *定义*：每个知识片段包含多少字符（例如 500 字）。
    *   *影响*：太小导致语义不完整，太大导致检索不精准且浪费 Token。
    *   *默认建议*：500-1000 字符。
2.  **重叠窗口 (Overlap)**：
    *   *定义*：相邻两个切片之间重复的内容长度（例如 50 字）。
    *   *影响*：防止一句话被从中间切断，保持语义连贯性。
    *   *默认建议*：10-20%。
