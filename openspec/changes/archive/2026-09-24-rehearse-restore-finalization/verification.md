# Restore rehearsal verification

- The focused integration contract passed, including a synthetic finalization failure that checks CI-visible output and the uploaded summary for credential, backup, and diagnostic markers.
- Strict OpenSpec validation, shell syntax checks, and `git diff --check` passed.
- Local disposable Docker integration passed for PostgreSQL 16.14 and 18.4 with the final restore harness. Each leg backed up and restored a populated database, finalized it offline, rejected captured old credentials, and verified the replacement credential and retained state through the public API.
- An initial PostgreSQL 16 rehearsal exposed an assertion using the wrong public identity field. The assertion was corrected against the published API schema; both final matrix runs passed. Raw run output and backup material were not added to this change.
- OCR identified two restore assertions that could miss changed historical data or hide a failing CLI exit. Both were corrected; both local PostgreSQL legs passed again, and a second OCR pass reported zero findings. Excluded documentation and OpenSpec files were reviewed locally.
