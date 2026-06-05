-- auto-generated definition
create table logs
(
    id                bigint auto_increment
        primary key,
    user_id           bigint                  null,
    created_at        bigint                  null,
    type              bigint                  null,
    content           longtext                null,
    username          varchar(191) default '' null,
    token_name        varchar(191) default '' null,
    model_name        varchar(191) default '' null,
    quota             bigint       default 0  null,
    prompt_tokens     bigint       default 0  null,
    completion_tokens bigint       default 0  null,
    use_time          bigint       default 0  null,
    is_stream         tinyint(1)              null,
    channel_id        bigint                  null,
    channel_name      longtext                null,
    token_id          bigint       default 0  null,
    `group`           varchar(191)            null,
    ip                varchar(191) default '' null,
    request_id        varchar(512) default '' null,
    other             longtext                null,
    task_id           varchar(80)             null comment '任务id',
    oem_id            bigint                  null,
    official_quota    bigint       default 0  null,
    cost_quota        bigint       default 0  null,
    system_quota      bigint       default 0  null,
    user_quota        bigint       default 0  null,
    platform_profit   bigint       default 0  null,
    oem_subsidy       bigint       default 0  null
)
    collate = utf8mb4_unicode_ci;

create index idx_created_at_id
    on logs (id, created_at);

create index idx_created_at_type
    on logs (created_at, type);

create index idx_logs_channel_id
    on logs (channel_id);

create index idx_logs_channel_id_created_at
    on logs (channel_id, created_at);

create index idx_logs_created_at
    on logs (created_at);

create index idx_logs_created_ip_token
    on logs (created_at, ip, token_id);

create index idx_logs_created_token_ip
    on logs (created_at, token_id, ip);

create index idx_logs_created_type_user
    on logs (created_at, type, user_id);

create index idx_logs_group
    on logs (`group`);

create index idx_logs_ip
    on logs (ip);

create index idx_logs_model_name
    on logs (model_name);

create index idx_logs_oem_id
    on logs (oem_id);

create index idx_logs_request_id
    on logs (request_id);

create index idx_logs_token_id
    on logs (token_id);

create index idx_logs_token_name
    on logs (token_name);

create index idx_logs_type_created_model
    on logs (type, created_at, model_name);

create index idx_logs_type_created_token
    on logs (type, created_at, token_id);

create index idx_logs_type_created_user
    on logs (type, created_at, user_id);

create index idx_logs_user_created_ip
    on logs (user_id, created_at, ip);

create index idx_logs_user_id
    on logs (user_id);

create index idx_logs_user_type_created
    on logs (user_id, type, created_at);

create index idx_logs_username
    on logs (username);

create index idx_logs_username_created_at
    on logs (username, created_at);

create index idx_user_id_id
    on logs (user_id, id);

create index index_username_model_name
    on logs (model_name, username);

