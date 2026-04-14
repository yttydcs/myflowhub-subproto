# Plan - varstore authoritative cache gate

## Workflow Information
- Repo: `D:/project/MyFlowHub3/repo/MyFlowHub-SubProto`
- Branch: `fix/varstore-authoritative-cache`
- Base: `main`
- Worktree: `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache`
- Current Stage: `3.1`

## Stage Records

### Initialization
- guide.md: not present in repo root
- base/worktree confirmation:
  - main repo path is dirty and treated as control-plane only
  - active execution worktree created at `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache`
  - active branch: `fix/varstore-authoritative-cache`

### Stage 1 - Requirements Analysis
#### Goal
- Fix VarStore query consistency so only authoritative subtree cache can answer `get/list/subscribe` directly.

#### Scope
- Must:
  - restrict direct-answer behavior in `varstore` `get/list/subscribe`
  - preserve existing cache propagation and record storage behavior
  - add regression tests for stale non-subtree cache paths
- Optional:
  - add minimal helper comments if they clarify the new gate
- Not in scope:
  - redesign VarStore caching model
  - change wire schema or action names
  - change Win UI logic directly

#### Use Cases
- local hub holds an old remote snapshot
- `var_changed` or `notify_set` updates display state, but manual refresh/get returns stale local hub data
- `list` or `subscribe` short-circuits on non-authoritative cache and stops reaching the real owner

#### Functional Requirements
- `get` may return local data only when `owner` is in current subtree and the record exists locally.
- `list` may return local names only when `owner` is in current subtree.
- `subscribe` may establish local subscription only when `owner` is in current subtree and the record exists locally.
- non-subtree cache may remain stored for propagation and UI continuity, but must not short-circuit `assist_get`, `assist_list`, or `assist_subscribe`.
- `var_changed` / `var_deleted` must continue to refresh local cache when forwarded through the hub.

#### Non-functional Requirements
- smallest safe change inside `varstore`
- no new persistence writes or cache sweep policy
- preserve current multi-hop pending and response behavior
- regression coverage for the changed decision points

#### Inputs / Outputs
- Inputs:
  - `get`, `list`, `subscribe` requests with `owner`, `name`, and `subscriber`
  - forwarded `var_changed` / `var_deleted` notifications
- Outputs:
  - direct `*_resp` only for authoritative subtree cache hits
  - upstream `assist_*` forwarding when the cache is non-authoritative or missing

#### Edge Cases
- `owner == 0`, invalid name, invalid subscriber still return current validation errors
- subtree owner with no local cache must still go upstream
- owner=self empty list semantics must remain `code=1` with explicit empty names array

#### Acceptance Criteria
- non-subtree owner with local stale record:
  - `get` forwards `assist_get`
  - `subscribe` forwards `assist_subscribe`
- non-subtree owner with local stale names:
  - `list` forwards `assist_list`
- subtree owner direct-hit behavior remains intact
- forwarded `var_changed` still updates local cache
- `go test ./varstore` passes

#### Risks
- `list` local-owner empty-set success behavior could regress if subtree gating is applied too broadly
- `subscribe` pending-dedupe behavior could regress if local/remote path split is inconsistent
- worktree baseline does not include the previously explored dirty-tree `var_changed` local-cache fix, so that fix must be carried explicitly here

#### Issue List
- none

### Stage 2 - Architecture Design
#### Overall Solution
- Keep `records` as VarStore's hop-visible cache, but separate two meanings:
  - cache can exist locally
  - cache is authoritative enough for direct reply
- direct reply is allowed only when `ownerInSubtree(ctx, owner)` is true and the handler-specific local data exists.

#### Alternatives Considered
- Remove non-subtree cache entirely:
  - rejected because spec treats hop cache as protocol behavior and this would reduce downstream continuity
- Add TTL or freshness metadata:
  - rejected because the current bug is a wrong authority gate, not just stale age

#### Module Responsibilities
- `varstore/varstore.go`
  - tighten direct-answer conditions in `handleGet`, `handleList`, and `handleSubscribe`
  - keep notification forwarding and local cache refresh in `OnReceive`, `handleNotify*`, and `handleVar*`
- `varstore/target_forward_test.go`
  - lock stale non-subtree cache behavior and forwarded-cache update behavior

#### Data / Call Flow
- `get`
  - request arrives
  - if `ownerInSubtree && lookupOwned hit`: reply locally
  - else: add pending waiter and forward `assist_get` to parent
- `list`
  - request arrives
  - if `ownerInSubtree`: use local `listNames`
  - else: forward `assist_list`
- `subscribe`
  - request arrives
  - if `ownerInSubtree && lookupOwned hit`: validate and add local subscription
  - else: add pending subscribe and forward `assist_subscribe`
- `var_changed` / `notify_set`
  - still do forward + local cache update
  - but updated cache does not by itself imply direct-answer authority

#### Interface Drafts
- external protocol unchanged
- internal gate shape:
  - `authoritativeSubtree := ownerInSubtree(ctx, owner)`
  - `get` / `subscribe` require `authoritativeSubtree && localRecordExists`
  - `list` requires `authoritativeSubtree`

#### Error Handling and Safety
- preserve current validation and permission errors
- never silently fall back to local cached data for non-subtree owner
- keep not-found and empty-list semantics aligned with current spec

#### Performance and Testing Strategy
- only adds constant-time subtree checks before direct reply
- no extra I/O
- regression tests will cover:
  - non-subtree `get/list/subscribe` forwarding
  - subtree direct-hit baseline
  - forwarded `var_changed` local cache refresh

#### Extensibility Design Points
- this split between cache existence and authority can later support versioned snapshots or more explicit remote-cache classes without redesigning the storage map

#### Issue List
- none

### Stage 3.1 - Planning
#### Project Goal and Current State
- Goal:
  - align `varstore` local-cache query behavior with stable spec so local hub only directly answers from authoritative subtree cache
- Current state:
  - clean worktree created from `main`
  - spec mismatch confirmed in `handleGet`, `handleList`, and `handleSubscribe`
  - prior exploratory dirty-tree fix for `var_changed` local cache update exists outside this worktree and must be re-applied here deliberately

#### Docs Governance Routing Decision
- Using `$m-docs`:
  - stable truth stays in `D:/project/MyFlowHub3/repo/MyFlowHub-Server/docs/specs/varstore.md`
  - workflow control stays in this worktree-root `plan.md`
  - completed implementation will archive into `docs/change/YYYY-MM-DD_topic.md`
  - reusable troubleshooting guidance will go to `docs/lessons` only if this workflow surfaces a new reusable pattern beyond current lessons

#### Related Requirements / Specs / Lessons
- Requirements impact: `none`
- Specs impact: `none`
- Related requirements:
  - none; this repo does not maintain stable requirements truth for VarStore behavior
- Related specs:
  - `D:/project/MyFlowHub3/repo/MyFlowHub-Server/docs/specs/varstore.md`
- Related lessons:
  - `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache/docs/lessons/capability-provider-observable-side-effects.md`
- Related changes:
  - `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache/docs/change/2026-03-05_varstore-crosshop-routing.md`
  - `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache/docs/change/2026-03-06_varstore-hop-align-subproto.md`
  - `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache/docs/change/2026-03-25_flow-set-child-notify.md`

#### Executable Task List
- [ ] `VARAUTH-1` tighten authoritative cache gate in query handlers
- [ ] `VARAUTH-2` carry forwarded `var_changed` local cache refresh fix into the worktree
- [ ] `VARAUTH-3` add regression tests for stale non-subtree cache and forwarded cache update
- [ ] `VARAUTH-4` run `go test ./varstore` and review changed paths

#### Task Details
##### `VARAUTH-1` - Tighten direct-answer authority checks
- Owner: main agent
- Worktree: `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache`
- Plan Path: `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache/plan.md`
- Goal:
  - make `get/list/subscribe` require authoritative subtree ownership before using local cache for direct replies
- Files / Modules:
  - `varstore/varstore.go`
- Write Set:
  - `varstore/varstore.go`
- Acceptance:
  - non-subtree local cache no longer short-circuits upstream query routing
  - subtree cache direct-hit behavior still works
- Test Points:
  - non-subtree `get/list/subscribe`
  - subtree direct hit
- Rollback:
  - revert the gate changes in `handleGet`, `handleList`, and `handleSubscribe`

##### `VARAUTH-2` - Preserve forwarded change local-cache updates
- Owner: main agent
- Worktree: `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache`
- Plan Path: `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache/plan.md`
- Goal:
  - ensure `var_changed` / `var_deleted` behave like `notify_*` for “forward + local handle” so refresh paths keep receiving updated cache state
- Files / Modules:
  - `varstore/varstore.go`
- Write Set:
  - `varstore/varstore.go`
- Acceptance:
  - forwarded `var_changed` updates local cache even when the frame continues to a child target
- Test Points:
  - forwarded `var_changed` regression test
- Rollback:
  - revert `OnReceive` local-handle action expansion

##### `VARAUTH-3` - Add focused regressions
- Owner: main agent
- Worktree: `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache`
- Plan Path: `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache/plan.md`
- Goal:
  - lock the stale cache bug and the forwarded change behavior with isolated unit tests
- Files / Modules:
  - `varstore/target_forward_test.go`
- Write Set:
  - `varstore/target_forward_test.go`
- Acceptance:
  - tests fail on old behavior and pass on the new gate
- Test Points:
  - stale non-subtree `get`
  - stale non-subtree `list`
  - stale non-subtree `subscribe`
  - forwarded `var_changed`
- Rollback:
  - revert new tests if the implementation plan changes completely

##### `VARAUTH-4` - Validate and review
- Owner: main agent
- Worktree: `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache`
- Plan Path: `D:/project/MyFlowHub3/worktrees/subproto-varstore-authoritative-cache/plan.md`
- Goal:
  - run focused validation and stage-3.3 review against the changed paths
- Files / Modules:
  - `varstore/*`
- Write Set:
  - none beyond diagnostic changes if needed
- Acceptance:
  - `go test ./varstore` passes
  - review checklist is explicitly recorded
- Test Points:
  - targeted module test run
- Rollback:
  - revert worktree branch changes before archive if validation fails irreparably

#### Dependencies
- No cross-repo code changes planned
- authoritative behavior depends on current `ownerInSubtree` and parent-forwarding semantics remaining unchanged

#### Risks and Notes
- `list` empty-list success semantics must stay explicit when owner is local
- stale cache may still exist by design; the fix is only to prevent it from answering authoritatively
- dirty main repo diffs are reference only and must not be copied wholesale

#### Parallelism Assessment
- No sub-agent dispatch
- Reason:
  - user did not explicitly authorize sub-agents
  - write set is narrow and tightly coupled inside `varstore`
  - sequential edit + test is faster and safer here

#### Issue List
- none

阻塞：否
进入 3.2
