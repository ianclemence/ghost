#!/usr/bin/env python3
"""Offline timezone conversion via zoneinfo. No network."""
import sys
from datetime import datetime

try:
    from zoneinfo import ZoneInfo
except ImportError:
    print("Error: zoneinfo unavailable on this Python.")
    sys.exit(1)

CITIES = {
    "bangkok": "Asia/Bangkok", "thailand": "Asia/Bangkok",
    "london": "Europe/London", "uk": "Europe/London",
    "paris": "Europe/Paris", "france": "Europe/Paris",
    "berlin": "Europe/Berlin", "germany": "Europe/Berlin",
    "tokyo": "Asia/Tokyo", "japan": "Asia/Tokyo",
    "singapore": "Asia/Singapore", "hong kong": "Asia/Hong_Kong",
    "hongkong": "Asia/Hong_Kong", "seoul": "Asia/Seoul", "korea": "Asia/Seoul",
    "sydney": "Australia/Sydney", "australia": "Australia/Sydney",
    "dubai": "Asia/Dubai", "uae": "Asia/Dubai",
    "new york": "America/New_York", "nyc": "America/New_York",
    "los angeles": "America/Los_Angeles", "la": "America/Los_Angeles",
    "chicago": "America/Chicago", "denver": "America/Denver",
    "utc": "UTC", "gmt": "UTC",
}


def resolve(name):
    key = name.strip().lower()
    if key in CITIES:
        return CITIES[key]
    # Try as direct IANA zone.
    try:
        ZoneInfo(name.strip())
        return name.strip()
    except Exception:
        pass
    # Substring suggestions.
    sug = [f"{c} -> {z}" for c, z in sorted(CITIES.items()) if key in c or c in key]
    return None, sug


def parse_time(s, tzname):
    s = s.strip().lower()
    tz = ZoneInfo(tzname)
    now = datetime.now(tz)
    if s == "now":
        return now
    import re
    m = re.match(r"(\d{1,2})(?::(\d{2}))?\s*(am|pm)?", s)
    if not m:
        return None
    h = int(m.group(1))
    mi = int(m.group(2) or 0)
    ap = m.group(3)
    if ap == "pm" and h < 12:
        h += 12
    if ap == "am" and h == 12:
        h = 0
    return now.replace(hour=h, minute=mi, second=0, microsecond=0)


def main():
    if len(sys.argv) != 4:
        print('Usage: clock.py "<time|now>" "<from city/zone>" "<to city/zone>"')
        print('Example: clock.py "9am" "Asia/Bangkok" "Europe/London"')
        sys.exit(1)
    src_in, dst_in = sys.argv[2], sys.argv[3]
    src = resolve(src_in)
    dst = resolve(dst_in)
    if isinstance(src, tuple):
        print(f"Error: unknown place '{src_in}'. Close matches:")
        for s in src[1][:8]:
            print(f"  {s}")
        sys.exit(1)
    if isinstance(dst, tuple):
        print(f"Error: unknown place '{dst_in}'. Close matches:")
        for s in dst[1][:8]:
            print(f"  {s}")
        sys.exit(1)
    dt = parse_time(sys.argv[1], src)
    if dt is None:
        print(f"Error: cannot parse time '{sys.argv[1]}'. Try '9am', '15:30', or 'now'.")
        sys.exit(1)
    out = dt.astimezone(ZoneInfo(dst))
    print(f"{dt.strftime('%I:%M %p')} {src} = {out.strftime('%I:%M %p')} {dst} ({out.strftime('%Y-%m-%d')})")


if __name__ == "__main__":
    main()
