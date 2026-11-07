#!/bin/sh
# Fails if tracked files contain things that must never be published:
# machine-specific paths, real-looking API keys or webhook secrets, private
# key blocks, or a committed .env. Run in CI and before making the repo public.
set -eu

fail=0
check() {
	label=$1
	pattern=$2
	hits=$(git ls-files -z | xargs -0 grep -I -n -E "$pattern" -- 2>/dev/null |
		grep -v -E '^scripts/check-public\.sh:' || true)
	if [ -n "$hits" ]; then
		echo "::error::$label"
		echo "$hits" | cut -c1-160
		fail=1
	fi
}

check "machine-specific paths" '[A-Za-z]:\\\\Users\\\\|/c/Users/|/home/[a-z]+/|/Users/[A-Za-z]+/'
check "Clerk secret keys" 'sk_(live|test)_[A-Za-z0-9]{20,}'
check "Clerk live publishable keys" 'pk_live_[A-Za-z0-9]{20,}'
check "useSend API keys" '\bus_[A-Za-z0-9]{24,}'
check "webhook secrets" 'whsec_[A-Za-z0-9+/=]{16,}'
check "GitHub tokens" '\b(ghp|gho|ghs|github_pat)_[A-Za-z0-9_]{20,}'
check "private key blocks" '-----BEGIN [A-Z ]*PRIVATE KEY-----'

if git ls-files | grep -E '(^|/)\.env$|(^|/)\.secrets/' >/dev/null; then
	echo "::error::.env or .secrets/ is tracked"
	git ls-files | grep -E '(^|/)\.env$|(^|/)\.secrets/'
	fail=1
fi

[ "$fail" -eq 0 ] && echo "public-safety check: OK"
exit "$fail"
