# Releases

For maintainers preparing a versioned release. GitHub Actions tests pushes and
pull requests, and builds draft releases for `v*` tags.

## Prerequisites

Configure repository Actions secrets `GPG_PRIVATE_KEY` (an ASCII-armored signing
private key) and `PASSPHRASE` (its passphrase). Register the matching public key
with the Terraform Registry. Provider installation still requires a GPG signature
on the checksum file; [GitHub OIDC attestations do not replace that signature](https://developer.hashicorp.com/terraform/registry/providers/publishing).

The release job also uses GitHub OIDC to create keyless provenance attestations
for archives and checksums. It needs `id-token: write`, `attestations: write` and
`contents: write`. Attestations require a public repository on Free/Pro/Team plans;
private repositories need an eligible Enterprise plan. See [GitHub's attestation guide](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).

Publish the core first, then pin its version in `go.mod` and update `go.sum`.
The release job uses `GOWORK=off` and rejects `v0.0.0` or local replacements.
Development CI can check out `byteshuttle/allot` while that placeholder remains;
set repository variable `ALLOT_CORE_REF` to a core commit for reproducible checks.
The default is `main`. That checkout requires the core repository to exist and
be readable by the runner. Released dependencies use normal Go module resolution.

## Prepare and build

Run the repository's checks before tagging. Install GoReleaser v2.20.2 to inspect
the configuration and build local release candidates:

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=publish,sign
```

A snapshot does not publish or obtain a GitHub OIDC attestation. With a GPG key
imported locally, set `GPG_FINGERPRINT` to its full fingerprint and omit `sign`
from the skip list to exercise provider checksum signing. GoReleaser requires a commit and an
origin remote to resolve build and release metadata.

Push a reviewed `v0.x.y` tag to run the release workflow. It runs Go tests and vet,
builds Linux/macOS binaries for amd64/arm64 with the release version embedded,
creates ZIP archives and checksums, and uploads them with the detached GPG checksum
signature to a **draft** GitHub release. It then attests the archives
and checksums and attaches an offline Sigstore verification bundle. Inspect the
assets before publishing the draft.
Windows is not a release target because the local storage adapter uses Unix file locking.

The provider binary names, archive names and versioned manifest follow Terraform
Registry conventions. Configure the Terraform Registry with the signing public
key and repository before publishing the draft. OpenTofu Registry registration
is a separate distribution step; the CI compatibility matrix does not register
or publish the provider in either registry.

## Verify downloaded assets

Verify an archive's provenance against the expected repository and release workflow:

```sh
gh attestation verify ARCHIVE.zip --repo byteshuttle/terraform-provider-allot --signer-workflow byteshuttle/terraform-provider-allot/.github/workflows/release.yml
```

For offline verification, download `provenance.sigstore.json` from the release
and add `--bundle provenance.sigstore.json` to the command. Use a GitHub CLI
version supporting artifact attestations. Verify the reported source tag/commit
matches the release you intended to install. A successful attestation establishes
build provenance; it does not certify the code's behavior.

To verify the Terraform Registry checksum signature, import the maintainer's
verified public key and run:

```sh
gpg --verify PROJECT_VERSION_SHA256SUMS.sig PROJECT_VERSION_SHA256SUMS
sha256sum --check --ignore-missing PROJECT_VERSION_SHA256SUMS
```

Replace `PROJECT_VERSION` with the downloaded asset prefix. Ensure the downloaded
archive appears as `OK`; other platforms' missing files are skipped. Compare the
signing fingerprint with the trusted published fingerprint.
