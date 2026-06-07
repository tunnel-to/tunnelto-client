#!/usr/bin/env python3
"""Manage OpenClaw Control UI origins for tunnel.to tunnels.

This helper updates gateway.controlUi.allowedOrigins in the local OpenClaw
JSON config. It keeps OpenClaw's normal origin allowlist model intact; it does
not enable host-header fallback or relax gateway security checks.

Default config path:
  ~/.openclaw/openclaw.json

Examples:
  manage-openclaw-origins.py list
  manage-openclaw-origins.py add https://claw.tunnel.to
  manage-openclaw-origins.py add http://claw.127.0.0.1.nip.io:8080
  manage-openclaw-origins.py remove https://claw.tunnel.to
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
import tempfile
from datetime import datetime
from pathlib import Path
from urllib.parse import urlsplit


DEFAULT_CONFIG_PATH = Path.home() / ".openclaw" / "openclaw.json"


class UserError(Exception):
    """An expected user-facing error."""


def fail(message: str) -> int:
    print(f"Error: {message}", file=sys.stderr)
    return 1


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Manage gateway.controlUi.allowedOrigins in OpenClaw config.",
        epilog="Origins must be exact scheme://host[:port] values, with no path, query, fragment, or wildcard.",
    )
    parser.add_argument(
        "--config",
        type=Path,
        default=Path(os.environ.get("OPENCLAW_CONFIG", DEFAULT_CONFIG_PATH)),
        help=f"OpenClaw JSON config path (default: {DEFAULT_CONFIG_PATH})",
    )
    parser.add_argument("--dry-run", action="store_true", help="print the change without writing the config")

    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("list", help="show configured Control UI origins")

    add_parser = subparsers.add_parser("add", help="allow an origin")
    add_parser.add_argument("origin", help="origin to add, for example https://claw.tunnel.to")

    remove_parser = subparsers.add_parser("remove", help="remove an allowed origin")
    remove_parser.add_argument("origin", help="origin to remove")

    return parser.parse_args(argv)


def normalize_origin(value: str) -> str:
    raw = value.strip().rstrip("/")
    if not raw:
        raise UserError("origin cannot be empty")
    if "*" in raw:
        raise UserError("origin cannot contain wildcards")

    parsed = urlsplit(raw)
    scheme = parsed.scheme.lower()
    if scheme not in {"http", "https"}:
        raise UserError("origin must start with http:// or https://")
    if not parsed.netloc:
        raise UserError("origin must include a host")
    if parsed.username or parsed.password:
        raise UserError("origin must not include credentials")
    if parsed.path not in {"", "/"} or parsed.query or parsed.fragment:
        raise UserError("origin must not include a path, query, or fragment")

    try:
        port = parsed.port
    except ValueError as exc:
        raise UserError(str(exc)) from exc

    host = parsed.hostname
    if not host:
        raise UserError("origin must include a host")

    host = host.lower()
    if ":" in host and not host.startswith("["):
        host = f"[{host}]"

    default_port = (scheme == "http" and port == 80) or (scheme == "https" and port == 443)
    if port and not default_port:
        host = f"{host}:{port}"

    return f"{scheme}://{host}"


def load_config(config_path: Path) -> dict:
    if not config_path.exists():
        raise UserError(f"config not found: {config_path}")
    try:
        with config_path.open(encoding="utf-8") as handle:
            data = json.load(handle)
    except json.JSONDecodeError as exc:
        raise UserError(f"config is not valid JSON: {exc}") from exc

    if not isinstance(data, dict):
        raise UserError("config root must be a JSON object")
    return data


def get_origins(data: dict) -> tuple[dict, list[str]]:
    gateway = data.setdefault("gateway", {})
    if not isinstance(gateway, dict):
        raise UserError("gateway exists but is not an object")

    control_ui = gateway.setdefault("controlUi", {})
    if not isinstance(control_ui, dict):
        raise UserError("gateway.controlUi exists but is not an object")

    existing = control_ui.get("allowedOrigins")
    if existing is None:
        return control_ui, []
    if not isinstance(existing, list):
        raise UserError("gateway.controlUi.allowedOrigins exists but is not a list")

    origins: list[str] = []
    seen: set[str] = set()
    for index, item in enumerate(existing):
        if not isinstance(item, str):
            raise UserError(f"allowedOrigins[{index}] is not a string")
        origin = normalize_origin(item)
        if origin not in seen:
            seen.add(origin)
            origins.append(origin)
    return control_ui, origins


def save_config(config_path: Path, data: dict) -> Path:
    timestamp = datetime.now().strftime("%Y%m%d%H%M%S%f")
    backup_path = config_path.with_name(f"{config_path.name}.bak.{timestamp}")
    shutil.copy2(config_path, backup_path)

    config_json = json.dumps(data, indent=2)
    directory = config_path.parent
    fd, temp_name = tempfile.mkstemp(prefix=f".{config_path.name}.", suffix=".tmp", dir=directory)
    temp_path = Path(temp_name)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(config_json)
            handle.write("\n")
        os.replace(temp_path, config_path)
    except Exception:
        temp_path.unlink(missing_ok=True)
        raise

    return backup_path


def print_origins(origins: list[str], heading: str = "Current gateway.controlUi.allowedOrigins:") -> None:
    print(heading)
    if origins:
        for origin in origins:
            print(f"- {origin}")
    else:
        print("(none)")


def run(argv: list[str]) -> int:
    args = parse_args(argv)
    config_path = args.config.expanduser()

    data = load_config(config_path)
    control_ui, origins = get_origins(data)

    if args.command == "list":
        print_origins(origins)
        if args.dry_run:
            print("\nDry run: no changes would be made.")
        return 0

    origin = normalize_origin(args.origin)
    if args.command == "add":
        changed = origin not in origins
        new_origins = origins if not changed else origins + [origin]
        result_text = "already present; no change needed" if not changed else "added"
    else:
        changed = origin in origins
        new_origins = [item for item in origins if item != origin]
        result_text = "not present; no change needed" if not changed else "removed"

    action_text = "Would update" if args.dry_run else "Updated"
    print_origins(new_origins, heading=f"{action_text} gateway.controlUi.allowedOrigins ({result_text}):")

    if not changed:
        print("\nNo changes written.")
        return 0
    if args.dry_run:
        print(f"\nDry run: no changes written to {config_path}")
        return 0

    control_ui["allowedOrigins"] = new_origins
    backup_path = save_config(config_path, data)
    print(f"\nConfig written to: {config_path}")
    print(f"Backup saved to: {backup_path}")
    print("\nRestart or reload the OpenClaw gateway for the change to take effect.")
    return 0


def main() -> int:
    try:
        return run(sys.argv[1:])
    except UserError as exc:
        return fail(str(exc))


if __name__ == "__main__":
    sys.exit(main())
