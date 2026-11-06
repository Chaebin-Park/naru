# NARU glossary

Use one name for each concept in NARU documents. The Korean column gives the preferred name for Korean prose. Keep commands, field names, paths, and quoted output unchanged.

This glossary explains terms. It does not approve a design or mark a planned feature as complete. Baselines, packs, policy review, and relay are implemented within the limits stated in their own documents. Waivers are deferred beyond the MVP. The design separates execution results, evidence sources, and exceptions. Existing receipts remain usable only when their meaning and evaluation rules are preserved. Otherwise, require a new check. Each implemented feature's document defines its storage format.

## Checks and decisions

| English term | Korean name | Meaning |
| --- | --- | --- |
| check | 검사 | A configured command that tests project inputs. |
| mode | 모드 | A named set of required checks. |
| receipt | 검증 기록 | A saved check result tied to its inputs. |
| evidence | 증거 | Information used to support a decision. A receipt is one form of evidence. |
| result | 실행 결과 | The outcome of a check run. Values include `pass`, `fail`, `timeout`, `error`, `cancelled`, and `not-run`. See [execution records](execution-records.md). |
| evidence source / provenance | 증거 출처 | Where evidence came from. `check` accepts the labels `local`, `agent-run`, and `ci`. Relay submissions record `human-relayed`. A label does not authenticate its author. |
| status | 검사 상태 | A receipt evaluated against current inputs and configuration. Values include `pass`, `fail`, `stale`, `missing`, `empty`, `not-run`, `running`, `interrupted`, and `cancelled`. See [execution records](execution-records.md) for the full list. |
| gate | 진행 판정 | A decision to allow or block an action. Each gate has its own exit policy. |
| decision report | 판정 보고서 | Data that explains which requirements are satisfied. The shared report format is proposed. |
| fresh receipt | 현재 입력에 맞는 검증 기록 | A receipt whose inputs and check definition still match. Freshness alone does not mean the check passed. |
| NOT VERIFIED | 검증 미완료 | Completion without verified checks. This is not a passing result. |
| fail closed | 판정 불가 시 차단 | Block an action when its requirements cannot be evaluated. |
| false pass | 잘못된 통과 판정 | A passing decision for work that does not meet requirements. |

A check runs a command. A gate evaluates evidence. A gate does not run checks automatically. A receipt records one run. A decision report explains the evaluated state.

## Feature terms

| English term | Korean name | Meaning |
| --- | --- | --- |
| baseline | 기준본 | An accepted reference used to compare later changes. Name the kind when the distinction matters. |
| surface | 외부 계약 항목 | Observable project contracts, such as routes or configuration keys. |
| surface baseline | 계약 기준본 | The accepted reference for external contract items. Approval accepts a specific change to that reference. |
| violation baseline | 기존 위반 기준 | Recorded existing violations used to detect new violations. It is separate from contract approval. Remove resolved items only after human approval. If they return, treat them as new violations and block. |
| pack | 플랫폼 묶음 | Platform checks, extractors, rule templates, and test fixtures. A pack is not a mode. For the MVP, packs are versioned files in the project repository. Automatic external downloads and updates are excluded. |
| relay | 사용자 중계 | A person runs a prepared request and submits evidence. NARU does not run the remote command itself. Valid results can support local completion. Valid passing results can also satisfy the corresponding pre-push check. Other required checks and NOT VERIFIED recovery conditions still apply. CI runs checks separately. See [relay](relay.md). |
| waiver | 기한부 예외 | Permission to leave a requirement unmet within a defined scope and time. It is not a passing receipt. This feature is deferred beyond the MVP. No waiver registration or acceptance is part of the MVP. |
| policy index | 정책 색인 | Links between tool rules and their reasons. |
| not-run | 실행하지 못함 | The result recorded when a declared prerequisite (a tool or OS) is unavailable, so the check did not run. It never satisfies a gate. |
| human-relayed | 사람이 중계한 출처 | The source label for evidence submitted through relay. |
| waived | 예외 허용 상태 | A proposed exception decision, separate from check success. It is deferred with waivers. |

`not-run` is an execution result, not an evidence label. Only relay submission records `human-relayed`. `waived` is not implemented. Its storage format and acceptance rules require a design decision.

## Design and testing

| English term | Korean name | Meaning |
| --- | --- | --- |
| core | 공통 처리부 | Shared execution and decision logic. |
| adapter | 연결 모듈 | Converts shared decisions into a tool's protocol and exit policy. |
| schema | 데이터 형식 | Rules for stored or exchanged fields and values. |
| canonical form | 표준 형식 | One consistent representation of equivalent content. |
| normalization | 정규화 | Convert equivalent content into the same representation. Preserve meaningful differences. |
| digest / hash | 내용 해시 | A value calculated from content to detect changes. |
| definition fingerprint | 검사 정의 식별값 | A representation of the check definition used in input hashing. Do not assume the representation is itself a hash. |
| fixture | 시험 자료 | Fixed inputs and expected conditions for a test. |
| golden output | 기대 출력 기준본 | Saved expected output used to detect test differences. |
| preflight | 실행 전 환경 확인 | Determine whether required tools and resources are available. |
| smoke test | 기본 동작 시험 | A small test of the main execution path. |
| clean checkout | 깨끗한 작업 사본 | Files from the target revision without reused local results. |
| working tree | 작업 트리 | Files currently present in a Git working copy. |
| revision | 리비전 | A selected version of repository content. |
| task | 작업 | A bounded unit with a goal and completion criteria. |
| DoD | 완료 조건 | Conditions that must hold before a task is complete. |
| FR | 기능 요구사항 | Required product behavior. |
| NFR | 비기능 요구사항 | Required quality, safety, or operating constraints. |
| AC | 수용 조건 | Evidence required to accept the delivered result. |
| ADR | 설계 결정 기록 | A decision with its context, alternatives, and consequences. |
| DAG | 순환 없는 의존 관계 | Dependencies that never lead back to the same task. |

## Writing rules

Use the English term in English prose and the Korean name in Korean prose. At first use, define an unfamiliar concept or link to this glossary. Diagram labels use the same meanings even when space requires English labels.

Keep technical identifiers unchanged. For example, write “검증 기록” in Korean prose and keep `receipt`, `inputs_hash`, and `naru check` unchanged when referring to code.

Do not rewrite historical output to match current wording. Explain old terms through this glossary. Do not replace a requirement, proposal, or measured result with a stronger claim.
