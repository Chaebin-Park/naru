# naru

용어: [공통 용어표](docs/glossary.md). 검사(check)는 명령 실행, 검증 기록(receipt)은 그 실행 결과와 입력의 기록이다.

> naru(나루): 강을 건너는 나루터. 변경은 사공이 확인한 뒤에만 건넌다.

`naru`는 AI 코딩 에이전트의 "완료"를 *보고*가 아니라 *검증*으로 만듭니다. 프로젝트에 필요한 검사를 실행하고, 각 검사가 의존하는 파일 내용에 묶인 검증 기록을 남깁니다. 필요한 검사가 실행되지 않았거나, 실패했거나, 실행 뒤 파일이 바뀌었으면 에이전트가 작업을 끝내지 못하게 막습니다.

[English README](README.md)

> 상태: 초기 개발(P0). `init`, `check`, `status`, `mode`, Claude Code Stop 진행 판정이 동작합니다. 기준본, 정책, 사용자 중계, 플랫폼 묶음은 이후 단계입니다.

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

취소된 실행은 `cancelled`로 남아 이전 성공을 재사용하지 않습니다. 부모 강제 종료 후 `running`이 남으면 자식 생존 여부를 추정하지 않고 자동 재시작을 막습니다. 남은 검사 프로세스를 확인·종료하고 현재 상태 파일을 별도로 보존·이동한 뒤 다시 실행해야 합니다. 잠금 파일은 지우지 않습니다. 출력 파이프를 잡고 남은 자식도 `owner_lost`로 재시작을 막습니다.

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
      { "matcher": "Write|Edit|MultiEdit|NotebookEdit|Bash",
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

Codex 등 다른 에이전트는 네이티브 훅을 지원하기 전까지 `AGENTS.md`에 "완료 보고 전에 `naru status`를 실행하고, 0이 아니면 `naru check`로 고친 뒤 반복한다"는 지침을 넣어 연결합니다.

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

기한부 예외는 MVP에서 제외하고 추후 검토합니다. 검증 미완료 종료는 기존 NOT VERIFIED 정책을 유지합니다.

## 라이선스

[MIT](LICENSE)

현재 동작·기록 형식·시험 대응표·후속 개선 항목: [현재 동작 기준 문서](docs/current-behavior.md).

개발 중 피드백: [로컬 정적 분석·취약점 보고](docs/development-analysis.md). 코드 수정 후 `sh scripts/devcheck.sh`를 실행하며, `-watch`는 터미널에서 변경 감시를 켭니다.
