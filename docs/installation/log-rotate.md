明白！**计划极简版：**
- 旧日志 **完全不管**
- 只配置 **新日志 api-*.log** 自动切割
- 只做：**配置文件 + 定时任务**，搞定收工

我直接给你**生产最终版、最简、一步到底**。

---

# 🔥 最终极简完整版（只做这 2 步）
## 第一步：创建 logrotate 配置
```bash
sudo cat > /etc/logrotate.d/gravitex-api <<'EOF'
/workplace/logs/api-*.log {
    daily
    rotate 180
    missingok
    notifempty
    copytruncate
    compress
    compresscmd /usr/bin/gzip
    compressoptions -9
    compressext .gz
    dateext
    dateformat -%Y%m%d
    dateyesterday
    olddir /workplace/logs/archive
    createolddir 0755 root root
    su root root
}
EOF
```

## 第二步：创建每天自动切割定时任务
```bash
sudo cat > /etc/cron.d/gravitex-logrotate <<'EOF'
0 1 * * * root /usr/sbin/logrotate /etc/logrotate.d/gravitex-api >/dev/null 2>&1
EOF
```

```bash
sudo chmod 644 /etc/cron.d/gravitex-logrotate
sudo systemctl restart crond
```

---

# ✅ 完成！
以后：
- 每天 **凌晨 1 点** 自动切割
- 自动压缩成 `.gz`
- 自动放到 `/workplace/logs/archive`
- 自动保留 **180 天**
- 旧日志完全不动

---

# 🧪 想测试能不能正常跑？（可选）
```bash
sudo logrotate -d /etc/logrotate.d/gravitex-api
```
不真正切割，只测试配置是否正确。

---

你现在 **全部搞定**，不用再管日志了！