**Follow-up protocol, frozen before model requests, 2026-11-15**

Test whether compression can be retained while avoiding the original pilot's
authoring errors. Three conditions: ordinary Go control; read/write Smol with a
short rule to keep argument commas and explicit returns/braces; read Smol and
write newly authored code in ordinary Go syntax accepted by the lowerer.
Existing Smol anchors may be copied unchanged. The formatter does compression.

Tasks: gin-gonic__gin-1957 is the known header-binding failure used to develop
the policies. gin-gonic__gin-3820 (optional file binding), gin-gonic__gin-3227
(redirect behavior), and caddyserver__caddy-5404 (lexer panic) were not evaluated
in the original pilot. Locations are supplied equally; filenames from the
reference patch informed localization. These are purposive Go tasks with
package-level tests that can execute on the host. This is not random sampling
or a full repository-agent evaluation. Report the three new tasks separately
from the known development task.

Two fresh episodes per task/condition: 24 episodes total. Same gpt-6.1-sol,
medium reasoning, JSON patch schema, no tools, and at most two patch/lowering/
original-test-build repairs as the first pilot. Fresh Go controls, not reused
historical counts. Rotate condition order across tasks and repetitions. Include
all input/output tokens and failures. No held-out test feedback reaches the
model. Freeze policy files and source bundles before the first request.

Upstream tests must fail on the original base and pass on the reference patch.
Use the unchanged benchmark test patches and test-selection expressions,
checking all expected test names. Restore original go.mod before tests; set
Go 1.22 only during translation. Preserve test comments and compiler directives.
The Go input is the same comment-free code as the Smol input.

Replay every final patch and check canonical Smol formatting separately from
task success. No parser changes, reference-solution copying, grammar-error
autocorrection, post-result policy tuning or retries after semantic failures.

A useful result is fewer total tokens with no observed task-success decline
against the paired Go controls. Eight episodes over four tasks cannot prove
non-inferiority or guarantee unchanged performance on arbitrary tasks. The new
tasks are held out from policy tuning, but remain public and from familiar repos.

Environment amendment before any Caddy-5404 model request: its pinned
quic-go v0.33.0 dependency rejects Go 1.21 and newer. Run that task's base,
reference, candidate and replay tests with checksum-verified Go 1.20.14
(distribution recorded in toolchain.json); keep the other tasks on Go 1.26.3.
The translator still uses Go 1.26.3. No dependencies, task selection, policy,
source bundle or upstream tests are changed to accommodate this build constraint.
