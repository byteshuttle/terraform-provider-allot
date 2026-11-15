# Terraform provider for Allot

Manage hierarchical IPv4, IPv6 and integer allocations through the shared Allot
library. The provider implements `allot_space` and `allot_claim`, using local or
S3 storage without running the CLI or an allocation server.

- [Provider configuration and lifecycle](docs/index.md)
- [Root spaces](docs/resources/space.md)
- [Child claims](docs/resources/claim.md)
- [Network example](examples/network/main.tf)
- [VLAN and SLA example](examples/integers/main.tf)

Pass a parent's `.ref` output into `within`; `.id` is its allocation UUID. Actual
values and bounds are in `.result`. Requests are immutable and changes require
replacement. Destroy releases only childless allocations. Import explicitly
selects a live allocation with `SPACE_KEY/UUID`.

## Local development

Prerequisites are Go 1.26 or later and Terraform. The provider pins the released
`github.com/byteshuttle/allot v0.1.0` module and builds independently of the core
checkout. CI sets `GOWORK=off` to verify that released dependency.

The optional parent `go.work` selects the sibling core for joint development.
Use `GOWORK=off` when checking the pinned version locally; no sibling replacement
path is stored in this repository's `go.mod`.

From this repository:

```sh
go build -o .local/bin/terraform-provider-allot .
mkdir -p .local
cat > .local/dev.tfrc <<EOF_CONFIG
provider_installation {
  dev_overrides {
    "byteshuttle/allot" = "$PWD/.local/bin"
  }
  direct {}
}
EOF_CONFIG
export TF_CLI_CONFIG_FILE="$PWD/.local/dev.tfrc"
export ALLOT_STORE="$PWD/.local/allocations"
terraform -chdir=examples/network plan
terraform -chdir=examples/network apply
```

These commands use a local development override. Skip `terraform init` in this
workflow: there is no published provider for it to discover. Keep the override
and the same store selected for subsequent plan, import and destroy commands.
The provider has no release or Registry distribution yet.

## Development checks and CI

```sh
make lint test build
make generate
make testacc
```

`make generate` renders `docs/` with the pinned `tfplugindocs` Go tool, using
schema descriptions and `templates/` as the source. Edit those inputs, then
regenerate; CI rejects generated documentation drift.

The standard `terraform-plugin-testing` harness exercises apply, refresh, import
of every selector, replacement and destroy. Complementary CLI tests cover plan
purity, explicit adoption, failed destroy with external children, root replacement
and request mode changes. `TF_ACC=1` enables these tests; plain `go test` skips them.

Use the same tests with OpenTofu:

```sh
TF_ACC=1 \
TF_ACC_TERRAFORM_PATH="$(command -v tofu)" \
TF_ACC_PROVIDER_NAMESPACE=byteshuttle \
TF_ACC_PROVIDER_HOST=registry.opentofu.org \
go test -race -count=1 -timeout 30m ./...
```

CI configures Terraform 1.6.6/1.17.2 and OpenTofu 1.6.3/1.13.9, with local storage
and a pinned Moto S3 fixture in every matrix entry. The
[fixture](.github/scripts/s3_fixture.py) serves requests serially to make Moto
conditional writes atomic; client stale-read/CAS retries remain exercised. These are configured targets;
a successful hosted run is needed to establish their CI status. Providers use
protocol 6; no broader CLI version support is implied by that protocol alone.

To run the S3 acceptance test locally, provide a dedicated existing bucket,
endpoint and standard AWS SDK credentials:

```sh
TF_ACC=1 TF_ACC_PROVIDER_NAMESPACE=byteshuttle \
ALLOT_TEST_S3_ENDPOINT=http://127.0.0.1:5000 \
ALLOT_TEST_S3_BUCKET=allot-integration \
ALLOT_TEST_S3_REGION=us-east-1 \
go test ./internal/provider -run '^TestAccS3Lifecycle$' -count=1 -v
```

It writes under a unique `provider-test-UUID` prefix and retains released root
records after destroy. It does not create or delete the bucket. An emulator check
does not establish live AWS or R2 compatibility.

See [release preparation and signing](docs/releasing.md) for independent provider
versions, core dependency requirements and draft release assets.
