# Make the verification report the product boundary

Agentic-go treats change verification as one deep module whose output is a
portable verification report. The CLI, GitHub Action, and MCP server are
adapters over that module, while Go package analysis and execution remain
language-specific implementation; this avoids coupling the product to MCP or
flattening future TypeScript support into Go-shaped abstractions.

