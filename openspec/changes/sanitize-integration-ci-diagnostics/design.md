## Context

The CI integration step runs `scripts/integration.sh` directly. On failure, that script saves `docker compose ps` and `docker compose logs` in `DANS_QA_LOG_DIR`, and the workflow uploads the whole directory. The script also writes arbitrary command output to the CI console. See `proposal.md` for the publication risk and the delta spec for the required behavior.

## Goals / Non-Goals

**Goals:** Preserve the failing exit status and a useful matrix/phase diagnostic while preventing raw integration output from reaching CI logs or artifacts.

**Non-Goals:** Redact arbitrary Docker output after capture, change local integration behavior, or change the numeric performance-measurement artifact.

## Decisions

1. Use a small CI-only shell entry point around the existing harness. It redirects the harness's stdout and stderr to runner-local storage, including the existing raw Compose diagnostics. On failure, it emits a fixed-format summary to the CI console and a separate artifact directory. This is safer than attempting to scrub unpredictable raw logs.
2. Have the harness record a fixed phase marker at a few existing boundaries. The entry point maps the pinned PostgreSQL image to `pg16` or `pg18`, accepts only known phase markers, and falls back to `unknown` when no valid marker exists. Summary fields are constructed from those constants and the numeric exit status; no captured output is interpolated. The entry point accepts the harness path as an argument so its publication behavior can be tested with a synthetic failing command.
3. Point the failure-only artifact upload at the summary directory, never at `DANS_QA_LOG_DIR`. Keep the existing success-path performance artifact unchanged; its files contain numeric measurements and fixed labels.
4. Exercise the entry point with synthetic credentials, paths, and operational identifiers in both captured output and raw-log files. Assert failure propagation and absence of those bytes from the console/summary; also verify the unknown-phase fallback and no failure artifact on success.
5. The unchanged macOS development-shell CI contract timed out twice with no test output, while its local equivalent and the instrumented CI run passed. Add fixed, nonsecret phase markers at the test's major boundaries and waits. A focused macOS `ps` fault injection reproduces a launcher-watchdog deadlock: a missing start-time lookup makes the watchdog exit before releasing the supervisor's readers. Let the watchdog wait for its direct child even when that lookup misses, and give the launcher scenario a test-local deadline. Keep raw captured process output private and preserve the existing status and lock assertions. The original CI timeouts cannot be attributed to this race from their empty logs.

## Risks / Trade-offs

- Less detail is available after a CI runner exits → The allowlisted phase identifies where to reproduce locally; raw diagnostics remain private only for the life of the runner.
- A new failure path may omit its phase marker → `unknown` is an explicit safe fallback and still preserves the nonzero result.
- The original CI timeouts lack a phase trace → Fix only the independently reproduced watchdog deadlock and keep fixed markers for any later failure.
