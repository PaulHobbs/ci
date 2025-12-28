# How the Submit Queue Works

The submit queue implements non-adaptive group testing using an 8×8 matrix design to efficiently test 64 CLs (changelists/commits):

## Two-Phase Testing Strategy

```
Phase 1 - Row Tests (parallel)          Phase 2 - Column Tests (if failures)
┌─────────────────────────┐             ┌─────────────────────────┐
│ Row 0: CL-1..CL-8  ───► PASS → Submit │                         │
│ Row 1: CL-9..CL-16 ───► PASS → Submit │                         │
│ Row 2: CL-17..CL-24 ──► FAIL ─────────┼──► Test all 8 columns   │
│ Row 3: CL-25..CL-32 ──► PASS → Submit │    to find culprit      │
│ ...                                   │                         │
└─────────────────────────┘             └─────────────────────────┘
```

Key insight: If row 2 fails and column 3 fails, the culprit is CL-20 (at position [2,3]).

## Stage Pipeline

1. **Kickoff** → Receives 64 CLs, creates workflow
2. **Config** → Stores matrix configuration
3. **Assignment** → Maps CLs to rows/columns
4. **Planning** → Creates 8 parallel row test stages
5. **Test Executor** → Runs actual tests on a batch of 8 CLs
6. **Minibatch Finished** → If row passed, submit those CLs immediately
7. **Batch Finished** → If any failures, create column tests for culprit finding

## Performance

- Best case: 8 tests (all rows pass)
- Worst case: 16 tests (8 rows + 8 columns)
- vs Sequential: 64 tests → 4-8× speedup

---

## Adapting for Real Git Repos and Builds

Yes, this can definitely be adapted. The current implementation has clear extension points:

### 1. Replace simulateTest() with Real Build/Test Logic

Currently at main.go:789-800:

```go
func simulateTest(testType string, batchID int, cls []string) bool {
    // Currently just simulates failures
    if testType == "row" && (batchID == 2 || batchID == 5) {
        return false
    }
    return true
}
```

Real implementation would:

```go
func executeRealTests(cls []string, repoPath string) (bool, error) {
    // 1. Create a temporary worktree or checkout
    // 2. Apply/cherry-pick all CLs in order
    // 3. Run build: go build ./... or make build
    // 4. Run tests: go test ./... or make test
    // 5. Return true if all pass, false if any fail

    for _, cl := range cls {
        if err := gitCherryPick(cl); err != nil {
            return false, err  // Merge conflict = failure
        }
    }

    if err := runBuild(); err != nil {
        return false, nil  // Build failure
    }

    if err := runTests(); err != nil {
        return false, nil  // Test failure
    }

    return true, nil
}
```

### 2. Replace CL Submission with Real VCS Integration

Currently just logs submissions. Real implementation:

```go
func submitCLs(cls []string) error {
    for _, cl := range cls {
        // For Gerrit:
        // gerrit review +2 $CL && gerrit submit $CL

        // For GitHub:
        // gh pr merge $PR_NUMBER

        // For GitLab:
        // glab mr merge $MR_ID
    }
    return nil
}
```

### 3. CL Source Integration

Currently CLs are passed as simple strings. Real integration would:

```go
// Fetch pending CLs from Gerrit
cls, err := gerritClient.QueryChanges("status:open label:Verified+1")

// Or from GitHub
prs, err := githubClient.PullRequests.List(ctx, owner, repo, opts)
```

### 4. Example Architecture for Real Use

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  Gerrit/GitHub  │────►│  Submit Queue    │────►│  Build Cluster  │
│  (CL source)    │     │  Orchestrator    │     │  (test runners) │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                               │
                               ▼
                        ┌──────────────┐
                        │  SQLite DB   │
                        │  (state)     │
                        └──────────────┘
```

### 5. Key Considerations for Production

| Aspect          | Current           | Production Needs              |
|-----------------|-------------------|-------------------------------|
| CL Source       | Hardcoded strings | Gerrit/GitHub API             |
| Test Execution  | simulateTest()    | Real git + build + test       |
| Isolation       | None              | Docker/VMs per batch          |
| Merge Conflicts | Not handled       | Detect & skip conflicting CLs |
| Flaky Tests     | Not handled       | Retry logic, quarantine       |
| Submission      | Logging only      | VCS API calls                 |
