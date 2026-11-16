"""Validate the Registry package checksum contract, or remove a public-key entry."""
import argparse
import hashlib
from pathlib import Path
import re


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("--remove-public-key", action="store_true")
    args = parser.parse_args()
    manifests = list(args.directory.glob("terraform-provider-allot_*_SHA256SUMS"))
    if len(manifests) != 1:
        parser.error("expected one provider checksum file")
    manifest = manifests[0]
    match = re.fullmatch(r"terraform-provider-allot_(\d+\.\d+\.\d+)_SHA256SUMS", manifest.name)
    if not match:
        parser.error("expected a stable semantic release version")
    prefix = f"terraform-provider-allot_{match[1]}_"
    lines = manifest.read_text().splitlines()
    entries = {}
    for line in lines:
        fields = line.split()
        if len(fields) != 2 or not re.fullmatch(r"[0-9a-f]{64}", fields[0]):
            parser.error("invalid SHA256SUMS entry")
        digest, name = fields
        if Path(name).name != name or name in entries:
            parser.error("unsafe or duplicate checksum filename")
        entries[name] = digest
    archives = {p.name for p in args.directory.glob(prefix + "*.zip")}
    if not archives:
        parser.error("no provider archives")
    expected = archives | {prefix + "manifest.json"}
    extra, missing = set(entries) - expected, expected - set(entries)
    if missing or (extra and not (args.remove_public_key and extra == {"signing-key.asc"})):
        parser.error(f"Registry checksum entries differ: unexpected={sorted(extra)}, missing={sorted(missing)}")
    for name, digest in entries.items():
        source = args.directory / name
        if not source.is_file() or hashlib.sha256(source.read_bytes()).hexdigest() != digest:
            parser.error(f"checksum mismatch or missing release asset: {name}")
    if args.remove_public_key and extra:
        manifest.write_text("\n".join(line for line in lines if line.split()[1] != "signing-key.asc") + "\n")
        print("Removed public key from package checksums; package hashes unchanged")
    else:
        print("Registry release checksums verified")


if __name__ == "__main__":
    main()
