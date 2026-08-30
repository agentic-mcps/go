# Organization module migration

`github.com/agentic-mcps/go` and
`github.com/ashwingopalsamy/agentic-go` are separate Go module identities.
GitHub redirects do not make one module path an alias for the other, and the
repositories are not automatically mirrored.

The organization repository preserves the implementation lineage, binaries,
MCP contracts, schemas, provider identifiers, analyzer predicates, and runtime
behavior of the personal v1 release. Its Go module and release coordinates are
new.

## Move an installation

Install the organization release over the existing binary names:

```sh
curl --fail --location --silent --show-error \
  https://raw.githubusercontent.com/agentic-mcps/go/v1.0.0/scripts/install.sh \
  | bash -s -- 1.0.0

agentic-go --version
agentic-go doctor
```

Update GitHub Actions references to:

```yaml
- uses: agentic-mcps/go@v1.0.0
```

Update source-checkout or advanced `go install` commands to use
`github.com/agentic-mcps/go`. The project exposes binaries and internal
packages rather than a public Go library, so no import-path compatibility shim
is provided.

The personal repository remains active under its original identity. Fixes move
between the repositories through explicit reviewed commits, not implicit
synchronization.
