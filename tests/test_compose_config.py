"""Check Compose environment wiring without building or starting containers."""

import json
import os
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]
DATABASE_URL = "postgres://compose.invalid/config_test"
OPTIONAL_SETTINGS = {
    "GKFEED_ALLOWED_ORIGINS": "https://reader.example,https://other.example",
    "GKFEED_SYNC_CURSOR_SECRET": "synthetic-primary-sync-secret-32-bytes",
    "GKFEED_JWT_SECRET": "synthetic-legacy-sync-secret-32-bytes",
    "GKFEED_WEBAUTHN_RP_ID": "reader.example",
    "GKFEED_WEBAUTHN_RP_ORIGIN": "https://reader.example",
    "GKFEED_WEBAUTHN_RP_DISPLAY": "Compose test reader",
    "GKFEED_ACCESS_TTL": "17m",
    "GKFEED_JWT_REFRESH_TTL": "240h",
    "OPENROUTER_API_KEY": "synthetic-openrouter-key",
}


def compose_config(settings):
    # Ignore host application settings and local .env files, including any DB URL.
    environment = {
        key: value
        for key, value in os.environ.items()
        if not key.startswith(("GKFEED_", "COMPOSE_", "OPENROUTER_"))
    }
    environment.update(settings)
    return subprocess.run(
        [
            "docker", "compose",
            "--file", str(ROOT / "docker-compose.yml"),
            "--env-file", os.devnull,
            "--project-name", f"gkfeed-compose-config-test-{os.getpid()}",
            "config", "--format", "json",
        ],
        env=environment,
        capture_output=True,
        text=True,
        check=False,
    )


class ComposeConfigTest(unittest.TestCase):
    def app_environment(self, settings):
        result = compose_config({"GKFEED_DATABASE_URL": DATABASE_URL, **settings})
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)["services"]["app"]["environment"]

    def test_optional_settings_reach_container(self):
        environment = self.app_environment(OPTIONAL_SETTINGS)
        for key, value in OPTIONAL_SETTINGS.items():
            with self.subTest(key=key):
                self.assertEqual(environment.get(key), value)
        self.assertEqual(environment["GKFEED_DATABASE_URL"], DATABASE_URL)
        self.assertEqual(environment["GKFEED_ADDRESS"], ":8086")

    def test_unset_or_empty_settings_preserve_application_defaults(self):
        for settings in ({}, dict.fromkeys(OPTIONAL_SETTINGS, "")):
            with self.subTest(settings="unset" if not settings else "empty"):
                environment = self.app_environment(settings)
                for key in OPTIONAL_SETTINGS:
                    self.assertEqual(environment.get(key), "", key)

    def test_legacy_secret_reaches_container_without_primary_secret(self):
        legacy = OPTIONAL_SETTINGS["GKFEED_JWT_SECRET"]
        environment = self.app_environment({"GKFEED_JWT_SECRET": legacy})
        self.assertEqual(environment.get("GKFEED_SYNC_CURSOR_SECRET"), "")
        self.assertEqual(environment.get("GKFEED_JWT_SECRET"), legacy)

    def test_database_url_remains_required(self):
        for settings in ({}, {"GKFEED_DATABASE_URL": ""}):
            with self.subTest(settings=settings):
                result = compose_config(settings)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("GKFEED_DATABASE_URL", result.stderr)


if __name__ == "__main__":
    unittest.main()
