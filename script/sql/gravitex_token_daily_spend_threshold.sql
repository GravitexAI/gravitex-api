-- Token 日消费告警阈值字段（DDL 备注，GORM AutoMigrate 会自动创建该列，此文件仅作记录）
alter table tokens add column daily_spend_threshold int default 0 comment '日消费告警阈值(quota单位,0=不告警)';
