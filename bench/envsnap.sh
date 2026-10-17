#!/bin/sh
# envsnap.sh — record the parts of a Claude Code / jevcc setup that an
# evaluation must not change, so a later snapshot can prove it was restored.
#
#   bench/envsnap.sh snap FILE        write a snapshot
#   bench/envsnap.sh diff OLD NEW     compare two snapshots (exit 1 on change)
#
# Config files are hashed. Directories whose entries matter (agents, skills,
# commands, projects) are listed by name. Logs, caches and history change with
# normal use and are left out; a bench run leaking into them is checked
# separately by session ID.
set -eu

dirs="${ENVSNAP_DIRS:-$HOME/.claude $HOME/.claude-2 $HOME/.jevcc}"

hash() { if [ -f "$1" ]; then shasum -a 256 "$1" | cut -c1-64; else echo missing; fi; }

snap() {
	for d in $dirs; do
		[ -d "$d" ] || { echo "dir $d missing"; continue; }
		for f in settings.json settings.local.json CLAUDE.md config.json claude-dashboard.local.json plugins/installed_plugins.json plugins/known_marketplaces.json; do
			[ -e "$d/$f" ] && echo "file $d/$f $(hash "$d/$f")"
		done
		for sub in agents skills commands projects; do
			[ -d "$d/$sub" ] || continue
			for e in "$d/$sub"/*; do
				[ -e "$e" ] || continue
				if [ -f "$e" ]; then echo "entry $e $(hash "$e")"; else echo "entry $e dir"; fi
			done
		done
	done
	[ -f "$HOME/.claude.json" ] && echo "file $HOME/.claude.json:mcpServers $(python3 -I -c 'import json,sys;print(json.dumps(json.load(open(sys.argv[1])).get("mcpServers",{}),sort_keys=True))' "$HOME/.claude.json" | shasum -a 256 | cut -c1-64)"
	true
}

case "${1:-}" in
snap)
	[ $# -eq 2 ] || { echo "usage: $0 snap FILE" >&2; exit 2; }
	snap | LC_ALL=C sort > "$2"
	echo "wrote $2 ($(wc -l < "$2" | tr -d ' ') entries)"
	;;
diff)
	[ $# -eq 3 ] || { echo "usage: $0 diff OLD NEW" >&2; exit 2; }
	if diff "$2" "$3"; then echo "unchanged"; else exit 1; fi
	;;
*)
	echo "usage: $0 snap FILE | diff OLD NEW" >&2
	exit 2
	;;
esac
