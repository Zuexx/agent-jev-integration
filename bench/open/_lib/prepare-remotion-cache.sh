#!/bin/sh
# Builds the offline node_modules the video tasks use, from the template's
# lockfile, plus Remotion's headless browser. Runs need no network after this.
#   bench/open/_lib/prepare-remotion-cache.sh [cache dir]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
dest=${1:-$HOME/.jevcc-bench/cache}/remotion-4.0.531
mkdir -p "$dest"
cp "$here/remotion-template/package.json" "$here/remotion-template/package-lock.json" "$dest/"
cd "$dest"
npm ci --no-audit --no-fund
mkdir -p src && printf "import {registerRoot} from 'remotion';\n" > src/index.ts
npx remotion browser ensure
rm -rf src package.json package-lock.json
echo "cache ready: $dest/node_modules"
