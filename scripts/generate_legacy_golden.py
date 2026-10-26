#!/usr/bin/env python3
"""Generate the v20 oracle using Pebble's Rust store in a temporary Cargo project.

The reference checkout is a path dependency and is never modified. Cargo output,
its target directory and the synthetic database all stay in the system temp dir.
No mailbox, installed profile or keyring is opened.
"""
import argparse
import hashlib
import json
import os
import sqlite3
import subprocess
import tempfile
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("pebble", type=Path, help="read-only Rust Pebble checkout")
args = parser.parse_args()
pebble = args.pebble.resolve()
root = Path(__file__).resolve().parents[1]
source = pebble / "crates/pebble-store/src/migrations.rs"
source_hash = hashlib.sha256(source.read_bytes()).hexdigest()

with tempfile.TemporaryDirectory(prefix="pebble-v20-golden-") as directory:
    temporary = Path(directory)
    (temporary / "src").mkdir()
    manifest = """[package]
name = "pebble-v20-golden"
version = "0.0.0"
edition = "2021"

[dependencies]
pebble-store = { path = %s }
rusqlite = { version = "0.39", features = ["bundled"] }
""" % json.dumps(str(pebble / "crates/pebble-store"))
    (temporary / "Cargo.toml").write_text(manifest)
    (temporary / "src/main.rs").write_text('''fn main() {
    let path = std::env::args().nth(1).expect("synthetic database path");
    let connection = rusqlite::Connection::open(path).unwrap();
    pebble_store::migrations::run_migrations(&connection).unwrap();
    let version: u32 = connection.pragma_query_value(None, "user_version", |r| r.get(0)).unwrap();
    assert_eq!(version, 20, "reference schema changed; review before refreshing golden");
}
''')
    database = temporary / "synthetic.sqlite"
    environment = dict(os.environ, CARGO_TARGET_DIR=str(temporary / "target"))
    subprocess.run(
        ["cargo", "run", "--quiet", "--manifest-path", str(temporary / "Cargo.toml"), "--", str(database)],
        env=environment, cwd=temporary, check=True,
    )
    with sqlite3.connect(database) as connection:
        objects = [
            dict(type=typ, name=name, table=table, sql=" ".join(sql.split()))
            for typ, name, table, sql in connection.execute(
                "SELECT type,name,tbl_name,sql FROM sqlite_master "
                "WHERE name NOT LIKE 'sqlite_%' AND sql IS NOT NULL ORDER BY type,name"
            )
        ]
    output = root / "internal/store/testdata/legacy_v20.json"
    output.parent.mkdir(parents=True, exist_ok=True)
    golden = dict(
        version=20,
        source="Pebble/crates/pebble-store/src/migrations.rs",
        source_sha256=source_hash,
        objects=objects,
    )
    output.write_text(json.dumps(golden, ensure_ascii=False, indent=2) + "\n")
    print(f"Generated {output}: {len(objects)} schema objects; Rust source sha256 {source_hash}")
    if hashlib.sha256(source.read_bytes()).hexdigest() != source_hash:
        raise SystemExit("reference migration source changed during generation")
