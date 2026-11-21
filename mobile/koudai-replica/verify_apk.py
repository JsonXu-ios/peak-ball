"""Verify that a local client replica preserves the supplied APK's implementation.

This verifies the artifact, not remote service compatibility or account entitlements.
No account credentials are needed or read.
"""
import argparse
import hashlib
import json
import re
import struct
import subprocess
import zipfile
from pathlib import Path


def digest(data):
    return hashlib.sha256(data).hexdigest()


def signature_entry(name):
    return bool(re.fullmatch(r"META-INF/(?:MANIFEST\.MF|[^/]+\.(?:SF|RSA|DSA|EC))", name, re.I))


def verify_resource_identity(before, after):
    if len(before) != len(after):
        return {"passed": False, "reason": "resource table length changed"}
    kind, head_size, size, package_count = struct.unpack_from("<HHII", before)
    if kind != 2 or size != len(before) or package_count != 1:
        return {"passed": False, "reason": "unexpected source resource table"}
    offset = head_size
    ranges = []
    while offset < size:
        kind, chunk_header, chunk_size = struct.unpack_from("<HHI", before, offset)
        if chunk_size < chunk_header or chunk_header < 8 or offset + chunk_size > size:
            return {"passed": False, "reason": "invalid resource chunk"}
        if kind == 0x200:
            ranges.append((offset + 12, offset + 268))
        offset += chunk_size
    if offset != size or len(ranges) != 1:
        return {"passed": False, "reason": "ambiguous resource package"}
    start, end = ranges[0]
    old_name = before[start:end].decode("utf-16le").split("\0", 1)[0]
    new_name = after[start:end].decode("utf-16le").split("\0", 1)[0]
    only_name_changed = before[:start] == after[:start] and before[end:] == after[end:]
    return {
        "passed": only_name_changed and old_name == "com.niudian.yun.cordova.koudai" and new_name == old_name + ".replica",
        "old_package_name": old_name,
        "new_package_name": new_name,
        "package_name_byte_range": [start, end],
        "resource_ids_and_content_unchanged": only_name_changed,
        "resource_table_bytes": len(before),
    }


def compare_implementation(original, replica):
    with zipfile.ZipFile(original) as source, zipfile.ZipFile(replica) as built:
        source_names = {n for n in source.namelist() if not n.endswith("/")}
        built_names = {n for n in built.namelist() if not n.endswith("/")}
        ignored = {"AndroidManifest.xml", "resources.arsc"}
        source_files = {n for n in source_names if n not in ignored and not signature_entry(n)}
        built_files = {n for n in built_names if n not in ignored and not signature_entry(n)}
        missing = sorted(source_files - built_files)
        extra = sorted(built_files - source_files)
        different = [n for n in sorted(source_files & built_files) if digest(source.read(n)) != digest(built.read(n))]
        counters = {
            "web_assets": sum(n.startswith("assets/www/") for n in source_files),
            "dex_files": sum(bool(re.fullmatch(r"classes\d*\.dex", n)) for n in source_files),
            "native_libraries": sum(n.startswith("lib/") and n.endswith(".so") for n in source_files),
            "all_preserved_files": len(source_files),
        }
        resource_check = verify_resource_identity(source.read("resources.arsc"), built.read("resources.arsc"))
        return {
            "passed": not missing and not extra and not different and resource_check["passed"],
            "counts": counters,
            "missing": missing,
            "extra": extra,
            "different": different,
            "manifest_changed": source.read("AndroidManifest.xml") != built.read("AndroidManifest.xml"),
            "resource_identity": resource_check,
        }


def run_checked(command):
    result = subprocess.run([str(v) for v in command], capture_output=True, text=True, encoding="utf-8", errors="replace")
    return {"exit_code": result.returncode, "output": (result.stdout + result.stderr).strip()}


def verify(original, replica, sdk_build_tools, java):
    original, replica, sdk_build_tools, java = map(Path, (original, replica, sdk_build_tools, java))
    report = {
        "source_sha256": digest(original.read_bytes()),
        "replica_sha256": digest(replica.read_bytes()),
        "replica_bytes": replica.stat().st_size,
        "implementation": compare_implementation(original, replica),
        "alignment": run_checked([sdk_build_tools / "zipalign.exe", "-c", "-P", "16", "4", replica]),
        "signature": run_checked([java, "-jar", sdk_build_tools / "lib/apksigner.jar", "verify", "--verbose", "--print-certs", replica]),
        "package": run_checked([sdk_build_tools / "aapt.exe", "dump", "badging", replica]),
        "runtime_verified": False,
        "login_verified": False,
        "remote_services_verified": False,
    }
    package_line = next((line for line in report["package"]["output"].splitlines() if line.startswith("package:")), "")
    report["debuggable"] = any(line.strip() == "application-debuggable" for line in report["package"]["output"].splitlines())
    match = re.search(r"name='([^']+)'", package_line)
    report["application_id"] = match.group(1) if match else None
    report["package"]["output"] = "\n".join(line for line in report["package"]["output"].splitlines() if line.startswith(("package:", "sdkVersion:", "targetSdkVersion:", "application-label:", "launchable-activity:", "native-code:", "application-debuggable")))
    report["passed"] = (
        report["implementation"]["passed"]
        and report["implementation"]["manifest_changed"]
        and all(report[k]["exit_code"] == 0 for k in ("alignment", "signature", "package"))
        and report["application_id"] == "com.niudian.yun.cordova.koudai.replica"
    )
    return report


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("original", type=Path)
    parser.add_argument("replica", type=Path)
    parser.add_argument("--build-tools", type=Path, required=True)
    parser.add_argument("--java", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    report = verify(args.original, args.replica, args.build_tools, args.java)
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({k: report[k] for k in ("passed", "application_id", "replica_bytes", "implementation")}, ensure_ascii=True))
    raise SystemExit(0 if report["passed"] else 1)
