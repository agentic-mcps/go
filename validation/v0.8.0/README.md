# v0.8.0 reproducible evaluation

This directory defines eight pinned historical tasks and the neutral result
contracts used to evaluate agentic-go. It contains no third-party source and no
paid-model result.

Build and run the local runner with Go 1.27. The scorer disables implicit
toolchain downloads, and some pinned tasks require a newer Go version than the
agentic-go module floor.

```sh
go build -o /tmp/agentic-go-eval ./validation/cmd/eval
```

Validate the complete corpus:

```sh
/tmp/agentic-go-eval validate \
  -tasks validation/v0.8.0/tasks
```

Prepare private, content-addressed fixture bundles from the user-cloned
repositories:

```sh
/tmp/agentic-go-eval prepare \
  -tasks validation/v0.8.0/tasks \
  -sources /path/to/agentic-go-validation \
  -output /path/to/private/evaluation/v0.8.0/bundles
```

The output path above is illustrative. Bundles and response artifacts belong
outside this repository. Preparation never changes the source clones.

Set up and score one candidate:

```sh
/tmp/agentic-go-eval setup \
  -task validation/v0.8.0/tasks/grpc-go-rbac-header-normalization/task.json \
  -bundle /path/to/grpc-go-rbac-header-normalization-<sha256>.bundle \
  -workspace /tmp/grpc-go-rbac-header-normalization

/tmp/agentic-go-eval score \
  -task validation/v0.8.0/tasks/grpc-go-rbac-header-normalization/task.json \
  -bundle /path/to/grpc-go-rbac-header-normalization-<sha256>.bundle \
  -workspace /tmp/grpc-go-rbac-header-normalization \
  -output /tmp/grpc-go-rbac-header-normalization-result.json
```

`setup` exposes only the base snapshot. `score` copies the candidate into a
temporary clone and overlays the target regression-test files there. It never
overwrites the candidate workspace.

Qualify a task before accepting its evidence:

```sh
/tmp/agentic-go-eval qualify \
  -task validation/v0.8.0/tasks/grpc-go-rbac-header-normalization/task.json \
  -bundle /path/to/grpc-go-rbac-header-normalization-<sha256>.bundle \
  -source /path/to/agentic-go-validation/grpc-go \
  -output /tmp/grpc-go-rbac-header-normalization-qualification.json
```

Qualification requires three independent results: the fixture base fails its
behavioral oracle, the historical reference fix passes it, and an otherwise
correct change with one forbidden path fails only the scope check.

Replay a checked-in MCP transcript against a built server:

```sh
/tmp/agentic-go-eval replay \
  -transcript validation/v0.8.0/tasks/grpc-go-rbac-header-normalization/replay.json \
  -server /tmp/agentic-go \
  -workspace /tmp/grpc-go-rbac-header-normalization \
  -artifacts /tmp/grpc-go-rbac-header-normalization-artifacts \
  -output /tmp/grpc-go-rbac-header-normalization-replay.json
```

The scorer executes trusted repository tests with the caller's privileges. Its
filesystem boundaries and output limits are containment controls, not a
sandbox.
