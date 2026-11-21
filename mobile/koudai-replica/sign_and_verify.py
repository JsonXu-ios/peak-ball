"""Align, locally sign and verify an independent replica APK. Never installs it."""
import argparse
import json
import os
import secrets
import subprocess
from pathlib import Path

from verify_apk import verify

ROOT = Path(__file__).resolve().parent
REPOSITORY = ROOT.parents[1]


def run(command):
    result = subprocess.run([str(v) for v in command], capture_output=True, text=True, encoding="utf-8", errors="replace")
    if result.returncode:
        raise RuntimeError(result.stdout + result.stderr)
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("unsigned", type=Path)
    parser.add_argument("--diagnostic", action="store_true", help="Produce the separately named debug-enabled diagnostic artifact")
    parser.add_argument("--source", type=Path, default=REPOSITORY / "reports/source-probes/koudai-mobile-20261119/base.apk")
    parser.add_argument("--jdk", type=Path, default=Path("C:/Program Files/Android/Android Studio/jbr"))
    parser.add_argument("--build-tools", type=Path, default=Path(os.environ["LOCALAPPDATA"]) / "Android/Sdk/build-tools/35.0.0")
    args = parser.parse_args()
    if not args.unsigned.is_file() or not args.source.is_file():
        parser.error("Both the source and unsigned APK must exist")
    java = args.jdk / "bin/java.exe"
    keytool = args.jdk / "bin/keytool.exe"
    for tool in [java, keytool, args.build_tools / "zipalign.exe", args.build_tools / "lib/apksigner.jar"]:
        if not tool.is_file():
            parser.error("Missing build tool: " + str(tool))

    private = ROOT / "private"
    output = ROOT / "out"
    private.mkdir(exist_ok=True)
    output.mkdir(exist_ok=True)
    keystore = private / "replica-test.p12"
    password_file = private / "keystore.pass"
    if keystore.exists() and not password_file.exists():
        raise RuntimeError("Existing signing key has no password file; refusing to replace it")
    if not password_file.exists():
        password_file.write_text(secrets.token_urlsafe(32) + "\n", encoding="ascii")
    if not keystore.exists():
        run([
            keytool, "-genkeypair", "-noprompt", "-keystore", keystore,
            "-storetype", "PKCS12", "-storepass:file", password_file,
            "-keypass:file", password_file, "-alias", "local-replica",
            "-keyalg", "RSA", "-keysize", "2048", "-validity", "3650",
            "-dname", "CN=Local Replica Test,OU=Development,O=Peak Ball,C=CN",
        ])
    suffix = "-diagnostic" if args.diagnostic else ""
    aligned = output / f"koudai-replica{suffix}-aligned.apk"
    signed = output / f"koudai-replica{suffix}-1.1.79.apk"
    run([args.build_tools / "zipalign.exe", "-f", "-P", "16", "4", args.unsigned, aligned])
    run([
        java, "-jar", args.build_tools / "lib/apksigner.jar", "sign",
        "--ks", keystore, "--ks-key-alias", "local-replica",
        "--ks-pass", "file:" + str(password_file),
        "--out", signed, aligned,
    ])
    report = verify(args.source, signed, args.build_tools, java)
    report["expected_debuggable"] = args.diagnostic
    report["passed"] = report["passed"] and report["debuggable"] == args.diagnostic
    report_path = output / f"verification{suffix}.json"
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({
        "passed": report["passed"],
        "apk": str(signed),
        "verification": str(report_path),
        "application_id": report["application_id"],
        "preserved": report["implementation"]["counts"],
        "runtime_verified": False,
        "installed_on_phone": False,
    }, ensure_ascii=True))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
