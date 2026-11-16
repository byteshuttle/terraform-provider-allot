# Releases

For maintainers preparing a versioned release. GitHub Actions tests pushes and
pull requests, and builds draft releases for `v*` tags.

## Prerequisites

The dedicated provider signing key is published as [signing-key.asc](../signing-key.asc)
and attached to releases. Its full fingerprint is:

```text
E671D56B52B1CA6F446A95330004DD2721D3F3F5
```

The key expires on 2028-11-14. Before expiry, extend it or rotate the key, update
GitHub's `GPG_PRIVATE_KEY` and `PASSPHRASE` Actions secrets, and update the public
key registered with the Terraform Registry. Keep a protected backup of the
private key, passphrase and revocation certificate outside the repository.

The checksum file lists provider archives and the Registry manifest only. The
public key is attached separately; including it in package checksums prevents
Registry ingestion. The release workflow validates this inventory before
attesting the draft.

The release workflow reads those secrets to sign the checksum file. Register
the public key and provider with the Terraform Registry before expecting Registry
installation to work. [OIDC attestations do not replace that signature](https://developer.hashicorp.com/terraform/registry/providers/publishing).

The release job also uses GitHub OIDC to create keyless provenance attestations
for archives and checksums. It needs `id-token: write`, `attestations: write` and
`contents: write`. Attestations require a public repository on Free/Pro/Team plans;
private repositories need an eligible Enterprise plan. See [GitHub's attestation guide](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).

The provider pins core `v0.1.0`. For core upgrades, publish the new core version
first, then update `go.mod` and `go.sum` and run the compatibility matrix. Both
CI and the release job use `GOWORK=off`; releases reject development placeholders
or local module replacements.

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
public key, check its fingerprint against the value above, and run:

```sh
gpg --import signing-key.asc
gpg --fingerprint E671D56B52B1CA6F446A95330004DD2721D3F3F5
gpg --verify PROJECT_VERSION_SHA256SUMS.sig PROJECT_VERSION_SHA256SUMS
sha256sum --check --ignore-missing PROJECT_VERSION_SHA256SUMS
```

Replace `PROJECT_VERSION` with the downloaded asset prefix. Ensure the downloaded
archive appears as `OK`; other platforms' missing files are skipped. Compare the
signing fingerprint with the trusted published fingerprint.

## Repair a rejected checksum inventory

If Registry ingestion rejects an existing release because its package checksum
file includes `signing-key.asc`, dispatch the **Repair release checksums** workflow
with that release tag. It verifies the original signature, provenance and asset
hashes, removes only the public-key entry, then signs and attests the corrected
checksum file. Provider archives, their hashes and tags stay unchanged. The
original offline bundle retains the archive attestations; the corrected checksum
file has a separate `checksums-provenance.sigstore.json` bundle.

```sh
gh workflow run repair-release-checksums.yml --repo byteshuttle/terraform-provider-allot -f tag=v0.1.1
```

After the workflow succeeds, verify the corrected signature and checksum-file
attestation from `repair-release-checksums.yml`, update any consumer pin of the
checksum file itself, and resync the provider in the Registry.
