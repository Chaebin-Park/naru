# Release artifact trust policy

Future signed releases use GitHub Artifact Attestations with the public
Sigstore instance. The GitHub-hosted release workflow in
`Chaebin-Park/naru` obtains a short-lived credential through GitHub Actions
OIDC. GitHub stores the bundle and the public transparency log records the
signing event. A managed key would require custody, rotation, revocation and
distribution of a trusted verification key. The keyless model avoids those
operational requirements and depends on GitHub's workflow identity and
Sigstore's trust infrastructure. GitHub lists artifact attestations for public
repositories on its current plans and does not charge for standard hosted
runner compute on public repositories.
[GitHub artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations)
[Sigstore identity-based signing](https://docs.sigstore.dev/cosign/signing/overview/)
[GitHub Actions billing](https://docs.github.com/en/actions/concepts/billing-and-usage)

## Production identity and permissions

- Trust `Chaebin-Park/naru/.github/workflows/release.yml` and the exact
  certificate identity
  `https://github.com/Chaebin-Park/naru/.github/workflows/release.yml@refs/tags/<version>`.
  The OIDC issuer is `https://token.actions.githubusercontent.com`.
- The maintainer identifies an approved version tag and its full peeled source
  commit. Checkout and packaging must use that exact commit. The workflow must
  be reviewed at that commit. Its signer digest and the source digest must both
  match the approved commit.
- Build on a GitHub-hosted runner, pin actions by full commit SHA, and grant the
  attestation job only `contents: read`, `id-token: write`, and
  `attestations: write`. Publication is a separate authorized operation.
- Forks and pull request events receive no signing or publication permissions.
  The isolated validation workflow uses its own signer identity and a test
  ref. Its attestations fail production identity and ref checks.
  [GitHub workflow permissions for attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations)

## Artifacts and verification

Attest the five final platform archive byte streams and the final
`SHA256SUMS` file with SLSA Provenance v1. Each archive must have a matching
bundle subject digest. Verify the checksum manifest before using it to check
the five archive hashes. Verify each archive's attestation before extraction,
then check its embedded version and source metadata. Establish the version
and expected commit from the approved release record independently of the
downloaded archive and bundle.

Run this for each downloaded archive and for `SHA256SUMS`. Set `version` and
`source_commit` to the approved release inputs.

```sh
gh attestation verify "$artifact" \
  --repo Chaebin-Park/naru \
  --cert-identity "https://github.com/Chaebin-Park/naru/.github/workflows/release.yml@refs/tags/$version" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com \
  --signer-digest "$source_commit" \
  --source-digest "$source_commit" \
  --source-ref "refs/tags/$version" \
  --predicate-type https://slsa.dev/provenance/v1 \
  --deny-self-hosted-runners
```

The same constraints apply to offline verification with the bundle and
trusted root acquired through GitHub's documented process.
[GitHub CLI verification flags](https://cli.github.com/manual/gh_attestation_verify)
[Offline verification](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline)

| Evidence | Required decision |
| --- | --- |
| Approved workflow, tag, source commit and matching artifact digest | Accept if verification exits zero and archive metadata agrees |
| Changed bytes or mismatched digest | Reject |
| Different workflow, repository, issuer or self-hosted runner | Reject |
| Different source commit or ref, even with valid signature | Reject |
| Missing bundle, wrong predicate or verifier failure | Reject |

## Expiration and recovery

Sigstore verifies the short-lived certificate against trusted signing-time
evidence. Do not accept an invalid timestamp or bypass the verifier when
trust material is unavailable. If the workflow, runner or release identity
is suspected of compromise, suspend signing and distribution, identify
affected versions and digests, and publish an advisory. Recover the trusted
workflow, review a new source commit and release under a new approved version.
Treat affected digests as rejected even if their historical signatures
still verify. Keep the original verification evidence for incident review.
[Sigstore timestamp verification](https://docs.sigstore.dev/cosign/verifying/timestamps/)

The current v0.2.0 release is unsigned. The isolated workflow must prove the
accept and reject cases above before this policy is used for publication.
