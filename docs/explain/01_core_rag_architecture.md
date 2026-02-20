# RAG 系统核心业务架构解析 (Business Overview)

## 1. 业务背景与核心价值

**Retrieval-Augmented Generation (RAG)** 是一种结合了“检索”与“生成”的技术架构，旨在解决大语言模型（LLM）的幻觉问题和知识滞后问题。简单来说，就是给大模型装上了一个“外挂知识库”，让它基于真实的企业文档回答问题。

**核心价值：**
1.  **私有化知识问答**：基于企业内部文档（PDF, Word, Excel等）进行回答，数据不上公网。
2.  **降低幻觉风险**：回答有据可依，直接引用原文段落，减少胡编乱造。
3.  **即时更新**：无需重新训练模型，上传新文档即可立即生效。

---

## 2. 核心业务流程全景 (The Big Picture)

对于业务方而言，RAG 系统主要包含两个核心阶段：
1.  **知识入库 (Indexing)**：把各种格式的文档“喂”给系统，转化为系统能理解的知识。
2.  **智能问答 (Retrieval & Generation)**：用户提问，系统检索相关知识，生成精准回答。

### 核心业务流转图 (Process Flow)

```mermaid
graph TD
    subgraph 用户侧 [User Side]
        User[用户 User]
        Doc[原始文档 Document]
        Query[用户提问 Query]
    end

    subgraph 知识处理中心 [Knowledge Processing Center]
        Parser[文档解析器 Parser]
        Chunker[切片处理器 Chunker]
        EmbedModel[语义向量模型 Embedding Model]
        VectorDB[(向量数据库 Vector DB)]
    end

    subgraph 智能问答引擎 [Intelligent Q&A Engine]
        Retriever[检索器 Retriever]
        Reranker[重排序模型 Reranker]
        LLM[大语言模型 LLM]
        Prompt[提示词工程 Prompt Engineering]
    end

    %% 知识入库流程
    Doc -->|1. 上传文档| Parser
    Parser -->|2. 提取文本| Chunker
    Chunker -->|3. 切分段落| EmbedModel
    EmbedModel -->|4. 生成向量| VectorDB

    %% 问答流程
    User -->|5. 发起提问| Query
    Query -->|6. 语义匹配| Retriever
    Retriever -->|7. 从库中召回| VectorDB
    VectorDB -.->|8. 返回相关片段| Retriever
    Retriever -->|9. 提交候选片段| Reranker
    Reranker -->|10. 筛选最佳上下文| Prompt
    Query -->|11. 原始问题| Prompt
    Prompt -->|12. 组装后的提示词| LLM
    LLM -->|13. 生成最终答案| User

    style User fill:#f9f,stroke:#333,stroke-width:2px
    style VectorDB fill:#bbf,stroke:#333,stroke-width:2px
    style LLM fill:#bfb,stroke:#333,stroke-width:2px
```

---

## 3. 关键业务实体与角色 (Key Entities)

| 实体/角色 | 业务含义 | 关键属性 |
| :--- | :--- | :--- |
| **知识库 (Knowledge Base)** | 存放某一类文档的容器，类似于文件夹。 | 名称、描述、可见范围（公开/私有） |
| **文档 (Document)** | 上传的原始文件。 | 文件名、格式、上传时间、处理状态 |
| **切片 (Chunk)** | 文档被切分后的最小语义单元（一段话）。 | 原始内容、所属文档、语义向量 |
| **会话 (Session)** | 用户与系统的对话历史。 | 对话ID、用户ID、创建时间 |

---

## 4. 系统交互时序图 (Interaction Flow)

描述用户从提问到获得答案的端到端交互过程。

```mermaid
sequenceDiagram
    participant User as 用户 User
    participant Frontend as 前端界面 Frontend
    participant Backend as 业务后端 Backend
    participant VectorDB as 向量数据库 VectorDB
    participant LLM as 大语言模型 LLM

    User->>Frontend: 输入问题：“公司的报销流程是怎样的？”
    Frontend->>Backend: 发送问答请求 [Question]

    activate Backend
    Note right of Backend: 1. 语义理解与检索
    Backend->>Backend: 将问题转化为向量
    Backend->>VectorDB: 检索最相似的 Top-K 个片段
    VectorDB-->>Backend: 返回相关文档切片 [Chunks]

    Note right of Backend: 2. 构建上下文
    Backend->>Backend: 组装 Prompt：“基于以下片段回答...”

    Note right of Backend: 3. 调用大模型
    Backend->>LLM: 发送完整 Prompt
    activate LLM
    LLM-->>Backend: 流式返回生成的答案 [Stream Answer]
    deactivate LLM

    Backend-->>Frontend: 实时推送答案片段
    deactivate Backend

    Frontend-->>User: 展示最终回答与引用来源
```
