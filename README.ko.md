# naru

용어: [공통 용어표](docs/glossary.md). 검사(check)는 명령 실행, 검증 기록(receipt)은 그 실행 결과와 입력의 기록이다.

> naru(나루): 강을 건너는 나루터. 변경은 사공이 확인한 뒤에만 건넌다.

`naru`는 AI 코딩 에이전트의 "완료"를 *보고*가 아니라 *검증*으로 만듭니다. 프로젝트에 필요한 검사를 실행하고, 각 검사가 의존하는 파일 내용에 묶인 검증 기록을 남깁니다. 필요한 검사가 실행되지 않았거나, 실패했거나, 실행 뒤 파일이 바뀌었으면 에이전트가 작업을 끝내지 못하게 막습니다.

[English README](README.md)

> 상태: 초기 개발(P0). `init`, `check`, `status`, `mode`, Claude Code Stop 진행 판정이 동작합니다. 계약 기준본, 정책 검토, 사용자 중계, 로컬 묶음, 그리고 Spring Boot·Android·Web 예제를 사용할 수 있습니다. 추가 플랫폼 묶음은 이후 단계입니다.

## 왜 필요한가

에이전트는 테스트를 돌리지 않고 "통과했다"고 보고하거나, 통과한 뒤에도 코드를 계속 고치곤 합니다. 입력 파일에 묶인 검증 기록이 두 경우를 모두 잡습니다.

| 상황 | `naru status` | Stop 진행 판정 |
| --- | --- | --- |
| 검사를 실행하지 않음 | `missing` | 차단 |
| 실행했지만 실패 | `fail` | 차단 |
| 통과한 뒤 입력 파일이 바뀜 | `stale` | 차단 |
| 현재 파일 기준으로 통과 | `pass` | 허용 |
| 입력 패턴에 맞는 파일이 하나도 없음 | `empty` | 차단 (검증 기록이 아무것도 증명하지 않음) |

새 실행은 `.naru/state/checks/<check>.json`에 schema 2 기록을 남깁니다. 이전 기록은 `state/history/<check>/<run_id>.json`에 보존하고 실행별 로그는 고유 ID로 만듭니다. 같은 검사 중복 실행을 막고, 실행 시작을 저장한 뒤 명령을 실행하며, 실행 전후 입력과 설정을 비교합니다. 구버전 schema 1 원본은 그대로 보존하지만 새 정책에서는 재검사가 필요합니다.

취소된 실행은 `cancelled`로 남아 이전 성공을 재사용하지 않습니다. 부모 강제 종료 후 `running`이 남으면 자식 생존 여부를 추정하지 않고 자동 재시작을 막습니다. 남은 검사 프로세스를 확인·종료하고 현재 상태 파일을 별도로 보존·이동한 뒤 다시 실행해야 합니다. 잠금 파일은 지우지 않습니다. 출력 파이프를 잡고 남은 자식도 `owner_lost`로 재시작을 막습니다. macOS/Linux에서는 셸 종료 후 프로세스 그룹이 사라졌는지 짧게 확인합니다. 남은 자식이 있거나 확인할 수 없으면 취소·시간 초과·비정상 종료에도 `owner_lost`로 차단합니다. 사용자 취소의 종료 코드 130은 유지합니다. 그룹 ID가 재사용될 수 있어 종료 후 같은 ID에 종료 신호를 반복해서 보내지 않습니다.

`status --json`은 schema 2이며 이유 코드, 실행 ID, 출처, 미검증 기록 존재 여부를 포함합니다. `running`·`cancelled`·`interrupted`·`invalid`·`incompatible`·`input-changed`·`input-error` 상태가 추가됩니다. 손상 기록은 `invalid`와 종료 1, IO 오류는 70입니다. 전후 해시는 파일 스냅샷이 아니므로 A→B→A 변화는 놓칠 수 있습니다. 물리적 전원 장애·네트워크 파일시스템·구버전 혼용은 보장하지 않습니다. 자세한 형식과 복구는 [실행 기록 문서](docs/execution-records.md)를 참고하세요.

진행 판정은 검사를 직접 실행하지 않고 검증 기록과 현재 파일만 비교하므로 빠릅니다.

## 구조

![naru 아키텍처](docs/images/naru-architecture.png)

![naru 작업 플로우](docs/images/naru-workflow.png)

도식은 계획된 전체 설계입니다. P0에서는 공통 처리부 CLI, 검증 기록, 모드, Stop 진행 판정을 구현했습니다.

## 설치

Go 1.23 이상이 필요합니다.

```sh
go install github.com/Chaebin-Park/naru/cmd/naru@latest
```

미출시 변경은 [배포 후보 준비·설치 시험·마이그레이션 안내](docs/release-preparation.md)를 따르세요. `@latest`는 게시된 모듈 버전을 설치하며 로컬 후보 준비는 릴리스 게시가 아닙니다.

## 빠른 시작

```sh
cd your-repo
naru init          # .naru/config.yaml 생성 (Go, Gradle, Maven, SwiftPM, Node, Python, Rust 감지)
naru check         # 현재 모드의 check 실행, receipt 기록
naru status        # check별 pass / fail / stale / missing
```

로그 쓰기·동기화·닫기에 실패하면 성공 완료 기록을 만들지 않고 내부 오류를 반환합니다. 시작 상태가 이미 저장됐으면 미충족으로 남습니다. 새 비공개 상태 폴더는 0700, 파일은 0600 권한으로 만들며 기존 권한은 바꾸지 않습니다. 정상 처리한 명령도 출력을 전달하지 못하면 오류로 종료합니다.

설정 형식, 모드, 훅 연결, 검증 기록, 종료 코드는 [README.md](README.md)와 같습니다. 요점은 다음과 같습니다.

- `inputs`는 저장소 루트 기준 glob입니다. `**`는 깊이와 상관없이 매칭하고, `**/*.go`는 루트의 파일도 포함합니다.
- git 저장소 안에서는 `.gitignore`에 걸린 파일이 해시에 들어가지 않습니다. `.naru/`와 `.git/`는 항상 제외합니다.
- 검사의 `run`·`workdir`·`inputs`나 전체 `exclude`를 바꿔도 검증 기록이 무효가 됩니다.
- `naru init`은 `inputs: ["**"]`와 스택별 제외 목록을 씁니다. 입력을 빠뜨리면 오래된 결과가 통과하므로 넓게 잡는 편이 안전합니다.
- 서브모듈·중첩 저장소는 내용까지 해시합니다. 실행 권한 비트도 해시에 들어갑니다. 매칭은 대소문자를 구분합니다.
- 모든 모드는 검사를 하나 이상 요구해야 합니다.
- 설정 파일에 모르는 키가 있으면 오류를 냅니다.

## Claude Code 연결

`naru`를 `PATH`에 두고 `.claude/settings.json`에 Stop 훅과 검사 실행 권한을 추가합니다.

```json
{
  "permissions": {
    "allow": ["Bash(naru check:*)", "Bash(naru status:*)"]
  },
  "hooks": {
    "PreToolUse": [
      { "matcher": "Write|Edit|NotebookEdit|Bash",
        "hooks": [{ "type": "command", "command": "naru gate pre-tool" }] }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "command": "naru gate stop" }] }
    ]
  }
}
```

차단할 때는 exit 2와 함께, 무엇이 충족되지 않았고 무엇을 실행해야 하는지 stderr로 알려 줍니다. 프로젝트 위치는 `$CLAUDE_PROJECT_DIR`을 먼저 보고 그다음 payload의 `cwd`를 봅니다. naru 프로젝트가 아니면 항상 허용하고, 설정이 잘못되었으면 차단합니다. 검증할 수 없는 상태는 통과가 아니기 때문입니다. 기본 모드가 아닌 모드로 통과하면 그 사실을 stderr에 남깁니다. 사람이 진행 판정을 끄려면 훅 환경에 `NARU_GATE=off`를 설정합니다.

- `naru gate pre-tool`은 에이전트가 `.naru/`(검증 기록, 로그, 설정, 저장된 모드)를 고치거나 모드를 바꾸는 것을 막습니다. 읽기는 허용합니다. 셸 명령 판별은 휴리스틱이라 보안 경계가 아니고, 최종 기준은 CI에서 깨끗한 상태로 다시 실행하는 `naru check`입니다.
- 훅 명령은 절대경로 대신 `naru gate stop`으로 씁니다. 에이전트는 훅 피드백에 보이는 명령을 그대로 따라 쓰기 때문에, 절대경로를 쓰면 `Bash(naru check:*)` 허용 규칙에 걸리지 않습니다.
- 에이전트가 진행할 수 없는 상태(예: `naru check` 권한 없음)에서 같은 차단이 한 Stop 흐름 안에서 `gate.max_repeats`번(기본 3) 반복되면, 종료를 허용하되 사용자에게 NOT VERIFIED를 알리고(`systemMessage`), `naru status`에도 다음 검증된 종료 전까지 경고가 남습니다. 파일 수정이나 새 실행처럼 진행이 생기면 횟수를 다시 셉니다. `gate: {max_repeats: 0}`이면 절대 포기하지 않습니다. 미검증 기록 저장·해제 또는 허용 안내 출력에 실패하면 Stop을 차단합니다.
- 실제 Claude Code 세션 검증 기록: [docs/verification.md](docs/verification.md)

Codex 0.166.0은 Stop·PreToolUse 네이티브 훅을 제공합니다. [연결 예제와 설치/해제 안내](examples/agent-hooks/README.md)에서 설정 형식과 신뢰 확인 절차를 확인하세요. NARU는 Codex `apply_patch`의 경로를 Claude 파일 도구와 별도로 검사합니다. `Edit|Write` matcher만 지정해도 패치 입력이 변환되지는 않습니다. Begin/End Patch 형식의 패치를 검사하며 알 수 없는 wrapper 문법은 추측하지 않고 차단합니다. 두 도구의 missing/pass·stale/재검사·NOT VERIFIED 실제 관찰은 [검증 기록](docs/verification.md)에 있습니다. Claude는 미신뢰 폴더에서 훅을 실행하면서도 프로젝트 허용 규칙은 무시할 수 있으므로 신뢰 절차를 확인하세요.

훅을 활성화하지 않은 도구는 `AGENTS.md`에 "완료 보고 전에 `naru status`를 실행하고, 0이 아니면 `naru check`로 고친 뒤 반복한다"는 지침을 넣을 수 있습니다. 지침만으로 훅 실행을 강제하지는 않습니다.

## Git pre-push 검사

Git 작업 트리 루트에 있는 naru 프로젝트에서 명시적으로 설치합니다.

```sh
naru hooks install
naru check --mode code    # code를 프로젝트 default_mode로 바꾸세요
naru gate pre-push        # push 없이 같은 판정 확인
```

설치된 실행 가능 훅은 `naru gate pre-push`를 호출하므로 훅의 `PATH`에 naru가 있어야 합니다. `default_mode`의 모든 검사가 현재 파일 기준 pass이고 `.naru/state/unverified.json`이 없을 때만 exit 0, 그 외에는 exit 1입니다. 설정 누락·오류, 검증 기록 읽기 오류, 손상된 NOT VERIFIED 기록도 차단합니다. 저장된 모드, `NARU_MODE`, `NARU_GATE=off`, Stop 반복 상한은 이 판정을 완화하지 않습니다. 검사를 자동 실행하지는 않습니다.

NOT VERIFIED 기록이 있으면 기본 모드로 검증한 뒤 검증된 Stop으로 기록을 해제합니다. 기본 모드가 `code`인 경우 프로젝트 루트에서:

```sh
naru check --mode code
# 위 검사가 성공한 경우에만 계속합니다:
env -u NARU_GATE -u CLAUDE_PROJECT_DIR NARU_MODE=code naru gate stop </dev/null
naru gate pre-push
```

기존 훅은 보존하며 `--force`를 명시하면 일반 파일만 교체합니다. 연결된 worktree는 훅을 공유하므로 다른 작업 복사본에도 영향을 줄 수 있습니다. `core.hooksPath`가 설정되어 있으면 설정·파일을 변경하지 않고 기존 관리자 연동을 안내합니다. 설치 명령은 상속된 `GIT_DIR`·`GIT_WORK_TREE`·`GIT_COMMON_DIR`이 있으면 거부합니다.

[Husky](https://typicode.github.io/husky/how-to.html)는 기존 `.husky/pre-push`의 다른 명령을 보존하면서 다음을 추가합니다.

```sh
naru gate pre-push || exit $?
```

[Lefthook](https://lefthook.dev/configuration/run/)은 기존 `lefthook.yml`에 다음 명령을 병합합니다.

```yaml
pre-push:
  commands:
    naru:
      run: naru gate pre-push
```

사람은 `git push --no-verify`로 우회할 수 있습니다. 이 검사는 현재 작업 트리를 평가하며, 전송되는 각 커밋·다른 브랜치·태그의 내용을 검증하지 않습니다. 로컬 보조 장치이므로 대상 리비전을 깨끗하게 checkout한 CI에서 `naru check`를 다시 실행해야 합니다.

## 현재 한계 (P0)

MVP에서는 macOS·Linux를 우선 검증하고 Windows는 제한 지원합니다. 실제 실행 검증과 빌드·정적 검사 결과를 구분하며 실행하지 않은 기능은 미검증으로 표시합니다.

- 검증 기록과 설정이 로컬 파일입니다. `naru gate pre-tool`이 흔한 수정 경로를 막지만 셸 판별은 휴리스틱이므로, CI에서 다시 검증해야 합니다.
- 검사는 `sh -c`로 하나씩 순서대로 실행합니다. Windows에서는 POSIX 셸이 필요하고, timeout 때 자식 프로세스까지 종료하지 못합니다.
- 검사가 남긴 백그라운드 프로세스가 출력을 열어 두면, naru는 종료 상태를 기록하고 5초 뒤 대기를 멈춥니다.
- 정책 검토는 명시적으로 실행합니다. `mode_policy`로 Stop/pre-push 판정에 포함할 수 있습니다.
- 사용자 중계는 증거를 제출한 사람을 인증하지 않으며, CI는 검사를 따로 실행해야 합니다. Android·Web 묶음은 공개 예제와 고정된 도구 버전만 다룹니다.

기한부 예외는 MVP에서 제외하고 추후 검토합니다. 검증 미완료 종료는 기존 NOT VERIFIED 정책을 유지합니다.

## 라이선스

[MIT](LICENSE)

현재 동작·기록 형식·시험 대응표·후속 개선 항목: [현재 동작 기준 문서](docs/current-behavior.md).

개발 중 피드백: [로컬 정적 분석·취약점 보고](docs/development-analysis.md). 코드 수정 후 `sh scripts/devcheck.sh`를 실행하며, `-watch`는 터미널에서 변경 감시를 켭니다.

작업 진행 규칙: [완료·커밋 검사](docs/task-gate.md). 이전 작업의 완료 커밋과 일치하는 검증 근거가 있어야 다음 작업을 시작합니다.

## 실행 환경 조건

검사별로 선택 항목 `requires`를 선언할 수 있습니다.

```yaml
checks:
  test:
    run: go test ./...
    inputs: ["**/*.go", "go.mod", "go.sum"]
    requires:
      tools: [go]
      os: [darwin, linux]
```

`tools`는 PATH에서 실행 파일을 찾고, `os`는 허용할 운영체제(`darwin`, `linux`, `windows`)를 지정합니다. OS 목록을 생략하면 운영체제를 제한하지 않습니다. 도구 이름은 최대 256바이트이며 경로·공백·셸 구문을 허용하지 않습니다. 중복 항목과 알 수 없는 키는 설정 오류입니다. 모든 검사는 기본적으로 `sh`도 필요합니다.

조건을 충족하지 못하면 명령 실행과 입력 해시 계산 없이 `not-run`을 기록하고 `check`는 1을 반환합니다. 사유는 `missing_tool` 또는 `unsupported_platform`이며 명령 종료 코드는 null입니다. 도구를 준비하거나 지원 환경으로 옮긴 뒤 검사를 다시 실행해야 합니다. 환경 복구만으로 저장된 미실행 기록이 성공으로 바뀌지 않습니다. 기존 Stop 우회 정책은 NOT VERIFIED 표시와 함께 유지됩니다.

선언한 조건은 입력 지문에 반영됩니다. 목록 순서는 무관하고 생략하면 기존 지문을 유지합니다. 도구 버전·데몬 상태·인증·서비스 연결은 확인하지 않습니다. 도구 설치나 CI 대리 실행도 자동으로 하지 않으며, 확인 후 명령 시작 사이에 환경이 바뀔 수 있습니다.

## 외부 계약 항목의 표준 형식

내부 [추출 계약 명세](docs/surface-contract.md)에 JSON 표준 형식, 결과 digest, 오류·빈 결과 구분을 정의하고 시험 자료와 테스트를 추가했습니다. 추출·비교 명령이 이 계약을 사용하며 승인은 아래 대화형 명령을 사용합니다.

## 로컬 플랫폼 묶음

설정의 `packs`로 저장소 안의 `packs/<이름>/pack.yaml`을 선택합니다. 검사는 `<묶음 ID>.<검사명>`으로 사용하며 manifest와 묶음 파일 변경도 검증 기록을 무효화합니다. [사용법과 예제](docs/packs.md)를 참고하세요. 다운로드·자동 선택·도구 버전 탐지는 하지 않습니다.

## 기준본 후보와 비교

`naru baseline extract <surface>`로 검증된 로컬 후보를 만들고 `naru baseline diff <surface>`로 추가·삭제·변경을 확인합니다. 두 명령 모두 `--json`을 지원합니다. 추출은 기준본을 덮어쓰지 않으며 처음이거나 변경된 결과는 검토가 필요합니다. [후보 상태와 종료 코드](docs/baselines.md)를 참고하세요. 승인은 아래 대화형 명령을 사용합니다.

`naru baseline approve <surface>`는 macOS/Linux 터미널에서 검토자·사유·전체 digest 확인을 받아 후보를 승인합니다. 저장 직전에 입력과 검토한 후보·기존 기준본을 다시 확인하고 snapshot과 승인 기록을 함께 저장합니다. 빈 후보는 별도 확인이 필요합니다. 구형 단독 snapshot은 새 승인 전까지 완료로 인정하지 않습니다. 터미널 확인은 사람의 신원을 인증하는 기능이 아닙니다. [승인 동작과 한계](docs/baselines.md#interactive-approval)를 참고하세요.

[Spring Boot 공개 예제 묶음](packs/spring-boot/README.md)은 API 응답, 프로필별 라우트, SQL 파일 해시, 설정 키의 변화를 보여줍니다. Java·Maven·Python이 필요하며 직접 활성화해야 합니다. 설정 값은 내보내지 않습니다. 빌드 검사만으로 승인된 외부 계약과의 일치를 자동 강제하지는 않습니다.

[Android 공개 예제 묶음](packs/android/README.md)은 Java 앱과 로컬 라이브러리를 빌드하고 실제 debug 병합 매니페스트와 해석된 의존성 그래프를 추출합니다. 지정된 Android·Gradle·JDK 도구와 직접 활성화가 필요합니다. 권한·컴포넌트의 exported 상태·의존성 변경을 기준본 명령으로 검토하며, 에뮬레이터나 기기는 사용하지 않습니다. [검증 범위](docs/android-verification.md)를 참고하세요.

[Web 공개 예제 묶음](packs/web/README.md)은 실제 Node HTTP 서버와 공유하는 라우트 목록, 로컬 workspace 의존성, 환경변수 키를 추출합니다. 격리한 설정으로 npm을 오프라인 실행합니다. 키 목록은 합성 `.env.example`에서만 읽고 값은 내보내지 않습니다. [검증 범위](docs/web-verification.md)를 참고하세요.

## 정책 검토

`naru policy list`는 `.naru/policy.yml`을 검증하고, `naru policy test [rule-id]`는 규칙에 연결된 정상·위반 fixture를 실제로 실행합니다. `naru policy check`는 전체 검사 결과를 갱신해 기존·신규·해결 위반을 구분합니다. 기존 위반은 경고하고 신규 위반은 차단합니다. 도구 출력이 없거나 불완전하면 해결로 인정하지 않습니다.

터미널에서 `naru policy bootstrap`으로 처음 알려진 위반을 승인하고, 이후 `naru policy approve`로 줄어든 목록만 승인합니다. 저장 전에 검토한 후보와 현재 입력을 다시 확인합니다. 위반 추가나 규칙 정의 변경에 따른 이전은 승인할 수 없습니다. 정책 저장소는 계약 기준본과 분리되며 Stop/pre-push 요구사항에 자동으로 추가되지 않습니다. [형식·명령·제한사항](docs/policy.md)을 참고하세요.

## 사용자 중계 검사

사람이 다른 환경에서 검사해야 한다면 `naru relay new --target 'test workstation' --artifact execution.log CHECK`로 요청하고, `naru relay submit --file submission.json PACKET_ID`로 결과와 산출물 해시를 제출합니다. 요청을 만들면 해당 검사의 현재 증거가 대기 상태로 바뀝니다. 유효한 `human-relayed` 성공은 로컬 Stop/pre-push의 근거가 될 수 있지만, CI는 `naru check --evidence ci`를 별도로 실행해야 합니다. NARU는 원격 명령을 실행하거나 제출자의 진술을 인증하지 않습니다. [요청·제출·만료·신뢰 한계](docs/relay.md)를 참고하세요.

`naru status --markdown`은 같은 판정·사유 코드·증거 출처를 Markdown 표로 출력합니다. `--json`과 동시에 사용할 수 없습니다. NOT VERIFIED는 별도 표시하며, 필수 검사가 통과해도 pre-push를 계속 차단합니다.

`mode_surfaces: {code: [api]}`로 모드에 필수 기준본을 지정할 수 있습니다. CLI status·Stop·pre-push는 최신 입력과 일치하고 승인된 기준본에 변경이 없을 때만 통과합니다. 필수 surface가 있는 보고서는 schema 3, 검사만 있는 보고서는 schema 2입니다. [필수 기준본 안내](docs/baselines.md#required-baselines-in-a-mode)를 참고하세요.

`mode_policy: {code: true}`로 정책 근거를 필수로 지정합니다. 공통 status와 gate는 근거 누락·stale·미승인·신규 위반을 차단합니다. 기존 위반은 경고하며, 기준 축소에는 사람 승인이 필요합니다. 정책을 선택한 보고서는 schema 3입니다. [정책 판정 안내](docs/policy.md#required-policy-evidence)를 참고하세요.

[공통 보고서 계약과 adapter 판정 비교](docs/common-report.md).

[실행 환경·신뢰성 검증·판정 성능](docs/reliability-performance.md): 10,000개 파일·100MiB warm-cache 측정에서 초기 p95 2초 목표를 충족했습니다. Go 1.23.12·Git Bash의 [Windows 실제 경로·검증 기록 생명주기 테스트](docs/windows-verification.md)는 통과했습니다. Windows 취소·하위 프로세스 정리·실제 pre-push는 미검증이며, 플랫폼별 Go 검증 범위는 CI 기록을 참고하세요.

[Web clean CI smoke](docs/ci-smoke.md)는 커밋된 소스만 사용하는 실행기와 고정된 GitHub Actions workflow를 제공합니다. Ubuntu·Go 1.23.12에서 실제 원격 실패·복구 검증을 마쳤으며 해당 문서에 리비전과 실행 링크를 기록했습니다.

[MVP 수용 근거와 남은 조건](docs/mvp-acceptance.md)에서 승인 후 실행한 실제 로컬 push 검증과 남은 환경 제한을 확인할 수 있습니다.
