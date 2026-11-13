#!/bin/bash
# 用法：db.sh main|log [dump] mysql参数...
# 从 Go 的 .env 解析 SQL_DSN / LOG_SQL_DSN，避免明文密码出现在命令行和输出里。
# ENV_FILE 默认 /workplace/.env（测试机），可用环境变量覆盖。
ENV_FILE=${ENV_FILE:-/workplace/.env}
which=$1; shift
key=SQL_DSN; [ "$which" = log ] && key=LOG_SQL_DSN
dsn=$(grep "^$key=" "$ENV_FILE" | head -1 | cut -d= -f2-)
user=${dsn%%:*}; rest=${dsn#*:}; pass=${rest%@tcp(*}; hp=${rest##*@tcp(}; hp=${hp%%)*}; host=${hp%:*}; port=${hp##*:}; db=${rest##*)/}; db=${db%%\?*}
export MYSQL_PWD="$pass"
if [ "$1" = dump ]; then
  shift
  exec mysqldump -h"$host" -P"$port" -u"$user" --single-transaction --set-gtid-purged=OFF "$db" "$@"
fi
exec mysql -h"$host" -P"$port" -u"$user" "$@" "$db"
