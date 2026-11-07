"""Check Compose environment wiring without building or starting containers."""

import json
import os
from pathlib import Path
import subprocess


ROOT = Path(__file__).resolve().parents[1]
SETTINGS = {
    "GKFEED_ALLOWED_ORIGINS": "https://reader.example,https://other.example",
    "GKFEED_SYNC_CURSOR_SECRET": "synthetic-primary-sync-secret-32-bytes",
    "GKFEED_JWT_SECRET": "synthetic-legacy-sync-secret-32-bytes",
    "GKFEED_WEBAUTHN_RP_ID": "reader.example",
    "GKFEED_WEBAUTHN_RP_ORIGIN": "https://reader.example",
    "GKFEED_WEBAUTHN_RP_DISPLAY": "Compose test reader",
    "GKFEED_ACCESS_TTL": "17m",
    "GKFEED_JWT_REFRESH_TTL": "240h",
}

# Ignore host settings and local .env files, including any real database URL.
environment = {
    key: value
    for key, value in os.environ.items()
    if not key.startswith(("GKFEED_", "COMPOSE_", "OPENROUTER_"))
}
environment["GKFEED_DATABASE_URL"] = "postgres://compose.invalid/config_test"

for settings in ({}, SETTINGS):
    result = subprocess.run(
        [
            "docker", "compose",
            "--file", str(ROOT / "docker-compose.yml"),
            "--env-file", os.devnull,
            "--project-name", f"gkfeed-compose-config-test-{os.getpid()}",
            "config", "--format", "json",
        ],
        env={**environment, **settings},
        capture_output=True,
        text=True,
        check=True,
    )
    actual = json.loads(result.stdout)["services"]["app"]["environment"]
    for key in SETTINGS:
        expected = settings.get(key, "")
        if actual.get(key) != expected:
            raise AssertionError(f"{key}: expected {expected!r}, got {actual.get(key)!r}")

print("Compose settings and defaults passed")
