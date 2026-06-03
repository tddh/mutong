// 重置 admin 账号密码工具
// 用法：
//
//	go run cmd/resetadmin/main.go \
//	  -host localhost -port 5432 -user mutong -pass dbpass -dbname mutong \
//	  -newpass "新密码"
//
// 或从配置文件读取 DB 连接信息：
//
//	go run cmd/resetadmin/main.go -config configs/config.core.yaml -newpass "新密码"
package main

import (
	"flag"
	"fmt"
	"os"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/services/auth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	var (
		configPath string
		host       string
		port       string
		user       string
		pass       string
		dbname     string
		newPass    string
		username   string
	)

	flag.StringVar(&configPath, "config", "", "配置文件路径 (configs/config.core.yaml)")
	flag.StringVar(&host, "host", "localhost", "PostgreSQL 主机")
	flag.StringVar(&port, "port", "5432", "PostgreSQL 端口")
	flag.StringVar(&user, "user", "mutong", "PostgreSQL 用户名")
	flag.StringVar(&pass, "pass", "", "PostgreSQL 密码")
	flag.StringVar(&dbname, "dbname", "mutong", "PostgreSQL 数据库名")
	flag.StringVar(&newPass, "newpass", "", "新的 admin 密码 (必填)")
	flag.StringVar(&username, "username", "admin", "要重置密码的用户名 (默认 admin)")
	flag.Parse()

	if newPass == "" {
		fmt.Fprintln(os.Stderr, "错误: 必须指定新密码 (-newpass)")
		flag.Usage()
		os.Exit(1)
	}

	if configPath != "" {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取配置文件失败: %v\n", err)
			os.Exit(1)
		}
		if cfg.Postgres.Host != "" {
			host = cfg.Postgres.Host
		}
		if cfg.Postgres.Port != "" {
			port = cfg.Postgres.Port
		}
		if cfg.Postgres.User != "" {
			user = cfg.Postgres.User
		}
		if cfg.Postgres.Pass != "" {
			pass = cfg.Postgres.Pass
		}
		if cfg.Postgres.Database != "" {
			dbname = cfg.Postgres.Database
		}
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		host, user, pass, dbname, port)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 PostgreSQL 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ 已连接 PostgreSQL (%s:%s/%s)\n", host, port, dbname)

	hash, err := auth.HashPassword(newPass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "密码哈希生成失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ 已生成 Argon2id 密码哈希\n")

	result := db.Table("users").Where("username = ?", username).Update("password_hash", hash)
	if result.Error != nil {
		fmt.Fprintf(os.Stderr, "更新密码失败: %v\n", result.Error)
		os.Exit(1)
	}
	if result.RowsAffected == 0 {
		fmt.Fprintf(os.Stderr, "警告: 未找到用户 '%s'，密码未更新\n", username)
		os.Exit(1)
	}

	fmt.Printf("✓ 用户 '%s' 密码已更新 (影响行数: %d)\n", username, result.RowsAffected)
	fmt.Println("\n请使用新密码登录。")
}
