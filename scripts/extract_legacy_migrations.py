#!/usr/bin/env python3
"""Copy legacy SQL verbatim from a read-only Pebble checkout.

Run only when intentionally refreshing the legacy migration port. Go helpers
implement the conditional/data migrations around these SQL blocks. The Rust
schema golden is generated separately by generate_legacy_golden.py.
"""
import argparse
import json
import re
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("pebble", type=Path)
args = parser.parse_args()
source = (args.pebble / "crates/pebble-store/src/migrations.rs").read_text()
root = Path(__file__).resolve().parents[1] / "internal/store/migrations"
root.mkdir(parents=True, exist_ok=True)

schema = re.search(r'const SCHEMA_V1: &str = r#"(.*?)"#;', source, re.S).group(1)
(root / "001.sql").write_text(schema.strip() + "\n")
for version in range(2, 21):
    start = source.index(f"    if version < {version} {{")
    end = source.find(f"    if version < {version+1} {{", start)
    if end == -1:
        end = source.index("const SCHEMA_V1:", start)
    block = source[start:end]
    # Rust execute_batch SQL here contains no escaped quotes or raw strings.
    scripts = re.findall(r'(?s)tx\.execute_batch\(\s*("(?:[^"\\]|\\.)*")\s*,?\s*\)', block)
    for index, literal in enumerate(scripts):
        sql = json.loads(literal, strict=False)
        # Leading indentation is immaterial to SQLite, but keep the exact SQL
        # tokens, constraints, triggers and data migration queries.
        sql = "\n".join(line.strip() for line in sql.strip().splitlines())
        (root / f"{version:03}_{index:02}.sql").write_text(sql + "\n")
    print(f"v{version}: {len(scripts)} SQL blocks")

# V12's conditional rebuild helper is outside run_migrations.
helper = source[source.index("fn rebuild_accounts_with_pop3_provider"):source.index("fn rebuild_snippets")]
scripts = re.findall(r'(?s)conn\.execute_batch\(\s*("(?:[^"\\]|\\.)*")\s*,?\s*\)', helper)
for index, literal in enumerate(scripts):
    (root / f"012_{index:02}.sql").write_text(json.loads(literal, strict=False).strip() + "\n")
