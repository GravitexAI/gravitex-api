MySQL

  -- ============================================
  -- OEM 数据库清理脚本 (MySQL)
  -- ============================================

  -- 1. 删除 OEM 相关表
  DROP TABLE IF EXISTS `oem_config`;
  DROP TABLE IF EXISTS `oem_discount`;
  DROP TABLE IF EXISTS `oem_user_discount`;
  DROP TABLE IF EXISTS `oem_account_logs`;

  -- 2. 删除 users 表上的 oem_id 列（如果存在）
  SET @db_name = DATABASE();
  SET @table_name = 'users';
  SET @column_name = 'oem_id';

  SELECT COUNT(*) INTO @col_exists
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @db_name
    AND TABLE_NAME = @table_name
    AND COLUMN_NAME = @column_name;

  SET @sql = IF(@col_exists > 0,
      CONCAT('ALTER TABLE `', @table_name, '` DROP COLUMN `', @column_name, '`'),
      'SELECT 1');
  PREPARE stmt FROM @sql;
  EXECUTE stmt;
  DEALLOCATE PREPARE stmt;

  -- 3. 删除 users 表上的 oem_id 索引（如果存在）
  SELECT COUNT(*) INTO @idx_exists
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = @db_name
    AND TABLE_NAME = @table_name
    AND COLUMN_NAME = 'oem_id';

  SET @sql = IF(@idx_exists > 0,
      CONCAT('ALTER TABLE `', @table_name, '` DROP INDEX `idx_users_oem_id`'),
      'SELECT 1');
  PREPARE stmt FROM @sql;
  EXECUTE stmt;
  DEALLOCATE PREPARE stmt;

  PostgreSQL

  -- ============================================
  -- OEM 数据库清理脚本 (PostgreSQL)
  -- ============================================

  -- 1. 删除 OEM 相关表
  DROP TABLE IF EXISTS oem_config CASCADE;
  DROP TABLE IF EXISTS oem_discount CASCADE;
  DROP TABLE IF EXISTS oem_user_discount CASCADE;
  DROP TABLE IF EXISTS oem_account_logs CASCADE;

  -- 2. 删除 users 表上的 oem_id 列（如果存在）
  ALTER TABLE users DROP COLUMN IF EXISTS oem_id;

  SQLite

  -- ============================================
  -- OEM 数据库清理脚本 (SQLite)
  -- 注意：SQLite 不支持 DROP COLUMN（3.35.0+ 才支持）
  -- 如果版本 >= 3.35.0 可以用下面的语句
  -- ============================================

  -- 1. 删除 OEM 相关表
  DROP TABLE IF EXISTS oem_config;
  DROP TABLE IF EXISTS oem_discount;
  DROP TABLE IF EXISTS oem_user_discount;
  DROP TABLE IF EXISTS oem_account_logs;

  -- 2. 删除 users 表上的 oem_id 列（SQLite >= 3.35.0）
  ALTER TABLE users DROP COLUMN oem_id;