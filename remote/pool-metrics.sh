#!/usr/bin/env bash
# ベンチ中のアプリの DB 接続プールを 0.25 秒ごとに記録する。1 秒未満の集中を見るため
# 「時刻 接続数 使用中 待ち回数 待ち時間(ns)」の形式で /tmp/pool-metrics.txt に書く
# usage: pool-metrics.sh seconds
set -uo pipefail

seconds=${1:?usage: pool-metrics.sh seconds}

rm -f /tmp/pool-metrics.txt
for ((i = 0; i < seconds * 4; i++)); do
  stats=$(curl -fsS -m 1 http://localhost:6060/debug/vars | tr -d ' \n' | grep -oE '"(OpenConnections|InUse|WaitCount|WaitDuration)":[0-9]+' | cut -d: -f2 | tr '\n' ' ')
  echo "$(date +%T.%N | cut -c1-12) $stats" >> /tmp/pool-metrics.txt
  sleep 0.25
done
