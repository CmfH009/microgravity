# Microgravity (`mg`) E2E Test Infrastructure

## Overview & Philosophy
The Microgravity End-to-End (E2E) testing framework provides opaque-box validation of the autonomous CLI agent and its core subsystems. The test architecture enforces behavioral correctness across four rigorous tiers:
1. **Tier 1 — Feature Coverage**: In-depth behavioral verification of individual strategic capabilities (minimum 5 dedicated test cases per feature).
2. **Tier 2 — Boundary & Corner Cases**: Edge cases including extreme file sizes, empty files, mixed LF/CRLF line endings, nested directories, unicode paths, and zero-token budgets.
3. **Tier 3 — Cross-Feature Combinations**: Multi-subsystem interplay such as subagents modifying CRLF files inside isolated git worktrees while governed by filesystem policy roots.
4. **Tier 4 — Real-World Application Workflows**: End-to-end multi-turn agent iterations simulating real software engineering tasks (context injection -> subagent delegation -> worktree modification -> event bus notification -> conversation context pruning).

## Feature Inventory & Test Coverage
| Feature ID | Feature Name | Package / Area | E2E Suite Focus |
|---|---|---|---|
| **F15** | `@file` Context Reference Ingestion | `internal/context`, `internal/engine` | Ingestion deduplication, single context injection, directory mapping, fuzzy fallback, quoted path parsing |
| **F16** | CRLF Normalization in File Editing | `internal/tools/fs.go` | `replace_file_content` matching across Windows CRLF and Unix LF, line-range restriction, CRLF disk write preservation |
| **F4** | Workspace Roots & Sandbox Isolation | `cmd`, `internal/policy` | `--add-dir` workspace root expansion, outside path rejection, `--sandbox` host command blocking, working directory validation |
| **F5** | Turn 0 & Directive Pruner Protection | `internal/pruner` | Block-structured preservation of root user intent (Turn 0), dynamic model context ceilings, middle turn eviction, 35% cap handling |
| **F17** | Subagent Worktree Isolation Execution | `internal/subagent`, `internal/git` | Subagent execution inside isolated git worktrees (`WorkspaceBranch`), automatic worktree teardown, EventBus completion delivery |

## Test Runner Commands
Execute the complete E2E test suite:
```powershell
go test -v -count=1 ./tests/e2e/...
```

Execute a specific tier or feature:
```powershell
# Tier 1 - Feature Coverage
go test -v -run "TestTier1" ./tests/e2e/...

# Tier 2 - Boundary Cases
go test -v -run "TestTier2" ./tests/e2e/...

# Tier 3 - Cross-Feature Combinations
go test -v -run "TestTier3" ./tests/e2e/...

# Tier 4 - Real-World Workflow
go test -v -run "TestTier4" ./tests/e2e/...
```

## Coverage Thresholds & Quality Gates
- **Tier 1 Feature Tests**: Minimum 5 passing tests per feature (F15, F16, F4, F5, F17) => 25+ tests minimum.
- **Pass Rate**: 100% required on all verified capabilities.
- **Flakiness**: Zero flakiness; all tests manage independent temporary directories, isolated git repositories, and self-contained mock states.
- **Side Effect Free**: All spawned worktrees, temporary files, and test artifacts are cleaned up via `t.Cleanup()`.
