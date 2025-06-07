-- docker/mysql/init-scripts/primary/01-create-repl-user.sql
-- 创建复制用户，'%' 表示允许从任何主机连接（在 Docker 网络内部通常是安全的）
-- 注意：生产环境应限制为从库的 IP 或特定子网
CREATE USER IF NOT EXISTS 'repl_user'@'%' IDENTIFIED BY 'repl_pass';
-- 授予 REPLICATION SLAVE 权限
GRANT REPLICATION SLAVE ON *.* TO 'repl_user'@'%';
-- 刷新权限使之生效
FLUSH PRIVILEGES;