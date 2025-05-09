#!/bin/bash
# docker/mysql/init-scripts/replica/01-configure-replication.sh
set -e # 遇到错误即退出

# 从环境变量获取主库信息和复制用户信息 (这些变量需要在 docker-compose.yaml 中定义)
MYSQL_PRIMARY_HOST="${MYSQL_PRIMARY_HOST:-mysql-primary}" # 默认使用服务名
MYSQL_REPL_USER="${MYSQL_REPL_USER:-repl_user}"
MYSQL_REPL_PASSWORD="${MYSQL_REPL_PASSWORD:-repl_pass}"
MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-root}" # 获取 root 密码

echo "Replica: Waiting for primary MySQL (${MYSQL_PRIMARY_HOST}) to start..."

# 使用 mysqladmin ping 循环等待主库可用
# 注意：这里的 -h"$MYSQL_PRIMARY_HOST" 使用的是服务名，Docker Compose 会解析
# -uroot 和 -p"$MYSQL_ROOT_PASSWORD" 用于连接检查
while ! mysqladmin ping -h"$MYSQL_PRIMARY_HOST" -uroot -p"$MYSQL_ROOT_PASSWORD" --silent; do
    echo "Replica: Waiting for primary..."
    sleep 2 # 每 2 秒检查一次
done

echo "Replica: Primary MySQL is available. Configuring replication..."

# 执行配置复制的 SQL 命令
# 使用 GTID 自动定位 (MASTER_AUTO_POSITION=1)
# 需要确保主库已启用 GTID (我们在 docker-compose command 中配置)
mysql -uroot -p"${MYSQL_ROOT_PASSWORD}" <<-EOSQL
  STOP REPLICA; -- 停止可能存在的旧复制进程 (MySQL 8+ 使用 REPLICA)
  CHANGE MASTER TO
    MASTER_HOST='${MYSQL_PRIMARY_HOST}',
    MASTER_USER='${MYSQL_REPL_USER}',
    MASTER_PASSWORD='${MYSQL_REPL_PASSWORD}',
    MASTER_AUTO_POSITION=1; -- 重要：使用 GTID 自动定位
  -- SET GLOBAL read_only = ON; -- 可选：确保从库只读 (也可以通过 command 参数设置)
  START REPLICA; -- 启动复制进程 (MySQL 8+ 使用 REPLICA)
  SELECT 'Replication configured successfully using GTID' AS status;
EOSQL

echo "Replica: Replication setup script finished."