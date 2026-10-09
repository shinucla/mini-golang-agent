#!/bin/sh
cd "$(dirname "$0")/.." || exit 1

need=$(awk '$1 == "go" { print $2; exit }' go.mod)

if ! command -v go >/dev/null 2>&1; then
	echo "Go is not installed, or it is not on your PATH. mga needs Go $need or newer."
	echo "Install it from https://go.dev/dl/, then add /usr/local/go/bin to your PATH."
	exit 1
fi

if ! out=$(go version 2>&1); then
	echo "'go version' failed:"
	echo "$out"
	echo "mga needs Go $need or newer. Install it from https://go.dev/dl/."
	exit 1
fi

have=$(echo "$out" | awk '{ print $3 }' | sed 's/^go//')

older() {
	awk -v a="$1" -v b="$2" 'BEGIN {
		split(a, x, "."); split(b, y, ".")
		for (i = 1; i <= 3; i++) {
			p = x[i] + 0; q = y[i] + 0
			if (p < q) exit 0
			if (q < p) exit 1
		}
		exit 1
	}'
}

case "$have" in
[0-9]*)
	if older "$have" "$need"; then
		echo "mga needs Go $need or newer, but the go on your PATH is Go $have ($(command -v go))."
		os=$(uname -s | tr '[:upper:]' '[:lower:]')
		case "$(uname -m)" in
		x86_64 | amd64) arch=amd64 ;;
		aarch64 | arm64) arch=arm64 ;;
		*) arch=$(uname -m) ;;
		esac
		file="go$need.$os-$arch.tar.gz"
		echo "Package managers such as apt often install an old Go. Install a new one from https://go.dev/dl/:"
		echo "  curl -LO https://go.dev/dl/$file"
		echo "  sudo rm -rf /usr/local/go"
		echo "  sudo tar -C /usr/local -xzf $file"
		echo "  export PATH=/usr/local/go/bin:\$PATH"
		echo "An old Go can change go.mod and go.sum. If 'git status' shows changes to them, run:"
		echo "  git checkout -- go.mod go.sum"
		exit 1
	fi
	;;
*)
	echo "Note: could not read the Go version from '$out'. mga needs Go $need or newer." >&2
	;;
esac

if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	if ! git diff --quiet -- go.mod go.sum 2>/dev/null; then
		echo "Warning: go.mod or go.sum has local changes. If you did not make them, restore them:" >&2
		echo "  git checkout -- go.mod go.sum" >&2
	fi
fi
