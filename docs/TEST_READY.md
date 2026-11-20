# Microgravity (`mg`) E2E Test Suite Ready Gate

## Status: READY & 100% PASSING

The End-to-End (E2E) opaque-box test suite for Microgravity (`mg`) is fully authored, integrated, and verified against `A:\projects\microgravity`.

## Runner Commands

### Full Suite Execution
```powershell
go test -v -count=1 ./tests/e2e/...
```

### Targeted Tier Execution
```powershell
# Tier 1 — Strategic Feature Coverage
go test -v -run "TestTier1" ./tests/e2e/...

# Tier 2 — Boundary & Corner Cases
go test -v -run "TestTier2" ./tests/e2e/...

# Tier 3 — Cross-Feature Combinations
go test -v -run "TestTier3" ./tests/e2e/...

# Tier 4 — Real-World Application Workflow
go test -v -run "TestTier4" ./tests/e2e/...
```

---

## Coverage & Quality Checklist

### Tier 1: Feature Coverage (>=5 Tests Per Feature) — Total: 30 Tests
- [x] **F15: `@file` Context Reference Ingestion** (6/5 tests)
  - `TestTier1_F15_SingleFileInjection`: Verifies single `@file` reference parses and injects content once.
  - `TestTier1_F15_MultipleDifferentFiles`: Verifies multiple distinct `@file` references parse and inject without cross-pollution.
  - `TestTier1_F15_NonExistentFilePreservation`: Verifies non-existent `@file` tokens are preserved in user prompt and not injected.
  - `TestTier1_F15_QuotedPathHandling`: Verifies quoted paths (double and single quotes) are stripped and resolved cleanly.
  - `TestTier1_F15_DirectoryContextInjection`: Verifies `@dir` references produce `[Directory Context from @...]` with recursive `- [DIR]` and `- [FILE]` entries.
  - `TestTier1_F15_SingleInjectionDeduplicationInTurn`: Verifies canonical ingestion in `turn.go` produces exactly 1 injection block.
- [x] **F16: CRLF Normalization in File Editing** (6/5 tests)
  - `TestTier1_F16_ExactCRLFFileMatching`: Verifies CRLF file with CRLF query replaces successfully and retains CRLF endings on disk.
  - `TestTier1_F16_LFQueryAgainstCRLFFile`: Verifies Unix LF query against Windows CRLF file normalizes, matches, and writes back preserving CRLF.
  - `TestTier1_F16_CRLFQueryAgainstLFFile`: Verifies Windows CRLF query against Unix LF file normalizes, matches, and writes back preserving LF.
  - `TestTier1_F16_LineRangeRestrictedCRLF`: Verifies `StartLine` and `EndLine` chunk restriction on CRLF files with CRLF preservation.
  - `TestTier1_F16_AllowMultipleCRLFReplacement`: Verifies `AllowMultiple=true` replaces all occurrences on CRLF files.
  - `TestTier1_F16_DuplicateTargetWithoutAllowMultipleFails`: Verifies unambiguous error returned when target appears multiple times without `AllowMultiple=true`.
- [x] **F4: Policy Engine Workspace Roots & Sandbox Isolation** (6/5 tests)
  - `TestTier1_F4_DefaultPolicyAllowsCwdAndTemp`: Verifies cwd and TempDir are approved roots.
  - `TestTier1_F4_OutsidePathRejectedByDefault`: Verifies paths outside approved roots return "outside approved workspace roots".
  - `TestTier1_F4_AddDirAllowsExternalDirectory`: Verifies directories added via `--add-dir` / `AddDirs` become approved workspace roots.
  - `TestTier1_F4_MultipleAddDirs`: Verifies multiple arbitrary external directories are permitted concurrently.
  - `TestTier1_F4_SandboxFlagDisablesHostCommands`: Verifies `--sandbox` (`allowHostCommands: false`) rejects host shell commands with policy error.
  - `TestTier1_F4_PolicyWorkingDirectoryValidation`: Verifies `CheckWorkingDirectory` enforces existing directory within approved roots.
- [x] **F5: Conversation Pruner Turn 0 & Directive Pinning** (6/5 tests)
  - `TestTier1_F5_Turn0RootIntentPreserved`: Verifies Turn 0 user goal is pinned at the start of conversation under high token overflow.
  - `TestTier1_F5_PruningEfficiencyUnderBudget`: Verifies conversations under model token budget are returned 100% untouched.
  - `TestTier1_F5_MiddleTurnsPrunedWithMarker`: Verifies middle turns are replaced by `[Earlier conversation context pruned for token efficiency]`.
  - `TestTier1_F5_ModelDynamicContextBudgetScaling`: Verifies dynamic budget scaling across Gemini (32,000), Gemma/Orbit (8,000), and standard models (12,000).
  - `TestTier1_F5_ExtremeTurn0SizeCapped`: Verifies oversized Turn 0 exceeding 35% ceiling is capped with `... [Turn 0 truncated]`.
  - `TestTier1_F5_FallbackWithoutExplicitUserTurns`: Verifies arbitrary text conversations are pruned from the beginning while preserving recent lines.
- [x] **F17: Subagent Git Worktree Isolation Execution** (6/5 tests)
  - `TestTier1_F17_SubagentBranchWorktreeCreation`: Verifies spawning with `WorkspaceBranch` creates isolated worktree at `.worktrees/subagent-<id>`.
  - `TestTier1_F17_SubagentBranchWorktreeCleanup`: Verifies completed subagent triggers safe worktree removal.
  - `TestTier1_F17_SubagentInheritWorkspace`: Verifies `WorkspaceInherit` operates in repo root without creating worktrees.
  - `TestTier1_F17_SubagentIsolatedFileModification`: Verifies files authored inside worktree do not leak into parent repository root.
  - `TestTier1_F17_SubagentEventBusNotification`: Verifies completion emits `subagent_responses` payload on EventBus.
  - `TestTier1_F17_SubagentCancellationHandling`: Verifies context cancellation marks subagent as `KILLED` (`StateKilled`).

---

### Tier 2: Boundary & Corner Cases — Total: 7 Tests
- [x] `TestTier2_Boundary_EmptyFile`: `replace_file_content` targeting 0-byte file returns clean not-found error without panic.
- [x] `TestTier2_Boundary_LargeFileOver500KB`: Files exceeding 500KB ceiling are skipped with informative context banner.
- [x] `TestTier2_Boundary_MixedCRLFAndLF`: Files with mixed CRLF and LF lines normalize for replacement and preserve CRLF.
- [x] `TestTier2_Boundary_NestedDirReferences`: Multi-level nested directories list tree hierarchy cleanly.
- [x] `TestTier2_Boundary_SpecialCharactersInPaths`: Paths with dots, dashes, underscores, and special characters resolve and edit accurately.
- [x] `TestTier2_Boundary_DeepWorktrees`: Long worktree directory paths with feature branch names execute safely.
- [x] `TestTier2_Boundary_ZeroTokensBudgetPruner`: Minimum floor boundaries for pruner token budget (1, 5, 10 tokens) execute without slice panics.

---

### Tier 3: Cross-Feature Combinations — Total: 4 Tests
- [x] `TestTier3_Combo_SubagentCRLFInWorktree`: Subagent in isolated worktree runs `replace_file_content` on CRLF file; edits succeed, CRLF preserved, parent repo untouched.
- [x] `TestTier3_Combo_FileRefPlusPolicyEngine`: `@file` reference resolving to `--add-dir` root is permitted, while external unauthorized file is rejected.
- [x] `TestTier3_Combo_Turn0PreservedWithFileContextUnderPruning`: Turn 0 containing large injected `@file` context is retained under token pressure.
- [x] `TestTier3_Combo_PolicySandboxWithSubagentTools`: Subagent manager running under restricted policy enforces filesystem bounds and command blocking.

---

### Tier 4: Real-World Application Workflow Scenario — Total: 1 Test
- [x] `TestTier4_Workflow_MultiStepAutonomousAgentCycle`:
  Full multi-turn cycle: User prompt with `@file` for buggy CRLF math service -> Context processed once -> Subagent spawned in isolated worktree -> Subagent uses `replace_file_content` on CRLF file in worktree -> Subagent creates unit tests in worktree -> Subagent publishes completion event to EventBus -> Multi-turn conversation expands -> Pruner triggers on token threshold -> Invariants asserted: Turn 0 preserved, CRLF line endings preserved, worktree cleaned up, parent repo file isolated.

### Meta & CLI Binary Invocation — Total: 2 Tests
- [x] `TestE2E_CLI_BinaryInvocation`: Microgravity binary execution verifying `--add-dir` and `--sandbox` flags.
- [x] `TestE2E_SuiteInventory`: Suite inventory sanity check.

**Grand Total: 43 Tests, 43 Passed, 0 Failed, 0 Regressions.**
