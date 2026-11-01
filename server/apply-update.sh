#!/bin/sh
set -eu
base=/opt/homenet-doctor
version="$1"
case "$version" in *[!0-9.]*|'') exit 1;; esac
mkdir "$base/update.lock" 2>/dev/null || exit 1
trap 'rmdir "$base/update.lock"' EXIT
old="$(readlink "$base/current")"
test -x "$base/releases/$version/homenet-doctor"
test "$("$base/releases/$version/homenet-doctor" --version)" = "$version"
ln -s "releases/$version" "$base/next"
mv -Tf "$base/next" "$base/current"
/etc/init.d/homenet-doctor restart
sleep 4
if ! curl -fsS --max-time 5 "http://$(uci -q get network.lan.ipaddr):8088/health" | grep -F "\"version\":\"$version\""; then
 ln -s "$old" "$base/rollback"
 mv -Tf "$base/rollback" "$base/current"
 /etc/init.d/homenet-doctor restart
 logger -t homenet-doctor 'Update failed; previous version restored'
 exit 1
fi
logger -t homenet-doctor "Updated to $version"
