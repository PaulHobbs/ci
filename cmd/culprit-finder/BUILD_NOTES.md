# Build Notes

## Dependencies

This CLI tool depends on:

1. **github.com/spf13/cobra** - CLI framework for commands and flags
2. **Internal packages** from the parent turboci-lite project:
   - `github.com/example/turboci-lite/culprit/domain`
   - `github.com/example/turboci-lite/culprit/runner`
   - `github.com/example/turboci-lite/internal/storage/sqlite`
   - `github.com/example/turboci-lite/pkg/id`

## Building

Since this is a subpackage of the main turboci-lite project, build from the parent directory:

```bash
# From the turboci-lite root:
cd cmd/culprit-finder
go build -o culprit-finder .
```

Or use the Makefile:

```bash
make build
```

## Go Module Setup

If building as part of the main project, the dependencies are already in the root `go.mod`.

If you want to build standalone (not recommended), you would need to:
1. Vendor or replace the internal dependencies
2. Add cobra to go.mod:
   ```
   go get github.com/spf13/cobra@latest
   ```

## Import Path Corrections

Note: The import paths use `github.com/example/turboci-lite` as a placeholder.
Update these to match your actual module path in:
- All files in `internal/cli/*.go`
- All files in `internal/session/*.go`
- All files in `internal/ui/*.go`

Replace:
```go
"github.com/example/turboci-lite/..."
```

With your actual module path (e.g., `"go.chromium.org/luci/turboci-lite/..."`).

## Testing Build

Quick test that everything compiles:

```bash
cd cmd/culprit-finder
go build .
./culprit-finder version
```

Expected output:
```
   _____      _            _ _     _____ _           _
  / ____|    | |          (_) |   |  __ (_)         | |
 | |    _   _| |_ __  _ __ _| |_  | |__) |_ __   __| | ___ _ __
 | |   | | | | | '_ \| '__| | __| |  ___/| '_ \ / _' |/ _ \ '__|
 | |___| |_| | | |_) | |  | | |_  | |    | | | | (_| |  __/ |
  \_____\__,_|_| .__/|_|  |_|\__| |_|    |_| |_|\__,_|\___|_|
               | |
               |_|

  Version: 1.0.0
  Flake-aware non-adaptive group testing for finding culprit commits
```
