#!/usr/bin/env python3
"""Solax EU cloud API client — login and backfill daily energy data."""

import argparse
import base64
import hashlib
import json
import os
import random
import sys
import time
from datetime import date, timedelta, datetime
from typing import Any, Optional, Union

try:
    import requests
    from Crypto.Cipher import AES
    from Crypto.Util.Padding import pad, unpad
except ImportError:
    sys.exit("Install dependencies: pip install -r requirements.txt")

KEY = b"***REDACTED***"
IV  = b"***REDACTED***"

BASE_URL      = "https://euapi.solaxcloud.com"
ORIGIN_LOGIN  = "https://www.solaxcloud.com"
ORIGIN_DATA   = "https://global.solaxcloud.com"
UA            = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/605.1.15 (KHTML, like Gecko) "
    "Version/26.2 Safari/605.1.15"
)


# ---------------------------------------------------------------------------
# Crypto helpers
# ---------------------------------------------------------------------------

def _encrypt(plaintext: str) -> str:
    data = plaintext.encode()
    cipher = AES.new(KEY, AES.MODE_CBC, IV)
    return base64.b64encode(cipher.encrypt(pad(data, AES.block_size))).decode()


def _decrypt(ciphertext_b64: str) -> Union[dict[str, Any], str]:
    raw = base64.b64decode(ciphertext_b64)
    cipher = AES.new(KEY, AES.MODE_CBC, IV)
    plaintext = unpad(cipher.decrypt(raw), AES.block_size).decode()
    try:
        return json.loads(plaintext)  # type: ignore[no-any-return]
    except json.JSONDecodeError:
        return plaintext


def _encrypt_json(data: dict[str, Any]) -> str:
    return _encrypt(json.dumps(data, separators=(",", ":")))


def _encrypt_qs(params: dict[str, Any]) -> str:
    return _encrypt("&".join(f"{k}={v}" for k, v in params.items()))


# ---------------------------------------------------------------------------
# Misc helpers
# ---------------------------------------------------------------------------

def _md5(s: str) -> str:
    return hashlib.md5(s.encode()).hexdigest()


def _rand_hex(n: int) -> str:
    return "".join(random.choices("0123456789abcdef", k=n))


def _ts_ms() -> int:
    return int(time.time() * 1000)


# ---------------------------------------------------------------------------
# Client
# ---------------------------------------------------------------------------

class SolaxClient:
    def __init__(self, email: str, password: str):
        self.email = email
        self.password = password
        self.session = requests.Session()
        self.token: Optional[str] = None
        self._token_expiry: float = 0

    # ------------------------------------------------------------------
    # Auth
    # ------------------------------------------------------------------

    def login(self) -> None:
        ts = _ts_ms()
        qs = _encrypt_qs({"timeStamp": ts, "requestId": _rand_hex(8)})
        body = _encrypt_json({
            "loginName": self.email,
            "password": _md5(self.password),
            "service": "",
        })

        headers = {
            "Accept": "application/json, text/plain, */*",
            "Content-Type": "application/json",
            "crytoVer": "1",
            "deviceId": _rand_hex(8),
            "deviceType": "3",
            "Lang": "en_US",
            "Origin": ORIGIN_LOGIN,
            "Referer": ORIGIN_LOGIN + "/",
            "source": "0",
            "websiteType": "0",
            "x-request-source": "3",
            "x-transaction-id": f"{_rand_hex(8)}-{random.randint(100000, 999999)}-{ts}",
            "User-Agent": UA,
        }

        resp = self.session.post(
            f"{BASE_URL}/unionUser/web/v2/public/login",
            params={"data": qs},
            json={"data": body},
            headers=headers,
        )
        resp.raise_for_status()

        result = _decrypt(resp.json()["data"])
        if not result.get("success"):
            raise RuntimeError(f"Login failed: {result.get('message')}")

        self.token = result["result"]["token"]
        # JWT exp is embedded in the token; keep a local expiry with a 60s buffer
        payload = json.loads(base64.b64decode(self.token.split(".")[1] + "=="))
        self._token_expiry = payload["exp"] - 60
        # acw_tc session cookie is stored automatically in self.session

    def _ensure_logged_in(self) -> None:
        if not self.token or time.time() >= self._token_expiry:
            self.login()

    # ------------------------------------------------------------------
    # Data
    # ------------------------------------------------------------------

    def get_energy_info(self, site_id: str, day: date, retries: int = 3) -> Optional[dict]:
        """Return the energy info dict for *day*, or None if no data exists.
        Retries up to *retries* times on transient errors with exponential backoff."""
        last_exc: Optional[Exception] = None
        for attempt in range(1, retries + 1):
            try:
                self._ensure_logged_in()

                ts = _ts_ms()
                body = _encrypt_json({
                    "year": day.year,
                    "month": day.month,
                    "day": day.day,
                    "siteId": site_id,
                    "dimension": 1,
                })

                headers = {
                    "Accept": "application/json, text/plain, */*",
                    "Content-Type": "application/json",
                    "crytoVer": "1",
                    "deviceId": f"{_rand_hex(8)}-{ts}",
                    "deviceType": "3",
                    "Lang": "en_US",
                    "Origin": ORIGIN_DATA,
                    "Referer": ORIGIN_DATA + "/",
                    "Permission-Version": "v7.2.0",
                    "platform": "1",
                    "queryTime": datetime.now().strftime("%Y-%m-%d %H:%M:%S"),
                    "source": "0",
                    "token": self.token,
                    "version": "blue",
                    "websiteType": "0",
                    "x-transaction-id": f"{_rand_hex(8)}-{ts}",
                    "User-Agent": UA,
                }

                qs = _encrypt_qs({"timeStamp": ts, "requestId": _rand_hex(8)})
                resp = self.session.post(
                    f"{BASE_URL}/zeus/v1/overview/energyInfo",
                    params={"data": qs},
                    json={"data": body},
                    headers=headers,
                )
                resp.raise_for_status()

                payload = resp.json()
                if "data" not in payload:
                    raise RuntimeError(f"Unexpected response for {day}: {payload}")
                result = _decrypt(payload["data"])
                if not result.get("success") or not result.get("result"):
                    if attempt < retries:
                        wait = 2 ** attempt
                        print(f"empty response — retrying in {wait}s...", end="  ", flush=True)
                        time.sleep(wait)
                        last_exc = RuntimeError(f"empty response for {day}")
                        continue
                    return None
                return result["result"]

            except Exception as exc:
                last_exc = exc
                if attempt < retries:
                    wait = 2 ** attempt
                    print(f"error ({exc}) — retrying in {wait}s...", end="  ", flush=True)
                    time.sleep(wait)

        raise last_exc or RuntimeError("All retries failed")

    # ------------------------------------------------------------------
    # Backfill
    # ------------------------------------------------------------------

    @staticmethod
    def _has_real_data(result: dict) -> bool:
        """Return False if the response is all-zeros (site exists but not yet installed)."""
        return (
            result.get("yield", {}).get("totalYield", 0) > 0
            or result.get("consumed", {}).get("totalConsumed", 0) > 0
        )

    def backfill(
        self,
        site_id: str,
        start: Optional[date] = None,
        since: Optional[date] = None,
        output_dir: str = "data",
        delay: float = 0.5,
        max_gap: int = 14,
    ) -> None:
        """
        Fetch daily energy data working backwards from *start* (default: today)
        until *max_gap* consecutive days return no data, or *since* date is reached.
        Saves each day as <output_dir>/YYYY-MM-DD.json.
        *delay* seconds are inserted between requests to avoid hammering the API.
        """
        if start is None:
            start = date.today()

        os.makedirs(output_dir, exist_ok=True)
        current = start
        saved = 0
        consecutive_empty = 0

        while True:
            if since and current < since:
                print(f"Reached --since date {since} — done.")
                break

            out_path = os.path.join(output_dir, f"{current}.json")
            if os.path.exists(out_path):
                print(f"{current}  already exists, skipping")
                consecutive_empty = 0
                current -= timedelta(days=1)
                continue

            print(f"{current}  fetching...", end="  ", flush=True)
            data = self.get_energy_info(site_id, current)

            if data is not None and not self._has_real_data(data):
                data = None  # treat all-zero response as no data

            if data is None:
                consecutive_empty += 1
                print(f"no data ({consecutive_empty}/{max_gap} consecutive)")
                if consecutive_empty >= max_gap:
                    print("Max consecutive empty days reached — done.")
                    break
            else:
                consecutive_empty = 0
                with open(out_path, "w") as f:
                    json.dump(data, f, indent=2, ensure_ascii=False)
                saved += 1
                print(f"saved ({saved} total)")

            current -= timedelta(days=1)
            time.sleep(delay)

        print(f"\nBackfill complete. {saved} file(s) written to '{output_dir}/'.")


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def main() -> None:
    parser = argparse.ArgumentParser(
        description="Fetch Solax daily energy data, working backwards until no data exists."
    )
    parser.add_argument("email",    nargs="?", default=os.environ.get("SOLAX_EMAIL"),
                        help="Solax account email (or set SOLAX_EMAIL)")
    parser.add_argument("password", nargs="?", default=os.environ.get("SOLAX_PASSWORD"),
                        help="Solax account password (or set SOLAX_PASSWORD)")
    parser.add_argument("site_id",  nargs="?", default=os.environ.get("SOLAX_SITE_ID"),
                        help="Site ID (or set SOLAX_SITE_ID)")
    parser.add_argument(
        "--start", "-s",
        default=None,
        metavar="YYYY-MM-DD",
        help="Date to start from (default: today)",
    )
    parser.add_argument(
        "--since",
        default=os.environ.get("SOLAX_SINCE"),
        metavar="YYYY-MM-DD",
        help="Stop at this date, exclusive (e.g. installation date). Also reads SOLAX_SINCE.",
    )
    parser.add_argument(
        "--output", "-o",
        default="data",
        metavar="DIR",
        help="Directory to write JSON files (default: ./data)",
    )
    parser.add_argument(
        "--delay", "-d",
        type=float,
        default=0.5,
        metavar="SECONDS",
        help="Delay between requests in seconds (default: 0.5)",
    )
    parser.add_argument(
        "--max-gap", "-g",
        type=int,
        default=14,
        metavar="DAYS",
        help="Stop after this many consecutive empty days (default: 14)",
    )
    args = parser.parse_args()

    missing = [name for name, val in [("email", args.email), ("password", args.password), ("site_id", args.site_id)] if not val]
    if missing:
        parser.error(f"Missing required values (pass as args or env vars): {', '.join(missing)}")

    start = date.fromisoformat(args.start) if args.start else date.today()
    since = date.fromisoformat(args.since) if args.since else None

    client = SolaxClient(args.email, args.password)
    print("Logging in...")
    client.login()
    print(f"OK. Backfilling from {start}{f' to {since}' if since else ''}...\n")
    client.backfill(args.site_id, start=start, since=since, output_dir=args.output, delay=args.delay, max_gap=args.max_gap)


if __name__ == "__main__":
    main()
