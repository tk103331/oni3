#!/usr/bin/env bash
# Update the Scoop bucket manifest for a published oni3 release.
# Required env: VERSION (e.g. v0.4.0), GH_TOKEN (PAT with push access to the bucket).
set -euo pipefail

VERSION_TAG="${VERSION:?VERSION is required}"
VERSION_NO_V="${VERSION_TAG#v}"
BUCKET_REPO="${BUCKET_REPO:-tk103331/scoop-bucket}"
BASE="https://github.com/tk103331/oni3/releases/download/${VERSION_TAG}"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

curl -fsSL "${BASE}/checksums.txt" -o "${workdir}/checksums.txt"

sha_for() {
  awk -v f="$1" '$2 == f { print $1 }' "${workdir}/checksums.txt"
}

AMD64_SHA="$(sha_for "oni3_${VERSION_NO_V}_windows_amd64.zip")"
ARM64_SHA="$(sha_for "oni3_${VERSION_NO_V}_windows_arm64.zip")"

if [ -z "$AMD64_SHA" ] || [ -z "$ARM64_SHA" ]; then
  echo "missing windows checksums in ${BASE}/checksums.txt" >&2
  cat "${workdir}/checksums.txt" >&2
  exit 1
fi

cat > "${workdir}/oni3.json" <<'JSON'
{
    "version": "__VERSION__",
    "description": "Oni3 - AI Agent TUI with tools, MCP and ACP support, built on CloudWeGo Eino",
    "homepage": "https://github.com/tk103331/oni3",
    "license": "MIT",
    "architecture": {
        "64bit": {
            "url": "https://github.com/tk103331/oni3/releases/download/v__VERSION__/oni3___VERSION___windows_amd64.zip",
            "hash": "__SHA_AMD64__"
        },
        "arm64": {
            "url": "https://github.com/tk103331/oni3/releases/download/v__VERSION__/oni3___VERSION___windows_arm64.zip",
            "hash": "__SHA_ARM64__"
        }
    },
    "bin": "oni3.exe",
    "checkver": "github",
    "autoupdate": {
        "architecture": {
            "64bit": {
                "url": "https://github.com/tk103331/oni3/releases/download/v$version/oni3_$dotVersion_windows_amd64.zip",
                "hash": {
                    "url": "https://github.com/tk103331/oni3/releases/download/v$version/checksums.txt",
                    "regex": "(?i)([a-f0-9]{64})\\s+oni3_$dotVersion_windows_amd64\\.zip"
                }
            },
            "arm64": {
                "url": "https://github.com/tk103331/oni3/releases/download/v$version/oni3_$dotVersion_windows_arm64.zip",
                "hash": {
                    "url": "https://github.com/tk103331/oni3/releases/download/v$version/checksums.txt",
                    "regex": "(?i)([a-f0-9]{64})\\s+oni3_$dotVersion_windows_arm64\\.zip"
                }
            }
        }
    }
}
JSON

sed \
  -e "s/__VERSION__/${VERSION_NO_V}/g" \
  -e "s/__SHA_AMD64__/${AMD64_SHA}/" \
  -e "s/__SHA_ARM64__/${ARM64_SHA}/" \
  "${workdir}/oni3.json" > "${workdir}/oni3.rendered.json"

git clone --depth 1 "https://x-access-token:${GH_TOKEN}@github.com/${BUCKET_REPO}.git" "${workdir}/bucket"
mkdir -p "${workdir}/bucket/bucket"
cp "${workdir}/oni3.rendered.json" "${workdir}/bucket/bucket/oni3.json"

cd "${workdir}/bucket"
git config user.name "github-actions[bot]"
git config user.email "github-actions[bot]@users.noreply.github.com"
git add bucket/oni3.json
if git diff --cached --quiet; then
  echo "manifest unchanged for ${VERSION_NO_V}"
  exit 0
fi
git commit -m "oni3 ${VERSION_NO_V}"
git push
