# naru

[English](README.md)

naru는 저장소 검사를 실행하고 각 결과가 어떤 입력에 적용되는지 기록합니다. 작업 완료나 Git 푸시 전에는 현재 파일에 유효한 통과 기록이 있는지 확인할 수 있습니다.

## 설치와 시작

Go 1.23 이상에서 [최신 공개 버전](https://github.com/Chaebin-Park/naru/releases/latest)을 설치합니다. 설치 이후 명령은 프로젝트 디렉터리에서 실행합니다.

```sh
go install github.com/Chaebin-Park/naru/cmd/naru@latest
naru init
naru check
naru status
```

초기화한 뒤 `.naru/config.yaml`을 검토하세요. 검사 명령이 사용하는 파일이 변경되면 이전 기록을 무효화할 수 있도록 명령과 입력 패턴을 지정합니다. 필요한 빌드 설정과 의존성 목록도 입력에 포함하세요. 모드는 필수 검사 집합을 선택합니다. 현재 작업 사본의 모드를 바꾸려면 `naru mode <name>`을 실행합니다.

## 결과 확인과 적용

| 결과 | 조치 |
| --- | --- |
| 통과 | 검사 기록이 현재 선언된 입력과 검사 정의에 일치합니다. |
| 누락 또는 오래된 기록 | 필수 검사를 다시 실행합니다. |
| 실패 | 검사 출력을 확인해 원인을 수정한 뒤 다시 실행합니다. |
| 무효 또는 중단 | 실행 기록을 확인하고 복구 절차를 따릅니다. |

자동화에는 `naru status --json`, 보고서에는 `naru status --markdown`을 사용합니다. 두 옵션은 동시에 사용할 수 없습니다. NOT VERIFIED와 같이 충족되지 않은 조건도 게이트가 차단합니다. 게이트는 기존 기록을 평가합니다. 새 기록을 만들려면 `naru check`를 실행합니다.

[Claude Code 및 Codex 예제](examples/agent-hooks/README.md)를 참고해 에이전트에 훅을 설정합니다. 검사 기록이 없을 때와 검사에 통과했을 때 각각 훅이 실제로 동작하는지 확인하세요. Git 훅에는 `naru gate pre-push`를 사용할 수 있습니다. CI는 자체 체크아웃에서 검사를 실행해야 합니다.

## 프로젝트 검사와 복구

- Android, Spring Boot, Web 프로젝트는 저장소 내 팩을 예제로 사용할 수 있습니다. 각 팩에 필요한 도구와 입력·내보내기 범위를 확인하세요. [팩](docs/packs.md)
- 기준본을 승인하기 전에 후보의 계약 변경 사항을 확인하세요. 사람이 전달한 검사에는 별도의 근거 출처와 유효 조건이 적용됩니다. [기준본](docs/baselines.md) · [검사 결과 전달](docs/relay.md)
- 로컬 검사 로그와 기록은 버전 관리에서 제외하세요. 검사가 중단되면 상태를 지우기 전에 복구 절차를 따릅니다. Windows에서는 취소 후에도 하위 프로세스가 남을 수 있습니다. [실행 기록과 복구](docs/execution-records.md)

## 다운로드와 지원 범위

릴리즈 페이지에서 운영체제와 아키텍처에 맞는 압축 파일을 내려받습니다. 압축을 풀기 전에 SHA-256 해시를 해당 릴리즈의 `SHA256SUMS`와 비교하세요. 빌드 증명이 있는 릴리즈는 신뢰 정책에 따라 워크플로, 태그, 소스 커밋을 검증합니다. [릴리즈](https://github.com/Chaebin-Park/naru/releases) · [릴리즈 신뢰 정책](docs/release-trust.md)

배포 파일은 macOS arm64/amd64, Linux arm64/amd64, Windows amd64용으로 빌드합니다. 네이티브 테스트는 정해진 실행 시나리오를 다룹니다. 사용할 셸, 취소 동작, 팩의 검증 범위를 확인하세요. Linux 성능 수치는 측정에 사용한 입력과 설정에 한정됩니다. 이후 격리 측정에서 합성 파일 10,000개 워크로드는 warm-cache p95 2초 목표를 충족하지 못했습니다. [플랫폼 검증](https://github.com/Chaebin-Park/naru/actions) · [측정 결과](docs/linux-status-measurement.md)

## 기여

[기여 안내](CONTRIBUTING.md)와 [변경 이력](CHANGELOG.md)을 참고하세요. [MIT 라이선스](LICENSE)로 배포합니다.
