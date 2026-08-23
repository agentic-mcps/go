## Summary

Describe the behavior changed and the contract or issue it implements.

## Precision and safety

- [ ] Public protocol inventory and schemas remain intentional.
- [ ] Execution, containment, cancellation, and output bounds remain truthful.
- [ ] Analyzer changes include positive and near-miss fixtures plus documented limitations.
- [ ] No external-validation claim is made without reviewed evidence.

## Verification

- [ ] `go test ./...`
- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `go build ./...`
- [ ] `staticcheck ./...`
- [ ] `golangci-lint run`
- [ ] `git diff --check`
