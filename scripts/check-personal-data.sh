#!/usr/bin/env bash
# Safety net for the "no personal data" rule in AGENTS.md. It scans tracked files for:
#   - home-directory paths of a real account (/home/<name>/, /Users/<name>/, C:\Users\<name>\)
#   - email addresses that are not on a reserved example domain
#   - public-looking IPv4 addresses (anything but loopback, private, link-local and documentation ranges)
# It cannot find names or prose: review those by eye. Allow-list a deliberate exception with the marker
# "personal-data: ok" on the same line.
#
#   scripts/check-personal-data.sh              scan the repository
#   scripts/check-personal-data.sh --self-test  check the detector itself
set -euo pipefail
cd "$(dirname "$0")/.."

scan() { # reads text on stdin, prints offending lines
  grep -vF 'personal-data: ok' |
    grep -E "(/home/[a-z_][a-z0-9_-]*/|/Users/[A-Za-z][A-Za-z0-9._-]*/|C:\\\\Users\\\\[A-Za-z])|([A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,})|(\b([0-9]{1,3}\.){3}[0-9]{1,3}\b)" |
    grep -vE '/home/(runner|user|name|me)/|/Users/(user|name|me)/' |
    awk '
      { line = $0; bad = 0
        # home paths of anything but a placeholder
        if (line ~ /\/home\/[a-z_][a-z0-9_-]*\// && line !~ /\/home\/(runner|user|name|me)\//) bad = 1
        if (line ~ /\/Users\/[A-Za-z]/ && line !~ /\/Users\/(user|name|me)\//) bad = 1
        if (line ~ /C:\\Users\\[A-Za-z]/) bad = 1
        # emails
        s = line
        while (match(s, /[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}/)) {
          e = substr(s, RSTART, RLENGTH); s = substr(s, RSTART + RLENGTH)
          d = tolower(e); sub(/.*@/, "", d)
          if (d !~ /(^|\.)example\.(org|com|net)$/ && d !~ /^(anthropic\.com|users\.noreply\.github\.com|github\.com)$/ && d !~ /\.(invalid|test|example|localhost)$/ && e !~ /^(git|noreply|no-reply)@/ && e !~ /^@/ && d !~ /\.(js|ts|tsx|json|go|yaml|yml|md|css|html|svg|png|sql|sh)$/) bad = 1
        }
        # IPv4
        s = line
        while (match(s, /([0-9]{1,3}\.){3}[0-9]{1,3}/)) {
          ip = substr(s, RSTART, RLENGTH); s = substr(s, RSTART + RLENGTH)
          n = split(ip, o, "."); ok = 0
          if (n == 4 && o[1] <= 255 && o[2] <= 255 && o[3] <= 255 && o[4] <= 255) {
            if (o[1] == 127 || o[1] == 10 || o[1] == 0 || (o[1] == 192 && o[2] == 168) || (o[1] == 172 && o[2] >= 16 && o[2] <= 31) ||
                (o[1] == 169 && o[2] == 254) || (o[1] == 100 && o[2] >= 64 && o[2] <= 127) || (o[1] == 192 && o[2] == 0 && o[3] == 2) ||
                (o[1] == 198 && o[2] == 51 && o[3] == 100) || (o[1] == 203 && o[2] == 0 && o[3] == 113) || (o[1] == 198 && (o[2] == 18 || o[2] == 19)) ||
                o[1] >= 224) ok = 1
            # four-part numbers that are really versions are not addresses: skip when the first octet is a 0 or digits like 1.2.3.4 in tests
            if (o[1] == 1 && o[2] == 2 && o[3] == 3 && o[4] == 4) ok = 1
            # well-known public resolvers and fixtures used by the SSRF tests, and the IETF 192.0.0.0/24 block
            if (ip == "8.8.8.8" || ip == "1.1.1.1" || ip == "6.6.6.6" || ip == "93.184.216.34" || (o[1] == 192 && o[2] == 0 && o[3] == 0)) ok = 1
            if (!ok) bad = 1
          }
        }
        if (bad) print
      }'
}

if [ "${1:-}" = "--self-test" ]; then
  fail=0
  expect_hit()  { printf '%s\n' "$1" | scan | grep -q . || { echo "self-test: should flag: $1"; fail=1; }; }
  expect_pass() { printf '%s\n' "$1" | scan | grep -q . && { echo "self-test: should allow: $1"; fail=1; }; true; }
  expect_hit  'cd /home/jdoe/projects/app'
  expect_hit  'contact jane.doe@gmail.com'
  expect_hit  'server at 45.33.32.156'
  expect_hit  'C:\Users\Jane\Documents'
  expect_pass 'user@example.org and docs at 203.0.113.9'
  expect_pass 'listen on 127.0.0.1 or 10.0.0.2 or 192.168.1.1'
  expect_pass 'volume /home/runner/work/app'
  expect_pass 'Co-Authored-By: Someone <noreply@anthropic.com>'
  expect_pass 'jane@example.net # personal-data: ok'
  [ "$fail" -eq 0 ] && echo "self-test: ok"
  exit "$fail"
fi

skip='(^|/)(pnpm-lock\.yaml|go\.sum|LICENSE|openapi\.json|schema\.d\.ts|check-personal-data\.sh)$|\.(png|jpg|svg|ico|woff2?|wasm)$'
hits=0
while IFS= read -r f; do
  [[ "$f" =~ $skip ]] && continue
  [ -f "$f" ] || continue
  out="$(scan < "$f" || true)"
  if [ -n "$out" ]; then
    printf '%s\n' "$out" | sed "s|^|$f: |"
    hits=1
  fi
done < <(git ls-files)
if [ "$hits" -ne 0 ]; then
  echo "check-personal-data: possible personal data above. Use reserved examples (example.org, 192.0.2.0/24, /path/to/...)," >&2
  echo "or mark a deliberate exception with 'personal-data: ok'." >&2
  exit 1
fi
echo "check-personal-data: ok"
