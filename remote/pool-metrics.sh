#!/usr/bin/env bash
# ベンチ中のアプリの DB 接続プールと DB までの経路を 0.25 秒ごとに記録する
# /tmp/pool-metrics.txt: 時刻 接続数 使用中 待ち回数 待ち時間(ns) 受信キュー(byte) 送信キュー(byte)
# 受信キューが溜まればアプリが読めておらず、送信キューが溜まれば経路が詰まっている
# /tmp/pool-ping.txt: DB までの往復時間 / /tmp/pool-vmstat.txt: アプリサーバーの CPU
# usage: pool-metrics.sh seconds db_ip
set -uo pipefail

seconds=${1:?usage: pool-metrics.sh seconds db_ip}
db_ip=${2:?usage: pool-metrics.sh seconds db_ip}

rm -f /tmp/pool-metrics.txt /tmp/pool-ping.txt /tmp/pool-vmstat.txt
vmstat -n -t 1 "$seconds" > /tmp/pool-vmstat.txt &
ping -D -i 0.2 -c $((seconds * 5)) "$db_ip" > /tmp/pool-ping.txt &

for ((i = 0; i < seconds * 4; i++)); do
  stats=$(curl -fsS -m 1 http://localhost:6060/debug/vars | tr -d ' \n' | grep -oE '"(OpenConnections|InUse|WaitCount|WaitDuration)":[0-9]+' | cut -d: -f2 | tr '\n' ' ')
  queues=$(ss -tn state established dst "$db_ip" | awk 'NR > 1 { r += $1; s += $2 } END { print r + 0, s + 0 }')
  echo "$(date +%T.%N | cut -c1-12) $stats $queues" >> /tmp/pool-metrics.txt
  sleep 0.25
done
wait
