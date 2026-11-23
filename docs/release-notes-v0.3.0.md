## English

- Read the streamlined English or Korean setup guide. The platform verification and release references are linked from the contribution guide.
- GitHub Actions covers native Linux with Go 1.23.12 and 1.27.4, native Windows paths and recovery, and clean-checkout Web checks.
- The release contains five platform archives and SHA256SUMS. Verify both the checksum manifest and the selected archive against the production workflow identity before extraction. [Artifact trust policy](https://github.com/Chaebin-Park/naru/blob/v0.3.0/docs/release-trust.md)
- The Linux status results describe their measured workloads. The isolated 10,000-file synthetic run missed its 2-second warm-cache p95 target. [Measurement results](https://github.com/Chaebin-Park/naru/blob/v0.3.0/docs/linux-status-measurement.md)

## 한국어

- 간결해진 영어·한국어 설치 안내를 제공합니다. 플랫폼 검증과 릴리즈 참조 문서는 기여 안내에서 확인할 수 있습니다.
- GitHub Actions에서 Go 1.23.12와 1.27.4로 Linux 네이티브 검사를 실행합니다. Windows 경로·복구와 Web 클린 체크아웃도 확인합니다.
- 배포 파일은 플랫폼별 압축 5개와 SHA256SUMS로 구성됩니다. 압축을 풀기 전에 정식 워크플로의 신원을 기준으로 체크섬 목록과 선택한 압축 파일을 모두 검증하세요. [배포 파일 신뢰 정책](https://github.com/Chaebin-Park/naru/blob/v0.3.0/docs/release-trust.md)
- Linux status 수치는 해당 실험의 입력과 설정에 한정됩니다. 격리된 합성 파일 10,000개 실험은 warm-cache p95 2초 목표를 충족하지 못했습니다. [측정 결과](https://github.com/Chaebin-Park/naru/blob/v0.3.0/docs/linux-status-measurement.md)
