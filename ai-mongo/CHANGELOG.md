# Changelog

All notable changes to the GoStack AI-Mongo extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-11-22
### Added
- **Initial Release**: Launched the official GoStack MongoDB Atlas Vector Search adapter (`gostack-extensions/ai-mongo`).
- **MongoVectorStore**: Implements the `ai.VectorStore` contract on top of MongoDB Atlas `$vectorSearch` aggregation, providing the same `Store`, `StoreBatch`, `Search`, and `Delete` API as `PgVectorStore`.
- **BulkWrite Upserts**: `StoreBatch` uses a single `BulkWrite` operation with upsert semantics — efficient batch ingestion without N round-trips.
- **Atlas Pre-Filter Support**: `SearchOptions.Filter` metadata is applied as a `$vectorSearch` pre-filter, executed at index level for maximum query efficiency.
- **MinScore Post-Filter**: Results below `SearchOptions.MinScore` are discarded after retrieval, matching `PgVectorStore` behavior.
- **AtlasIndexJSON**: Returns the complete JSON payload to register a Vector Search index via the Atlas UI or Atlas CLI — no manual JSON authoring required.
- **Custom Field Names**: `EmbeddingField`, `ContentField`, and `IndexName` are configurable via `Options`; defaults (`embedding`, `content`, `vector_index`) work out of the box.
- **Test-Injectable Interface**: Internal `mongoCollection` interface allows full unit testing with mock collections — no live Atlas connection required for the test suite.
- **Compile-Time Contract Check**: `var _ ai.VectorStore = (*MongoVectorStore)(nil)` guarantees interface compliance at build time.
