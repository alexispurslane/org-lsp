# org-lsp Architecture

This document provides a high-level overview of the `org-lsp` architecture, its core components, and the data flow within the system.

## High-Level Overview

`org-lsp` is a Language Server Protocol (LSP) implementation for Org mode files. It is designed to be a minimal yet powerful server, focusing on navigation and linking capabilities. The architecture is based on a layered approach, with a clear separation of concerns between the LSP protocol handling and the Org mode parsing and analysis.

The server is written in Go and uses the `go.lsp.dev/protocol` library for LSP types and the `go-org` library for parsing Org mode files.

## Core Components

The project is organized into the following main packages:

*   **`cmd/server`**: The entry point of the application. It handles command-line flags, sets up the server, and starts the LSP communication.
*   **`server`**: The core of the LSP server. It implements the `protocol.Server` interface from `go.lsp.dev/protocol` and contains the handlers for the different LSP requests (e.g., `textDocument/definition`, `textDocument/hover`). It is responsible for translating between the LSP types and the internal domain types, as well as performing the actual operations required by the LSP such as searching the index, or performing code actions.
*   **`orgscanner`**: The domain-specific logic for parsing and analyzing Org mode files. It is responsible for scanning the workspace for `.org` files, parsing them into an Abstract Syntax Tree (AST), and building an index of headings, UUIDs, and tags. This package is designed to be independent of the LSP protocol.
*   **`lspstream`**: A utility package that provides a custom `jsonrpc2.Stream` implementation with a larger buffer to handle large LSP messages.
*   **`integration`**: Contains the integration tests for the LSP server. It uses a custom testing framework (`LSPTestContext`) to run an LSP client and make requests to the server.

## Data Flow

The data flow in `org-lsp` can be illustrated with the lifecycle of a `textDocument/definition` request:

1.  **Client Request:** The LSP client (e.g., VS Code, Emacs) sends a `textDocument/definition` request to the server when the user triggers the "Go to Definition" command on a link.
2.  **LSP Stream:** The `lspstream` package reads the request from the transport (stdio or TCP) and decodes the JSON-RPC message.
3.  **Server Handler:** The `server.ServerImpl.Definition` handler is called with the request parameters.
4.  **State Access:** The handler acquires a read lock on the global `serverState` to safely access the state.
5.  **Document Retrieval:** The handler retrieves the parsed document from the `serverState.OpenDocs` map.
6.  **AST Traversal:** The handler uses the `findNodeAtPosition` utility to find an AST node of the desired type (a link in this case) at the cursor position, if it exists.
7.  **Link Resolution:** Depending on the link protocol (`file:` or `id:`), the handler calls `resolveFileLink` or `resolveIDLink` to resolve the link to a file path and a position.
    *   `resolveFileLink` resolves file links based on the current document's path.
    *   `resolveIDLink` looks up the UUID in the `orgscanner.ProcessedFiles.UuidIndex` to find the location of the corresponding heading.
8.  **Return:** The handler creates a `protocol.Location` object with the resolved file path and position and returns it.
9.  **Response:** The server handler translates the protocol.Location object into a JSON-RPC response message, and sends it back to the client.
9.  **Client Action:** The client receives the response and opens the target file at the specified location.

## State Management

The server's state is managed by two main components:

*   **`server.State`:** This struct holds the global state of the server, including:
    *   A map of open documents (`OpenDocs`).
    *   A map of document versions (`DocVersions`).
    *   A map of raw document content (`RawContent`).
    *   A pointer to the `OrgScanner`.
    *   A `sync.RWMutex` to protect the maps from concurrent access.
*   **`orgscanner.OrgScanner`:** This struct is responsible for scanning the workspace and maintaining an index of Org mode files. It contains:
    *   `ProcessedFiles`: A struct that holds the index of files, UUIDs, and tags.
        *   `Files`: A `sync.Map` of file paths to `FileInfo` objects.
        *   `UuidIndex`: A `sync.Map` of UUIDs to `HeaderLocation` objects.
        *   `TagMap`: A map of tags to a set of file paths.
    *   `LastScanTime`: The time of the last completed scan.
    *   A `sync.RWMutex` to protect the `ProcessedFiles` index during scans.

The `OrgScanner` performs an incremental scan of the workspace, which means it only re-parses files that have been modified since the last scan. This makes the indexing process efficient.

## Concurrency Model

The LSP server can receive concurrent requests from the client. To ensure thread safety, the server uses the following concurrency model:

*   **`server.State` Mutex:** All access to the `OpenDocs`, `DocVersions`, and `RawContent` maps in the `server.State` is protected by a `sync.RWMutex`. Handlers that modify the state (e.g., `DidOpen`, `DidChange`) acquire a write lock, while handlers that only read the state (e.g., `Definition`, `Hover`) acquire a read lock.
*   **`orgscanner.OrgScanner` Mutex:** The `OrgScanner.Process` method acquires a write lock on the scanner's mutex to ensure that only one scan is running at a time. The `Scan` method acquires a read lock to safely access the `ProcessedFiles` index.
*   **`sync.Map`:** The `Files` and `UuidIndex` in `ProcessedFiles` are `sync.Map`s, which are safe for concurrent reads and writes.

## Testing

The project has a comprehensive suite of integration tests in the `integration` package. The testing strategy is based on the following components:

*   **`LSPTestContext`:** A custom testing framework that manages the server lifecycle, creates a temporary directory for each test, and provides helper functions for creating files, opening documents, and making LSP requests.
*   **Gherkin-style Tests:** The tests are written in a readable, Gherkin-like style using `Given`, `When`, and `Then` helpers. This makes the tests easy to understand and maintain.
*   **Test-driven Development:** The `AGENTS.md` file encourages a test-driven development workflow, where developers write tests before implementing new features.
