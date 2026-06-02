# Security Policy

## 报告漏洞

如果你发现安全漏洞，请**不要**在公开 Issue 中报告。

请发送邮件至 mutong_cm@163.com，我们将尽快响应。

## 支持版本

| 版本 | 支持状态 |
|------|---------|
| latest | ✅ 活跃支持 |

## 安全检查清单

- [ ] 所有密码/密钥通过环境变量注入，不硬编码在配置文件中
- [ ] kubeconfig 文件不提交到 Git
- [ ] 生产环境 `configs/*.yaml` 不包含真实凭据
- [ ] LLM API Key 通过 `MUTONG_LLM_API_KEY` 环境变量设置
- [ ] 数据库密码通过 `MUTONG_DB_PASSWORD` 环境变量设置
