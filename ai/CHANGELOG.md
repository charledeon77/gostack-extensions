# Changelog

All notable changes to the GoStack AI extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-11-22
### Added
- **Initial Release**: Launched the official GoStack AI Toolkit extension (`gostack-extensions/ai`).
- **Vector Embeddings (pgvector)**: Native `Vector` (`[]float32`) type implementing `sql.Scanner` and `driver.Valuer` with distance metrics (`<=>`, `<->`, `<#>`) and DDL helpers (`HNSW`, `IVFFlat`).
- **Pluggable VectorStore**: Shipped `PgVectorStore` for production PostgreSQL with SQL injection identifier regex shields, and `MemoryVectorStore` for instant in-memory unit tests.
- **Shipped Default Index**: Added `CreateDefaultIndexSQL` recommending HNSW with Cosine distance for sub-millisecond approximate nearest neighbor lookups out of the box.
- **Multi-Tenant Metadata Filtering & MinScore**: Filter queries by user/tenant with relevance score gating.
- **RecursiveChunker & Sanitizer**: Hierarchical natural language text chunking with sliding-window overlap and HTML markup sanitization.
- **Resilient HTTPEmbedder**: Standard HTTP client for OpenAI and Ollama with automated exponential backoff on HTTP 429 rate limits and 5xx server errors.
- **PromptTemplate & Conversation**: Reusable prompt templates, format filters, `SearchResults.AsContext()`, and sliding-window context management (`Window(lastN)`).
- **Structured JSON Extractor**: Zero-dependency markdown fence stripper (`ExtractJSON`) for unmarshaling LLM output into Go structs.
- **Decoupled ToolRegistry**: Standalone tool catalog with schema export to OpenAI function calling (`ToOpenAITools`).
- **Authenticated MCP Server**: JSON-RPC 2.0 Model Context Protocol server with constant-time API key verification (`subtle.ConstantTimeCompare`) executed before body read, and 10 MB DoS protection.
- **Citadel Service Provider**: Automatic IoC container bindings for `"ai.tools"`, `"ai.prompts"`, `"ai.mcp"`, and `"ai.store.memory"`.
