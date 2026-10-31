-- 合計時間が長い順と、実行回数が多い順。リージョン間は 1 往復 100ms なので回数も見る
\echo === total_exec_time
SELECT calls, round(total_exec_time) AS total_ms, round(mean_exec_time::numeric, 2) AS mean_ms, rows,
       left(regexp_replace(query, '\s+', ' ', 'g'), 150) AS query
FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 20;
\echo === calls
SELECT calls, round(total_exec_time) AS total_ms, round(mean_exec_time::numeric, 2) AS mean_ms, rows,
       left(regexp_replace(query, '\s+', ' ', 'g'), 150) AS query
FROM pg_stat_statements ORDER BY calls DESC LIMIT 20;
