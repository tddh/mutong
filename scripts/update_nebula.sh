#!/bin/bash

# =============================================================================
# Nebula Graph nGQL 更新脚本
# 用途：从 docs/ngql 文件中读取 nGQL 语句并执行，用于更新 Nebula Graph 数据库结构
# 注意：请确保已安装 nebula-console 客户端工具
# =============================================================================

# 配置参数（请根据实际环境修改，或通过环境变量设置）
NEBULA_HOST="${NEBULA_HOST:-localhost}"
NEBULA_PORT="${NEBULA_PORT:-9669}"
NEBULA_USER="${NEBULA_USER:-root}"
NEBULA_PASS="${NEBULA_PASS:-nebula}"
NEBULA_SPACE="mutong"
NGQL_FILE="./schema.ngql"
NEBULA_CONSOLE="nebula-console"
LOG_FILE="update_nebula.log"

# 检查 nebula-console 是否安装
check_nebula_console() {
    if ! command -v $NEBULA_CONSOLE &> /dev/null; then
        echo "错误: 未找到 nebula-console 工具，请先安装"
        echo "安装说明: 请从 https://github.com/vesoft-inc/nebula-console/releases 下载对应版本"
        exit 1
    fi
}

# 检查 ngql 文件是否存在
check_ngql_file() {
    if [ ! -f $NGQL_FILE ]; then
        echo "错误: 未找到 $NGQL_FILE 文件"
        exit 1
    fi
}

# 初始化日志
init_log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] 开始执行 Nebula Graph nGQL 更新脚本" > $LOG_FILE
    echo "配置信息:"
    echo "  主机: $NEBULA_HOST"
    echo "  端口: $NEBULA_PORT"
    echo "  用户名: $NEBULA_USER"
    echo "  空间: $NEBULA_SPACE"
    echo "  nGQL文件: $NGQL_FILE"
    echo "  日志文件: $LOG_FILE"
    echo "----------------------------------------"
}

# 执行 nGQL 语句
execute_ngql() {
    local ngql_statements=()
    local current_statement=""
    local is_comment=false
    local is_string=false

    echo "[$(date '+%Y-%m-%d %H:%M:%S')] 正在解析 nGQL 文件..." | tee -a $LOG_FILE

    # 逐行读取文件，处理多行语句
    while IFS= read -r line; do
        # 跳过空行
        if [[ -z "$line" ]]; then
            continue
        fi

        # 检查是否是注释行（以 # 开头且前面没有内容）
        if [[ "$line" =~ ^[[:space:]]*# ]]; then
            continue
        fi

        # 处理引号内的分号
        for (( i=0; i<${#line}; i++ )); do
            char="${line:$i:1}"
            if [[ "$char" == "'" || "$char" == '"' ]]; then
                is_string=!$is_string
            fi
        done

        # 添加当前行到语句中
        current_statement="$current_statement $line"

        # 如果语句以分号结尾且不在字符串内，则认为是完整语句
        if [[ "$current_statement" =~ ;[[:space:]]*$ ]] && [ "$is_string" = false ]; then
            ngql_statements+=("$current_statement")
            current_statement=""
        fi
    done < "$NGQL_FILE"

    # 处理最后一个语句（如果没有分号结尾）
    if [[ -n "$current_statement" ]]; then
        ngql_statements+=($current_statement)
    fi

    echo "[$(date '+%Y-%m-%d %H:%M:%S')] 成功解析 ${#ngql_statements[@]} 条 nGQL 语句" | tee -a $LOG_FILE
    echo "----------------------------------------"

    # 创建临时文件保存要执行的语句
    local temp_file=$(mktemp)
    echo "USE \`$NEBULA_SPACE\`;" > $temp_file

    local ddl_count=0
    # 筛选并添加 DDL 语句到临时文件
    for stmt in "${ngql_statements[@]}"; do
        stmt_trimmed=$(echo "$stmt" | xargs)
        # 检查是否是 DDL 语句（CREATE、ALTER、DROP 等）
        if [[ "$stmt_trimmed" =~ ^(CREATE|ALTER|DROP|USE)[[:space:]]+ ]]; then
            echo "$stmt_trimmed" >> $temp_file
            ((ddl_count++))
        fi
    done

    echo "[$(date '+%Y-%m-%d %H:%M:%S')] 筛选出 $ddl_count 条 DDL 语句准备执行" | tee -a $LOG_FILE
    echo "----------------------------------------"

    # 执行 DDL 语句
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] 开始执行 DDL 语句..." | tee -a $LOG_FILE
    echo "执行命令: $NEBULA_CONSOLE -addr $NEBULA_HOST -port $NEBULA_PORT -u $NEBULA_USER -p $NEBULA_PASS -f $temp_file" | tee -a $LOG_FILE

    # 执行并捕获输出
    local console_output=$($NEBULA_CONSOLE -addr $NEBULA_HOST -port $NEBULA_PORT -u $NEBULA_USER -p $NEBULA_PASS -f $temp_file 2>&1)
    local exit_code=$?

    # 保存输出到日志
    echo "$console_output" >> $LOG_FILE

    # 清理临时文件
    rm -f $temp_file

    # 检查执行结果
    if [ $exit_code -eq 0 ]; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] DDL 语句执行成功" | tee -a $LOG_FILE
    else
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] DDL 语句执行失败，错误代码: $exit_code" | tee -a $LOG_FILE
        echo "详细错误信息请查看日志文件: $LOG_FILE" | tee -a $LOG_FILE
        exit 1
    fi
}

# 主函数
main() {
    check_nebula_console
    check_ngql_file
    init_log
    execute_ngql
    echo "----------------------------------------"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Nebula Graph nGQL 更新脚本执行完成" | tee -a $LOG_FILE
}

# 执行主函数
main